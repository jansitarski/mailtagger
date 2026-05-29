package setup

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
)

const (
	// TokenCookieName is the name of the cookie storing the setup token.
	TokenCookieName = "mailtagger_setup_token"

	// TokenLength is the number of random bytes in the token.
	TokenLength = 32
)

// Token represents a one-time setup token.
type Token struct {
	value  string
	logger *slog.Logger
}

// GenerateToken creates a new random setup token.
func GenerateToken(logger *slog.Logger) (*Token, error) {
	if logger == nil {
		logger = slog.Default()
	}

	b := make([]byte, TokenLength)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}

	token := hex.EncodeToString(b)
	return &Token{
		value:  token,
		logger: logger,
	}, nil
}

// Value returns the token string.
func (t *Token) Value() string {
	return t.value
}

// LogToken prints the token to logs with clear instructions.
func (t *Token) LogToken(addr string) {
	t.logger.Info("=== SETUP TOKEN ===")
	t.logger.Info("Use this token to access the setup wizard")
	t.logger.Info("Token", "value", t.value)
	t.logger.Info(fmt.Sprintf("Visit: http://%s/setup?token=%s", addr, t.value))
	t.logger.Info("===================")
}

// Validate checks if the provided token matches.
func (t *Token) Validate(provided string) bool {
	return subtle.ConstantTimeCompare([]byte(t.value), []byte(provided)) == 1
}

// TokenMiddleware creates middleware that validates the setup token.
// It checks for the token in:
// 1. Cookie (TokenCookieName)
// 2. Query parameter (token)
// If found in query param but not cookie, it sets the cookie and redirects.
func (t *Token) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check cookie first
		cookie, err := r.Cookie(TokenCookieName)
		if err == nil && t.Validate(cookie.Value) {
			next.ServeHTTP(w, r)
			return
		}

		// Check query parameter
		queryToken := r.URL.Query().Get("token")
		if queryToken != "" && t.Validate(queryToken) {
		// Set cookie and redirect to remove token from URL
		http.SetCookie(w, &http.Cookie{
			Name:     TokenCookieName,
			Value:    t.value,
			Path:     "/setup",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode, // Lax allows cookie on OAuth redirects
			// Secure should be true in production with HTTPS
			// We'll set it based on the request scheme
			Secure: r.TLS != nil,
		})

			// Redirect to same path without query token
			q := r.URL.Query()
			q.Del("token")
			redirectURL := r.URL.Path
			if len(q) > 0 {
				redirectURL += "?" + q.Encode()
			}
			http.Redirect(w, r, redirectURL, http.StatusFound)
			return
		}

		// No valid token - return 401
		t.logger.Warn("setup access denied: invalid or missing token", "remote_addr", r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"setup token required","hint":"add ?token=YOUR_TOKEN to the URL"}`))
	})
}
