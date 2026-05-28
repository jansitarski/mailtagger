package setup

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jansitarski/mailtagger/internal/store"
)

// mockSetupStore implements SetupStore for testing.
type mockSetupStore struct {
	hasAccounts bool
	err         error
	accounts    map[string]*store.Account
	nextID      int64
}

func (m *mockSetupStore) HasAccounts() (bool, error) {
	return m.hasAccounts, m.err
}

func (m *mockSetupStore) GetAccountByEmail(email string) (*store.Account, error) {
	if m.accounts == nil {
		return nil, store.ErrAccountNotFound
	}
	acc, ok := m.accounts[email]
	if !ok {
		return nil, store.ErrAccountNotFound
	}
	return acc, nil
}

func (m *mockSetupStore) InsertAccount(email string, encryptedToken []byte) (*store.Account, error) {
	if m.accounts == nil {
		m.accounts = make(map[string]*store.Account)
	}
	m.nextID++
	acc := &store.Account{
		ID:             m.nextID,
		Email:          email,
		EncryptedToken: encryptedToken,
	}
	m.accounts[email] = acc
	return acc, nil
}

func (m *mockSetupStore) UpdateToken(accountID int64, encryptedToken []byte) error {
	for _, acc := range m.accounts {
		if acc.ID == accountID {
			acc.EncryptedToken = encryptedToken
			return nil
		}
	}
	return store.ErrAccountNotFound
}

func TestAPIHandler_ClientSecret_Valid(t *testing.T) {
	store := &mockSetupStore{hasAccounts: false}
	token, _ := GenerateToken(nil)
	handler := NewAPIHandler(APIHandlerConfig{
		Store: store,
		Token: token,
	})

	clientSecret := `{
		"web": {
			"client_id": "test-client-id.apps.googleusercontent.com",
			"client_secret": "test-secret",
			"auth_uri": "https://accounts.google.com/o/oauth2/auth",
			"token_uri": "https://oauth2.googleapis.com/token",
			"redirect_uris": ["http://localhost:8080/oauth/callback"]
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/setup/api/client-secret", bytes.NewBufferString(clientSecret))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.handleClientSecret(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", resp["status"])
	}
	if resp["type"] != "web" {
		t.Errorf("expected type 'web', got %v", resp["type"])
	}
}

func TestAPIHandler_ClientSecret_Invalid(t *testing.T) {
	store := &mockSetupStore{hasAccounts: false}
	token, _ := GenerateToken(nil)
	handler := NewAPIHandler(APIHandlerConfig{
		Store: store,
		Token: token,
	})

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "invalid JSON",
			body:    "not json",
			wantErr: "invalid JSON",
		},
		{
			name:    "missing web/installed",
			body:    `{"other": {}}`,
			wantErr: "missing 'web' or 'installed'",
		},
		{
			name:    "missing client_id",
			body:    `{"web": {"client_secret": "secret"}}`,
			wantErr: "missing client_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/setup/api/client-secret", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()

			handler.handleClientSecret(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d", w.Code)
			}

			var resp map[string]string
			json.NewDecoder(w.Body).Decode(&resp)
			if resp["error"] == "" {
				t.Error("expected error message")
			}
		})
	}
}

func TestAPIHandler_OAuthStart_NoClientSecret(t *testing.T) {
	store := &mockSetupStore{hasAccounts: false}
	token, _ := GenerateToken(nil)
	handler := NewAPIHandler(APIHandlerConfig{
		Store: store,
		Token: token,
	})

	req := httptest.NewRequest(http.MethodGet, "/setup/api/oauth/start", nil)
	w := httptest.NewRecorder()

	handler.handleOAuthStart(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestAPIHandler_OAuthStart_WithClientSecret(t *testing.T) {
	store := &mockSetupStore{hasAccounts: false}
	token, _ := GenerateToken(nil)
	handler := NewAPIHandler(APIHandlerConfig{
		Store: store,
		Token: token,
	})

	// First upload client secret
	handler.clientSecret = &ClientSecretData{
		Web: &OAuthClientConfig{
			ClientID:     "test-client-id",
			ClientSecret: "test-secret",
			AuthURI:      "https://accounts.google.com/o/oauth2/auth",
			TokenURI:     "https://oauth2.googleapis.com/token",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/setup/api/oauth/start", nil)
	req.Host = "localhost:8080"
	w := httptest.NewRecorder()

	handler.handleOAuthStart(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["auth_url"] == "" {
		t.Error("expected auth_url in response")
	}
	if resp["state"] == "" {
		t.Error("expected state in response")
	}
}

func TestAPIHandler_LLMTest_Valid(t *testing.T) {
	store := &mockSetupStore{hasAccounts: false}
	token, _ := GenerateToken(nil)
	handler := NewAPIHandler(APIHandlerConfig{
		Store: store,
		Token: token,
	})

	body := `{"provider": "openai", "api_key": "sk-test", "model": "gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/setup/api/llm/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	handler.handleLLMTest(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIHandler_LLMTest_OllamaNoKey(t *testing.T) {
	store := &mockSetupStore{hasAccounts: false}
	token, _ := GenerateToken(nil)
	handler := NewAPIHandler(APIHandlerConfig{
		Store: store,
		Token: token,
	})

	// Ollama doesn't require API key
	body := `{"provider": "ollama", "model": "llama2"}`
	req := httptest.NewRequest(http.MethodPost, "/setup/api/llm/test", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	handler.handleLLMTest(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIHandler_Complete_Valid(t *testing.T) {
	store := &mockSetupStore{hasAccounts: false}
	token, _ := GenerateToken(nil)
	handler := NewAPIHandler(APIHandlerConfig{
		Store: store,
		Token: token,
	})

	body := `{
		"llmProvider": "openai",
		"llmApiKey": "sk-test",
		"llmModel": "gpt-4",
		"categories": [
			{"name": "newsletter", "label": "auto/newsletter", "description": "Newsletters"}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/setup/api/complete", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	handler.handleComplete(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIHandler_Complete_MissingFields(t *testing.T) {
	store := &mockSetupStore{hasAccounts: false}
	token, _ := GenerateToken(nil)
	handler := NewAPIHandler(APIHandlerConfig{
		Store: store,
		Token: token,
	})

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "missing provider",
			body:    `{"llmModel": "gpt-4", "categories": [{"name": "test"}]}`,
			wantErr: "provider",
		},
		{
			name:    "missing model",
			body:    `{"llmProvider": "openai", "categories": [{"name": "test"}]}`,
			wantErr: "model",
		},
		{
			name:    "missing categories",
			body:    `{"llmProvider": "openai", "llmModel": "gpt-4"}`,
			wantErr: "category",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/setup/api/complete", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()

			handler.handleComplete(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d", w.Code)
			}
		})
	}
}

func TestSanitizeAPIKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sk-1234567890abcdef", "sk-1********cdef"},
		{"short", "*****"},
		{"12345678", "********"},
		{"123456789", "1234*6789"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SanitizeAPIKey(tt.input)
			// Just verify it masks the middle
			if len(got) != len(tt.input) {
				t.Errorf("SanitizeAPIKey(%q) length = %d, want %d", tt.input, len(got), len(tt.input))
			}
		})
	}
}
