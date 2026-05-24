package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthChecker_NoWatchers(t *testing.T) {
	hc := NewHealthChecker(5 * time.Minute)

	if !hc.Healthy() {
		t.Fatal("expected healthy with no watchers")
	}

	rec := httptest.NewRecorder()
	hc.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %q", resp.Status)
	}
}

func TestHealthChecker_HealthyWatcher(t *testing.T) {
	hc := NewHealthChecker(5 * time.Minute)
	hc.Tick("primary")

	if !hc.Healthy() {
		t.Fatal("expected healthy after recent tick")
	}

	rec := httptest.NewRecorder()
	hc.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHealthChecker_UnhealthyWatcher(t *testing.T) {
	hc := NewHealthChecker(1 * time.Millisecond)
	hc.Tick("primary")

	// Wait for 3x poll interval to expire
	time.Sleep(5 * time.Millisecond)

	if hc.Healthy() {
		t.Fatal("expected unhealthy after timeout")
	}

	rec := httptest.NewRecorder()
	hc.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "unhealthy" {
		t.Fatalf("expected status unhealthy, got %q", resp.Status)
	}
}

func TestHealthChecker_MultipleWatchers(t *testing.T) {
	hc := NewHealthChecker(1 * time.Millisecond)
	hc.Tick("primary")
	hc.Tick("work")

	// Let one expire
	time.Sleep(5 * time.Millisecond)
	hc.Tick("primary") // refresh only primary

	if hc.Healthy() {
		t.Fatal("expected unhealthy when one watcher expired")
	}
}
