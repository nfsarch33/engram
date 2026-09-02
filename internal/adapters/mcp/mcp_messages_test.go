package mcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// TestAdapterHandleAdd_ChatStyleObjects is the fleet-wide write-path
// regression pinned at the MCP boundary: agents that send messages as
// {role, content} objects (the shape every other memory API takes) must
// store them, not get "text must not be empty".
func TestAdapterHandleAdd_ChatStyleObjects(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)
	result, err := a.HandleTool(context.Background(), "engram_add", map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "object-shaped memory"},
			"and a plain string",
		},
		"user_id":      "u1",
		"workspace_id": "ws-1",
	})
	if err != nil {
		t.Fatalf("engram_add with object messages: %v", err)
	}
	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	recs, ok := resMap["memories"].([]engram.MemoryRecord)
	if !ok || len(recs) != 2 {
		t.Fatalf("want 2 stored records, got %v", resMap["memories"])
	}
	if recs[0].Text != "object-shaped memory" || recs[1].Text != "and a plain string" {
		t.Fatalf("texts stored out of order or mangled: %q / %q", recs[0].Text, recs[1].Text)
	}
	if recs[0].WorkspaceID != "ws-1" {
		t.Fatalf("workspace scope lost: %q", recs[0].WorkspaceID)
	}
}

// TestAdapterHandleAdd_ObjectsWithoutText: an object that carries no text is
// still an empty add, reported as such rather than stored blank.
func TestAdapterHandleAdd_ObjectsWithoutText(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)
	_, err := a.HandleTool(context.Background(), "engram_add", map[string]any{
		"messages": []any{map[string]any{"role": "user"}},
		"user_id":  "u1",
	})
	if err == nil || !strings.Contains(err.Error(), "text must not be empty") {
		t.Fatalf("textless objects: err=%v, want ErrEmptyText", err)
	}
}

// TestAdapterSearch_IsScopedByWorkspace: through the whole adapter stack, a
// search scoped to one workspace never returns another workspace's memory.
func TestAdapterSearch_IsScopedByWorkspace(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)
	ctx := context.Background()
	for _, ws := range []string{"ws-a", "ws-b"} {
		if _, err := a.HandleTool(ctx, "engram_add", map[string]any{
			"messages": []any{"same text in " + ws}, "user_id": "u1", "workspace_id": ws,
		}); err != nil {
			t.Fatalf("add %s: %v", ws, err)
		}
	}
	result, err := a.HandleTool(ctx, "engram_search", map[string]any{
		"query": "same text", "user_id": "u1", "workspace_id": "ws-b", "top_k": float64(10),
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	resMap := result.(map[string]any)
	if resMap["count"] != 1 {
		t.Fatalf("workspace scope leaked: count=%v, want 1 (results=%v)", resMap["count"], resMap["results"])
	}
}
