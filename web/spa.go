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
		if f, err := sub.Open(trimLeadingSlash(r.URL.Path)); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
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
