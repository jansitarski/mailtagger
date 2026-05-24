package http

import (
	"testing"
	"time"
)

func TestOAuthStateSigner_RoundTrip(t *testing.T) {
	key := []byte("test-hmac-key-32-bytes-long!!!!!")
	signer := NewOAuthStateSigner(key, 10*time.Minute)

	state, err := signer.Sign("user@gmail.com")
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	email, err := signer.Validate(state)
	if err != nil {
		t.Fatalf("failed to validate: %v", err)
	}

	if email != "user@gmail.com" {
		t.Errorf("expected user@gmail.com, got %q", email)
	}
}

func TestOAuthStateSigner_InvalidSignature(t *testing.T) {
	key := []byte("test-hmac-key-32-bytes-long!!!!!")
	signer := NewOAuthStateSigner(key, 10*time.Minute)

	state, err := signer.Sign("user@gmail.com")
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	// Tamper with the state
	tampered := state[:len(state)-2] + "xx"

	_, err = signer.Validate(tampered)
	if err == nil {
		t.Fatal("expected validation to fail with tampered state")
	}
}

func TestOAuthStateSigner_WrongKey(t *testing.T) {
	key1 := []byte("key-one-32-bytes-long-here!!!!!!!")
	key2 := []byte("key-two-32-bytes-long-here!!!!!!!")

	signer1 := NewOAuthStateSigner(key1, 10*time.Minute)
	signer2 := NewOAuthStateSigner(key2, 10*time.Minute)

	state, err := signer1.Sign("user@gmail.com")
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	_, err = signer2.Validate(state)
	if err == nil {
		t.Fatal("expected validation to fail with wrong key")
	}
}

func TestOAuthStateSigner_Expired(t *testing.T) {
	key := []byte("test-hmac-key-32-bytes-long!!!!!")
	// Use a negative TTL to guarantee immediate expiry
	signer := NewOAuthStateSigner(key, -1*time.Second)

	state, err := signer.Sign("user@gmail.com")
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	_, err = signer.Validate(state)
	if err == nil {
		t.Fatal("expected validation to fail with expired state")
	}
}

func TestOAuthStateSigner_Malformed(t *testing.T) {
	key := []byte("test-hmac-key-32-bytes-long!!!!!")
	signer := NewOAuthStateSigner(key, 10*time.Minute)

	_, err := signer.Validate("no-dot-separator")
	if err == nil {
		t.Fatal("expected error for malformed state")
	}

	_, err = signer.Validate("")
	if err == nil {
		t.Fatal("expected error for empty state")
	}
}
