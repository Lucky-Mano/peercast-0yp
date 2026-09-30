package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/titagaki/peercast-0yp/internal/channel"
)

func TestHandleAPIConfig_GenrePrefix(t *testing.T) {
	s := &Server{
		store:      channel.NewStoreWithGenrePrefix("vp"),
		ypIndexURL: "https://example.com/yp/index.txt",
		pcpAddress: "pcp://example.com/",
	}
	w := httptest.NewRecorder()
	s.handleAPIConfig(w, httptest.NewRequest(http.MethodGet, "/yp/api/config", nil))

	var got configJSON
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := configJSON{
		YPIndexURL:  "https://example.com/yp/index.txt",
		PCPAddress:  "pcp://example.com/",
		GenrePrefix: "vp",
	}
	if got != want {
		t.Errorf("config = %+v, want %+v", got, want)
	}
}
