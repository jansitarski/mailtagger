package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/jansitarski/mailtagger/internal/store"
)

// TokenStore defines the interface needed by the OAuth handler to persist tokens.
type TokenStore interface {
	GetAccountByEmail(email string) (*store.Account, error)
	InsertAccount(email string, encryptedToken []byte) (*store.Account, error)
	UpdateToken(accountID int64, encryptedToken []byte) error
}

// StateValidator validates and parses the OAuth state parameter.
// Returns the account email embedded in the state, or an error if invalid.
type StateValidator func(state string) (email string, err error)

// OAuthHandler handles the /oauth/callback endpoint.
type OAuthHandler struct {
	oauthConfig    *oauth2.Config
	encryptionKey  []byte
	store          TokenStore
	stateValidator StateValidator
	logger         *slog.Logger
}

// OAuthHandlerConfig contains dependencies for the OAuth callback handler.
type OAuthHandlerConfig struct {
	OAuthConfig    *oauth2.Config
	EncryptionKey  []byte
	Store          TokenStore
	StateValidator StateValidator
	Logger         *slog.Logger
}

// NewOAuthHandler creates a new OAuth callback handler.
func NewOAuthHandler(cfg OAuthHandlerConfig) *OAuthHandler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &OAuthHandler{
		oauthConfig:    cfg.OAuthConfig,
		encryptionKey:  cfg.EncryptionKey,
		store:          cfg.Store,
		stateValidator: cfg.StateValidator,
		logger:         cfg.Logger,
	}
}

// ServeHTTP handles the OAuth callback request.
func (h *OAuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check for error from OAuth provider
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		h.logger.Error("oauth provider returned error", "error", errParam, "description", desc)
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("OAuth error: %s", errParam))
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		h.respondError(w, http.StatusBadRequest, "missing code parameter")
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		h.respondError(w, http.StatusBadRequest, "missing state parameter")
		return
	}

	// Validate state and extract email
	email, err := h.stateValidator(state)
	if err != nil {
		h.logger.Warn("invalid oauth state", "error", err)
		h.respondError(w, http.StatusBadRequest, "invalid state parameter")
		return
	}

	// Exchange code for token
	token, err := h.oauthConfig.Exchange(r.Context(), code)
	if err != nil {
		h.logger.Error("failed to exchange code for token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "failed to exchange authorization code")
		return
	}

	// Serialize token to JSON
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		h.logger.Error("failed to marshal token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Encrypt token
	encrypted, err := store.EncryptToken(tokenJSON, h.encryptionKey)
	if err != nil {
		h.logger.Error("failed to encrypt token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Store or update account
	existing, err := h.store.GetAccountByEmail(email)
	if err == nil {
		// Update existing account
		if err := h.store.UpdateToken(existing.ID, encrypted); err != nil {
			h.logger.Error("failed to update token", "email", email, "error", err)
			h.respondError(w, http.StatusInternalServerError, "failed to store token")
			return
		}
		h.logger.Info("updated oauth token", "email", email)
	} else if errors.Is(err, store.ErrAccountNotFound) {
		// Insert new account
		if _, err := h.store.InsertAccount(email, encrypted); err != nil {
			h.logger.Error("failed to insert account", "email", email, "error", err)
			h.respondError(w, http.StatusInternalServerError, "failed to store token")
			return
		}
		h.logger.Info("stored new oauth token", "email", email)
	} else {
		// Unexpected database error
		h.logger.Error("failed to lookup account", "email", email, "error", err)
		h.respondError(w, http.StatusInternalServerError, "failed to store token")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"email":  email,
	})
}

func (h *OAuthHandler) respondError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
