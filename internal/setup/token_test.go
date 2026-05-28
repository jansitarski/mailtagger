package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	token, err := GenerateToken(nil)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Token should be 64 hex chars (32 bytes)
	if len(token.Value()) != 64 {
		t.Errorf("expected token length 64, got %d", len(token.Value()))
	}

	// Tokens should be unique
	token2, err := GenerateToken(nil)
	if err != nil {
		t.Fatalf("failed to generate second token: %v", err)
	}
	if token.Value() == token2.Value() {
		t.Error("expected unique tokens")
	}
}

func TestToken_Validate(t *testing.T) {
	token, _ := GenerateToken(nil)

	if !token.Validate(token.Value()) {
		t.Error("token should validate its own value")
	}

	if token.Validate("wrong-token") {
		t.Error("token should reject wrong value")
	}

	if token.Validate("") {
		t.Error("token should reject empty value")
	}
}

func TestTokenMiddleware_ValidCookie(t *testing.T) {
	token, _ := GenerateToken(nil)

	handler := token.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.AddCookie(&http.Cookie{
		Name:  TokenCookieName,
		Value: token.Value(),
	})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestTokenMiddleware_ValidQueryParam(t *testing.T) {
	token, _ := GenerateToken(nil)

	handler := token.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/setup?token="+token.Value(), nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should redirect and set cookie
	if w.Code != http.StatusFound {
		t.Errorf("expected status 302 (redirect), got %d", w.Code)
	}

	// Check cookie was set
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == TokenCookieName && c.Value == token.Value() {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cookie to be set")
	}

	// Check redirect location doesn't contain token
	location := w.Header().Get("Location")
	if strings.Contains(location, "token=") {
		t.Errorf("redirect URL should not contain token: %s", location)
	}
}

func TestTokenMiddleware_InvalidToken(t *testing.T) {
	token, _ := GenerateToken(nil)

	handler := token.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/setup?token=wrong-token", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestTokenMiddleware_NoToken(t *testing.T) {
	token, _ := GenerateToken(nil)

	handler := token.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestTokenMiddleware_InvalidCookie_ValidQuery(t *testing.T) {
	token, _ := GenerateToken(nil)

	handler := token.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/setup?token="+token.Value(), nil)
	req.AddCookie(&http.Cookie{
		Name:  TokenCookieName,
		Value: "invalid-cookie",
	})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should redirect (accept query param even with invalid cookie)
	if w.Code != http.StatusFound {
		t.Errorf("expected status 302, got %d", w.Code)
	}
}

func TestTokenMiddleware_PreservesOtherQueryParams(t *testing.T) {
	token, _ := GenerateToken(nil)

	handler := token.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/setup?token="+token.Value()+"&page=2", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	location := w.Header().Get("Location")
	if !strings.Contains(location, "page=2") {
		t.Errorf("redirect URL should preserve other params: %s", location)
	}
	if strings.Contains(location, "token=") {
		t.Errorf("redirect URL should not contain token: %s", location)
	}
}
