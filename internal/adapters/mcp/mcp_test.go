package mcp_test

import (
	"context"
	"testing"

	"github.com/nfsarch33/engram/internal/adapters/history/sqlite"
	mcpadapter "github.com/nfsarch33/engram/internal/adapters/mcp"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/app/engramsvc"
)

type stubEmbedder struct{ dim int }

func (e *stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dim)
		v[0] = float32(i + 1)
		out[i] = v
	}
	return out, nil
}

func makeAdapter(t *testing.T) *mcpadapter.Adapter {
	t.Helper()
	hist, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { hist.Close() })
	vec, _ := inmem.NewStore()
	svc, err := engramsvc.NewService(vec, hist, nil, &stubEmbedder{dim: 8}, engramsvc.Config{
		CollectionName: "test", EmbeddingDim: 8,
	})
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	return mcpadapter.NewAdapter(svc)
}

func TestAdapterCreation(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)
	if a == nil {
		t.Error("NewAdapter should not return nil")
	}
}

func TestAdapterTools(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)
	tools := a.Tools()
	if len(tools) == 0 {
		t.Error("adapter should expose at least one tool")
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}

	required := []string{
		"engram_add",
		"engram_search",
		"engram_get",
		"engram_update",
		"engram_delete",
		"engram_history",
	}
	for _, r := range required {
		if !names[r] {
			t.Errorf("missing required tool: %s", r)
		}
	}
}

func TestAdapterHandleAdd(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)
	result, err := a.HandleTool(context.Background(), "engram_add", map[string]any{
		"messages": []any{"user likes Go"},
		"user_id":  "u1",
	})
	if err != nil {
		t.Fatalf("engram_add: %v", err)
	}
	if result == nil {
		t.Error("engram_add should return a result")
	}
}

func TestAdapterHandleSearch(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)
	// Add first.
	a.HandleTool(context.Background(), "engram_add", map[string]any{ //nolint:errcheck
		"messages": []any{"user likes Go"},
		"user_id":  "u1",
	})

	result, err := a.HandleTool(context.Background(), "engram_search", map[string]any{
		"query":   "Go",
		"user_id": "u1",
		"top_k":   float64(5),
	})
	if err != nil {
		t.Fatalf("engram_search: %v", err)
	}
	if result == nil {
		t.Error("engram_search should return a result")
	}
}

func TestAdapterHandleUnknownTool(t *testing.T) {
	t.Parallel()
	a := makeAdapter(t)
	_, err := a.HandleTool(context.Background(), "engram_nonexistent", nil)
	if err == nil {
		t.Error("unknown tool should return error")
	}
}
