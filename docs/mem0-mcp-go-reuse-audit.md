# mem0-mcp-go Reuse Audit for Engram

Source: the companion `mem0-mcp-go` project (see `github.com/nfsarch33/mem0-mcp-go`)

## Patterns Evaluated

### internal/config/ -- env var loading

**Pattern**: `Load() Config` reads `os.Getenv` with typed helpers (`getenvBool`, `getenvDuration`, `getenv`). No external dep.

**Engram adaptation**: Use same pattern for `ENGRAM_ADDR`, `ENGRAM_DB_PATH`, `ENGRAM_LLM_URL`, `ENGRAM_EMBED_URL`, etc. Reuse helper functions directly. No viper needed (keep it stdlib).

**Decision**: ADAPT -- copy the `getenv*` helpers, define `EngramConfig` struct.

---

### internal/cache/ -- LRU cache

**Pattern**: `container/list`-backed LRU with TTL. `Get(key string) ([]byte, bool)`, `Set(key string, val []byte)`. Thread-safe with `sync.Mutex`. ~100 lines, zero external deps.

**Engram use**: Cache embedding vectors by input text hash to avoid duplicate API calls. Cache search results (short TTL).

**Decision**: REUSE DIRECTLY -- copy `cache.go` into `internal/cache/cache.go`. No changes needed.

---

### internal/mem0/client.go -- HTTP client with retry

**Pattern**: `Client{httpClient *http.Client, baseURL, apiKey}` with `Options{Timeout, CacheMaxItems, CacheTTL}`. Uses `http.Client{Timeout: timeout}` directly -- no external retry lib. Request errors returned as-is; caller decides retry policy.

**Engram use**: LLM adapter and Embedding adapter need similar HTTP clients.

**Decision**: ADAPT -- use same `Options` struct pattern and `http.Client` directly. For LLM/embed adapters, add exponential backoff in the caller (no external dep needed).

---

### internal/outbox/ -- circuit breaker + NDJSON buffer

**Pattern**: `Outbox{filePath, upstream UpstreamWriter, cb *CircuitBreaker}`. On upstream failure, appends NDJSON to file. Background goroutine drains when circuit closes. Circuit breaker: `Open/Closed/HalfOpen` with `FailureThreshold` + `ResetTimeout`.

**Engram use**: Not needed in v1 (Engram is the database, not a client). Relevant for a future "sync to remote" feature.

**Decision**: DEFER -- not needed for v1 adapters.

---

### internal/metrics/ -- Prometheus counters

**Pattern**: `Collector` with `sync.Map` backing; `Counter(name)`, `Histogram(name)` methods. No `prometheus/client_golang` dep -- uses custom lightweight registry.

**Engram use**: Could add Prometheus metrics to HTTP handler and service layer.

**Decision**: DEFER -- add in a dedicated metrics sprint. v1 uses `log/slog` only.

---

### internal/middleware/ -- MCP request middleware

**Pattern**: Wraps `mcp.Server` handler; logs method/duration/error via `slog`. Thin wrapper.

**Engram use**: Engram MCP adapter handles this directly in `HandleTool`.

**Decision**: NOT NEEDED -- already covered.

---

### internal/tools/ -- MCP tool Registry

**Pattern**: `Registry{client, defaults}` with `Tools() []RegisteredTool`. `RegisteredTool{Tool mcp.Tool, Handler func}`. Separates tool definitions from handlers cleanly.

**Engram use**: Our MCP adapter already follows this pattern implicitly. Could extract to a `Registry` for consistency.

**Decision**: ALREADY IMPLEMENTED -- Engram MCP adapter mirrors this pattern.

---

### internal/clicmd/ -- flag-based CLI

**Pattern**: Subcommand dispatch via `os.Args[1]` string switch + `flag.FlagSet` per command. `Deps{Client, Config, Stdout, Stderr}` struct for testability. `Run(ctx, args, stdout, stderr) int` entry point for testing.

**Engram use**: `cmd/engramcli` can use this exact pattern (flag-based, no cobra needed, zero external deps).

**Decision**: ADAPT DIRECTLY -- copy the `Deps` + `Run()` pattern. Use `flag.FlagSet` per subcommand.

---

## Summary Table

| Package | Decision | Notes |
|---|---|---|
| `config` | ADAPT | Copy `getenv*` helpers, define `EngramConfig` |
| `cache` | REUSE | Copy as `internal/cache/cache.go` |
| `mem0/client` | ADAPT | Same HTTP client pattern for LLM/embed adapters |
| `outbox` | DEFER | Not needed in v1 |
| `metrics` | DEFER | Add in metrics sprint |
| `middleware` | NOT NEEDED | Already in HandleTool |
| `tools` | ALREADY DONE | MCP adapter mirrors this |
| `clicmd` | ADAPT | Use `Deps`+`Run()` pattern for `engramcli` |

## External Deps NOT Introduced

- No cobra (use `flag`)
- No viper (use `os.Getenv`)
- No prometheus/client_golang (defer)
- No retry library (manual exponential backoff)

This keeps Engram's direct dep count at 3 (mcp-go, ulid, sqlite).
