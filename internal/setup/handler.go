// Package setup provides the web setup wizard for first-run configuration.
package setup

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// AccountChecker checks if accounts exist in the store.
type AccountChecker interface {
	HasAccounts() (bool, error)
}

// Handler serves the setup wizard or returns 503 if setup is complete.
type Handler struct {
	store  AccountChecker
	logger *slog.Logger
}

// NewHandler creates a new setup handler.
func NewHandler(store AccountChecker, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		store:  store,
		logger: logger,
	}
}

// ServeHTTP serves the setup wizard or returns 503 if setup is complete.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hasAccounts, err := h.store.HasAccounts()
	if err != nil {
		h.logger.Error("failed to check accounts", "error", err)
		h.respondError(w, http.StatusInternalServerError, "failed to check setup status")
		return
	}

	if hasAccounts {
		// Setup is complete - return 503
		h.respondError(w, http.StatusServiceUnavailable, "setup wizard is no longer available")
		return
	}

	// Serve setup wizard (placeholder for now)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>mailtagger Setup</title>
    <meta charset="utf-8">
</head>
<body>
    <h1>mailtagger Setup Wizard</h1>
    <p>Setup wizard will be implemented here.</p>
</body>
</html>`))
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
