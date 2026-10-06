package httpd

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pcp "github.com/titagaki/peercast-pcp/pcp"

	"github.com/titagaki/peercast-0yp/internal/channel"
)

func makeChannelState(name, genre string, listeners, relays int, hits []channel.Hit) channel.ChannelState {
	var chanID pcp.GnuID
	for i := range chanID {
		chanID[i] = 0x01
	}
	return channel.ChannelState{
		Info: channel.Info{
			ID:          chanID,
			Name:        name,
			Bitrate:     256,
			ContentType: "FLV",
			Genre:       "yp" + genre,
			Desc:        "A description",
			URL:         "http://example.com",
			Comment:     "A comment",
			Track: channel.Track{
				Title:   "Song Title",
				Artist:  "Artist Name",
				Album:   "Album Name",
				Contact: "http://contact.example.com",
			},
		},
		Hits:      hits,
		Listeners: listeners,
		Relays:    relays,
	}
}

func parseIndexLine(t *testing.T, line string) []string {
	t.Helper()
	line = strings.TrimSuffix(line, "\n")
	fields := strings.Split(line, "<>")
	if len(fields) != 19 {
		t.Fatalf("expected 19 fields, got %d in line: %q", len(fields), line)
	}
	return fields
}

func TestWriteIndexLine_BasicFormat(t *testing.T) {
	hit := channel.Hit{
		SessionID:  makeID(0xAA),
		Tracker:    true,
		GlobalAddr: net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 7144},
		UpTime:     3661, // 1h 1m 1s → "61:01" (truncates to minutes)
	}
	cs := makeChannelState("My Channel", "Music", 5, 2, []channel.Hit{hit})

	var buf bytes.Buffer
	writeIndexLine(&buf, cs, channel.DefaultGenrePrefix)

	fields := parseIndexLine(t, buf.String())
	if fields[0] != "My Channel" {
		t.Errorf("field[0] name = %q, want My Channel", fields[0])
	}
	if fields[2] != "1.2.3.4:7144" {
		t.Errorf("field[2] tracker = %q, want 1.2.3.4:7144", fields[2])
	}
	if fields[3] != "http://example.com" {
		t.Errorf("field[3] url = %q", fields[3])
	}
	if fields[4] != "Music" {
		t.Errorf("field[4] genre = %q, want Music", fields[4])
	}
	if fields[6] != "5" {
		t.Errorf("field[6] listeners = %q, want 5", fields[6])
	}
	if fields[7] != "2" {
		t.Errorf("field[7] relays = %q, want 2", fields[7])
	}
	if fields[8] != "256" {
		t.Errorf("field[8] bitrate = %q, want 256", fields[8])
	}
	if fields[16] != "click" {
		t.Errorf("field[16] = %q, want click", fields[16])
	}
	if fields[17] != "A comment" {
		t.Errorf("field[17] comment = %q, want A comment", fields[17])
	}
}

func TestWriteIndexLine_Duration(t *testing.T) {
	tests := []struct {
		uptime   uint32
		wantDur  string
	}{
		{0, "0:00"},
		{59, "0:00"},    // under 1 minute → 0:00
		{60, "0:01"},    // exactly 1 minute
		{3600, "1:00"},  // 1 hour
		{3661, "1:01"},  // 1 hour 1 minute
		{7322, "2:02"},  // 2 hours 2 minutes
	}
	for _, tc := range tests {
		hit := channel.Hit{Tracker: true, GlobalAddr: net.TCPAddr{IP: net.ParseIP("1.1.1.1"), Port: 7144}, UpTime: tc.uptime}
		cs := makeChannelState("Chan", "Music", 0, 0, []channel.Hit{hit})

		var buf bytes.Buffer
		writeIndexLine(&buf, cs, channel.DefaultGenrePrefix)
		fields := parseIndexLine(t, buf.String())

		if fields[15] != tc.wantDur {
			t.Errorf("uptime=%d: duration = %q, want %q", tc.uptime, fields[15], tc.wantDur)
		}
	}
}

func TestWriteIndexLine_HiddenListeners(t *testing.T) {
	cs := makeChannelState("Chan", "Music?", 10, 5, nil)

	var buf bytes.Buffer
	writeIndexLine(&buf, cs, channel.DefaultGenrePrefix)
	fields := parseIndexLine(t, buf.String())

	if fields[6] != "-1" {
		t.Errorf("listeners = %q, want -1 for hidden genre", fields[6])
	}
	if fields[7] != "-1" {
		t.Errorf("relays = %q, want -1 for hidden genre", fields[7])
	}
}

func TestWriteIndexLine_DirectFlag(t *testing.T) {
	hitDirect := channel.Hit{Tracker: true, Direct: true, GlobalAddr: net.TCPAddr{IP: net.ParseIP("1.1.1.1"), Port: 7144}}
	hitNoDirect := channel.Hit{Tracker: true, Direct: false, GlobalAddr: net.TCPAddr{IP: net.ParseIP("2.2.2.2"), Port: 7144}}

	for _, tc := range []struct {
		hits []channel.Hit
		want string
	}{
		{[]channel.Hit{hitDirect}, "1"},
		{[]channel.Hit{hitNoDirect}, "0"},
		{nil, "0"},
	} {
		cs := makeChannelState("Chan", "Music", 0, 0, tc.hits)
		var buf bytes.Buffer
		writeIndexLine(&buf, cs, channel.DefaultGenrePrefix)
		fields := parseIndexLine(t, buf.String())
		if fields[18] != tc.want {
			t.Errorf("directFlag = %q, want %q", fields[18], tc.want)
		}
	}
}

func TestWriteIndexLine_NoTracker(t *testing.T) {
	// No tracker hit → empty tracker address field.
	cs := makeChannelState("Chan", "Music", 0, 0, nil)

	var buf bytes.Buffer
	writeIndexLine(&buf, cs, channel.DefaultGenrePrefix)
	fields := parseIndexLine(t, buf.String())

	if fields[2] != "" {
		t.Errorf("tracker field = %q, want empty when no tracker hit", fields[2])
	}
}

func TestHandleIndexTxt_Empty(t *testing.T) {
	s := &Server{store: channel.NewStore()}
	req := httptest.NewRequest(http.MethodGet, "/index.txt", nil)
	w := httptest.NewRecorder()
	s.handleIndexTxt(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body should be empty for empty store, got %q", w.Body.String())
	}
}

func TestHandleIndexTxt_OneChannel(t *testing.T) {
	store := channel.NewStore()
	addChannel(store, "Test Channel", makeID(0x01), makeID(0x02), makeID(0x03), 5)

	s := &Server{store: store}
	req := httptest.NewRequest(http.MethodGet, "/index.txt", nil)
	w := httptest.NewRecorder()
	s.handleIndexTxt(w, req)

	body := w.Body.String()
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "Test Channel<>") {
		t.Errorf("line does not start with channel name: %q", lines[0])
	}
}

func TestHandleIndexTxt_ConfiguredGenrePrefix(t *testing.T) {
	store := channel.NewStoreWithGenrePrefix("vp")
	chanID := makeID(0x01)
	store.AddHit(channel.Info{ID: chanID, BroadcastID: makeID(0x02), Name: "VP Channel", Genre: "vpABC:?ゲーム"},
		channel.Hit{SessionID: makeID(0x03), Tracker: true, GlobalAddr: net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 7144}, NumListeners: 5})

	s := &Server{store: store}
	w := httptest.NewRecorder()
	s.handleIndexTxt(w, httptest.NewRequest(http.MethodGet, "/index.txt", nil))

	fields := parseIndexLine(t, w.Body.String())
	if fields[4] != "ゲーム" {
		t.Errorf("field[4] genre = %q, want ゲーム", fields[4])
	}
	if fields[6] != "-1" || fields[7] != "-1" {
		t.Errorf("listeners/relays = %q/%q, want -1/-1", fields[6], fields[7])
	}
}
