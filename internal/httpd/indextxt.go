package httpd

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/titagaki/peercast-0yp/internal/channel"
)

func (s *Server) handleIndexTxt(w http.ResponseWriter, r *http.Request) {
	states := s.store.SnapshotOrdered()

	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	for _, cs := range states {
		writeIndexLine(w, cs, s.store.GenrePrefix())
	}
	if s.ypName != "" {
		writeStatusLine(w, s.ypName, s.ypURL, time.Since(s.startTime))
	}
	for _, info := range s.infoLines {
		writeInfoLine(w, info.Name, info.URL, info.Comment)
	}
}

// writeInfoLine writes a single announcement line above the status line in index.txt.
// Uses all-zero ID, Listeners/Relays=-9, ContentType=RAW.
func writeInfoLine(w io.Writer, name, contactURL, comment string) {
	fmt.Fprintf(w, "%s<>%s<><>%s<><>%s<>-9<>-9<>0<>RAW<><><><><><>00:00<>click<><>0\n",
		name,
		strings.Repeat("0", 32),
		contactURL,
		comment,
	)
}

// writeStatusLine writes a YP status line at the end of index.txt.
// Format mirrors p-at.net: a regular 19-field channel line with ID=all-zeros,
// Listeners/Relays=-9, ContentType=RAW, and uptime info in the Comment field.
func writeStatusLine(w io.Writer, name, ypURL string, uptime time.Duration) {
	d := int(uptime.Seconds())
	days, d := d/86400, d%86400
	hours, d := d/3600, d%3600
	mins, secs := d/60, d%60

	var uptimeStr string
	switch days {
	case 0:
		uptimeStr = fmt.Sprintf("%d:%02d:%02d", hours, mins, secs)
	case 1:
		uptimeStr = fmt.Sprintf("1 day, %d:%02d:%02d", hours, mins, secs)
	default:
		uptimeStr = fmt.Sprintf("%d days, %d:%02d:%02d", days, hours, mins, secs)
	}
	displayName := name + "◆Status"
	fmt.Fprintf(w, "%s<>%s<><>%s<><>%s<>-9<>-9<>0<>RAW<><><><><><>0:00<>click<><>0\n",
		displayName,
		strings.Repeat("0", 32),
		ypURL,
		"Uptime="+uptimeStr,
	)
}

func writeIndexLine(w io.Writer, cs channel.ChannelState, genrePrefix string) {
	info := cs.Info
	track := info.Track

	// Field 19: 1 if any Hit has Direct=true (PCPHostFlags1Direct).
	directFlag := "0"
	for _, h := range cs.Hits {
		if h.Direct {
			directFlag = "1"
			break
		}
	}

	// Field 7/8: mask listeners/relays if genre contains "?".
	listeners := fmt.Sprintf("%d", cs.Listeners)
	relays := fmt.Sprintf("%d", cs.Relays)
	if strings.Contains(info.Genre, "?") {
		listeners = "-1"
		relays = "-1"
	}

	// Field 3: tracker IP:port from first Hit with Tracker=true.
	trackerAddr := ""
	uptime := uint32(0)
	for _, h := range cs.Hits {
		if h.Tracker && h.GlobalAddr.IP != nil {
			trackerAddr = fmt.Sprintf("%s:%d", h.GlobalAddr.IP, h.GlobalAddr.Port)
			uptime = h.UpTime
			break
		}
	}

	// Field 16: H:MM from uptime seconds.
	minutes := uptime / 60
	duration := fmt.Sprintf("%d:%02d", minutes/60, minutes%60)

	fmt.Fprintf(w, "%s<>%s<>%s<>%s<>%s<>%s<>%s<>%s<>%d<>%s<>%s<>%s<>%s<>%s<>%s<>%s<>click<>%s<>%s\n",
		info.Name,
		fmt.Sprintf("%x", info.ID[:]),
		trackerAddr,
		info.URL,
		channel.GenreDisplay(genrePrefix, info.Genre),
		info.Desc,
		listeners,
		relays,
		info.Bitrate,
		info.ContentType,
		track.Artist,
		track.Album,
		track.Title,
		track.Contact,
		url.QueryEscape(info.Name),
		duration,
		info.Comment,
		directFlag,
	)
}
