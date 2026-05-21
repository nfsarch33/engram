package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockDoctorServer returns a test server with all endpoints needed by doctor.
func mockDoctorServer(t *testing.T, searchOK bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"}) //nolint:errcheck
	})
	mux.HandleFunc("POST /search", func(w http.ResponseWriter, _ *http.Request) {
		if !searchOK {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{"error": "embedder not configured"}) //nolint:errcheck
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}}) //nolint:errcheck
	})
	mux.HandleFunc("POST /memories", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode([]map[string]any{{"ID": "01JTEST_DOCTOR"}}) //nolint:errcheck
	})
	mux.HandleFunc("DELETE /memories/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDoctor_AllPass(t *testing.T) {
	t.Parallel()
	srv := mockDoctorServer(t, true)
	stdout, _, code := runCLI(t, []string{"doctor", "--addr", srv.URL})
	if code != 0 {
		t.Errorf("want exit 0, got %d; stdout=%q", code, stdout)
	}
	if !strings.Contains(stdout, "all checks passed") {
		t.Errorf("want 'all checks passed', got %q", stdout)
	}
	if !strings.Contains(stdout, "[ OK ] healthz") {
		t.Errorf("want healthz OK, got %q", stdout)
	}
}

func TestDoctor_SearchWarn(t *testing.T) {
	t.Parallel()
	srv := mockDoctorServer(t, false)
	stdout, _, code := runCLI(t, []string{"doctor", "--addr", srv.URL})
	if code == 0 {
		t.Error("want non-zero exit when search fails")
	}
	if !strings.Contains(stdout, "[WARN] search") {
		t.Errorf("want search WARN, got %q", stdout)
	}
}

func TestDoctor_Unreachable(t *testing.T) {
	t.Parallel()
	stdout, _, code := runCLI(t, []string{"doctor", "--addr", "http://127.0.0.1:19999"})
	if code == 0 {
		t.Error("want non-zero exit for unreachable daemon")
	}
	if !strings.Contains(stdout, "[FAIL]") {
		t.Errorf("want FAIL, got %q", stdout)
	}
}
