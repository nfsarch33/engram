package agentrace_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/agentrace"
)

func TestAgentraceEmit_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.ndjson")

	emitter, err := agentrace.NewEmitter(path)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	defer emitter.Close()

	emitter.Emit("engram_add", 42*time.Millisecond, true)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty log file")
	}

	var ev agentrace.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.Tool != "engram_add" {
		t.Errorf("tool = %q, want engram_add", ev.Tool)
	}
	if ev.LatencyMS != 42 {
		t.Errorf("latency_ms = %d, want 42", ev.LatencyMS)
	}
	if !ev.Success {
		t.Error("success = false, want true")
	}
}

func TestAgentraceEmit_Format(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.ndjson")

	emitter, err := agentrace.NewEmitter(path)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	defer emitter.Close()

	before := time.Now()
	emitter.Emit("engram_search", 7*time.Millisecond, false)
	after := time.Now()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var ev agentrace.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if ev.EventType != "mcp_call" {
		t.Errorf("event = %q, want mcp_call", ev.EventType)
	}
	if ev.Server != "engram" {
		t.Errorf("server = %q, want engram", ev.Server)
	}

	ts, err := time.Parse(time.RFC3339Nano, ev.Timestamp)
	if err != nil {
		t.Fatalf("timestamp parse: %v (raw: %q)", err, ev.Timestamp)
	}
	if ts.Before(before) || ts.After(after.Add(time.Second)) {
		t.Errorf("timestamp %v not in [%v, %v]", ts, before, after)
	}
}

func TestAgentraceEmit_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.ndjson")

	emitter, err := agentrace.NewEmitter(path)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	defer emitter.Close()

	const goroutines = 20
	const callsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				emitter.Emit("engram_add", time.Duration(j)*time.Millisecond, true)
			}
		}()
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := goroutines * callsPerGoroutine
	if len(lines) != want {
		t.Errorf("got %d lines, want %d", len(lines), want)
	}

	for i, line := range lines {
		var ev agentrace.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("line %d: unmarshal: %v", i, err)
		}
	}
}

func TestAgentraceEmit_FileError(t *testing.T) {
	t.Parallel()

	// Non-writable path: use a directory as the file path.
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	emitter, err := agentrace.NewEmitter(path)
	if err == nil {
		emitter.Close()
		t.Fatal("expected error for non-writable path, got nil")
	}
}

func TestAgentraceEmit_NilEmitter(t *testing.T) {
	t.Parallel()
	// A nil Emitter should not panic.
	var emitter *agentrace.Emitter
	emitter.Emit("engram_add", time.Millisecond, true) // should be a no-op
}
