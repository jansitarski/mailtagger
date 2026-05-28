package setup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockStore implements AccountChecker for testing.
type mockStore struct {
	hasAccounts bool
	err         error
}

func (m *mockStore) HasAccounts() (bool, error) {
	return m.hasAccounts, m.err
}

func TestHandler_NoAccounts_ServesWizard(t *testing.T) {
	store := &mockStore{hasAccounts: false}
	handler := NewHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type text/html, got %s", contentType)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty body")
	}
}

func TestHandler_HasAccounts_Returns503(t *testing.T) {
	store := &mockStore{hasAccounts: true}
	handler := NewHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] == "" {
		t.Error("expected error message in response")
	}
}

func TestHandler_StoreError_Returns500(t *testing.T) {
	store := &mockStore{err: errTestStore}
	handler := NewHandler(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

var errTestStore = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test store error" }

func TestHandler_IsSetupMode(t *testing.T) {
	tests := []struct {
		name        string
		hasAccounts bool
		wantSetup   bool
	}{
		{"no accounts = setup mode", false, true},
		{"has accounts = not setup mode", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockStore{hasAccounts: tt.hasAccounts}
			handler := NewHandler(store, nil)

			isSetup, err := handler.IsSetupMode()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if isSetup != tt.wantSetup {
				t.Errorf("IsSetupMode() = %v, want %v", isSetup, tt.wantSetup)
			}
		})
	}
}
