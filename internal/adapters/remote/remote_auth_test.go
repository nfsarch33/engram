package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubBearerDaemon answers 401 to every request lacking "Authorization: Bearer
// <key>" — the plane's gate behaviour (ADR-0102 engram vhost). With the header
// it serves the two endpoints the forwarder needs at startup.
func stubBearerDaemon(t *testing.T, key string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("bearer token not accepted"))
			return
		}
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestClientWithoutKeyRejectedByGate: the plain New() client carries no
// Authorization, so a gated daemon must refuse it — this is the 401 wsl3 saw
// before the bearer existed.
func TestClientWithoutKeyRejectedByGate(t *testing.T) {
	t.Parallel()
	srv := stubBearerDaemon(t, "test-key")
	defer srv.Close()
	c, err := New(srv.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = c.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("Ping err=%v, want a 401 from the gate", err)
	}
}

// TestClientWithKeyPassesGate: WithAPIKey attaches the bearer on every
// request; the same daemon that refused the bare client serves this one.
// Mutant (drop the header set in do()): this test fails with 401.
func TestClientWithKeyPassesGate(t *testing.T) {
	t.Parallel()
	srv := stubBearerDaemon(t, "test-key")
	defer srv.Close()
	c, err := New(srv.URL, 2*time.Second, WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping through the gate: %v", err)
	}
}

// TestWithAPIKeyEmptyAttachesNothing: an empty key (loopback default) must not
// send a malformed or empty Authorization header.
func TestWithAPIKeyEmptyAttachesNothing(t *testing.T) {
	t.Parallel()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	c, err := New(srv.URL, 2*time.Second, WithAPIKey(""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if seen != "" {
		t.Fatalf("Authorization header sent with empty key: %q", seen)
	}
}
