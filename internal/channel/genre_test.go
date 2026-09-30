package channel

import "testing"

func TestGenreDisplay(t *testing.T) {
	tests := []struct {
		prefix string
		input  string
		want   string
	}{
		{"yp", "ap:Music", "Music"},           // namespace stripped without the prefix
		{"yp", "yp:Rock", "Rock"},             // prefix and empty namespace
		{"yp", "Music", "Music"},              // no prefix
		{"yp", "", ""},                        // empty string
		{"yp", "yp:Rock:Heavy", "Rock:Heavy"}, // only first colon stripped
		{"yp", ":", ""},                       // colon only
		{"yp", "yp?@@ゲーム", "ゲーム"},             // hide and port-check flags
		{"vp", "vpABC:?ゲーム", "ゲーム"},           // configured prefix
		{"vp", "vp音楽", "音楽"},                  // configured prefix without flags
	}
	for _, tc := range tests {
		got := GenreDisplay(tc.prefix, tc.input)
		if got != tc.want {
			t.Errorf("GenreDisplay(%q, %q) = %q, want %q", tc.prefix, tc.input, got, tc.want)
		}
	}
}
