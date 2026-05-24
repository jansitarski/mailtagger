package auth

import (
	"net/url"
	"strings"
	"testing"
)

func TestOAuthConfig(t *testing.T) {
	cs := &ClientSecret{
		ClientID:     "test-client.apps.googleusercontent.com",
		ClientSecret: "GOCSPX-test",
		AuthURI:      "https://accounts.google.com/o/oauth2/auth",
		TokenURI:     "https://oauth2.googleapis.com/token",
		RedirectURIs: []string{"http://localhost"},
	}

	cfg := cs.OAuthConfig("http://localhost:12345")

	if cfg.ClientID != cs.ClientID {
		t.Errorf("ClientID = %q, want %q", cfg.ClientID, cs.ClientID)
	}
	if cfg.ClientSecret != cs.ClientSecret {
		t.Errorf("ClientSecret = %q, want %q", cfg.ClientSecret, cs.ClientSecret)
	}
	if cfg.RedirectURL != "http://localhost:12345" {
		t.Errorf("RedirectURL = %q, want %q", cfg.RedirectURL, "http://localhost:12345")
	}
	if len(cfg.Scopes) != 2 {
		t.Errorf("Scopes len = %d, want 2", len(cfg.Scopes))
	}

	// Check scopes include gmail.modify and userinfo.email
	scopeMap := make(map[string]bool)
	for _, s := range cfg.Scopes {
		scopeMap[s] = true
	}
	if !scopeMap[ScopeGmailModify] {
		t.Errorf("Scopes missing gmail.modify")
	}
	if !scopeMap[ScopeUserInfoEmail] {
		t.Errorf("Scopes missing userinfo.email")
	}
}

func TestAuthCodeURL(t *testing.T) {
	cs := &ClientSecret{
		ClientID:     "test-client.apps.googleusercontent.com",
		ClientSecret: "GOCSPX-test",
		AuthURI:      "https://accounts.google.com/o/oauth2/auth",
		TokenURI:     "https://oauth2.googleapis.com/token",
		RedirectURIs: []string{"http://localhost"},
	}

	authURL := cs.AuthCodeURL("http://localhost:8888/callback", "test-state-123")

	// Parse the URL
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("failed to parse auth URL: %v", err)
	}

	// Check base URL
	if !strings.HasPrefix(authURL, "https://accounts.google.com/o/oauth2/auth") {
		t.Errorf("auth URL base = %q, want https://accounts.google.com/o/oauth2/auth prefix", u.String())
	}

	q := u.Query()

	// Check required parameters
	if q.Get("client_id") != cs.ClientID {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), cs.ClientID)
	}
	if q.Get("redirect_uri") != "http://localhost:8888/callback" {
		t.Errorf("redirect_uri = %q, want %q", q.Get("redirect_uri"), "http://localhost:8888/callback")
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want %q", q.Get("response_type"), "code")
	}
	if q.Get("state") != "test-state-123" {
		t.Errorf("state = %q, want %q", q.Get("state"), "test-state-123")
	}

	// Check access_type=offline (for refresh token)
	if q.Get("access_type") != "offline" {
		t.Errorf("access_type = %q, want %q", q.Get("access_type"), "offline")
	}

	// Check prompt=consent (to always get refresh token)
	if q.Get("prompt") != "consent" {
		t.Errorf("prompt = %q, want %q", q.Get("prompt"), "consent")
	}

	// Check scopes
	scope := q.Get("scope")
	if !strings.Contains(scope, "gmail.modify") {
		t.Errorf("scope missing gmail.modify: %q", scope)
	}
	if !strings.Contains(scope, "userinfo.email") {
		t.Errorf("scope missing userinfo.email: %q", scope)
	}
}
