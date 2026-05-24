package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// OAuthState is the payload embedded in the state parameter.
type OAuthState struct {
	Email     string `json:"email"`
	ExpiresAt int64  `json:"exp"`
}

// OAuthStateSigner creates and validates HMAC-signed OAuth state parameters.
type OAuthStateSigner struct {
	key []byte
	ttl time.Duration
}

// NewOAuthStateSigner creates a signer with the given HMAC key and state TTL.
func NewOAuthStateSigner(key []byte, ttl time.Duration) *OAuthStateSigner {
	return &OAuthStateSigner{key: key, ttl: ttl}
}

// Sign creates a signed state string for the given email.
func (s *OAuthStateSigner) Sign(email string) (string, error) {
	state := OAuthState{
		Email:     email,
		ExpiresAt: time.Now().Add(s.ttl).Unix(),
	}

	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("failed to marshal state: %w", err)
	}

	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	sig := mac.Sum(nil)

	// Format: base64(payload).base64(signature)
	encoded := base64.RawURLEncoding.EncodeToString(payload) +
		"." +
		base64.RawURLEncoding.EncodeToString(sig)

	return encoded, nil
}

// Validate verifies the HMAC signature and expiry of a state string.
// Returns the email embedded in the state.
func (s *OAuthStateSigner) Validate(stateStr string) (string, error) {
	// Split into payload and signature
	dot := -1
	for i := len(stateStr) - 1; i >= 0; i-- {
		if stateStr[i] == '.' {
			dot = i
			break
		}
	}
	if dot < 0 {
		return "", fmt.Errorf("malformed state: missing separator")
	}

	payloadB64 := stateStr[:dot]
	sigB64 := stateStr[dot+1:]

	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return "", fmt.Errorf("failed to decode state payload: %w", err)
	}

	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return "", fmt.Errorf("failed to decode state signature: %w", err)
	}

	// Verify HMAC
	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	expected := mac.Sum(nil)

	if !hmac.Equal(sig, expected) {
		return "", fmt.Errorf("invalid state signature")
	}

	// Parse payload
	var state OAuthState
	if err := json.Unmarshal(payload, &state); err != nil {
		return "", fmt.Errorf("failed to parse state payload: %w", err)
	}

	// Check expiry
	if time.Now().Unix() > state.ExpiresAt {
		return "", fmt.Errorf("state expired")
	}

	return state.Email, nil
}
