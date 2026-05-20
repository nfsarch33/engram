// Command engramd is the Engram memory engine daemon (HTTP + MCP).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nfsarch33/engram/internal/adapters/history/sqlite"
	"github.com/nfsarch33/engram/internal/adapters/httpapi"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(ctx, logger); err != nil {
		fmt.Fprintf(os.Stderr, "engramd: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	logger.Info("engramd starting", "version", "0.1.0")

	dbPath := envOr("ENGRAM_DB_PATH", "engram.db")
	addr := envOr("ENGRAM_ADDR", ":8280")
	collectionName := envOr("ENGRAM_COLLECTION", "engram")

	hist, err := sqlite.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("history store: %w", err)
	}
	defer hist.Close()

	vec, err := inmem.NewStore()
	if err != nil {
		return fmt.Errorf("vector store: %w", err)
	}

	// Embedder is nil here; a production build injects a real implementation.
	// NewService returns ErrMissingEmbedder when nil is passed, so we treat
	// this as a fatal startup error in non-test builds.
	svc, err := engramsvc.NewService(vec, hist, nil, nil, engramsvc.Config{
		CollectionName: collectionName,
		EmbeddingDim:   1536,
	})
	if err != nil {
		if errors.Is(err, engram.ErrMissingEmbedder) {
			return fmt.Errorf("no embedder configured; set ENGRAM_EMBEDDER_URL or build with an embedder plugin")
		}
		return fmt.Errorf("service: %w", err)
	}

	handler := httpapi.NewHandler(svc)

	srv := &http.Server{
		Addr:        addr,
		Handler:     handler,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "addr", addr)
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

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
