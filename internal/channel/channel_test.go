package channel_test

import (
	"net"
	"sync"
	"testing"
	"time"

	pcp "github.com/titagaki/peercast-pcp/pcp"

	"github.com/titagaki/peercast-0yp/internal/channel"
)

func makeGnuID(b byte) pcp.GnuID {
	var id pcp.GnuID
	for i := range id {
		id[i] = b
	}
	return id
}

func makeInfo(chanID, bcID pcp.GnuID, name string) channel.Info {
	return channel.Info{ID: chanID, BroadcastID: bcID, Name: name, Genre: "ypTest"}
}

func makeHit(sessionID, chanID pcp.GnuID, globalIP string, globalPort int, recv bool) channel.Hit {
	return channel.Hit{
		SessionID:  sessionID,
		ChanID:     chanID,
		GlobalAddr: net.TCPAddr{IP: net.ParseIP(globalIP), Port: globalPort},
		Recv:       recv,
	}
}

// TestAddHit_basic verifies that a hit can be registered and appears in Snapshot.
func TestAddHit_basic(t *testing.T) {
	s := channel.NewStore()
	chanID := makeGnuID(0x01)
	bcID := makeGnuID(0x02)
	sessID := makeGnuID(0x03)

	info := makeInfo(chanID, bcID, "Test Channel")
	hit := makeHit(sessID, chanID, "1.2.3.4", 7144, true)

	s.AddHit(info, hit)

	snap := s.Snapshot()
	hl, ok := snap[chanID]
	if !ok {
		t.Fatal("channel not found in snapshot")
	}
	if hl.Info.Name != "Test Channel" {
		t.Errorf("name = %q, want %q", hl.Info.Name, "Test Channel")
	}
	if len(hl.Hits) != 1 {
		t.Fatalf("hit count = %d, want 1", len(hl.Hits))
	}
	if hl.Hits[0].SessionID != sessID {
		t.Errorf("sessionID mismatch")
	}
}

// TestAddHit_dedup verifies that a second hit with the same session ID
// replaces the first (upsert semantics).
func TestAddHit_dedup(t *testing.T) {
	s := channel.NewStore()
	chanID := makeGnuID(0x01)
	bcID := makeGnuID(0x02)
	sessID := makeGnuID(0x03)

	info := makeInfo(chanID, bcID, "Chan")
	s.AddHit(info, makeHit(sessID, chanID, "1.2.3.4", 7144, true))
	s.AddHit(info, makeHit(sessID, chanID, "5.6.7.8", 7144, true)) // same session, new IP

	snap := s.Snapshot()
	if len(snap[chanID].Hits) != 1 {
		t.Errorf("expected 1 hit after dedup, got %d", len(snap[chanID].Hits))
	}
	if snap[chanID].Hits[0].GlobalAddr.IP.String() != "5.6.7.8" {
		t.Errorf("expected updated IP 5.6.7.8, got %s", snap[chanID].Hits[0].GlobalAddr.IP)
	}
}

// TestAddHit_bcidImmutability verifies that a different BroadcastID is rejected.
func TestAddHit_bcidImmutability(t *testing.T) {
	s := channel.NewStore()
	chanID := makeGnuID(0x01)
	bcID1 := makeGnuID(0x02)
	bcID2 := makeGnuID(0x99)
	sessID1 := makeGnuID(0x03)
	sessID2 := makeGnuID(0x04)

	s.AddHit(makeInfo(chanID, bcID1, "Chan"), makeHit(sessID1, chanID, "1.1.1.1", 7144, true))
	// Attempt to register with a different bcID — must be silently dropped.
	s.AddHit(makeInfo(chanID, bcID2, "Hijacked"), makeHit(sessID2, chanID, "2.2.2.2", 7144, true))

	snap := s.Snapshot()
	if snap[chanID].Info.Name == "Hijacked" {
		t.Error("bcID immutability violation: name was overwritten")
	}
	if len(snap[chanID].Hits) != 1 {
		t.Errorf("expected 1 hit, got %d", len(snap[chanID].Hits))
	}
}

// TestDelHit removes a hit and checks that the channel list is pruned.
func TestDelHit(t *testing.T) {
	s := channel.NewStore()
	chanID := makeGnuID(0x01)
	bcID := makeGnuID(0x02)
	sessID := makeGnuID(0x03)

	s.AddHit(makeInfo(chanID, bcID, "Chan"), makeHit(sessID, chanID, "1.2.3.4", 7144, true))
	s.DelHit(chanID, sessID)

	snap := s.Snapshot()
	if _, ok := snap[chanID]; ok {
		t.Error("expected channel to be pruned after last hit removed")
	}
}

// TestRemoveDeadHits verifies that stale hits are expired.
func TestRemoveDeadHits(t *testing.T) {
	s := channel.NewStore()
	chanID := makeGnuID(0x01)
	bcID := makeGnuID(0x02)
	sessID := makeGnuID(0x03)

	s.AddHit(makeInfo(chanID, bcID, "Chan"), makeHit(sessID, chanID, "1.2.3.4", 7144, true))

	// Immediately running cleanup with a large timeout should keep the hit.
	s.RemoveDeadHits(180 * time.Second)
	if _, ok := s.Snapshot()[chanID]; !ok {
		t.Fatal("hit should not have been removed yet")
	}

	// Running with a zero timeout should remove everything.
	s.RemoveDeadHits(0)
	if _, ok := s.Snapshot()[chanID]; ok {
		t.Error("hit should have been removed by zero-timeout cleanup")
	}
}

// TestAddHit_rejectNonYPGenre verifies that channels without the yp genre prefix are ignored.
func TestAddHit_rejectNonYPGenre(t *testing.T) {
	s := channel.NewStore()
	chanID := makeGnuID(0x01)
	bcID := makeGnuID(0x02)
	sessID := makeGnuID(0x03)

	info := channel.Info{ID: chanID, BroadcastID: bcID, Name: "Chan", Genre: "Music"}
	s.AddHit(info, makeHit(sessID, chanID, "1.2.3.4", 7144, true))

	if _, ok := s.Snapshot()[chanID]; ok {
		t.Error("channel without yp genre prefix should not have been registered")
	}
}

// TestAddHit_configuredGenrePrefix verifies that a Store accepts only its configured prefix.
func TestAddHit_configuredGenrePrefix(t *testing.T) {
	s := channel.NewStoreWithGenrePrefix("vp")
	vpID := makeGnuID(0x01)
	ypID := makeGnuID(0x11)

	s.AddHit(channel.Info{ID: vpID, BroadcastID: makeGnuID(0x02), Name: "VP", Genre: "vp?ゲーム"},
		makeHit(makeGnuID(0x03), vpID, "1.2.3.4", 7144, true))
	s.AddHit(channel.Info{ID: ypID, BroadcastID: makeGnuID(0x12), Name: "YP", Genre: "ypゲーム"},
		makeHit(makeGnuID(0x13), ypID, "1.2.3.5", 7144, true))

	snap := s.Snapshot()
	if _, ok := snap[vpID]; !ok {
		t.Error("channel with the configured vp prefix should have been registered")
	}
	if _, ok := snap[ypID]; ok {
		t.Error("channel with the default yp prefix should not be registered when vp is configured")
	}
	if got := s.GenrePrefix(); got != "vp" {
		t.Errorf("GenrePrefix() = %q, want vp", got)
	}
}

// TestAddHit_rejectEmptyName verifies that hits with empty channel names are ignored.
func TestAddHit_rejectEmptyName(t *testing.T) {
	s := channel.NewStore()
	chanID := makeGnuID(0x01)
	bcID := makeGnuID(0x02)
	sessID := makeGnuID(0x03)

	s.AddHit(makeInfo(chanID, bcID, ""), makeHit(sessID, chanID, "1.2.3.4", 7144, true))

	if _, ok := s.Snapshot()[chanID]; ok {
		t.Error("channel with empty name should not have been registered")
	}
}

// TestStore_MultipleHitsPerChannel verifies that distinct nodes for the same
// channel are all stored (no spurious dedup).
func TestStore_MultipleHitsPerChannel(t *testing.T) {
	s := channel.NewStore()
	chanID := makeGnuID(0x01)
	bcID := makeGnuID(0x02)

	info := makeInfo(chanID, bcID, "Multi Channel")
	// Three different session IDs and global IPs → no dedup should fire.
	s.AddHit(info, makeHit(makeGnuID(0xA1), chanID, "1.1.1.1", 7144, true))
	s.AddHit(info, makeHit(makeGnuID(0xA2), chanID, "2.2.2.2", 7144, true))
	s.AddHit(info, makeHit(makeGnuID(0xA3), chanID, "3.3.3.3", 7144, true))

	snap := s.Snapshot()
	hl, ok := snap[chanID]
	if !ok {
		t.Fatal("channel not found in snapshot")
	}
	if len(hl.Hits) != 3 {
		t.Errorf("expected 3 hits, got %d", len(hl.Hits))
	}
}

// TestStore_ConcurrentAccess checks that concurrent AddHit/DelHit does not
// race. Run with: go test -race ./channel/...
func TestStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	s := channel.NewStore()
	chanID := makeGnuID(0x01)
	bcID := makeGnuID(0x02)
	info := makeInfo(chanID, bcID, "Race Channel")

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sessID := makeGnuID(byte(n))
			s.AddHit(info, makeHit(sessID, chanID, "1.2.3.4", 7144, true))
			s.DelHit(chanID, sessID)
		}(i)
	}
	wg.Wait()
}
