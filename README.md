# Engram

Personal Go memory engine for agentic systems. Hexagonal architecture with
zero CGO (`modernc.org/sqlite`), ULID v2 identifiers, and a thin
HTTP + MCP surface so any agent — Claude Code, Cursor, IronClaw, custom
Go services — can persist and recall memory through the same canonical
endpoints.

- Module: `github.com/nfsarch33/engram`
- Go: 1.26.3 (GVM canonical)
- Direct deps: `github.com/mark3labs/mcp-go`, `github.com/oklog/ulid/v2`,
  `modernc.org/sqlite`
- License: personal-use; not for redistribution.

## What is Engram

Engram is a replacement for self-hosted memory services. It provides:

- A **service core** (`internal/app/engramsvc`) modelled on add / search /
  get / update / delete / history, plus an MCP-shaped tool surface.
- A **history adapter** backed by SQLite (`internal/adapters/history/sqlite`).
- A **vector store** with two adapters: in-memory (default) and Qdrant
  (`internal/adapters/vectorstore/{inmem,qdrant}`), the latter implemented
  with pure `net/http` so the three-direct-deps cap holds.
- An **embedder adapter** speaking the OpenAI `/v1/embeddings` shape, which
  also matches Ollama's OpenAI-compatible endpoint
  (`internal/adapters/embeddings/openai`).
- An **HTTP API** at port 8280 and an **MCP stdio server** for direct
  agent integration (`internal/adapters/{httpapi,mcp}`).
- A reference **CLI** at `cmd/engramcli` for human-driven smoke tests.

The MCP adapter includes optional backward-compatible aliases for agents
migrating from other memory services. The canonical tools use the
`engram_` prefix.

## Quickstart (local, no embedder)

```bash
make build
./bin/engramd --no-embed &
./bin/engramcli health
./bin/engramcli add --user-id u1 --message user:"I like Go"
# search needs an embedder; with --no-embed it errors loudly
```

## Quickstart (live, Ollama embedder)

```bash
export ENGRAM_EMBED_URL="http://<your-ollama-host>:11434/v1"
export ENGRAM_EMBED_MODEL="nomic-embed-text"
export ENGRAM_EMBEDDING_DIM=768
export ENGRAM_DB_PATH="$HOME/.engram/engram.db"
mkdir -p "$(dirname "$ENGRAM_DB_PATH")"
./bin/engramd &
./bin/engramcli add --user-id u1 --message user:"I like Go"
./bin/engramcli search --user-id u1 --query "favourite language"
```

## Configuration

All configuration is via `ENGRAM_*` environment variables.

| Variable | Default | Description |
|---|---|---|
| `ENGRAM_ADDR` | `:8280` | HTTP listen address. |
| `ENGRAM_DB_PATH` | `engram.db` | SQLite history DB path. |
| `ENGRAM_COLLECTION` | `engram` | Vector collection name. |
| `ENGRAM_EMBEDDING_DIM` | `1536` | Embedding dimension. Use `768` for Ollama `nomic-embed-text`. |
| `ENGRAM_EMBED_URL` | (empty) | OpenAI-compatible `/v1/embeddings` base URL. Empty = no embedder. |
| `ENGRAM_EMBED_MODEL` | `text-embedding-3-small` | Embedder model name. |
| `ENGRAM_EMBED_KEY` | (empty) | Embedder API key, if required. |
| `ENGRAM_LLM_URL` | (empty) | OpenAI-compatible chat completions base URL. |
| `ENGRAM_LLM_KEY` | (empty) | LLM API key. |
| `ENGRAM_LLM_MODEL` | `gpt-4o-mini` | LLM model name. |
| `ENGRAM_TIMEOUT` | `30s` | Per-request timeout. |
| `ENGRAM_LOG_LEVEL` | `info` | slog level. |
| `ENGRAM_MEM0COMPAT_ADDR` | `:8281` | Mem0-compat HTTP shim listen address (requires `--mem0-compat`). |
| `ENGRAM_API_KEY` | (empty) | API key gate for mem0-compat shim (`X-API-Key` header). |
| `ENGRAM_QDRANT_URL` | (empty) | Qdrant HTTP URL. When set, vectors persist in Qdrant instead of in-memory. |
| `ENGRAM_QDRANT_KEY` | (empty) | Qdrant API key. |
| `ENGRAM_EMBED_FALLBACK_URL` | (empty) | Fallback embedder URL. Creates a chain: primary then fallback. |
| `ENGRAM_EMBED_FALLBACK_MODEL` | `embo-01` | Fallback embedder model. |
| `ENGRAM_EMBED_FALLBACK_KEY` | (empty) | Fallback embedder API key. |

## HTTP API

Listening on `ENGRAM_ADDR` (default `:8280`):

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/memories` | Add memories (`{messages, user_id, ...}`). |
| `POST` | `/search` | Semantic search (`{query, user_id, top_k}`). |
| `GET`  | `/memories/{id}` | Fetch one memory. |
| `PUT`  | `/memories/{id}` | Update text. |
| `DELETE` | `/memories/{id}` | Delete. |
| `GET`  | `/memories/{id}/history` | List change events. |
| `GET`  | `/healthz` | Liveness probe. |

Errors map to: 400 (`ErrEmptyText`, `ErrInvalidTopK`), 404 (`ErrNotFound`),
500 (anything else).

## Mem0-compatible HTTP shim

Run with `--mem0-compat` to serve a second HTTP listener on
`ENGRAM_MEM0COMPAT_ADDR` (default `:8281`) that accepts the same request
shapes as Mem0 OSS. Existing agents wired to Mem0 can switch to Engram with
zero config changes beyond the endpoint URL.

```bash
./bin/engramd --mem0-compat
# Canonical API on :8280, Mem0-compat shim on :8281
```

Auth uses `X-API-Key` header with the value from `ENGRAM_API_KEY`.

## MCP stdio server

Run the daemon in MCP stdio mode (no HTTP) for direct integration with an
agent runtime:

```bash
./bin/engramd --mcp-stdio --no-http
```

Eleven tools are exposed (6 canonical + 5 backward-compatible aliases):

- `engram_add(messages, user_id, agent_id, run_id, app_id, workspace_id, infer)`
- `engram_search(query, user_id, ..., top_k)`
- `engram_get(id)`
- `engram_update(id, text)`
- `engram_delete(id)`
- `engram_history(id)`

Backward-compatible aliases (for agents migrating from other memory services):

- `mem0_add` -> `engram_add`
- `mem0_search` -> `engram_search`
- `mem0_get_all` -> list all (filter by user/agent/app/run/workspace)
- `mem0_delete` -> `engram_delete`
- `mem0_doctor` -> health check

To run HTTP and MCP at the same time, omit `--no-http`:

```bash
./bin/engramd --mcp-stdio
```

## CLI

```bash
engramcli health
engramcli add --user-id u1 --agent-id ax --message user:"I like Go" --message assistant:"Got it"
engramcli search --user-id u1 --query "Go" --top-k 5
engramcli get --id 01HK...
engramcli delete --id 01HK...
```

`ENGRAM_ADDR` (or the daemon's actual listen address) is honoured by the
CLI.

## Build and test

```bash
make build           # ./bin/engramd, ./bin/engramcli
make test            # go test ./...
make test-race       # go test -race ./...
make build-linux     # GOOS=linux GOARCH=amd64 cross-compile
make docker-build    # docker build -t engramd:<version> .
make check           # fmt + vet + test-race (CI gate)
```

Live integration tests against an Ollama embedder are gated behind the
`integration` build tag and skip unless `ENGRAM_LIVE_OLLAMA_URL` is set:

```bash
ENGRAM_LIVE_OLLAMA_URL=http://<your-ollama-host>:11434 \
  go test -tags integration -race -count=1 ./internal/integration/...
```

## Docker

```bash
make docker-build
docker run --rm -p 8280:8280 \
  -e ENGRAM_DB_PATH=/data/engram.db \
  -e ENGRAM_ADDR=:8280 \
  -v engram-data:/data \
  engramd:<version>
```

For local development with optional Qdrant:

```bash
docker compose up -d            # engramd only
docker compose --profile qdrant up -d   # engramd + qdrant
```

## Remote deployment

Daemon can be installed as a systemd unit on any Linux host. See
`scripts/install-engramd.sh` for the canonical install procedure
(cross-compile, copy binary, register systemd unit, start). The script
is idempotent and configurable via environment variables.

## Constraints (do not regress)

- Backward-compatible aliases use `mem0_` prefix only in the MCP adapter
  surface; core domain code avoids the term.
- Three direct dependencies maximum. Any new direct dep needs an ADR.
- `crypto/rand` for ULID entropy, never `math/rand`.
- `SetMaxOpenConns(1)` on `:memory:` SQLite stores.
- All public API takes `context.Context` as the first argument.
- Logging via `log/slog` only.
- TDD: tests before implementation; race-clean is a hard gate.

## Layout

```
cmd/
  engramd/          # daemon (HTTP + MCP stdio)
  engramcli/        # reference CLI
internal/
  domain/engram/    # ports, types, errors
  app/engramsvc/    # service core (orchestration)
  adapters/
    embeddings/     # openai-compatible (works with Ollama)
    history/sqlite/ # change-event journal
    httpapi/        # HTTP handlers
    llm/openai/     # LLM client (for infer=true)
    mcp/            # MCP tool adapter + stdio server
    vectorstore/    # inmem + qdrant
  cache/            # LRU+TTL cache
  config/           # env config loader
  integration/      # end-to-end tests
pkg/engram/         # public SDK (re-exports)
```
