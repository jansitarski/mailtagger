package admin

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

// StaticHandler returns an http.Handler that serves the embedded admin SPA.
// All non-file requests are served index.html for SPA routing.
func StaticHandler() http.Handler {
	subFS, _ := fs.Sub(staticFiles, "static")
	fileServer := http.FileServer(http.FS(subFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := r.URL.Path
		if path == "/" || path == "" {
			path = "index.html"
		}

		// Check if file exists in embedded FS
		if f, err := subFS.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for SPA routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
