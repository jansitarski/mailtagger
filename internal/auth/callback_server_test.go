package auth

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestCallbackServer_Port(t *testing.T) {
	srv, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error = %v", err)
	}
	defer srv.Close()

	port := srv.Port()
	if port <= 0 || port > 65535 {
		t.Errorf("Port() = %d, want valid port number", port)
	}
}

func TestCallbackServer_RedirectURL(t *testing.T) {
	srv, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error = %v", err)
	}
	defer srv.Close()

	url := srv.RedirectURL()
	expected := fmt.Sprintf("http://127.0.0.1:%d/", srv.Port())
	if url != expected {
		t.Errorf("RedirectURL() = %q, want %q", url, expected)
	}
}

func TestCallbackServer_SuccessfulAuth(t *testing.T) {
	srv, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error = %v", err)
	}

	ctx := context.Background()
	resultCh := make(chan CallbackResult)
	errCh := make(chan error)

	// Start waiting for callback in goroutine
	go func() {
		result, err := srv.WaitForCallback(ctx, 5*time.Second)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Simulate browser redirect callback
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/?code=test-code-123&state=test-state", srv.Port())
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("response status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Check result
	select {
	case result := <-resultCh:
		if result.Code != "test-code-123" {
			t.Errorf("Code = %q, want %q", result.Code, "test-code-123")
		}
		if result.State != "test-state" {
			t.Errorf("State = %q, want %q", result.State, "test-state")
		}
		if result.Error != "" {
			t.Errorf("Error = %q, want empty", result.Error)
		}
	case err := <-errCh:
		t.Fatalf("WaitForCallback() error = %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback result")
	}
}

func TestCallbackServer_OAuthError(t *testing.T) {
	srv, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error = %v", err)
	}

	ctx := context.Background()
	resultCh := make(chan CallbackResult)
	errCh := make(chan error)

	go func() {
		result, err := srv.WaitForCallback(ctx, 5*time.Second)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	time.Sleep(50 * time.Millisecond)

	// Simulate OAuth error
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/?error=access_denied&state=test-state", srv.Port())
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("response status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	select {
	case result := <-resultCh:
		if result.Error != "access_denied" {
			t.Errorf("Error = %q, want %q", result.Error, "access_denied")
		}
	case err := <-errCh:
		t.Fatalf("WaitForCallback() error = %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback result")
	}
}

func TestCallbackServer_Timeout(t *testing.T) {
	srv, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error = %v", err)
	}

	ctx := context.Background()
	_, err = srv.WaitForCallback(ctx, 100*time.Millisecond)
	if err != ErrAuthTimeout {
		t.Errorf("WaitForCallback() error = %v, want %v", err, ErrAuthTimeout)
	}
}

func TestCallbackServer_ContextCanceled(t *testing.T) {
	srv, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error)
	go func() {
		_, err := srv.WaitForCallback(ctx, 5*time.Second)
		errCh <- err
	}()

	// Cancel context after short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != ErrAuthCanceled {
			t.Errorf("WaitForCallback() error = %v, want %v", err, ErrAuthCanceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error")
	}
}

func TestCallbackServer_DoubleClose(t *testing.T) {
	srv, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error = %v", err)
	}

	// Double close should not panic
	if err := srv.Close(); err != nil {
		t.Errorf("first Close() error = %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}
