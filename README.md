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

Engram is a personal-fleet replacement for self-hosted memory services. It
provides:

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

Engram intentionally avoids any reference to other memory systems in code:
no symbol or string contains the word `mem0`. The repo is a drop-in
foundation for personal agents only.

## Quickstart (local, no embedder)

```bash
make build
./bin/engramd --no-embed &
./bin/engramcli health
./bin/engramcli add --user-id u1 --message user:"I like Go"
# search needs an embedder; with --no-embed it errors loudly
```

## Quickstart (live, Ollama on wsl1)

```bash
export ENGRAM_EMBED_URL="http://wsl1:11434/v1"
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

## MCP stdio server

Run the daemon in MCP stdio mode (no HTTP) for direct integration with an
agent runtime:

```bash
./bin/engramd --mcp-stdio --no-http
```

Six tools are exposed:

- `engram_add(messages, user_id, agent_id, run_id, app_id, workspace_id, infer)`
- `engram_search(query, user_id, ..., top_k)`
- `engram_get(id)`
- `engram_update(id, text)`
- `engram_delete(id)`
- `engram_history(id)`

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
make build-linux     # GOOS=linux GOARCH=amd64 cross-compile (for wsl1 deploy)
make docker-build    # docker build -t engramd:<version> .
make check           # fmt + vet + test-race (CI gate)
```

Live integration tests against an Ollama embedder are gated behind the
`integration` build tag and skip unless `ENGRAM_LIVE_OLLAMA_URL` is set:

```bash
ENGRAM_LIVE_OLLAMA_URL=http://wsl1:11434 \
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

## wsl1 deployment

Daemon installed as a systemd unit. See `scripts/install-engramd-wsl1.sh`
for the canonical install procedure (cross-compile, scp, register, start).
The runbook is documented at
`~/Code/global-kb/sop/engram-memory-engine.md`.

## Constraints (do not regress)

- Zero `mem0` terms in any `.go` file.
- Three direct dependencies maximum. Any new direct dep needs an ADR under
  `~/Code/global-kb/adrs/`.
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
