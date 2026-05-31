package gmail

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// stubTokenStore returns a fixed account with placeholder ciphertext.
type stubTokenStore struct{}

func (stubTokenStore) GetAccountByEmail(string) (int64, []byte, error) {
	return 1, []byte("ciphertext"), nil
}
func (stubTokenStore) UpdateToken(int64, []byte) error { return nil }

// stubCrypto fails decryption with a configurable error.
type stubCrypto struct{ decryptErr error }

func (stubCrypto) EncryptToken(p, k []byte) ([]byte, error) { return p, nil }
func (c stubCrypto) DecryptToken(ct, k []byte) ([]byte, error) {
	return nil, c.decryptErr
}

func TestStoreTokenSource_KeyMismatchMessage(t *testing.T) {
	src := NewStoreTokenSource(
		context.Background(),
		"user@example.com",
		&oauth2.Config{},
		stubTokenStore{},
		stubCrypto{decryptErr: errors.New("failed to decrypt: cipher: message authentication failed")},
		make([]byte, 32),
	)

	_, err := src.Token()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"user@example.com", "encryption key does not match", "MAILTAGGER_ENCRYPTION_KEY", "mailtagger auth"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q; got: %s", want, msg)
		}
	}
}

func TestStoreTokenSource_OtherDecryptError(t *testing.T) {
	src := NewStoreTokenSource(
		context.Background(),
		"user@example.com",
		&oauth2.Config{},
		stubTokenStore{},
		stubCrypto{decryptErr: errors.New("ciphertext too short")},
		make([]byte, 32),
	)

	_, err := src.Token()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "user@example.com") || !strings.Contains(msg, "ciphertext too short") {
		t.Errorf("unexpected error message: %s", msg)
	}
	// The key-mismatch hint should NOT appear for non-auth failures.
	if strings.Contains(msg, "encryption key does not match") {
		t.Errorf("did not expect key-mismatch hint for a non-auth decrypt error; got: %s", msg)
	}
}
