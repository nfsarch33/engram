// Command engramd is the Engram memory engine HTTP daemon.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
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
	logger.Info("engramd starting", "version", "0.1.0-dev")
	// TODO(Epic 7): wire Qdrant + SQLite + OpenAI adapters and start HTTP server.
	<-ctx.Done()
	logger.Info("engramd shutting down")
	return nil
}
