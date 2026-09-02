# Changelog

## [Unreleased]

### Fixed

- Write path: `engram_add` (MCP) and `POST /memories` accept messages as
  chat-style `{role, content}` / `{text}` objects as well as bare strings.
  Objects were silently dropped, so every writer using that shape failed with
  "text must not be empty" while search kept working.
- Vector search honours scope filters (`user_id`, `agent_id`, `workspace_id`)
  on the in-memory store; a workspace-scoped query no longer returns other
  workspaces' records. Equal scores order deterministically by id.
- History listings order by insertion sequence (rowid) instead of
  `created_at`, so a host clock that steps backwards cannot reorder history.
- CI: golangci-lint pinned to a version built with a Go new enough for
  `go.mod`; the unpinned action failed to load and left every downstream job
  "skipped".

### Added

- Durable vector index (`vectorstore/sqlitevec`, the default): vectors are
  written through to `ENGRAM_VECTOR_DB_PATH` and restored at boot, so a
  restart neither empties semantic search nor re-spends embedding calls.
  `ENGRAM_VECTOR_STORE=inmem` keeps the previous process-memory behaviour.
- `engramd --reindex-missing`: embeds and indexes only the history records
  missing from the vector index (restore from backup, embedder change).
- Metrics `engram_vector_indexed_count` and `engram_vector_index_gap`.
- Live recall evaluation (`internal/integration`, build tag `integration`,
  gated on `ENGRAM_LIVE_URL`): a fixed corpus scored as recall@5 / MRR@5
  with a `rubric_version` envelope written to `ENGRAM_RECALL_OUT`.

## [1.0.0] - 2026-05-25

Engram is now the PRIMARY personal memory engine, replacing Mem0 OSS
(retired per ADR-063, 2026-05-24).

### Highlights

- Production-stable systemd daemon on the target host with hardened unit file
- Mem0 OSS wire-compatible HTTP shim (`--mem0-compat` on :8281)
- MCP stdio server (`--mcp-stdio`) with full tool surface
- ULID-based memory IDs with per-user namespace isolation
- Hybrid vector search: in-memory (default) or Qdrant-backed
- Embeddings via Ollama nomic-embed-text (768-dim, zero API cost)
- LLM-powered fact extraction (`infer:true`) via qwen2.5:3b-instruct
- SQLite-backed history with full change audit trail
- Remote tunnel access via SSH port forward (loopback only)

### Architecture

- Go hexagonal architecture (domain/ports/adapters)
- Adapters: embeddings (OpenAI-compat + chain fallback), vectorstore
  (in-memory + Qdrant), history (SQLite), LLM (OpenAI-compat), HTTP API,
  Mem0-compat shim, MCP stdio
- Zero external dependencies beyond Ollama for embeddings/LLM

### Configuration

Environment variables (via `/etc/engramd/engramd.env`):

| Variable | Purpose |
|----------|---------|
| `ENGRAM_ADDR` | Native HTTP API bind address |
| `ENGRAM_MEM0COMPAT_ADDR` | Mem0-compat shim bind address |
| `ENGRAM_DB_PATH` | SQLite database path |
| `ENGRAM_EMBED_URL` | Embedding endpoint (OpenAI-compatible) |
| `ENGRAM_EMBED_MODEL` | Embedding model name |
| `ENGRAM_LLM_URL` | LLM endpoint for infer path |
| `ENGRAM_LLM_MODEL` | LLM model for fact extraction |
| `ENGRAM_TIMEOUT` | Request timeout (default 30s, prod 120s) |

### MCP Tools

| Tool | Description |
|------|-------------|
| `engram_add` | Store a memory with optional infer |
| `engram_search` | Semantic vector search |
| `engram_get` | Retrieve all memories for a user |
| `engram_update` | Update existing memory by ID |
| `engram_delete` | Delete memory by ID |
| `engram_history` | Audit trail for a memory ID |
| `engram_doctor` | Health check and diagnostics |

Legacy `mem0_*` aliases route through the compat shim for backward
compatibility with existing agent rules and workflows.

### Known Limitations

- `infer:true` path requires warm Ollama model; cold-start latency
  (~28s for qwen2.5:3b-instruct) can exceed client-side timeouts.
  Recommendation: use vLLM router (port 8787) for production infer
  workloads once ENGRAM_LLM_URL is pointed there.
- ~32 historical Mem0 memories not migrated (auth-blocked; non-critical).
- Mem0 Docker stack decommission scheduled after 2026-05-27.

### Migration from Mem0 OSS

1. Engram daemon replaces the Mem0 Docker stack (PostgreSQL + pgvector)
2. Mem0-compat shim accepts identical HTTP payloads on :8281
3. MCP tool aliases (`mem0_add` -> `engram_add`) maintain compatibility
4. Agent rules updated to reference Engram as PRIMARY (ADR-063)
