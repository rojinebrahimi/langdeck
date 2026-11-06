// Package web embeds the compiled React UI into the Go binary, so LangDeck
// ships as a single self-contained executable.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the single-page app. Unknown paths fall back to index.html so
// client-side routes work on reload. Hashed assets are cached for a year.
func Handler() http.Handler {
	sub, _ := fs.Sub(dist, "dist")
	return handler(sub)
}

func handler(sub fs.FS) http.Handler {
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if strings.HasPrefix(p, "api/") {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(sub, p); err != nil {
			index, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.Error(w, "UI not built. Run `make ui` first.", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(index)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
