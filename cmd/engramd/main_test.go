package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	embedopenai "github.com/nfsarch33/engram/internal/adapters/embeddings/openai"
	llmopenai "github.com/nfsarch33/engram/internal/adapters/llm/openai"
	"github.com/nfsarch33/engram/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

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

func TestRunWith_Mem0CompatRejectsAddrCollision(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Addr:           ":8280",
		Mem0CompatAddr: ":8280",
		EmbedBaseURL:   "http://localhost:11434/v1",
		EmbeddingDim:   1536,
		Timeout:        time.Second,
	}

	err := runWith(context.Background(), discardLogger(), cfg, runOpts{mem0Compat: true})
	if err == nil {
		t.Fatal("expected addr-collision error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "must differ") {
		t.Errorf("error: want 'must differ' message, got %q", got)
	}
}

func TestRunWith_Mem0CompatRequiresAddr(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Addr:           ":8280",
		Mem0CompatAddr: "",
		EmbedBaseURL:   "http://localhost:11434/v1",
		EmbeddingDim:   1536,
		Timeout:        time.Second,
	}

	err := runWith(context.Background(), discardLogger(), cfg, runOpts{mem0Compat: true})
	if err == nil {
		t.Fatal("expected missing-addr error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "ENGRAM_MEM0COMPAT_ADDR") {
		t.Errorf("error: want ENGRAM_MEM0COMPAT_ADDR hint, got %q", got)
	}
}

func TestRunWith_NoHTTPRequiresMCPStdio(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	err := runWith(context.Background(), discardLogger(), cfg, runOpts{noHTTP: true})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "--mcp-stdio") {
		t.Errorf("error: want --mcp-stdio hint, got %q", got)
	}
}
