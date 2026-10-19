package pcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	pcp "github.com/titagaki/peercast-pcp/pcp"

	"github.com/titagaki/peercast-0yp/internal/channel"
)

type testConn struct {
	mu            sync.Mutex
	reader        io.Reader
	written       bytes.Buffer
	deadlines     []time.Time
	readDeadlines []time.Time
	closed        bool
}

func (c *testConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *testConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.written.Write(p)
}
func (c *testConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}
func (c *testConn) LocalAddr() net.Addr  { return &net.TCPAddr{} }
func (c *testConn) RemoteAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (c *testConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadlines = append(c.deadlines, t)
	c.mu.Unlock()
	return nil
}
func (c *testConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadlines = append(c.readDeadlines, t)
	c.mu.Unlock()
	return nil
}
func (c *testConn) SetWriteDeadline(time.Time) error { return nil }

func newTestServer(t *testing.T, max int) *Server {
	t.Helper()
	cfg := DefaultConfig()
	cfg.MaxConnections = max
	srv, err := New(channel.NewStore(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func handshakeInput(t *testing.T, id pcp.GnuID) []byte {
	t.Helper()
	var input bytes.Buffer
	for _, atom := range []*pcp.Atom{
		pcp.NewIntAtom(pcp.PCPConnect, 1),
		pcp.NewParentAtom(pcp.PCPHelo,
			pcp.NewStringAtom(pcp.PCPHeloAgent, "TestClient/1.0"),
			pcp.NewIntAtom(pcp.PCPHeloVersion, 1218),
			pcp.NewIDAtom(pcp.PCPHeloSessionID, id),
			pcp.NewShortAtom(pcp.PCPHeloPort, 7144),
		),
	} {
		if err := atom.Write(&input); err != nil {
			t.Fatal(err)
		}
	}
	return input.Bytes()
}

func TestAdmitConnStartsOneFixedThirtySecondDeadline(t *testing.T) {
	srv := newTestServer(t, 1)
	acceptedAt := time.Date(2026, 10, 19, 1, 2, 3, 0, time.UTC)
	conn := &testConn{reader: bytes.NewReader([]byte{0x00})}
	if !srv.admitConn(context.Background(), conn, acceptedAt) {
		t.Fatal("admitConn rejected available connection")
	}
	srv.handleConn(context.Background(), conn)

	if len(conn.deadlines) != 1 {
		t.Fatalf("SetDeadline calls = %d, want one fixed handshake deadline", len(conn.deadlines))
	}
	if want := acceptedAt.Add(30 * time.Second); !conn.deadlines[0].Equal(want) {
		t.Fatalf("handshake deadline = %v, want %v", conn.deadlines[0], want)
	}
	if len(conn.readDeadlines) != 0 {
		t.Fatalf("runtime read deadlines before handshake = %v", conn.readDeadlines)
	}
}

func TestHandshakeRestoresRuntimeReadDeadline(t *testing.T) {
	srv := newTestServer(t, 1)
	conn := &testConn{reader: bytes.NewReader(handshakeInput(t, testID(1)))}
	acceptedAt := time.Now()
	if !srv.admitConn(context.Background(), conn, acceptedAt) {
		t.Fatal("admitConn rejected available connection")
	}
	srv.handleConn(context.Background(), conn)

	if len(conn.deadlines) != 2 {
		t.Fatalf("SetDeadline calls = %d, want admission deadline and handshake clear", len(conn.deadlines))
	}
	if !conn.deadlines[0].Equal(acceptedAt.Add(handshakeTimeout)) {
		t.Fatalf("handshake deadline = %v", conn.deadlines[0])
	}
	if !conn.deadlines[1].IsZero() {
		t.Fatalf("post-handshake SetDeadline = %v, want zero", conn.deadlines[1])
	}
	if len(conn.readDeadlines) != 1 || conn.readDeadlines[0].IsZero() {
		t.Fatalf("runtime read deadlines = %v, want established-session idle deadline", conn.readDeadlines)
	}
	if len(srv.conns) != 0 {
		t.Fatal("completed connection retained its admission slot")
	}
}

func TestConnectionLimitCountsHandshakesAndReleasesSlot(t *testing.T) {
	srv := newTestServer(t, 1)
	first := &testConn{reader: bytes.NewReader(nil)}
	if !srv.admitConn(context.Background(), first, time.Now()) {
		t.Fatal("first connection was not admitted")
	}

	refused := &testConn{reader: bytes.NewReader(nil)}
	if srv.admitConn(context.Background(), refused, time.Now()) {
		t.Fatal("connection beyond MaxConnections was admitted")
	}
	if !refused.closed {
		t.Fatal("refused connection was not closed")
	}

	srv.releaseConn(first)
	reused := &testConn{reader: bytes.NewReader(nil)}
	if !srv.admitConn(context.Background(), reused, time.Now()) {
		t.Fatal("released connection slot was not reusable")
	}
	srv.releaseConn(reused)
}

func TestHandshakeDeadlineClosesStalledConnectionsAndReleasesLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		partial bool
	}{
		{name: "silent peer"},
		{name: "partial initial atom", partial: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t, 1)
			peer, serverSide := net.Pipe()
			defer peer.Close()
			conn := tcpRemoteConn{Conn: serverSide}

			// Make the production 30-second deadline expire shortly, without
			// waiting 30 seconds in the test.
			acceptedAt := time.Now().Add(-handshakeTimeout + 250*time.Millisecond)
			deadline := acceptedAt.Add(handshakeTimeout)
			if !srv.admitConn(context.Background(), conn, acceptedAt) {
				t.Fatal("connection was not admitted")
			}

			done := make(chan struct{})
			go func() {
				srv.handleConn(context.Background(), conn)
				close(done)
			}()
			if tc.partial {
				if _, err := peer.Write([]byte{0x00}); err != nil {
					t.Fatalf("write partial atom: %v", err)
				}
			}
			refusedPeer, refusedServer := net.Pipe()
			defer refusedPeer.Close()
			if srv.admitConn(context.Background(), tcpRemoteConn{Conn: refusedServer}, time.Now()) {
				t.Fatal("connection beyond MaxConnections was admitted during a handshake")
			}

			select {
			case <-done:
				if time.Now().Before(deadline) {
					t.Fatal("handler returned before the fixed handshake deadline")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("stalled handshake did not time out")
			}

			if len(srv.conns) != 0 {
				t.Fatal("timed-out handshake retained its connection slot")
			}
			_ = peer.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := peer.Read(make([]byte, 1)); err == nil {
				t.Fatal("peer connection remained open after handshake timeout")
			}

			reusePeer, reuseServer := net.Pipe()
			defer reusePeer.Close()
			reuseConn := tcpRemoteConn{Conn: reuseServer}
			if !srv.admitConn(context.Background(), reuseConn, time.Now()) {
				t.Fatal("timed-out handshake slot was not reusable")
			}
			_ = reuseConn.Close()
			srv.releaseConn(reuseConn)
		})
	}
}

func TestDuplicateSessionDoesNotReplaceExistingSession(t *testing.T) {
	srv := newTestServer(t, 2)
	id := testID(4)
	existing := &session{conn: &testConn{}, clientID: id}
	srv.sessions[id] = existing
	conn := &testConn{reader: bytes.NewReader(handshakeInput(t, id))}
	if !srv.admitConn(context.Background(), conn, time.Now()) {
		t.Fatal("duplicate connection was not admitted for handshake")
	}
	srv.handleConn(context.Background(), conn)
	if got := srv.sessions[id]; got != existing {
		t.Fatal("duplicate handshake disturbed the existing session")
	}
	if !conn.closed {
		t.Fatal("duplicate connection was not closed")
	}
}

func TestCloseAllConnectionsClosesPendingHandshake(t *testing.T) {
	srv := newTestServer(t, 1)
	conn := &testConn{reader: bytes.NewReader(nil)}
	if !srv.admitConn(context.Background(), conn, time.Now()) {
		t.Fatal("connection was not admitted")
	}
	srv.closeAllConnections()
	if !conn.closed {
		t.Fatal("pending handshake connection was not closed")
	}
}

func TestAdmissionAfterShutdownIsRejectedAndClosed(t *testing.T) {
	srv := newTestServer(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Complete the shutdown snapshot before presenting a late accepted socket.
	cancel()
	srv.closeAllConnections()
	peer, serverSide := net.Pipe()
	defer peer.Close()
	conn := tcpRemoteConn{Conn: serverSide}
	if srv.admitConn(ctx, conn, time.Now()) {
		t.Fatal("connection was admitted after shutdown completed")
	}
	if len(srv.conns) != 0 {
		t.Fatal("late connection retained an admission slot")
	}
	assertPeerClosed(t, peer)
}

func TestShutdownClosesBlockedHandlerAndReleasesSlot(t *testing.T) {
	srv := newTestServer(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	peer, serverSide := net.Pipe()
	defer peer.Close()
	readStarted := make(chan struct{})
	conn := tcpRemoteConn{Conn: &readSignalConn{Conn: serverSide, readStarted: readStarted}}
	if !srv.admitConn(ctx, conn, time.Now()) {
		t.Fatal("live connection was not admitted")
	}
	done := make(chan struct{})
	go func() {
		srv.handleConn(ctx, conn)
		close(done)
	}()
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("handler did not enter the initial handshake read")
	}
	if _, err := peer.Write([]byte{0x00}); err != nil {
		t.Fatalf("write partial initial atom: %v", err)
	}

	cancel()
	srv.closeAllConnections()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not release the blocked handshake handler")
	}
	if len(srv.conns) != 0 {
		t.Fatal("shutdown handler retained its admission slot")
	}
	assertPeerClosed(t, peer)
}

func assertPeerClosed(t *testing.T, peer net.Conn) {
	t.Helper()
	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return
		}
		t.Fatalf("set peer read deadline: %v", err)
	}
	if _, err := peer.Read(make([]byte, 1)); err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("read from closed peer: %v", err)
		}
	} else {
		t.Fatal("peer connection remained open")
	}
}

func TestAdmissionRejectsDeadlineSetupFailure(t *testing.T) {
	srv := newTestServer(t, 1)
	conn := &deadlineErrorConn{testConn: &testConn{reader: bytes.NewReader(nil)}}
	if srv.admitConn(context.Background(), conn, time.Now()) {
		t.Fatal("connection with failed deadline setup was admitted")
	}
	if !conn.closed {
		t.Fatal("connection with failed deadline setup was not closed")
	}
	if len(srv.conns) != 0 {
		t.Fatal("connection with failed deadline setup consumed a slot")
	}
}

type deadlineErrorConn struct{ *testConn }

func (c *deadlineErrorConn) SetDeadline(time.Time) error { return errors.New("deadline failure") }

type tcpRemoteConn struct{ net.Conn }

func (c tcpRemoteConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}

type readSignalConn struct {
	net.Conn
	readStarted chan struct{}
	readOnce    sync.Once
}

func (c *readSignalConn) Read(p []byte) (int, error) {
	c.readOnce.Do(func() { close(c.readStarted) })
	return c.Conn.Read(p)
}

func testID(b byte) (id pcp.GnuID) {
	for i := range id {
		id[i] = b
	}
	return id
}

var _ net.Conn = (*testConn)(nil)
