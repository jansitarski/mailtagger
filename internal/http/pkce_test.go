package http

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestGeneratePKCE(t *testing.T) {
	pkce, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("failed to generate PKCE: %v", err)
	}

	if pkce.Method != "S256" {
		t.Errorf("expected method S256, got %q", pkce.Method)
	}

	// Verifier should be 43 chars (32 bytes base64url without padding)
	if len(pkce.Verifier) != 43 {
		t.Errorf("expected verifier length 43, got %d", len(pkce.Verifier))
	}

	// Challenge should be the S256 of verifier
	expected := computeS256Challenge(pkce.Verifier)
	if pkce.Challenge != expected {
		t.Errorf("challenge mismatch: got %q, expected %q", pkce.Challenge, expected)
	}
}

func TestGeneratePKCE_Uniqueness(t *testing.T) {
	pkce1, _ := GeneratePKCE()
	pkce2, _ := GeneratePKCE()

	if pkce1.Verifier == pkce2.Verifier {
		t.Error("two generated PKCE params should have different verifiers")
	}
}

func TestComputeS256Challenge(t *testing.T) {
	// RFC 7636 example (adapted)
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])

	got := computeS256Challenge(verifier)
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestAuthCodeURLWithPKCE(t *testing.T) {
	cfg := &oauth2.Config{
		ClientID: "test-client",
		Endpoint: oauth2.Endpoint{
			AuthURL: "https://accounts.google.com/o/oauth2/auth",
		},
		RedirectURL: "http://localhost:8080/oauth/callback",
		Scopes:      []string{"email"},
	}

	pkce := &PKCEParams{
		Verifier:  "test-verifier",
		Challenge: "test-challenge",
		Method:    "S256",
	}

	url := AuthCodeURLWithPKCE(cfg, "test-state", pkce)

	if !strings.Contains(url, "code_challenge=test-challenge") {
		t.Errorf("URL missing code_challenge: %s", url)
	}
	if !strings.Contains(url, "code_challenge_method=S256") {
		t.Errorf("URL missing code_challenge_method: %s", url)
	}
	if !strings.Contains(url, "state=test-state") {
		t.Errorf("URL missing state: %s", url)
	}
}

func TestExchangeCodeWithPKCE(t *testing.T) {
	var receivedVerifier string

	fakeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		receivedVerifier = r.Form.Get("code_verifier")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"at","token_type":"Bearer"}`))
	}))
	defer fakeSrv.Close()

	cfg := &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: fakeSrv.URL + "/token",
		},
	}

	token, err := ExchangeCodeWithPKCE(cfg, "test-code", "my-verifier")
	if err != nil {
		t.Fatalf("exchange failed: %v", err)
	}

	if token.AccessToken != "at" {
		t.Errorf("expected access_token 'at', got %q", token.AccessToken)
	}

	if receivedVerifier != "my-verifier" {
		t.Errorf("expected verifier 'my-verifier', got %q", receivedVerifier)
	}
}
