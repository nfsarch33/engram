package engramsvc

import (
	"context"
	"fmt"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// Config holds wiring-level parameters for the Engram service.
type Config struct {
	CollectionName string
	EmbeddingDim   int
}

// Service orchestrates memory operations using domain ports.
// All external dependencies are injected; no concrete providers here.
type Service struct {
	vec      engram.VectorStore
	hist     engram.HistoryStore
	llm      engram.LLMClient
	embedder engram.Embedder
	cfg      Config
}

// NewService constructs the service and ensures the vector collection exists.
func NewService(
	vec engram.VectorStore,
	hist engram.HistoryStore,
	llm engram.LLMClient,
	embedder engram.Embedder,
	cfg Config,
) (*Service, error) {
	if embedder == nil {
		return nil, engram.ErrMissingEmbedder
	}
	if cfg.CollectionName == "" {
		cfg.CollectionName = "engram_memories"
	}
	if cfg.EmbeddingDim == 0 {
		cfg.EmbeddingDim = 1536
	}
	if err := vec.EnsureCollection(context.Background(), cfg.CollectionName, cfg.EmbeddingDim); err != nil {
		return nil, fmt.Errorf("engramsvc: ensure collection: %w", err)
	}
	return &Service{vec: vec, hist: hist, llm: llm, embedder: embedder, cfg: cfg}, nil
}

// AddRequest is the input to Service.Add.
type AddRequest struct {
	Messages    []string
	UserID      string
	AgentID     string
	RunID       string
	AppID       string
	WorkspaceID string
	Metadata    map[string]any
	Infer       bool // true = LLM-assisted fact extraction + dedup
}

// SearchRequest is the input to Service.Search.
type SearchRequest struct {
	Query       string
	UserID      string
	AgentID     string
	RunID       string
	AppID       string
	WorkspaceID string
	TopK        int
}

// GetRequest is the input to Service.Get.
type GetRequest struct {
	ID engram.MemoryID
}

// DeleteRequest is the input to Service.Delete.
type DeleteRequest struct {
	ID engram.MemoryID
}

// UpdateRequest is the input to Service.Update.
type UpdateRequest struct {
	ID   engram.MemoryID
	Text string
}
