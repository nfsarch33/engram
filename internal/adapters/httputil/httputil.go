// Package httputil provides shared HTTP client configuration for Engram adapters:
// connection pooling, timeouts, and exponential backoff retry.
package httputil

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"time"
)

// PooledTransport returns an http.Transport configured for connection pooling.
func PooledTransport(maxIdle, maxPerHost int) *http.Transport {
	if maxIdle <= 0 {
		maxIdle = 100
	}
	if maxPerHost <= 0 {
		maxPerHost = 20
	}
	return &http.Transport{
		MaxIdleConns:        maxIdle,
		MaxIdleConnsPerHost: maxPerHost,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// PooledClient returns an *http.Client with connection pooling and a timeout.
func PooledClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: PooledTransport(100, 20),
	}
}

// RetryConfig controls exponential backoff behavior.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// DefaultRetryConfig returns sane defaults: 3 attempts, 500ms base, 10s max.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    10 * time.Second,
	}
}

// Retry executes fn with exponential backoff. fn should return (result, shouldRetry, error).
// If shouldRetry is false on error, the error is returned immediately without further attempts.
func Retry[T any](ctx context.Context, cfg RetryConfig, fn func() (T, bool, error)) (T, error) {
	var zero T
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 500 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 10 * time.Second
	}

	var lastErr error
	for attempt := range cfg.MaxAttempts {
		result, shouldRetry, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !shouldRetry {
			return zero, err
		}
		if attempt < cfg.MaxAttempts-1 {
			delay := backoffDelay(attempt, cfg.BaseDelay, cfg.MaxDelay)
			select {
			case <-ctx.Done():
				return zero, fmt.Errorf("retry cancelled: %w", ctx.Err())
			case <-time.After(delay):
			}
		}
	}
	return zero, fmt.Errorf("exhausted %d attempts: %w", cfg.MaxAttempts, lastErr)
}

func backoffDelay(attempt int, base, max time.Duration) time.Duration {
	delay := time.Duration(float64(base) * math.Pow(2, float64(attempt)))
	if delay > max {
		delay = max
	}
	jitter := time.Duration(rand.Int64N(int64(delay / 4)))
	return delay + jitter
}

// IsRetryable returns true for HTTP status codes that are typically transient.
func IsRetryable(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
