package httputil_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/adapters/httputil"
)

func TestPooledClient(t *testing.T) {
	t.Parallel()
	c := httputil.PooledClient(5 * time.Second)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.Timeout != 5*time.Second {
		t.Errorf("want timeout 5s, got %v", c.Timeout)
	}
}

func TestPooledClient_DefaultTimeout(t *testing.T) {
	t.Parallel()
	c := httputil.PooledClient(0)
	if c.Timeout != 30*time.Second {
		t.Errorf("want default 30s, got %v", c.Timeout)
	}
}

func TestPooledTransport(t *testing.T) {
	t.Parallel()
	tr := httputil.PooledTransport(50, 10)
	if tr.MaxIdleConns != 50 {
		t.Errorf("want MaxIdleConns=50, got %d", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 10 {
		t.Errorf("want MaxIdleConnsPerHost=10, got %d", tr.MaxIdleConnsPerHost)
	}
}

func TestRetry_SuccessFirst(t *testing.T) {
	t.Parallel()
	calls := 0
	result, err := httputil.Retry(context.Background(), httputil.DefaultRetryConfig(), func() (string, bool, error) {
		calls++
		return "ok", false, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("want ok, got %s", result)
	}
	if calls != 1 {
		t.Errorf("want 1 call, got %d", calls)
	}
}

func TestRetry_SuccessAfterRetries(t *testing.T) {
	t.Parallel()
	calls := 0
	cfg := httputil.RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	result, err := httputil.Retry(context.Background(), cfg, func() (int, bool, error) {
		calls++
		if calls < 3 {
			return 0, true, errors.New("transient")
		}
		return 42, false, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Errorf("want 42, got %d", result)
	}
	if calls != 3 {
		t.Errorf("want 3 calls, got %d", calls)
	}
}

func TestRetry_ExhaustedAttempts(t *testing.T) {
	t.Parallel()
	calls := 0
	cfg := httputil.RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
	_, err := httputil.Retry(context.Background(), cfg, func() (string, bool, error) {
		calls++
		return "", true, errors.New("always fails")
	})
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if calls != 2 {
		t.Errorf("want 2 calls, got %d", calls)
	}
}

func TestRetry_NonRetryableError(t *testing.T) {
	t.Parallel()
	calls := 0
	cfg := httputil.RetryConfig{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
	_, err := httputil.Retry(context.Background(), cfg, func() (string, bool, error) {
		calls++
		return "", false, errors.New("permanent")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("want 1 call (no retry), got %d", calls)
	}
}

func TestRetry_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	cfg := httputil.RetryConfig{MaxAttempts: 5, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second}
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := httputil.Retry(ctx, cfg, func() (string, bool, error) {
		calls++
		return "", true, errors.New("transient")
	})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestIsRetryable(t *testing.T) {
	t.Parallel()
	retryable := []int{429, 500, 502, 503, 504}
	for _, code := range retryable {
		if !httputil.IsRetryable(code) {
			t.Errorf("expected %d to be retryable", code)
		}
	}
	nonRetryable := []int{200, 201, 400, 401, 403, 404}
	for _, code := range nonRetryable {
		if httputil.IsRetryable(code) {
			t.Errorf("expected %d to NOT be retryable", code)
		}
	}
}
