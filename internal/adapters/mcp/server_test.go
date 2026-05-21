package mcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	mcpadapter "github.com/nfsarch33/engram/internal/adapters/mcp"
)

// driveStdio writes one JSON-RPC request and returns the parsed response.
// It uses io.Pipe so the goroutine running Serve can read/write concurrently.
func driveStdio(t *testing.T, a *mcpadapter.Adapter, requests []map[string]any) []map[string]any {
	t.Helper()

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	srv := mcpadapter.NewServer(a, "engram", "test")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.Serve(ctx, stdinR, stdoutW)
		_ = stdoutW.Close()
	}()

	// Writer goroutine: feed requests as newline-delimited JSON.
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for _, req := range requests {
			b, err := json.Marshal(req)
			if err != nil {
				t.Errorf("marshal request: %v", err)
				return
			}
			if _, err := stdinW.Write(append(b, '\n')); err != nil {
				return
			}
		}
	}()

	// Reader: collect N responses (one per non-notification request).
	expected := 0
	for _, req := range requests {
		if _, hasID := req["id"]; hasID {
			expected++
		}
	}

	scanner := bufio.NewScanner(stdoutR)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := make([]map[string]any, 0, expected)
	doneRead := make(chan struct{})
	go func() {
		defer close(doneRead)
		for len(out) < expected && scanner.Scan() {
			var msg map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
				t.Errorf("unmarshal response: %v", err)
				return
			}
			// Skip notifications (no id) and any pre-init noise.
			if _, ok := msg["id"]; ok {
				out = append(out, msg)
			}
		}
	}()

	<-writeDone
	select {
	case <-doneRead:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for %d responses; got %d", expected, len(out))
	}

	cancel()
	_ = stdinW.Close()
	_ = stdoutR.Close()
	wg.Wait()

	return out
}

func TestServerInitializeAndListTools(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)

	resps := driveStdio(t, a, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2024-11-05",
				"clientInfo":      map[string]any{"name": "test", "version": "1"},
			},
		},
		{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/list",
		},
	})

	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d: %#v", len(resps), resps)
	}

	if resps[0]["error"] != nil {
		t.Fatalf("initialize error: %v", resps[0]["error"])
	}

	listResp := resps[1]
	if listResp["error"] != nil {
		t.Fatalf("tools/list error: %v", listResp["error"])
	}
	result, ok := listResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list result missing: %#v", listResp)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools array missing: %#v", result)
	}

	want := map[string]bool{
		"engram_add": true, "engram_search": true, "engram_get": true,
		"engram_update": true, "engram_delete": true, "engram_history": true,
	}
	got := make(map[string]bool, len(tools))
	for _, t := range tools {
		m, _ := t.(map[string]any)
		if name, ok := m["name"].(string); ok {
			got[name] = true
		}
	}
	for name := range want {
		if !got[name] {
			t.Errorf("missing tool in tools/list: %s (got %v)", name, got)
		}
	}
}

func TestServerCallEngramAdd(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)

	resps := driveStdio(t, a, []map[string]any{
		{
			"jsonrpc": "2.0", "id": 1, "method": "initialize",
			"params": map[string]any{
				"protocolVersion": "2024-11-05",
				"clientInfo":      map[string]any{"name": "test", "version": "1"},
			},
		},
		{
			"jsonrpc": "2.0", "id": 2, "method": "tools/call",
			"params": map[string]any{
				"name": "engram_add",
				"arguments": map[string]any{
					"messages": []any{"user likes Go"},
					"user_id":  "u1",
				},
			},
		},
	})

	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
	if resps[1]["error"] != nil {
		t.Fatalf("tools/call error: %v", resps[1]["error"])
	}
	result, ok := resps[1]["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call result missing: %#v", resps[1])
	}
	// MCP CallToolResult always carries a content array.
	if _, hasContent := result["content"]; !hasContent {
		t.Errorf("expected content in tools/call result, got %#v", result)
	}
}

func TestServerCallUnknownToolErrors(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)

	resps := driveStdio(t, a, []map[string]any{
		{
			"jsonrpc": "2.0", "id": 1, "method": "initialize",
			"params": map[string]any{
				"protocolVersion": "2024-11-05",
				"clientInfo":      map[string]any{"name": "test", "version": "1"},
			},
		},
		{
			"jsonrpc": "2.0", "id": 2, "method": "tools/call",
			"params": map[string]any{"name": "engram_unknown", "arguments": map[string]any{}},
		},
	})

	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
	// Unknown tool: protocol-level error or isError content.
	r := resps[1]
	if r["error"] == nil {
		result, _ := r["result"].(map[string]any)
		if isErr, _ := result["isError"].(bool); !isErr {
			t.Errorf("expected error or isError=true for unknown tool, got %#v", r)
		}
	}
}
