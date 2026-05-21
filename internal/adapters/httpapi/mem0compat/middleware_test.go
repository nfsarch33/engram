package mem0compat

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKeyMiddleware_RejectsMissingHeader(t *testing.T) {
	t.Parallel()

	mw := APIKeyMiddleware("secret")
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/memories", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", rec.Code)
	}
	if called {
		t.Fatal("handler was called despite missing header")
	}
}

func TestAPIKeyMiddleware_RejectsWrongHeader(t *testing.T) {
	t.Parallel()

	mw := APIKeyMiddleware("secret")
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/memories", nil)
	req.Header.Set("X-API-Key", "wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", rec.Code)
	}
}

func TestAPIKeyMiddleware_AllowsCorrectHeader(t *testing.T) {
	t.Parallel()

	mw := APIKeyMiddleware("secret")
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodPost, "/memories", nil)
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler was not invoked")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status: want 418, got %d", rec.Code)
	}
}

// TestAPIKeyMiddleware_BypassesHealthAndSetup verifies the two endpoints that
// mem0-mcp-go and the Mem0 OSS UI probe before injecting the API key.
func TestAPIKeyMiddleware_BypassesHealthAndSetup(t *testing.T) {
	t.Parallel()

	mw := APIKeyMiddleware("secret")
	for _, path := range []string{"/healthz", "/auth/setup-status"} {
		called := false
		h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if !called {
			t.Fatalf("path %s: handler not invoked", path)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("path %s: status want 200, got %d", path, rec.Code)
		}
	}
}

// TestAPIKeyMiddleware_EmptyKeyDisablesGate verifies that an empty configured
// key opens the surface (parity with engramd's existing httpapi).
func TestAPIKeyMiddleware_EmptyKeyDisablesGate(t *testing.T) {
	t.Parallel()

	mw := APIKeyMiddleware("")
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/memories", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler not invoked despite empty key")
	}
}
