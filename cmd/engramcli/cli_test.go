package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockServer returns a test server that handles engram HTTP API requests.
func mockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// POST /memories -> 201 with array of MemoryRecord
	mux.HandleFunc("POST /memories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode([]map[string]any{{"id": "01JTEST00000000000000AAAAA", "text": "hello world"}})
	})

	// POST /search -> 200 with array of SearchResult{record, score}
	mux.HandleFunc("POST /search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"record": map[string]any{"id": "01JTEST00000000000000AAAAA", "text": "hello"}, "score": 0.95},
		})
	})

	// GET /memories/{id}
	mux.HandleFunc("GET /memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "notfound" {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":       id,
			"messages": []map[string]any{{"role": "user", "content": "stored message"}},
		})
	})

	// DELETE /memories/{id}
	mux.HandleFunc("DELETE /memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// GET /healthz
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})

	return httptest.NewServer(mux)
}

func runCLI(t *testing.T, args []string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	deps := Deps{
		Stdout: &outBuf,
		Stderr: &errBuf,
	}
	code = Run(deps, args)
	return outBuf.String(), errBuf.String(), code
}

func TestRun_NoArgs_PrintsUsage(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI(t, []string{})
	if code == 0 {
		t.Error("want non-zero exit for no args")
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "sage") {
		t.Errorf("want usage hint in output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestRun_HealthCmd(t *testing.T) {
	t.Parallel()
	srv := mockServer(t)
	defer srv.Close()
	stdout, _, code := runCLI(t, []string{"health", "--addr", srv.URL})
	if code != 0 {
		t.Errorf("want exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "ok") {
		t.Errorf("want ok in output, got %q", stdout)
	}
}

func TestRun_AddCmd(t *testing.T) {
	t.Parallel()
	srv := mockServer(t)
	defer srv.Close()
	stdout, _, code := runCLI(t, []string{"add", "--addr", srv.URL,
		"--message", "user:hello world"})
	if code != 0 {
		t.Errorf("want exit 0, got %d (stdout=%q)", code, stdout)
	}
	if !strings.Contains(stdout, "01JTEST") {
		t.Errorf("want ID in output, got %q", stdout)
	}
}

func TestRun_SearchCmd(t *testing.T) {
	t.Parallel()
	srv := mockServer(t)
	defer srv.Close()
	stdout, _, code := runCLI(t, []string{"search", "--addr", srv.URL,
		"--query", "hello"})
	if code != 0 {
		t.Errorf("want exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "hello") {
		t.Errorf("want result in output, got %q", stdout)
	}
}

func TestRun_GetCmd(t *testing.T) {
	t.Parallel()
	srv := mockServer(t)
	defer srv.Close()
	stdout, _, code := runCLI(t, []string{"get", "--addr", srv.URL,
		"--id", "01JTEST00000000000000AAAAA"})
	if code != 0 {
		t.Errorf("want exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "stored message") {
		t.Errorf("want memory content in output, got %q", stdout)
	}
}

func TestRun_GetCmd_NotFound(t *testing.T) {
	t.Parallel()
	srv := mockServer(t)
	defer srv.Close()
	_, _, code := runCLI(t, []string{"get", "--addr", srv.URL, "--id", "notfound"})
	if code == 0 {
		t.Error("want non-zero exit for 404")
	}
}

func TestRun_DeleteCmd(t *testing.T) {
	t.Parallel()
	srv := mockServer(t)
	defer srv.Close()
	_, _, code := runCLI(t, []string{"delete", "--addr", srv.URL, "--id", "01JTEST00000000000000AAAAA"})
	if code != 0 {
		t.Errorf("want exit 0, got %d", code)
	}
}

func TestRun_UnknownCmd(t *testing.T) {
	t.Parallel()
	_, _, code := runCLI(t, []string{"unknown-command"})
	if code == 0 {
		t.Error("want non-zero exit for unknown command")
	}
}
