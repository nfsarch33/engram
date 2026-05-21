package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func mockMem0Server(t *testing.T, memories []mem0Memory) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /memories", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(memories) //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func mockEngramServer(t *testing.T, importCount *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /memories", func(w http.ResponseWriter, _ *http.Request) {
		importCount.Add(1)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode([]map[string]any{{"ID": "imported"}}) //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestMigrate_FromMem0(t *testing.T) {
	t.Parallel()
	memories := []mem0Memory{
		{ID: "m1", Memory: "user likes Go", UserID: "alice"},
		{ID: "m2", Memory: "user prefers dark mode", UserID: "alice"},
		{ID: "m3", Memory: "user works at Acme Corp", UserID: "bob"},
	}
	mem0 := mockMem0Server(t, memories)
	var count atomic.Int64
	engram := mockEngramServer(t, &count)

	stdout, _, code := runCLI(t, []string{
		"migrate", "--from-mem0",
		"--endpoint", mem0.URL,
		"--addr", engram.URL,
	})
	if code != 0 {
		t.Errorf("want exit 0, got %d; stdout=%q", code, stdout)
	}
	if count.Load() != 3 {
		t.Errorf("want 3 imports, got %d", count.Load())
	}
	if !strings.Contains(stdout, "3 imported") {
		t.Errorf("want '3 imported' in output, got %q", stdout)
	}
}

func TestMigrate_DryRun(t *testing.T) {
	t.Parallel()
	memories := []mem0Memory{
		{ID: "m1", Memory: "user likes Go", UserID: "alice"},
	}
	mem0 := mockMem0Server(t, memories)
	var count atomic.Int64
	engram := mockEngramServer(t, &count)

	stdout, _, code := runCLI(t, []string{
		"migrate", "--from-mem0",
		"--endpoint", mem0.URL,
		"--addr", engram.URL,
		"--dry-run",
	})
	if code != 0 {
		t.Errorf("want exit 0, got %d", code)
	}
	if count.Load() != 0 {
		t.Errorf("dry run should not import, got %d", count.Load())
	}
	if !strings.Contains(stdout, "dry run complete") {
		t.Errorf("want 'dry run complete', got %q", stdout)
	}
}

func TestMigrate_EmptySource(t *testing.T) {
	t.Parallel()
	mem0 := mockMem0Server(t, []mem0Memory{})
	stdout, _, code := runCLI(t, []string{
		"migrate", "--from-mem0",
		"--endpoint", mem0.URL,
	})
	if code != 0 {
		t.Errorf("want exit 0 for empty source, got %d", code)
	}
	if !strings.Contains(stdout, "nothing to migrate") {
		t.Errorf("want 'nothing to migrate', got %q", stdout)
	}
}

func TestMigrate_NoFromFlag(t *testing.T) {
	t.Parallel()
	_, stderr, code := runCLI(t, []string{"migrate"})
	if code == 0 {
		t.Error("want non-zero exit without --from-mem0")
	}
	if !strings.Contains(stderr, "--from-mem0") {
		t.Errorf("want --from-mem0 hint, got %q", stderr)
	}
}

func TestMigrate_NoEndpoint(t *testing.T) {
	t.Parallel()
	_, stderr, code := runCLI(t, []string{"migrate", "--from-mem0"})
	if code == 0 {
		t.Error("want non-zero exit without --endpoint")
	}
	if !strings.Contains(stderr, "--endpoint") {
		t.Errorf("want --endpoint hint, got %q", stderr)
	}
}

func TestMigrate_SkipsEmptyMemory(t *testing.T) {
	t.Parallel()
	memories := []mem0Memory{
		{ID: "m1", Memory: "real content", UserID: "alice"},
		{ID: "m2", Memory: "", UserID: "alice"},
	}
	mem0 := mockMem0Server(t, memories)
	var count atomic.Int64
	engram := mockEngramServer(t, &count)

	stdout, _, code := runCLI(t, []string{
		"migrate", "--from-mem0",
		"--endpoint", mem0.URL,
		"--addr", engram.URL,
	})
	if code != 0 {
		t.Errorf("want exit 0, got %d", code)
	}
	if count.Load() != 1 {
		t.Errorf("want 1 import (skipping empty), got %d", count.Load())
	}
	if !strings.Contains(stdout, "1 imported") {
		t.Errorf("want '1 imported', got %q", stdout)
	}
	if !strings.Contains(stdout, "1 skipped") {
		t.Errorf("want '1 skipped', got %q", stdout)
	}
}
