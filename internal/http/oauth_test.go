package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"

	"github.com/jansitarski/mailtagger/internal/store"
)

// mockTokenStore is a test double for TokenStore.
type mockTokenStore struct {
	accounts map[string]*store.Account
	nextID   int64
}

func newMockTokenStore() *mockTokenStore {
	return &mockTokenStore{accounts: make(map[string]*store.Account), nextID: 1}
}

func (m *mockTokenStore) GetAccountByEmail(email string) (*store.Account, error) {
	acc, ok := m.accounts[email]
	if !ok {
		return nil, fmt.Errorf("account not found")
	}
	return acc, nil
}

func (m *mockTokenStore) InsertAccount(email string, encryptedToken []byte) (*store.Account, error) {
	acc := &store.Account{ID: m.nextID, Email: email, EncryptedToken: encryptedToken}
	m.nextID++
	m.accounts[email] = acc
	return acc, nil
}

func (m *mockTokenStore) UpdateToken(accountID int64, encryptedToken []byte) error {
	for _, acc := range m.accounts {
		if acc.ID == accountID {
			acc.EncryptedToken = encryptedToken
			return nil
		}
	}
	return fmt.Errorf("account not found")
}

func TestOAuthHandler_MissingCode(t *testing.T) {
	h := NewOAuthHandler(OAuthHandlerConfig{
		OAuthConfig:    &oauth2.Config{},
		EncryptionKey:  make([]byte, 32),
		Store:          newMockTokenStore(),
		StateValidator: func(s string) (string, error) { return "test@gmail.com", nil },
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state=valid", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOAuthHandler_MissingState(t *testing.T) {
	h := NewOAuthHandler(OAuthHandlerConfig{
		OAuthConfig:    &oauth2.Config{},
		EncryptionKey:  make([]byte, 32),
		Store:          newMockTokenStore(),
		StateValidator: func(s string) (string, error) { return "", nil },
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=abc", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOAuthHandler_InvalidState(t *testing.T) {
	h := NewOAuthHandler(OAuthHandlerConfig{
		OAuthConfig:   &oauth2.Config{},
		EncryptionKey: make([]byte, 32),
		Store:         newMockTokenStore(),
		StateValidator: func(s string) (string, error) {
			return "", fmt.Errorf("invalid state")
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=abc&state=bad", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOAuthHandler_ProviderError(t *testing.T) {
	h := NewOAuthHandler(OAuthHandlerConfig{
		OAuthConfig:    &oauth2.Config{},
		EncryptionKey:  make([]byte, 32),
		Store:          newMockTokenStore(),
		StateValidator: func(s string) (string, error) { return "test@gmail.com", nil },
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?error=access_denied&error_description=user+denied", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] == "" {
		t.Fatal("expected error in response body")
	}
}

func TestOAuthHandler_ExchangeFailure(t *testing.T) {
	// Use a fake token endpoint that returns an error
	fakeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer fakeSrv.Close()

	cfg := &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: fakeSrv.URL + "/token",
		},
	}

	h := NewOAuthHandler(OAuthHandlerConfig{
		OAuthConfig:    cfg,
		EncryptionKey:  make([]byte, 32),
		Store:          newMockTokenStore(),
		StateValidator: func(s string) (string, error) { return "test@gmail.com", nil },
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=badcode&state=valid", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestOAuthHandler_Success_NewAccount(t *testing.T) {
	// Fake OAuth token endpoint
	fakeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"at","token_type":"Bearer","refresh_token":"rt","expiry":"2030-01-01T00:00:00Z"}`))
	}))
	defer fakeSrv.Close()

	cfg := &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: fakeSrv.URL + "/token",
		},
	}

	ms := newMockTokenStore()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	h := NewOAuthHandler(OAuthHandlerConfig{
		OAuthConfig:    cfg,
		EncryptionKey:  key,
		Store:          ms,
		StateValidator: func(s string) (string, error) { return "user@gmail.com", nil },
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=goodcode&state=valid", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Verify account was stored
	acc, err := ms.GetAccountByEmail("user@gmail.com")
	if err != nil {
		t.Fatalf("account not stored: %v", err)
	}
	if len(acc.EncryptedToken) == 0 {
		t.Fatal("expected encrypted token to be stored")
	}

	// Verify we can decrypt it
	decrypted, err := store.DecryptToken(acc.EncryptedToken, key)
	if err != nil {
		t.Fatalf("failed to decrypt stored token: %v", err)
	}

	var tok oauth2.Token
	if err := json.Unmarshal(decrypted, &tok); err != nil {
		t.Fatalf("failed to unmarshal decrypted token: %v", err)
	}
	if tok.AccessToken != "at" {
		t.Errorf("expected access_token 'at', got %q", tok.AccessToken)
	}
}

func TestOAuthHandler_Success_ExistingAccount(t *testing.T) {
	fakeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"new-at","token_type":"Bearer","refresh_token":"new-rt"}`))
	}))
	defer fakeSrv.Close()

	cfg := &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: fakeSrv.URL + "/token",
		},
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	ms := newMockTokenStore()
	ms.InsertAccount("user@gmail.com", []byte("old-encrypted"))

	h := NewOAuthHandler(OAuthHandlerConfig{
		OAuthConfig:    cfg,
		EncryptionKey:  key,
		Store:          ms,
		StateValidator: func(s string) (string, error) { return "user@gmail.com", nil },
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=goodcode&state=valid", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Verify token was updated (not the old value)
	acc, _ := ms.GetAccountByEmail("user@gmail.com")
	if string(acc.EncryptedToken) == "old-encrypted" {
		t.Fatal("token was not updated")
	}
}
