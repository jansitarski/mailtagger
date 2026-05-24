package gmail

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
)

func TestDefaultRateLimiterConfig(t *testing.T) {
	config := DefaultRateLimiterConfig()

	if config.RequestsPerSecond != 10.0 {
		t.Errorf("expected RequestsPerSecond=10.0, got %f", config.RequestsPerSecond)
	}
	if config.Burst != 20 {
		t.Errorf("expected Burst=20, got %d", config.Burst)
	}
	if config.MaxRetries != 5 {
		t.Errorf("expected MaxRetries=5, got %d", config.MaxRetries)
	}
	if config.BaseDelay != 1*time.Second {
		t.Errorf("expected BaseDelay=1s, got %v", config.BaseDelay)
	}
	if config.MaxDelay != 60*time.Second {
		t.Errorf("expected MaxDelay=60s, got %v", config.MaxDelay)
	}
	if config.BackoffFactor != 2.0 {
		t.Errorf("expected BackoffFactor=2.0, got %f", config.BackoffFactor)
	}
}

func TestNewRateLimiter(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerSecond: 5.0,
		Burst:             10,
		MaxRetries:        3,
		BaseDelay:         500 * time.Millisecond,
		MaxDelay:          30 * time.Second,
		BackoffFactor:     2.0,
	}

	rl := NewRateLimiter(config)

	if rl == nil {
		t.Fatal("expected non-nil rate limiter")
	}
	if rl.limiter == nil {
		t.Error("expected non-nil limiter")
	}
	if rl.maxRetries != 3 {
		t.Errorf("expected maxRetries=3, got %d", rl.maxRetries)
	}
}

func TestRateLimiter_Wait(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerSecond: 1000.0, // High rate to avoid delays in test
		Burst:             1,
		MaxRetries:        1,
		BaseDelay:         1 * time.Millisecond,
		MaxDelay:          1 * time.Second,
		BackoffFactor:     2.0,
	}

	rl := NewRateLimiter(config)
	ctx := context.Background()

	// Should not block with high rate
	err := rl.Wait(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRateLimiter_Wait_ContextCancelled(t *testing.T) {
	config := DefaultRateLimiterConfig()
	rl := NewRateLimiter(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := rl.Wait(ctx)
	if err == nil {
		t.Error("expected error when context is cancelled")
	}
}

func TestRateLimiter_Do_Success(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerSecond: 1000.0,
		Burst:             10,
		MaxRetries:        3,
		BaseDelay:         1 * time.Millisecond,
		MaxDelay:          1 * time.Second,
		BackoffFactor:     2.0,
	}

	rl := NewRateLimiter(config)
	ctx := context.Background()

	callCount := 0
	err := rl.Do(ctx, func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestRateLimiter_Do_NonRetryableError(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerSecond: 1000.0,
		Burst:             10,
		MaxRetries:        3,
		BaseDelay:         1 * time.Millisecond,
		MaxDelay:          1 * time.Second,
		BackoffFactor:     2.0,
	}

	rl := NewRateLimiter(config)
	ctx := context.Background()

	testErr := errors.New("non-retryable error")
	callCount := 0

	err := rl.Do(ctx, func() error {
		callCount++
		return testErr
	})

	if err != testErr {
		t.Errorf("expected %v, got %v", testErr, err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call (no retry), got %d", callCount)
	}
}

func TestRateLimiter_Do_RetryOn429(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerSecond: 1000.0,
		Burst:             10,
		MaxRetries:        3,
		BaseDelay:         1 * time.Millisecond,
		MaxDelay:          1 * time.Second,
		BackoffFactor:     2.0,
	}

	rl := NewRateLimiter(config)
	ctx := context.Background()

	callCount := 0
	err429 := &googleapi.Error{
		Code:    http.StatusTooManyRequests,
		Message: "rate limit exceeded",
	}

	err := rl.Do(ctx, func() error {
		callCount++
		if callCount < 3 {
			return err429
		}
		return nil // Success on 3rd attempt
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls (2 retries), got %d", callCount)
	}
}

func TestRateLimiter_Do_RetryOn5xx(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerSecond: 1000.0,
		Burst:             10,
		MaxRetries:        3,
		BaseDelay:         1 * time.Millisecond,
		MaxDelay:          1 * time.Second,
		BackoffFactor:     2.0,
	}

	rl := NewRateLimiter(config)
	ctx := context.Background()

	callCount := 0
	err500 := &googleapi.Error{
		Code:    http.StatusInternalServerError,
		Message: "internal server error",
	}

	err := rl.Do(ctx, func() error {
		callCount++
		if callCount < 2 {
			return err500
		}
		return nil // Success on 2nd attempt
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (1 retry), got %d", callCount)
	}
}

func TestRateLimiter_Do_MaxRetriesExceeded(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerSecond: 1000.0,
		Burst:             10,
		MaxRetries:        2,
		BaseDelay:         1 * time.Millisecond,
		MaxDelay:          1 * time.Second,
		BackoffFactor:     2.0,
	}

	rl := NewRateLimiter(config)
	ctx := context.Background()

	callCount := 0
	err429 := &googleapi.Error{
		Code:    http.StatusTooManyRequests,
		Message: "rate limit exceeded",
	}

	err := rl.Do(ctx, func() error {
		callCount++
		return err429 // Always fail
	})

	if err == nil {
		t.Error("expected error when max retries exceeded")
	}
	// MaxRetries=2 means 3 total attempts (1 initial + 2 retries)
	if callCount != 3 {
		t.Errorf("expected 3 calls (1 initial + 2 retries), got %d", callCount)
	}
}

func TestRateLimiter_isRetryable(t *testing.T) {
	rl := NewRateLimiter(DefaultRateLimiterConfig())

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "non-googleapi error",
			err:      errors.New("generic error"),
			expected: false,
		},
		{
			name: "429 error",
			err: &googleapi.Error{
				Code: http.StatusTooManyRequests,
			},
			expected: true,
		},
		{
			name: "500 error",
			err: &googleapi.Error{
				Code: http.StatusInternalServerError,
			},
			expected: true,
		},
		{
			name: "503 error",
			err: &googleapi.Error{
				Code: http.StatusServiceUnavailable,
			},
			expected: true,
		},
		{
			name: "400 error",
			err: &googleapi.Error{
				Code: http.StatusBadRequest,
			},
			expected: false,
		},
		{
			name: "404 error",
			err: &googleapi.Error{
				Code: http.StatusNotFound,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rl.isRetryable(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestRateLimiter_calculateBackoff(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerSecond: 10.0,
		Burst:             20,
		MaxRetries:        5,
		BaseDelay:         1 * time.Second,
		MaxDelay:          10 * time.Second,
		BackoffFactor:     2.0,
	})

	tests := []struct {
		name     string
		attempt  int
		expected time.Duration
	}{
		{
			name:     "attempt 0",
			attempt:  0,
			expected: 1 * time.Second, // 1 * 2^0 = 1
		},
		{
			name:     "attempt 1",
			attempt:  1,
			expected: 2 * time.Second, // 1 * 2^1 = 2
		},
		{
			name:     "attempt 2",
			attempt:  2,
			expected: 4 * time.Second, // 1 * 2^2 = 4
		},
		{
			name:     "attempt 3",
			attempt:  3,
			expected: 8 * time.Second, // 1 * 2^3 = 8
		},
		{
			name:     "attempt 4 (capped)",
			attempt:  4,
			expected: 10 * time.Second, // 1 * 2^4 = 16, capped at 10
		},
		{
			name:     "attempt 5 (capped)",
			attempt:  5,
			expected: 10 * time.Second, // 1 * 2^5 = 32, capped at 10
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rl.calculateBackoff(tt.attempt, nil)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestRateLimiter_SetRate(t *testing.T) {
	rl := NewRateLimiter(DefaultRateLimiterConfig())

	// Set new rate
	rl.SetRate(20.0, 40)

	// Assert the limiter state directly instead of making time-based Wait calls
	if rl.limiter.Limit() != 20.0 {
		t.Errorf("expected limit=20.0, got %f", rl.limiter.Limit())
	}
	if rl.limiter.Burst() != 40 {
		t.Errorf("expected burst=40, got %d", rl.limiter.Burst())
	}
}

func TestRateLimiter_isRetryable_WrappedError(t *testing.T) {
	rl := NewRateLimiter(DefaultRateLimiterConfig())

	// Wrap a 429 error with fmt.Errorf %w
	apiErr := &googleapi.Error{Code: http.StatusTooManyRequests}
	wrapped := fmt.Errorf("context: %w", apiErr)

	if !rl.isRetryable(wrapped) {
		t.Error("expected wrapped 429 error to be retryable")
	}

	// Wrap a 500 error with fmt.Errorf %w
	serverErr := &googleapi.Error{Code: http.StatusInternalServerError}
	wrappedServer := fmt.Errorf("something failed: %w", serverErr)

	if !rl.isRetryable(wrappedServer) {
		t.Error("expected wrapped 500 error to be retryable")
	}

	// Wrap a 400 error - should NOT be retryable
	badReq := &googleapi.Error{Code: http.StatusBadRequest}
	wrappedBadReq := fmt.Errorf("bad: %w", badReq)

	if rl.isRetryable(wrappedBadReq) {
		t.Error("expected wrapped 400 error to NOT be retryable")
	}
}

func TestRateLimiter_Do_RetryWrappedError(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerSecond: 1000.0,
		Burst:             10,
		MaxRetries:        3,
		BaseDelay:         1 * time.Millisecond,
		MaxDelay:          1 * time.Second,
		BackoffFactor:     2.0,
	}

	rl := NewRateLimiter(config)
	ctx := context.Background()

	callCount := 0
	// Even though the raw error is returned (not wrapped), verify the flow works
	err429 := &googleapi.Error{
		Code:    http.StatusTooManyRequests,
		Message: "rate limit exceeded",
	}

	err := rl.Do(ctx, func() error {
		callCount++
		if callCount < 2 {
			return err429
		}
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestRateLimiter_calculateBackoff_RetryAfterSeconds(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerSecond: 10.0,
		Burst:             20,
		MaxRetries:        5,
		BaseDelay:         1 * time.Second,
		MaxDelay:          60 * time.Second,
		BackoffFactor:     2.0,
	})

	// Create error with Retry-After header (delta-seconds)
	header := make(http.Header)
	header.Set("Retry-After", "30")
	apiErr := &googleapi.Error{
		Code:   http.StatusTooManyRequests,
		Header: header,
	}

	delay := rl.calculateBackoff(0, apiErr)
	if delay != 30*time.Second {
		t.Errorf("expected 30s from Retry-After, got %v", delay)
	}
}

func TestRateLimiter_calculateBackoff_RetryAfterCapped(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerSecond: 10.0,
		Burst:             20,
		MaxRetries:        5,
		BaseDelay:         1 * time.Second,
		MaxDelay:          10 * time.Second, // Low max
		BackoffFactor:     2.0,
	})

	// Retry-After value exceeds maxDelay
	header := make(http.Header)
	header.Set("Retry-After", "120")
	apiErr := &googleapi.Error{
		Code:   http.StatusTooManyRequests,
		Header: header,
	}

	delay := rl.calculateBackoff(0, apiErr)
	if delay != 10*time.Second {
		t.Errorf("expected Retry-After to be capped at maxDelay=10s, got %v", delay)
	}
}

func TestRateLimiter_calculateBackoff_RetryAfterHTTPDate(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerSecond: 10.0,
		Burst:             20,
		MaxRetries:        5,
		BaseDelay:         1 * time.Second,
		MaxDelay:          60 * time.Second,
		BackoffFactor:     2.0,
	})

	// Create error with Retry-After header (HTTP-date format)
	futureTime := time.Now().Add(5 * time.Second)
	header := make(http.Header)
	header.Set("Retry-After", futureTime.UTC().Format(http.TimeFormat))
	apiErr := &googleapi.Error{
		Code:   http.StatusTooManyRequests,
		Header: header,
	}

	delay := rl.calculateBackoff(0, apiErr)
	// Should be approximately 5 seconds (within tolerance for test execution time)
	if delay < 4*time.Second || delay > 6*time.Second {
		t.Errorf("expected ~5s from Retry-After HTTP-date, got %v", delay)
	}
}
