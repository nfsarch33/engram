package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRunShadow_BothSuccess(t *testing.T) {
	t.Parallel()

	engramSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "abc-123", "score": 0.95},
				{"id": "def-456", "score": 0.80},
			},
		})
	}))
	defer engramSrv.Close()

	mem0Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "abc-123", "score": 0.92},
				{"id": "ghi-789", "score": 0.75},
			},
		})
	}))
	defer mem0Srv.Close()

	tmpLog := filepath.Join(t.TempDir(), "shadow.ndjson")

	var stdout, stderr bytes.Buffer
	deps := Deps{Stdout: &stdout, Stderr: &stderr}
	code := runShadow(deps, []string{
		"--engram-addr", engramSrv.URL,
		"--mem0-addr", mem0Srv.URL,
		"--query", "test query",
		"--user-id", "testuser",
		"--log", tmpLog,
	})

	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, stderr.String())
	}

	data, err := os.ReadFile(tmpLog)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	var entry ShadowLogEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}
	if entry.EngramCount != 2 {
		t.Errorf("want engram_count=2, got %d", entry.EngramCount)
	}
	if entry.Mem0Count != 2 {
		t.Errorf("want mem0_count=2, got %d", entry.Mem0Count)
	}
	if entry.Query != "test query" {
		t.Errorf("want query='test query', got %q", entry.Query)
	}
}

func TestRunShadow_EngramDown(t *testing.T) {
	t.Parallel()

	mem0Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer mem0Srv.Close()

	tmpLog := filepath.Join(t.TempDir(), "shadow.ndjson")

	var stdout, stderr bytes.Buffer
	deps := Deps{Stdout: &stdout, Stderr: &stderr}
	code := runShadow(deps, []string{
		"--engram-addr", "http://127.0.0.1:1",
		"--mem0-addr", mem0Srv.URL,
		"--query", "test",
		"--log", tmpLog,
	})

	if code != 0 {
		t.Fatalf("want exit 0 (logs discrepancy, doesn't fail), got %d", code)
	}

	data, err := os.ReadFile(tmpLog)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	var entry ShadowLogEntry
	json.Unmarshal(data[:len(data)-1], &entry) //nolint:errcheck
	if !entry.Discrepancy {
		t.Error("expected discrepancy=true when engram is down")
	}
}

func TestRunShadow_MissingQuery(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	deps := Deps{Stdout: &stdout, Stderr: &stderr}
	code := runShadow(deps, []string{})
	if code != 1 {
		t.Errorf("want exit 1 for missing query, got %d", code)
	}
}

func TestComputeOverlap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b []string
		want float64
	}{
		{"identical", []string{"a", "b"}, []string{"a", "b"}, 1.0},
		{"no overlap", []string{"a", "b"}, []string{"c", "d"}, 0.0},
		{"partial", []string{"a", "b", "c"}, []string{"a", "b", "d"}, 2.0 / 3.0},
		{"both empty", nil, nil, 1.0},
		{"one empty", []string{"a"}, nil, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeOverlap(tt.a, tt.b)
			if abs64(got-tt.want) > 0.01 {
				t.Errorf("want %f, got %f", tt.want, got)
			}
		})
	}
}

func abs64(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
