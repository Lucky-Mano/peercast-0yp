// Package server implements the PeerCast root (YP) server.
// It listens for incoming CIN (Control-In) connections, performs the PCP
// handshake, and maintains the channel registry via periodic tracker updates.
package pcp

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	pcp "github.com/titagaki/peercast-pcp/pcp"

	"github.com/titagaki/peercast-0yp/internal/channel"
)

// Protocol-level constants that are part of the PCP implementation and
// are not operator-configurable.
const (
	agentString      = "PeerCastRoot/0.1 (Go)"
	serverVersion    = uint32(1218)
	serverVersionVP  = uint32(27)
	handshakeTimeout = 30 * time.Second
)

var versionExPrefix = [2]byte{'Y', 'P'}

const versionExNumber = uint16(1)

// Config holds operator-configurable server parameters.
type Config struct {
	MaxConnections   int           // maximum simultaneous CIN connections (default 100)
	UpdateInterval   time.Duration // how often to request tracker updates (default 120s)
	HitTimeout       time.Duration // dead-hit removal threshold (default 180s)
	MinClientVersion uint32        // minimum accepted client version (default 1200)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxConnections:   100,
		UpdateInterval:   120 * time.Second,
		HitTimeout:       180 * time.Second,
		MinClientVersion: 1200,
	}
}

// ----------------------------------------------------------------------------
// Server
// ----------------------------------------------------------------------------

// Server is the PeerCast root (YP) server. It accepts CIN connections,
// handles the PCP handshake, dispatches incoming bcst atoms to the channel
// store, and periodically broadcasts root settings to all connected clients.
type Server struct {
	cfg       Config
	sessionID pcp.GnuID
	store     *channel.Store

	mu       sync.Mutex
	sessions map[pcp.GnuID]*session
	conns    map[net.Conn]struct{}
}

// New creates a Server with a random session ID.
func New(store *channel.Store, cfg Config) (*Server, error) {
	var id pcp.GnuID
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	return &Server{
		cfg:       cfg,
		sessionID: id,
		store:     store,
		sessions:  make(map[pcp.GnuID]*session),
		conns:     make(map[net.Conn]struct{}),
	}, nil
}

// SessionID returns the server's own session ID.
func (srv *Server) SessionID() pcp.GnuID { return srv.sessionID }

// Serve accepts connections from ln and serves them until ctx is cancelled.
// Returns nil when the context is done.
func (srv *Server) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()                // unblock Accept
		srv.closeAllConnections() // terminate handshakes and existing sessions
	}()

	go srv.cleanupLoop(ctx)
	go srv.broadcastLoop(ctx)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				slog.Error("accept error", "err", err)
				continue
			}
		}
		acceptedAt := time.Now()
		if !srv.admitConn(ctx, conn, acceptedAt) {
			continue
		}
		go srv.handleConn(ctx, conn)
	}
}

// ListenAndServe starts a TCP listener on addr and serves connections until
// ctx is cancelled. Returns nil when the context is done.
func (srv *Server) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return srv.Serve(ctx, ln)
}

// admitConn reserves a connection slot and starts its fixed handshake deadline.
// The timestamp is captured immediately after Accept so time spent waiting for
// admission also counts against the handshake budget.
func (srv *Server) admitConn(ctx context.Context, conn net.Conn, acceptedAt time.Time) bool {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if ctx.Err() != nil || len(srv.conns) >= srv.cfg.MaxConnections {
		_ = conn.Close()
		return false
	}
	if err := conn.SetDeadline(acceptedAt.Add(handshakeTimeout)); err != nil {
		_ = conn.Close()
		return false
	}
	srv.conns[conn] = struct{}{}
	return true
}

func (srv *Server) releaseConn(conn net.Conn) {
	srv.mu.Lock()
	delete(srv.conns, conn)
	srv.mu.Unlock()
}

// closeAllConnections closes both in-progress handshakes and active sessions.
func (srv *Server) closeAllConnections() {
	srv.mu.Lock()
	conns := make([]net.Conn, 0, len(srv.conns))
	for conn := range srv.conns {
		conns = append(conns, conn)
	}
	srv.mu.Unlock()

	for _, c := range conns {
		c.Close()
	}
}

// cleanupLoop removes dead hits from the channel store every 500 ms.
func (srv *Server) cleanupLoop(ctx context.Context) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			srv.store.RemoveDeadHits(srv.cfg.HitTimeout)
		}
	}
}

// broadcastLoop sends a bcst > root > upd packet to every connected CIN
// session every UpdateInterval seconds.
func (srv *Server) broadcastLoop(ctx context.Context) {
	t := time.NewTicker(srv.cfg.UpdateInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			srv.broadcastRootSettings()
		}
	}
}

// ----------------------------------------------------------------------------
// Per-connection session
// ----------------------------------------------------------------------------

type session struct {
	conn     net.Conn
	remoteIP net.IP
	clientID pcp.GnuID
	sendCh   chan *pcp.Atom
	done     chan struct{}
}

func (s *session) sendLoop() {
	for {
		select {
		case <-s.done:
			return
		case a := <-s.sendCh:
			if err := a.Write(s.conn); err != nil {
				return
			}
		}
	}
}

// ----------------------------------------------------------------------------
// Connection handling
// ----------------------------------------------------------------------------

func (srv *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer srv.releaseConn(conn)
	defer conn.Close()
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr)
	remoteIP := remoteAddr.IP

	readTimeout := srv.cfg.UpdateInterval + 60*time.Second

	// ── Step 1: Read "pcp\n" version atom ────────────────────────────────
	vAtom, err := pcp.ReadAtom(conn)
	if err != nil {
		return
	}
	if vAtom.Tag != pcp.PCPConnect {
		slog.Debug("unexpected first atom", "tag", vAtom.Tag, "addr", remoteAddr)
		return
	}

	// ── Step 2: Read HELO ────────────────────────────────────────────────
	heloAtom, err := pcp.ReadAtom(conn)
	if err != nil {
		return
	}
	if heloAtom.Tag != pcp.PCPHelo {
		slog.Debug("expected helo", "got", heloAtom.Tag, "addr", remoteAddr)
		return
	}
	helo, err := pcp.ParseHeloPacket(heloAtom)
	if err != nil {
		slog.Debug("bad helo", "addr", remoteAddr, "err", err)
		return
	}
	slog.Info("HELO", "addr", remoteAddr, "agent", helo.Agent, "ver", helo.Version)

	// If client sent ping (FW_UNKNOWN), test the port; otherwise use declared port.
	confirmedPort := helo.Port
	if helo.Ping != 0 {
		confirmedPort = checkPort(remoteIP, helo.Ping)
		slog.Debug("ping check", "addr", remoteIP, "ping", helo.Ping, "result", confirmedPort)
	}

	// ── Step 3: Send OLEH ────────────────────────────────────────────────
	if err := writeOleh(conn, srv.sessionID, remoteIP, confirmedPort); err != nil {
		return
	}

	// ── Step 4: Send informational root atoms (no upd yet) ───────────────
	if err := writeRootAtoms(conn, srv.cfg, false); err != nil {
		return
	}

	// ── Step 5: Validate ─────────────────────────────────────────────────
	sendQuit := func(code uint32) {
		_ = pcp.NewIntAtom(pcp.PCPQuit, code).Write(conn)
	}

	if helo.Version < srv.cfg.MinClientVersion {
		sendQuit(pcp.PCPErrorQuit + pcp.PCPErrorBadAgent)
		return
	}
	if helo.SessionID.IsEmpty() {
		sendQuit(pcp.PCPErrorQuit + pcp.PCPErrorNotIdentified)
		return
	}
	if helo.SessionID == srv.sessionID {
		return // loopback — silently close
	}

	srv.mu.Lock()
	if _, dup := srv.sessions[helo.SessionID]; dup {
		srv.mu.Unlock()
		sendQuit(pcp.PCPErrorQuit + pcp.PCPErrorAlreadyConnected)
		return
	}
	sess := &session{
		conn:     conn,
		remoteIP: remoteIP,
		clientID: helo.SessionID,
		sendCh:   make(chan *pcp.Atom, 16),
		done:     make(chan struct{}),
	}
	srv.sessions[helo.SessionID] = sess
	srv.mu.Unlock()

	defer func() {
		close(sess.done)
		srv.mu.Lock()
		delete(srv.sessions, helo.SessionID)
		srv.mu.Unlock()
		slog.Info("CIN disconnected", "addr", remoteAddr)
	}()

	slog.Info("CIN connected", "addr", remoteAddr, "agent", helo.Agent, "ver", helo.Version)

	// ── Step 6: Send ok(0) + root > upd ──────────────────────────────────
	if err := pcp.NewIntAtom(pcp.PCPOK, 0).Write(conn); err != nil {
		return
	}
	rootUpd := pcp.NewParentAtom(pcp.PCPRoot, pcp.NewEmptyAtom(pcp.PCPRootUpdate))
	if err := rootUpd.Write(conn); err != nil {
		return
	}
	// The fixed deadline covers every handshake read and write. Once accepted,
	// restore the established session's separate idle-read timeout below.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return
	}

	// ── Step 7: Start send goroutine, then enter read loop ───────────────
	go sess.sendLoop()

	for {
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		atom, err := pcp.ReadAtom(conn)
		if err != nil {
			if ctx.Err() == nil {
				slog.Debug("read error", "addr", remoteAddr, "err", err)
			}
			return
		}
		switch atom.Tag {
		case pcp.PCPBcst:
			srv.processBcst(sess, atom)
		case pcp.PCPQuit:
			code, _ := atom.GetInt()
			slog.Info("client quit", "addr", remoteAddr, "code", code)
			return
		}
	}
}

// ----------------------------------------------------------------------------
// Outgoing packet builders
// ----------------------------------------------------------------------------

func writeOleh(w io.Writer, serverSID pcp.GnuID, remoteIP net.IP, clientPort uint16) error {
	rip, _ := pcp.IPv4ToUint32(remoteIP)
	return (&pcp.HeloPacket{
		Agent:     agentString,
		SessionID: serverSID,
		Version:   serverVersion,
		RemoteIP:  rip,
		Port:      clientPort,
	}).BuildOlehAtom().Write(w)
}

func writeRootAtoms(w io.Writer, cfg Config, withUpd bool) error {
	interval := uint32(cfg.UpdateInterval.Seconds())
	children := []*pcp.Atom{
		pcp.NewIntAtom(pcp.PCPRootUpdInt, interval),
		pcp.NewStringAtom(pcp.PCPRootURL, ""),
		pcp.NewIntAtom(pcp.PCPRootCheckVer, cfg.MinClientVersion),
		pcp.NewIntAtom(pcp.PCPRootNext, interval),
		pcp.NewStringAtom(pcp.PCPMesgASCII, ""),
	}
	if withUpd {
		children = append(children, pcp.NewEmptyAtom(pcp.PCPRootUpdate))
	}
	return pcp.NewParentAtom(pcp.PCPRoot, children...).Write(w)
}

func (srv *Server) broadcastRootSettings() {
	interval := uint32(srv.cfg.UpdateInterval.Seconds())

	rootAtom := pcp.NewParentAtom(pcp.PCPRoot,
		pcp.NewIntAtom(pcp.PCPRootUpdInt, interval),
		pcp.NewStringAtom(pcp.PCPRootURL, ""),
		pcp.NewIntAtom(pcp.PCPRootCheckVer, srv.cfg.MinClientVersion),
		pcp.NewIntAtom(pcp.PCPRootNext, interval),
		pcp.NewStringAtom(pcp.PCPMesgASCII, ""),
		pcp.NewEmptyAtom(pcp.PCPRootUpdate),
	)

	bcst := pcp.NewParentAtom(pcp.PCPBcst,
		pcp.NewByteAtom(pcp.PCPBcstGroup, pcp.PCPBcstGroupTrackers),
		pcp.NewByteAtom(pcp.PCPBcstHops, 0),
		pcp.NewByteAtom(pcp.PCPBcstTTL, 7),
		pcp.NewIDAtom(pcp.PCPBcstFrom, srv.sessionID),
		pcp.NewIntAtom(pcp.PCPBcstVersion, serverVersion),
		pcp.NewIntAtom(pcp.PCPBcstVersionVP, serverVersionVP),
		pcp.NewBytesAtom(pcp.PCPBcstVersionExPrefix, versionExPrefix[:]),
		pcp.NewShortAtom(pcp.PCPBcstVersionExNumber, versionExNumber),
		rootAtom,
	)

	srv.mu.Lock()
	sessions := make([]*session, 0, len(srv.sessions))
	for _, s := range srv.sessions {
		sessions = append(sessions, s)
	}
	srv.mu.Unlock()

	for _, s := range sessions {
		select {
		case s.sendCh <- bcst:
		default:
		}
	}
}

// ----------------------------------------------------------------------------
// Incoming bcst processing
// ----------------------------------------------------------------------------

func (srv *Server) processBcst(sess *session, atom *pcp.Atom) {
	var routingChanID pcp.GnuID
	var chanInfo *channel.Info
	var hit *channel.Hit

	for _, child := range atom.Children() {
		switch child.Tag {
		case pcp.PCPBcstChanID:
			routingChanID, _ = child.GetID()
		case pcp.PCPChan:
			info := channel.ParseChanAtom(child, routingChanID)
			chanInfo = &info
		case pcp.PCPHost:
			h := channel.ParseHostAtom(child, routingChanID, sess.remoteIP)
			hit = &h
		}
	}

	if hit == nil {
		return
	}

	if hit.Recv && chanInfo != nil {
		srv.store.AddHit(*chanInfo, *hit)
	} else if !hit.Recv {
		cid := hit.ChanID
		if cid.IsEmpty() {
			cid = routingChanID
		}
		srv.store.DelHit(cid, hit.SessionID)
	}
}

// ----------------------------------------------------------------------------
// Atom parsers
// ----------------------------------------------------------------------------

// checkPort tries to TCP-connect to ip:port with a 1-second timeout.
// Returns the port on success, 0 on failure.
func checkPort(ip net.IP, port uint16) uint16 {
	addr := net.TCPAddr{IP: ip, Port: int(port)}
	conn, err := net.DialTimeout("tcp", addr.String(), time.Second)
	if err != nil {
		return 0
	}
	conn.Close()
	return port
}
