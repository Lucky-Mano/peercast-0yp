// Package channel manages the in-memory registry of PeerCast channels and
// their associated relay nodes (hits). All exported methods are safe for
// concurrent use.
package channel

import (
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	pcp "github.com/titagaki/peercast-pcp/pcp"
)

// Info holds the metadata for a single PeerCast channel.
// It maps directly to the chan > info sub-container in the PCP wire format.
type Info struct {
	ID          pcp.GnuID // PCP tag: id
	BroadcastID pcp.GnuID // PCP tag: bcid — immutable once set (ownership key)
	Name        string    // PCP tag: name
	Bitrate     uint32    // PCP tag: bitr (kbps)
	ContentType string    // PCP tag: type  (e.g. "FLV", "MKV", "MP3")
	MIMEType    string    // PCP tag: styp  (e.g. "video/x-flv")
	StreamExt   string    // PCP tag: sext  (e.g. ".flv")
	Genre       string    // PCP tag: gnre
	Desc        string    // PCP tag: desc
	URL         string    // PCP tag: url
	Comment     string    // PCP tag: cmnt
	Track       Track
}

// Track holds per-track info within a channel (chan > trck sub-container).
type Track struct {
	Title   string // PCP tag: titl
	Artist  string // PCP tag: crea
	Contact string // PCP tag: url
	Album   string // PCP tag: albm
}

// Hit represents a single relay/tracker node for a channel.
// It maps to the host atom inside a bcst packet.
type Hit struct {
	SessionID    pcp.GnuID   // PCP tag: id   — node's session ID
	ChanID       pcp.GnuID   // PCP tag: cid  — associated channel
	GlobalAddr   net.TCPAddr // PCP tags: ip+port pair [0] — public address
	LocalAddr    net.TCPAddr // PCP tags: ip+port pair [1] — LAN address
	NumListeners uint32      // PCP tag: numl
	NumRelays    uint32      // PCP tag: numr — connected relay count
	UpTime       uint32      // PCP tag: uptm (seconds)
	Version      uint32      // PCP tag: ver
	VersionVP    uint32      // PCP tag: vevp
	VersionExPfx [2]byte     // PCP tag: vexp (2 ASCII bytes, e.g. "YT")
	VersionExNum uint16      // PCP tag: vexn
	OldPos       uint32      // PCP tag: oldp
	NewPos       uint32      // PCP tag: newp

	// Flags decoded from flg1 byte.
	Tracker    bool // bit 0: this node is the tracker (source)
	Relay      bool // bit 1: relay slots available
	Direct     bool // bit 2: direct connections available
	Firewalled bool // bit 3: behind firewall (push required)
	Recv       bool // bit 4: currently receiving the stream
	CIN        bool // bit 5: CIN connection slots available

	NumHops  int       // hop count at reception (from bcst routing header)
	LastSeen time.Time // wall-clock time of the most recent update
}

// HitList groups all known Hits for a single channel together with the
// channel's current metadata.
type HitList struct {
	Info        Info
	Hits        []Hit
	LastHitTime time.Time
	seq         uint64 // insertion sequence; lower = registered earlier
}

// ChannelState is an aggregate snapshot of one channel for archive.Recorder.
type ChannelState struct {
	Info      Info
	Hits      []Hit
	Listeners int    // sum of NumListeners across all Hits
	Relays    int    // sum of NumRelays across all Hits
	Seq       uint64 // insertion order (lower = earlier registered)
}

// Store is a thread-safe, in-memory registry of channel hit lists.
type Store struct {
	mu          sync.RWMutex
	lists       map[pcp.GnuID]*HitList
	nextSeq     uint64 // monotonically increasing; incremented on first AddHit for a channel
	genrePrefix string // channels whose genre lacks this prefix are not for this YP
}

// NewStore returns an empty Store that accepts genres prefixed with
// DefaultGenrePrefix.
func NewStore() *Store {
	return NewStoreWithGenrePrefix(DefaultGenrePrefix)
}

// NewStoreWithGenrePrefix returns an empty Store that accepts only channels
// whose genre starts with prefix.
func NewStoreWithGenrePrefix(prefix string) *Store {
	return &Store{lists: make(map[pcp.GnuID]*HitList), genrePrefix: prefix}
}

// GenrePrefix returns the YP prefix this Store accepts.
func (s *Store) GenrePrefix() string {
	return s.genrePrefix
}

// AddHit registers or refreshes a hit for the given channel.
//
// Validation rules (matching C++ ChanInfo::update and ChanHitList::addHit):
//   - info.ID must be non-zero and info.Name must be non-empty.
//   - If a BroadcastID is already registered for this channel, the incoming
//     info.BroadcastID must match (BCID immutability / ownership check).
//
// Duplicate detection (in order):
//  1. If an existing hit shares both GlobalAddr and LocalAddr, it is replaced.
//  2. Otherwise, if an existing hit shares SessionID (IP changed), it is
//     replaced.
//  3. If no match is found, the hit is appended.
func (s *Store) AddHit(info Info, hit Hit) {
	if info.ID.IsEmpty() || info.Name == "" || !strings.HasPrefix(info.Genre, s.genrePrefix) {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	hl, ok := s.lists[info.ID]
	if !ok {
		hl = &HitList{seq: s.nextSeq}
		s.nextSeq++
		s.lists[info.ID] = hl
	}

	// BCID immutability: once set, reject mismatching BroadcastIDs.
	if !hl.Info.BroadcastID.IsEmpty() && hl.Info.BroadcastID != info.BroadcastID {
		return
	}

	hl.Info = info
	hl.LastHitTime = time.Now()
	hit.LastSeen = time.Now()

	for i := range hl.Hits {
		h := &hl.Hits[i]
		sameAddr := h.GlobalAddr.IP.Equal(hit.GlobalAddr.IP) &&
			h.GlobalAddr.Port == hit.GlobalAddr.Port &&
			h.LocalAddr.IP.Equal(hit.LocalAddr.IP) &&
			h.LocalAddr.Port == hit.LocalAddr.Port
		if sameAddr || h.SessionID == hit.SessionID {
			hl.Hits[i] = hit
			return
		}
	}
	hl.Hits = append(hl.Hits, hit)
}

// DelHit removes the hit identified by sessionID from the channel's hit list.
// If the hit list becomes empty it is pruned from the store.
func (s *Store) DelHit(chanID pcp.GnuID, sessionID pcp.GnuID) {
	if chanID.IsEmpty() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	hl, ok := s.lists[chanID]
	if !ok {
		return
	}

	for i, h := range hl.Hits {
		if h.SessionID == sessionID {
			// Swap-delete: order doesn't matter for a hit list.
			hl.Hits[i] = hl.Hits[len(hl.Hits)-1]
			hl.Hits = hl.Hits[:len(hl.Hits)-1]
			break
		}
	}

	if len(hl.Hits) == 0 {
		delete(s.lists, chanID)
	}
}

// RemoveDeadHits removes any Hit whose LastSeen is older than timeout, then
// prunes empty HitLists. Designed to be called from a timer goroutine
// (e.g. every 500 ms) to replicate C++ ChanMgr::clearDeadHits behaviour
// (180-second hard-coded timeout in the reference implementation).
func (s *Store) RemoveDeadHits(timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, hl := range s.lists {
		live := hl.Hits[:0]
		for _, h := range hl.Hits {
			if now.Sub(h.LastSeen) < timeout {
				live = append(live, h)
			}
		}
		hl.Hits = live
		if len(hl.Hits) == 0 {
			delete(s.lists, id)
		}
	}
}

// SnapshotOrdered returns all current channels as ChannelState, ordered by
// registration sequence (the order channels were first added via AddHit).
func (s *Store) SnapshotOrdered() []ChannelState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ChannelState, 0, len(s.lists))
	for _, hl := range s.lists {
		var listeners, relays int
		for _, h := range hl.Hits {
			listeners += int(h.NumListeners)
			relays += int(h.NumRelays)
		}
		hits := make([]Hit, len(hl.Hits))
		copy(hits, hl.Hits)
		out = append(out, ChannelState{
			Info:      hl.Info,
			Hits:      hits,
			Listeners: listeners,
			Relays:    relays,
			Seq:       hl.seq,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// Snapshot returns a deep copy of all current hit lists, suitable for
// read-only use (e.g. serving an HTTP channel listing).
func (s *Store) Snapshot() map[pcp.GnuID]HitList {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[pcp.GnuID]HitList, len(s.lists))
	for id, hl := range s.lists {
		hits := make([]Hit, len(hl.Hits))
		copy(hits, hl.Hits)
		out[id] = HitList{
			Info:        hl.Info,
			Hits:        hits,
			LastHitTime: hl.LastHitTime,
		}
	}
	return out
}
