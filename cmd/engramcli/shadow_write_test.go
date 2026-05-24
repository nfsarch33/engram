package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunShadowWrite_BothSucceed_LogsDualID(t *testing.T) {
	t.Parallel()

	mem0Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem0-id-A"})
	}))
	defer mem0Srv.Close()

	engramSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": "engram-id-B"})
	}))
	defer engramSrv.Close()

	tmpLog := filepath.Join(t.TempDir(), "engram-shadow.ndjson")

	var stdout, stderr bytes.Buffer
	deps := Deps{Stdout: &stdout, Stderr: &stderr}
	code := runShadowWrite(deps, []string{
		"--engram-addr", engramSrv.URL,
		"--mem0-addr", mem0Srv.URL,
		"--app-id", "test-app",
		"--user-id", "test-user",
		"--message", "user:test payload",
		"--log", tmpLog,
	})
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, stderr.String())
	}

	data, err := os.ReadFile(tmpLog)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var rec ShadowWriteRecord
	if err := json.Unmarshal(bytes.TrimRight(data, "\n"), &rec); err != nil {
		t.Fatalf("decode rec: %v -- raw=%s", err, data)
	}
	if rec.A != "mem0-id-A" {
		t.Errorf("a = %q, want mem0-id-A", rec.A)
	}
	if rec.B != "engram-id-B" {
		t.Errorf("b = %q, want engram-id-B", rec.B)
	}
	if rec.AppID != "test-app" {
		t.Errorf("app_id = %q", rec.AppID)
	}
	if rec.PayloadHash == "" || len(rec.PayloadHash) != 64 {
		t.Errorf("payload_hash invalid: %q", rec.PayloadHash)
	}
	if rec.Timestamp == "" {
		t.Errorf("ts empty")
	}
	if rec.Diverged {
		t.Errorf("diverged=true on dual success")
	}
}

func TestRunShadowWrite_EngramFails_RecordsDivergence(t *testing.T) {
	t.Parallel()

	mem0Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem0-only"})
	}))
	defer mem0Srv.Close()

	tmpLog := filepath.Join(t.TempDir(), "engram-shadow.ndjson")

	var stdout, stderr bytes.Buffer
	deps := Deps{Stdout: &stdout, Stderr: &stderr}
	code := runShadowWrite(deps, []string{
		"--engram-addr", "http://127.0.0.1:1",
		"--mem0-addr", mem0Srv.URL,
		"--app-id", "test-app",
		"--message", "user:engram-down",
		"--log", tmpLog,
	})
	// Mem0 wrote successfully; shadow-mode never blocks the canonical write
	// path even if the secondary fails. Exit 0, but log marks divergence.
	if code != 0 {
		t.Fatalf("want exit 0 (mem0 succeeded), got %d", code)
	}

	data, _ := os.ReadFile(tmpLog)
	var rec ShadowWriteRecord
	json.Unmarshal(bytes.TrimRight(data, "\n"), &rec)
	if !rec.Diverged {
		t.Error("expected diverged=true when engram down")
	}
	if rec.A != "mem0-only" {
		t.Errorf("a = %q", rec.A)
	}
	if rec.B != "" {
		t.Errorf("b should be empty on engram failure, got %q", rec.B)
	}
	if !strings.Contains(rec.EngramError, "127.0.0.1:1") {
		t.Errorf("engram_error should contain unreachable addr, got %q", rec.EngramError)
	}
}

func TestRunShadowWrite_Mem0Fails_Exit1(t *testing.T) {
	t.Parallel()
	// Mem0 is the canonical write path. If it fails, the shadow harness must
	// surface non-zero exit so the caller can retry; secondary success cannot
	// mask primary failure.
	engramSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": "engram-orphan"})
	}))
	defer engramSrv.Close()

	tmpLog := filepath.Join(t.TempDir(), "engram-shadow.ndjson")

	var stdout, stderr bytes.Buffer
	deps := Deps{Stdout: &stdout, Stderr: &stderr}
	code := runShadowWrite(deps, []string{
		"--engram-addr", engramSrv.URL,
		"--mem0-addr", "http://127.0.0.1:1",
		"--app-id", "test-app",
		"--message", "user:mem0-down",
		"--log", tmpLog,
	})
	if code != 1 {
		t.Fatalf("want exit 1 (mem0 down = canonical path failure), got %d", code)
	}
	data, _ := os.ReadFile(tmpLog)
	var rec ShadowWriteRecord
	json.Unmarshal(bytes.TrimRight(data, "\n"), &rec)
	if !rec.Diverged {
		t.Error("expected diverged=true when mem0 fails")
	}
}

func TestPayloadHash_Deterministic(t *testing.T) {
	t.Parallel()
	h1 := payloadHash([]byte(`{"a":1}`))
	h2 := payloadHash([]byte(`{"a":1}`))
	h3 := payloadHash([]byte(`{"a":2}`))
	if h1 != h2 {
		t.Errorf("hash should be deterministic: %s vs %s", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("hash should differ for different input")
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64 (sha256 hex)", len(h1))
	}
}

func TestRunShadowWrite_RestrictsToConfiguredAppID(t *testing.T) {
	t.Parallel()
	// Per SOP §3.1: only app_id=test-app is dual-written during shadow.
	// Other app IDs must skip the secondary write entirely.
	mem0Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem0-coord"})
	}))
	defer mem0Srv.Close()

	engramHits := 0
	engramSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		engramHits++
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": "should-not-fire"})
	}))
	defer engramSrv.Close()

	tmpLog := filepath.Join(t.TempDir(), "engram-shadow.ndjson")
	var stdout, stderr bytes.Buffer
	deps := Deps{Stdout: &stdout, Stderr: &stderr}
	code := runShadowWrite(deps, []string{
		"--engram-addr", engramSrv.URL,
		"--mem0-addr", mem0Srv.URL,
		"--app-id", "cursor-coordination",
		"--allow-app", "test-app",
		"--message", "user:coord-only",
		"--log", tmpLog,
	})
	if code != 0 {
		t.Fatalf("want exit 0 when mem0 succeeded, got %d", code)
	}
	if engramHits != 0 {
		t.Errorf("engram should not be hit for non-allow-listed app_id, got %d hits", engramHits)
	}
	// Log entry should still be written so we can audit which writes were skipped.
	data, _ := os.ReadFile(tmpLog)
	var rec ShadowWriteRecord
	json.Unmarshal(bytes.TrimRight(data, "\n"), &rec)
	if !rec.Skipped {
		t.Error("expected skipped=true for non-allow-listed app_id")
	}
}
