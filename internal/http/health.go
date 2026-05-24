package http

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// HealthChecker tracks watcher liveness and reports health status.
type HealthChecker struct {
	mu           sync.RWMutex
	lastTicks    map[string]time.Time
	pollInterval time.Duration
}

// NewHealthChecker creates a HealthChecker with the given poll interval.
// The endpoint reports unhealthy if any watcher hasn't ticked within 3x pollInterval.
func NewHealthChecker(pollInterval time.Duration) *HealthChecker {
	return &HealthChecker{
		lastTicks:    make(map[string]time.Time),
		pollInterval: pollInterval,
	}
}

// Tick records a successful tick for the named watcher.
func (h *HealthChecker) Tick(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastTicks[name] = time.Now()
}

// Healthy returns true if all registered watchers have ticked within 3x poll interval.
// Returns true if no watchers are registered (startup grace).
func (h *HealthChecker) Healthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.lastTicks) == 0 {
		return true
	}

	deadline := time.Now().Add(-3 * h.pollInterval)
	for _, last := range h.lastTicks {
		if last.Before(deadline) {
			return false
		}
	}
	return true
}

// healthResponse is the JSON response body for the health endpoint.
type healthResponse struct {
	Status   string            `json:"status"`
	Watchers map[string]string `json:"watchers,omitempty"`
}

// Handler returns an http.HandlerFunc for the /healthz endpoint.
func (h *HealthChecker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		healthy := h.Healthy()

		resp := healthResponse{
			Watchers: make(map[string]string),
		}

		h.mu.RLock()
		for name, last := range h.lastTicks {
			resp.Watchers[name] = last.Format(time.RFC3339)
		}
		h.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		if healthy {
			resp.Status = "ok"
			w.WriteHeader(http.StatusOK)
		} else {
			resp.Status = "unhealthy"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(resp)
	}
}
