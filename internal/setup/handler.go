// Package setup provides the web setup wizard for first-run configuration.
package setup

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

// AccountChecker checks if accounts exist in the store.
type AccountChecker interface {
	HasAccounts() (bool, error)
}

// Handler serves the setup wizard or returns 503 if setup is complete.
type Handler struct {
	store     AccountChecker
	logger    *slog.Logger
	staticFS  http.Handler
}

// NewHandler creates a new setup handler.
func NewHandler(store AccountChecker, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}

	// Create a sub-filesystem rooted at "static"
	staticContent, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(staticContent))

	return &Handler{
		store:    store,
		logger:   logger,
		staticFS: fileServer,
	}
}

// ServeHTTP serves the setup wizard.
// Access control is handled by the token middleware, so this just serves the static files.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Determine what file to serve
	path := r.URL.Path
	
	// Strip /setup prefix if present
	if strings.HasPrefix(path, "/setup/") {
		path = strings.TrimPrefix(path, "/setup")
	} else if path == "/setup" || path == "" {
		path = "/"
	}

	// For SPA routing, serve index.html for non-file paths (paths without extensions)
	if path == "/" || (!strings.Contains(path, ".") && !strings.HasPrefix(path, "/api/")) {
		// Serve index.html directly
		content, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			h.respondError(w, http.StatusInternalServerError, "failed to load setup wizard")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(content)
		return
	}

	// For other static files, use the file server
	r2 := new(http.Request)
	*r2 = *r
	r2.URL = &url.URL{
		Path: path,
	}

	h.staticFS.ServeHTTP(w, r2)
}

// IsSetupMode returns true if the application is in setup mode (no accounts).
func (h *Handler) IsSetupMode() (bool, error) {
	hasAccounts, err := h.store.HasAccounts()
	if err != nil {
		return false, err
	}
	return !hasAccounts, nil
}

func (h *Handler) respondError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
