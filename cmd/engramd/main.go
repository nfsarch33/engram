// Command engramd is the Engram memory engine daemon (HTTP + MCP stdio).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	embedchain "github.com/nfsarch33/engram/internal/adapters/embeddings/chain"
	embedopenai "github.com/nfsarch33/engram/internal/adapters/embeddings/openai"
	"github.com/nfsarch33/engram/internal/adapters/history/sqlite"
	"github.com/nfsarch33/engram/internal/adapters/httpapi"
	"github.com/nfsarch33/engram/internal/adapters/httpapi/mem0compat"
	llmopenai "github.com/nfsarch33/engram/internal/adapters/llm/openai"
	mcpadapter "github.com/nfsarch33/engram/internal/adapters/mcp"
	"github.com/nfsarch33/engram/internal/adapters/remote"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/qdrant"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/sqlitevec"
	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/config"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

type runOpts struct {
	noEmbed        bool
	mcpStdio       bool
	noHTTP         bool
	mem0Compat     bool
	reindexMissing bool
	remote         string // daemon URL: MCP stdio proxies to it instead of opening stores
}

func main() {
	var opts runOpts
	flag.BoolVar(&opts.noEmbed, "no-embed", false, "start without embedder (dev/test mode; search unavailable)")
	flag.BoolVar(&opts.mcpStdio, "mcp-stdio", false, "serve MCP via stdio JSON-RPC (in addition to HTTP unless --no-http)")
	flag.BoolVar(&opts.noHTTP, "no-http", false, "disable HTTP server (typically combined with --mcp-stdio)")
	flag.BoolVar(&opts.mem0Compat, "mem0-compat", false, "additionally serve a Mem0 OSS-compatible HTTP shim on ENGRAM_MEM0COMPAT_ADDR (default :8281)")
	flag.StringVar(&opts.remote, "remote", os.Getenv("ENGRAM_BASE_URL"), "with --mcp-stdio --no-http: forward every tool call to the running daemon at this URL (default $ENGRAM_BASE_URL) instead of opening local stores and an embedder")
	flag.BoolVar(&opts.reindexMissing, "reindex-missing", false, "at startup, embed and index every history record missing from the vector index (spends embedding calls only on the gap), then serve")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(ctx, logger, opts); err != nil {
		fmt.Fprintf(os.Stderr, "engramd: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, opts runOpts) error {
	return runWith(ctx, logger, config.Load(), opts)
}

// runWith is the testable entry point that takes an explicit config so unit
// tests can exercise validation branches without touching env vars.
func runWith(ctx context.Context, logger *slog.Logger, cfg config.Config, opts runOpts) error {
	if opts.noHTTP && !opts.mcpStdio {
		return fmt.Errorf("--no-http requires --mcp-stdio (otherwise the daemon would have nothing to serve)")
	}
	if opts.remote != "" {
		if !opts.mcpStdio || !opts.noHTTP {
			return fmt.Errorf("--remote is the MCP stdio proxy mode: it requires --mcp-stdio --no-http")
		}
		return runRemoteMCP(ctx, logger, cfg, opts.remote)
	}

	if opts.mem0Compat {
		if cfg.Mem0CompatAddr == "" {
			return fmt.Errorf("--mem0-compat requires ENGRAM_MEM0COMPAT_ADDR (default :8281)")
		}
		if cfg.Mem0CompatAddr == cfg.Addr && !opts.noHTTP {
			return fmt.Errorf("--mem0-compat addr (%s) must differ from canonical HTTP addr (%s)", cfg.Mem0CompatAddr, cfg.Addr)
		}
	}

	logger.Info("engramd starting",
		"version", version,
		"addr", cfg.Addr,
		"db", cfg.DBPath,
		"embedder", cfg.HasEmbedder(),
		"llm", cfg.HasLLM(),
		"no_embed", opts.noEmbed,
		"mcp_stdio", opts.mcpStdio,
		"no_http", opts.noHTTP,
		"mem0_compat", opts.mem0Compat,
		"mem0_compat_addr", cfg.Mem0CompatAddr,
	)

	hist, err := sqlite.NewStore(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("history store: %w", err)
	}
	defer hist.Close()

	vec, err := buildVectorStore(cfg, logger)
	if err != nil {
		return fmt.Errorf("vector store: %w", err)
	}
	if closer, ok := vec.(io.Closer); ok {
		defer closer.Close()
	}

	embedder, llm := buildAdapters(cfg, opts.noEmbed)

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
	if err := reportIndexCoverage(ctx, logger, svc, vec, opts.reindexMissing); err != nil {
		return err
	}

	serverErr := make(chan error, 3)
	var srv *http.Server
	var mem0Srv *http.Server

	if !opts.noHTTP {
		handler := httpapi.NewHandler(svc)
		srv = &http.Server{
			Addr:        cfg.Addr,
			Handler:     handler,
			BaseContext: func(net.Listener) context.Context { return ctx },
		}
		go func() {
			logger.Info("HTTP server listening", "addr", cfg.Addr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- fmt.Errorf("http: %w", err)
				return
			}
		}()
	}

	if opts.mem0Compat {
		mem0Handler := mem0compat.NewHandler(svc, cfg.APIKey)
		mem0Srv = &http.Server{
			Addr:        cfg.Mem0CompatAddr,
			Handler:     mem0Handler,
			BaseContext: func(net.Listener) context.Context { return ctx },
		}
		go func() {
			logger.Info("Mem0 OSS-compat shim listening", "addr", cfg.Mem0CompatAddr)
			if err := mem0Srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- fmt.Errorf("mem0-compat: %w", err)
				return
			}
		}()
	}

	if opts.mcpStdio {
		adapter := mcpadapter.NewAdapter(svc)
		mcpSrv := mcpadapter.NewServer(adapter, "engram", version)
		go func() {
			logger.Info("MCP stdio server listening")
			if err := mcpSrv.Serve(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
				serverErr <- fmt.Errorf("mcp: %w", err)
				return
			}
			// Stdio EOF or graceful shutdown: cancel parent ctx so HTTP also stops.
			serverErr <- nil
		}()
	}

	select {
	case err := <-serverErr:
		if err != nil {
			return err
		}
	case <-ctx.Done():
	}

	logger.Info("engramd shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	if srv != nil {
		if err := srv.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
	}
	if mem0Srv != nil {
		if err := mem0Srv.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("graceful mem0-compat shutdown: %w", err)
		}
	}
	return nil
}

// runRemoteMCP serves MCP over stdio by forwarding to a running daemon. No
// history file, no vector index, no embedder: the proxy cannot hold state
// the daemon does not, so what an agent writes is what search serves.
func runRemoteMCP(ctx context.Context, logger *slog.Logger, cfg config.Config, target string) error {
	client, err := remote.New(target, cfg.Timeout)
	if err != nil {
		return err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx); err != nil {
		return fmt.Errorf("remote daemon %s: %w", target, err)
	}
	logger.Info("MCP stdio proxying to daemon", "version", version, "remote", target)
	mcpSrv := mcpadapter.NewServer(mcpadapter.NewAdapter(client), "engram", version)
	if err := mcpSrv.Serve(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("mcp: %w", err)
	}
	logger.Info("engramd (remote MCP) shutting down")
	return nil
}

// buildVectorStore returns a Qdrant-backed store when ENGRAM_QDRANT_URL is
// set (so docker-compose.prod.yaml's Qdrant config is honoured at runtime),
// otherwise the store ENGRAM_VECTOR_STORE names: the durable SQLite-backed
// index by default, which restores itself at boot, or the process-memory
// index that a restart empties.
func buildVectorStore(cfg config.Config, logger *slog.Logger) (engram.VectorStore, error) {
	if cfg.HasQdrant() {
		logger.Info("using Qdrant vector store", "url", cfg.QdrantURL)
		return qdrant.New(qdrant.Options{
			BaseURL:    cfg.QdrantURL,
			APIKey:     cfg.QdrantAPIKey,
			Collection: cfg.Collection,
			Timeout:    cfg.Timeout,
		}), nil
	}
	switch cfg.VectorStore {
	case config.VectorStoreInmem:
		logger.Warn("using in-memory vector store: the index is lost on every restart",
			"hint", "set ENGRAM_VECTOR_STORE=sqlite to persist it")
		return inmem.NewStore()
	case config.VectorStoreSQLite, "":
		store, err := sqlitevec.Open(cfg.VectorDBPath)
		if err != nil {
			return nil, err
		}
		loaded, _ := store.LoadStats()
		logger.Info("using durable sqlite vector store", "path", cfg.VectorDBPath, "restored", loaded)
		return store, nil
	default:
		return nil, fmt.Errorf("unknown ENGRAM_VECTOR_STORE %q (want %q or %q)", cfg.VectorStore, config.VectorStoreSQLite, config.VectorStoreInmem)
	}
}

// reportIndexCoverage logs how much of history the vector index serves,
// runs the reindex when asked, and warns when records are invisible to
// search. A store that cannot report its IDs (Qdrant) is logged as such.
func reportIndexCoverage(ctx context.Context, logger *slog.Logger, svc *engramsvc.Service, vec engram.VectorStore, reindex bool) error {
	if s, ok := vec.(*sqlitevec.Store); ok {
		loaded, evicted := s.LoadStats()
		if evicted > 0 {
			logger.Warn("vector rows evicted for embedding-dimension mismatch; they will be re-embedded by --reindex-missing",
				"loaded", loaded, "evicted", evicted)
		}
	}
	if reindex {
		n, err := svc.ReindexMissing(ctx)
		if err != nil {
			return fmt.Errorf("reindex missing: %w", err)
		}
		logger.Info("reindex complete", "reindexed", n)
	}
	stats, err := svc.IndexStats(ctx)
	if err != nil {
		if errors.Is(err, engramsvc.ErrIndexNotInspectable) {
			logger.Info("vector index coverage unknown: store cannot report its ids")
			return nil
		}
		return fmt.Errorf("index stats: %w", err)
	}
	logger.Info("vector index coverage", "records", stats.Records, "indexed", stats.Indexed, "missing", stats.Missing)
	if stats.Missing > 0 {
		logger.Warn("records missing from the vector index are invisible to semantic search",
			"missing", stats.Missing, "hint", "restart with --reindex-missing")
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
		primary := embedopenai.New(embedopenai.Options{
			BaseURL: cfg.EmbedBaseURL,
			APIKey:  cfg.EmbedAPIKey,
			Model:   cfg.EmbedModel,
			Dim:     cfg.EmbeddingDim,
			Timeout: cfg.Timeout,
		})
		if cfg.HasEmbedFallback() {
			fallback := embedopenai.New(embedopenai.Options{
				BaseURL: cfg.EmbedFallbackURL,
				APIKey:  cfg.EmbedFallbackKey,
				Model:   cfg.EmbedFallbackModel,
				Dim:     cfg.EmbeddingDim,
				Timeout: cfg.Timeout,
			})
			chain, chainErr := embedchain.New(
				embedchain.WithProvider("primary", primary),
				embedchain.WithProvider("fallback", fallback),
				// v14857: attach Prometheus observer so cost alerting works.
				// engram_embedder_fallback_total{from,to,reason} increments
				// every time chain falls through to the paid fallback.
				embedchain.WithObserver(embedchain.NewPrometheusObserver()),
			)
			if chainErr == nil {
				embedder = chain
			} else {
				embedder = primary
			}
		} else {
			embedder = primary
		}
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
