package http

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"

	"golang.org/x/oauth2"
)

// PKCEParams holds the PKCE code verifier and challenge for an OAuth flow.
type PKCEParams struct {
	Verifier  string
	Challenge string
	Method    string // always "S256"
}

// GeneratePKCE generates a new PKCE code verifier and S256 challenge.
// The verifier is a 32-byte random value, base64url-encoded (43 chars).
func GeneratePKCE() (*PKCEParams, error) {
	verifierBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, verifierBytes); err != nil {
		return nil, err
	}

	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	challenge := computeS256Challenge(verifier)

	return &PKCEParams{
		Verifier:  verifier,
		Challenge: challenge,
		Method:    "S256",
	}, nil
}

// computeS256Challenge computes the S256 code challenge from a verifier.
// challenge = BASE64URL(SHA256(verifier))
func computeS256Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// AuthCodeURLWithPKCE returns an authorization URL with PKCE parameters.
func AuthCodeURLWithPKCE(cfg *oauth2.Config, state string, pkce *PKCEParams) string {
	return cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", pkce.Challenge),
		oauth2.SetAuthURLParam("code_challenge_method", pkce.Method),
	)
}

// ExchangeCodeWithPKCE exchanges an authorization code for a token using the PKCE verifier.
func ExchangeCodeWithPKCE(cfg *oauth2.Config, code string, verifier string) (*oauth2.Token, error) {
	return cfg.Exchange(
		oauth2.NoContext,
		code,
		oauth2.SetAuthURLParam("code_verifier", verifier),
	)
}
