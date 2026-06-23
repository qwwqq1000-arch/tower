package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:spa/dist
var spaFiles embed.FS

// SPAHandler serves the built SPA with client-side-routing fallback to index.html.
func SPAHandler() http.Handler {
	sub, err := fs.Sub(spaFiles, "spa/dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	index, _ := fs.ReadFile(sub, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// try static file; if missing, serve index.html (SPA route)
		p := trimLeadingSlash(r.URL.Path)
		if f, err := sub.Open(p); err == nil {
			f.Close()
			// index.html must never be cached or the browser keeps referencing stale,
			// content-hashed bundle filenames after a deploy (old hash → 404 → blank
			// page). The hashed assets themselves are immutable and may cache freely.
			if p == "index.html" {
				w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA client-side route → index.html; never cache (same reason).
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

func trimLeadingSlash(p string) string {
	if p == "/" || p == "" {
		return "index.html"
	}
	if p[0] == '/' {
		return p[1:]
	}
	return p
}
