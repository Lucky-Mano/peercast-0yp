package httpd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/titagaki/peercast-0yp/internal/channel"
)

type channelJSON struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Genre        string    `json:"genre"`
	Desc         string    `json:"desc"`
	URL          string    `json:"url"`
	Comment      string    `json:"comment"`
	Bitrate      uint32    `json:"bitrate"`
	ContentType  string    `json:"contentType"`
	Track        trackJSON `json:"track"`
	Tracker      addrJSON  `json:"tracker"`
	NumListeners int       `json:"numListeners"`
	NumRelays    int       `json:"numRelays"`
	UpTime       uint32    `json:"upTime"`
}

type trackJSON struct {
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	Album   string `json:"album"`
	Contact string `json:"contact"`
}

type addrJSON struct {
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Firewalled bool   `json:"firewalled"`
}

func buildChannelJSON(hl channel.HitList, genrePrefix string) channelJSON {
	info := hl.Info
	out := channelJSON{
		ID:          fmt.Sprintf("%x", info.ID[:]),
		Name:        info.Name,
		Genre:       channel.GenreDisplay(genrePrefix, info.Genre),
		Desc:        info.Desc,
		URL:         info.URL,
		Comment:     info.Comment,
		Bitrate:     info.Bitrate,
		ContentType: info.ContentType,
		Track: trackJSON{
			Title:   info.Track.Title,
			Artist:  info.Track.Artist,
			Album:   info.Track.Album,
			Contact: info.Track.Contact,
		},
	}
	hidden := strings.Contains(info.Genre, "?")
	for _, hit := range hl.Hits {
		if hit.Tracker {
			ip := ""
			if hit.GlobalAddr.IP != nil {
				ip = hit.GlobalAddr.IP.String()
			}
			out.Tracker = addrJSON{
				IP:         ip,
				Port:       hit.GlobalAddr.Port,
				Firewalled: hit.Firewalled,
			}
			if hidden {
				out.NumListeners = -1
				out.NumRelays = -1
			} else {
				out.NumListeners = int(hit.NumListeners)
				out.NumRelays = int(hit.NumRelays)
			}
			out.UpTime = hit.UpTime
			break
		}
	}
	return out
}

func (s *Server) handleAPIChannels(w http.ResponseWriter, r *http.Request) {
	snap := s.store.Snapshot()

	entries := make([]channelJSON, 0, len(snap))
	for _, hl := range snap {
		entries = append(entries, buildChannelJSON(hl, s.store.GenrePrefix()))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}
