// Command engramd is the Engram memory engine daemon (HTTP + MCP).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	embedopenai "github.com/nfsarch33/engram/internal/adapters/embeddings/openai"
	"github.com/nfsarch33/engram/internal/adapters/history/sqlite"
	"github.com/nfsarch33/engram/internal/adapters/httpapi"
	llmopenai "github.com/nfsarch33/engram/internal/adapters/llm/openai"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/config"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

func main() {
	noEmbed := flag.Bool("no-embed", false, "start without embedder (dev/test mode; search unavailable)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(ctx, logger, *noEmbed); err != nil {
		fmt.Fprintf(os.Stderr, "engramd: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, noEmbed bool) error {
	cfg := config.Load()

	logger.Info("engramd starting", "version", "0.2.0",
		"addr", cfg.Addr,
		"db", cfg.DBPath,
		"embedder", cfg.HasEmbedder(),
		"llm", cfg.HasLLM(),
		"no_embed", noEmbed,
	)

	hist, err := sqlite.NewStore(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("history store: %w", err)
	}
	defer hist.Close()

	vec, err := inmem.NewStore()
	if err != nil {
		return fmt.Errorf("vector store: %w", err)
	}

	embedder, llm := buildAdapters(cfg, noEmbed)

	svc, err := engramsvc.NewService(vec, hist, llm, embedder, engramsvc.Config{
		CollectionName: cfg.Collection,
		EmbeddingDim:   cfg.EmbeddingDim,
	})
	if err != nil {
		if errors.Is(err, engram.ErrMissingEmbedder) {
			return fmt.Errorf("no embedder configured; set ENGRAM_EMBED_URL or pass --no-embed for dev mode")
		}
		return fmt.Errorf("service: %w", err)
	}

	handler := httpapi.NewHandler(svc)

	srv := &http.Server{
		Addr:        cfg.Addr,
		Handler:     handler,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("engramd shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return nil
}

// buildAdapters constructs the embedder and LLM client from cfg.
// When noEmbed is true, a noopEmbedder is returned so the service starts
// without an embedder URL (useful for dev/test; search will fail at runtime).
// When cfg.HasEmbedder() is false and noEmbed is false, both are nil and the
// caller receives ErrMissingEmbedder from engramsvc.NewService.
func buildAdapters(cfg config.Config, noEmbed bool) (engram.Embedder, engram.LLMClient) {
	var embedder engram.Embedder
	var llm engram.LLMClient

	if cfg.HasEmbedder() {
		embedder = embedopenai.New(embedopenai.Options{
			BaseURL: cfg.EmbedBaseURL,
			APIKey:  cfg.EmbedAPIKey,
			Model:   cfg.EmbedModel,
			Dim:     cfg.EmbeddingDim,
			Timeout: cfg.Timeout,
		})
	} else if noEmbed {
		embedder = &noopEmbedder{}
	}

	if cfg.HasLLM() {
		llm = llmopenai.New(llmopenai.Options{
			BaseURL: cfg.LLMBaseURL,
			APIKey:  cfg.LLMAPIKey,
			Model:   cfg.LLMModel,
			Timeout: cfg.Timeout,
		})
	}

	return embedder, llm
}

// noopEmbedder satisfies engram.Embedder but always returns an error.
// It allows the daemon to start in --no-embed mode for development; any
// operation that reaches EmbedBatch will fail with a clear message.
type noopEmbedder struct{}

func (*noopEmbedder) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, fmt.Errorf("embedder not configured; start engramd with ENGRAM_EMBED_URL to enable search")
}
