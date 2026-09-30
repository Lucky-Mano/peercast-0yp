package channel

import "strings"

// DefaultGenrePrefix is the YP prefix used when none is configured.
const DefaultGenrePrefix = "yp"

// GenreDisplay strips the YP control prefix from a genre string and returns
// only the display portion. Format: {prefix}[NS:][?][@@@]genre
func GenreDisplay(prefix, genre string) string {
	s := strings.TrimPrefix(genre, prefix)
	// strip optional namespace (alphanum chars followed by ":")
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[i+1:]
	}
	// strip listener-hide flag and port-check flags
	s = strings.TrimLeft(s, "?@")
	return s
}
