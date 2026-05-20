package main

import (
	"context"
	"testing"
	"time"

	embedopenai "github.com/nfsarch33/engram/internal/adapters/embeddings/openai"
	llmopenai "github.com/nfsarch33/engram/internal/adapters/llm/openai"
	"github.com/nfsarch33/engram/internal/config"
)

func TestBuildAdapters_NoEmbedderURL_NoFlag(t *testing.T) {
	t.Parallel()
	cfg := config.Config{} // no EmbedBaseURL
	embedder, llm := buildAdapters(cfg, false)
	if embedder != nil {
		t.Errorf("expected nil embedder, got %T", embedder)
	}
	if llm != nil {
		t.Errorf("expected nil llm, got %T", llm)
	}
}

func TestBuildAdapters_NoEmbedderURL_WithNoEmbedFlag(t *testing.T) {
	t.Parallel()
	cfg := config.Config{} // no EmbedBaseURL
	embedder, llm := buildAdapters(cfg, true)
	if embedder == nil {
		t.Fatal("expected noop embedder, got nil")
	}
	if llm != nil {
		t.Errorf("expected nil llm, got %T", llm)
	}
	// noop embedder must return error on EmbedBatch
	_, err := embedder.EmbedBatch(context.Background(), []string{"hello"})
	if err == nil {
		t.Error("noopEmbedder.EmbedBatch should return an error")
	}
}

func TestBuildAdapters_WithEmbedderURL(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		EmbedBaseURL: "http://localhost:11434/v1",
		EmbedAPIKey:  "sk-test",
		EmbedModel:   "text-embedding-3-small",
		EmbeddingDim: 1536,
		Timeout:      10 * time.Second,
	}
	embedder, _ := buildAdapters(cfg, false)
	if _, ok := embedder.(*embedopenai.Embedder); !ok {
		t.Errorf("expected *embedopenai.Embedder, got %T", embedder)
	}
}

func TestBuildAdapters_WithLLMURL(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		EmbedBaseURL: "http://localhost:11434/v1",
		LLMBaseURL:   "http://localhost:11434/v1",
		LLMAPIKey:    "sk-test",
		LLMModel:     "gpt-4o-mini",
		Timeout:      10 * time.Second,
	}
	_, llm := buildAdapters(cfg, false)
	if _, ok := llm.(*llmopenai.Client); !ok {
		t.Errorf("expected *llmopenai.Client, got %T", llm)
	}
}

func TestNoopEmbedder_EmbedBatch_ReturnsError(t *testing.T) {
	t.Parallel()
	e := &noopEmbedder{}
	_, err := e.EmbedBatch(context.Background(), []string{"test"})
	if err == nil {
		t.Error("noopEmbedder.EmbedBatch should always return error")
	}
}

func TestNoopEmbedder_EmbedBatch_EmptyInput(t *testing.T) {
	t.Parallel()
	e := &noopEmbedder{}
	_, err := e.EmbedBatch(context.Background(), nil)
	if err == nil {
		t.Error("noopEmbedder.EmbedBatch should return error even for empty input")
	}
}
