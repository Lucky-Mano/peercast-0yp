package httpd

import (
	"embed"
	"io/fs"
	"net/http"
)

// The web UI is built into dist/web (see web/vite.config.ts) and is not
// committed. dist/.gitkeep keeps the embed pattern valid when it is absent.
//
//go:embed all:dist
var distFS embed.FS

var topPageHTML = []byte(`<!DOCTYPE html>
<html lang="ja">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>yayaue.me</title>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body {
      font-family: monospace;
      background: #fff;
      color: #111;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
    }
    .site { text-align: center; }
    .domain { font-size: 2.5rem; font-weight: 900; letter-spacing: -0.02em; }
    .links { margin-top: 1.5rem; display: flex; gap: 1.5rem; justify-content: center; }
    a { color: #888; text-decoration: none; font-size: 0.9rem; }
    a:hover { color: #111; }
  </style>
</head>
<body>
  <div class="site">
    <p class="domain">yayaue.me</p>
    <div class="links">
      <a href="/yp/">令和のYP 0yp</a>
    </div>
  </div>
</body>
</html>
`)

func spaHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist/web")
	if err != nil {
		panic(err)
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "web UI is not built", http.StatusNotFound)
		})
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file as-is; fall back to index.html for SPA routing.
		f, err := sub.Open(r.URL.Path[1:]) // strip leading "/"
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Not found → serve index.html so React Router handles it.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
