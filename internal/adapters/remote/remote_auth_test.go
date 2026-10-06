package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
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
		case "/search":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestClientWithoutKeyRejectedByGate: the plain New() client carries no
// Authorization, so a gated daemon must refuse it — the 401 a remote client
// saw before the bearer existed.
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
// This row covers the Ping path (Ping hand-rolls its request).
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

// TestDoBackedCallThroughGate: every MemoryService call (Add, Search, Get,
// ...) goes through do(); this row proves the bearer reaches THAT path, which
// the Ping row alone cannot. Mutant (drop c.auth in do() only): this test
// fails with the gate's 401.
func TestDoBackedCallThroughGate(t *testing.T) {
	t.Parallel()
	srv := stubBearerDaemon(t, "test-key")
	defer srv.Close()
	bare, err := New(srv.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = bare.Search(context.Background(), engramsvc.SearchRequest{Query: "anything", TopK: 1})
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("bare Search err=%v, want the gate 401", err)
	}
	keyed, err := New(srv.URL, 2*time.Second, WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := keyed.Search(context.Background(), engramsvc.SearchRequest{Query: "anything", TopK: 1}); err != nil {
		t.Fatalf("keyed Search through the gate: %v", err)
	}
}

// TestWithAPIKeyRefusedOverCleartextNonLoopback: the key must never travel in
// the clear; New refuses the combination at construction time. Loopback http
// and any https base stay allowed (table).
func TestWithAPIKeyRefusedOverCleartextNonLoopback(t *testing.T) {
	t.Parallel()
	cases := []struct {
		base string
		ok   bool
	}{
		{"http://10.9.8.7:8280", false},
		{"http://example.invalid:8280", false},
		{"http://127.0.0.1:8280", true},
		{"http://localhost:8280", true},
		{"https://example.invalid", true},
	}
	for _, tc := range cases {
		_, err := New(tc.base, time.Second, WithAPIKey("k"))
		if tc.ok && err != nil {
			t.Errorf("New(%q): unexpected error %v", tc.base, err)
		}
		if !tc.ok && (err == nil || !strings.Contains(err.Error(), "cleartext")) {
			t.Errorf("New(%q): err=%v, want the cleartext refusal", tc.base, err)
		}
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
