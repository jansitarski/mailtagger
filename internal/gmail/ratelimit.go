package gmail

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/api/googleapi"
)

// RateLimiter manages Gmail API rate limiting using token bucket algorithm
// and exponential backoff for 429 and 5xx errors.
type RateLimiter struct {
	limiter        *rate.Limiter
	maxRetries     int
	baseDelay      time.Duration
	maxDelay       time.Duration
	backoffFactor  float64
}

// RateLimiterConfig configures the rate limiter behavior.
type RateLimiterConfig struct {
	// RequestsPerSecond is the sustained rate limit (token bucket refill rate)
	RequestsPerSecond float64
	// Burst is the maximum burst size (token bucket capacity)
	Burst int
	// MaxRetries is the maximum number of retry attempts for 429/5xx errors
	MaxRetries int
	// BaseDelay is the initial delay for exponential backoff
	BaseDelay time.Duration
	// MaxDelay is the maximum delay between retries
	MaxDelay time.Duration
	// BackoffFactor is the multiplier for exponential backoff (typically 2.0)
	BackoffFactor float64
}

// DefaultRateLimiterConfig returns a sensible default configuration.
// Gmail API has a quota of 250 units per user per second for read operations.
// We set a conservative default to avoid hitting limits.
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		RequestsPerSecond: 10.0,      // Conservative rate
		Burst:             20,         // Allow short bursts
		MaxRetries:        5,          // Retry up to 5 times
		BaseDelay:         1 * time.Second,
		MaxDelay:          60 * time.Second,
		BackoffFactor:     2.0,        // Double delay each retry
	}
}

// NewRateLimiter creates a new rate limiter with the given configuration.
func NewRateLimiter(config RateLimiterConfig) *RateLimiter {
	return &RateLimiter{
		limiter:       rate.NewLimiter(rate.Limit(config.RequestsPerSecond), config.Burst),
		maxRetries:    config.MaxRetries,
		baseDelay:     config.BaseDelay,
		maxDelay:      config.MaxDelay,
		backoffFactor: config.BackoffFactor,
	}
}

// Wait blocks until a token is available or context is cancelled.
// This should be called before making any Gmail API request.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.limiter.Wait(ctx)
}

// Do executes a function with rate limiting and automatic retry on 429/5xx errors.
// The function f should return the result and error from the Gmail API call.
func (rl *RateLimiter) Do(ctx context.Context, f func() error) error {
	var lastErr error

	for attempt := 0; attempt <= rl.maxRetries; attempt++ {
		// Wait for rate limiter token
		if err := rl.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter wait failed: %w", err)
		}

		// Execute the function
		err := f()
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if this is a retryable error
		if !rl.isRetryable(err) {
			return err // Non-retryable error, fail immediately
		}

		// Don't retry if we've exhausted attempts
		if attempt == rl.maxRetries {
			break
		}

		// Calculate backoff delay
		delay := rl.calculateBackoff(attempt, err)

		// Wait before retrying
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("max retries (%d) exceeded: %w", rl.maxRetries, lastErr)
}

// isRetryable determines if an error should trigger a retry.
// Returns true for 429 (rate limit) and 5xx (server) errors.
func (rl *RateLimiter) isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check for googleapi.Error
	if apiErr, ok := err.(*googleapi.Error); ok {
		// 429 - Too Many Requests (rate limit)
		if apiErr.Code == http.StatusTooManyRequests {
			return true
		}
		// 5xx - Server errors
		if apiErr.Code >= 500 && apiErr.Code < 600 {
			return true
		}
	}

	return false
}

// calculateBackoff calculates the delay before the next retry.
// Uses exponential backoff with jitter. If the error includes a Retry-After
// header, that value is respected.
func (rl *RateLimiter) calculateBackoff(attempt int, err error) time.Duration {
	// Check for Retry-After header in the error
	if apiErr, ok := err.(*googleapi.Error); ok {
		for _, header := range apiErr.Header {
			if len(header) > 0 {
				// Try to parse Retry-After as seconds
				if retryAfter, parseErr := time.ParseDuration(header[0] + "s"); parseErr == nil {
					// Cap at maxDelay
					if retryAfter > rl.maxDelay {
						return rl.maxDelay
					}
					return retryAfter
				}
			}
		}
	}

	// Calculate exponential backoff: baseDelay * (backoffFactor ^ attempt)
	delay := float64(rl.baseDelay) * math.Pow(rl.backoffFactor, float64(attempt))

	// Cap at maxDelay
	if delay > float64(rl.maxDelay) {
		delay = float64(rl.maxDelay)
	}

	return time.Duration(delay)
}

// SetRate updates the rate limiter's requests per second and burst size.
// This can be used to dynamically adjust rate limits based on quota usage.
func (rl *RateLimiter) SetRate(requestsPerSecond float64, burst int) {
	rl.limiter.SetLimit(rate.Limit(requestsPerSecond))
	rl.limiter.SetBurst(burst)
}
