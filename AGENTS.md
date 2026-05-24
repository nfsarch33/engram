# Engram -- Agent Guidelines

- Repo: `https://github.com/nfsarch33/engram`
- **Purpose**: Personal Go memory engine replacing Mem0 OSS (ADR-063). Hexagonal
  architecture with HTTP API, MCP server, mem0-compat shim, CLI, and migration tools.
- **Status**: PRIMARY memory engine (switched 2026-05-24). Mem0 OSS is RETIRED.

## Build & Test

```bash
make check          # fmt + vet + test-race
make test-race      # go test -race -count=1 ./...
make build-linux    # cross-compile linux/amd64
make docker-build   # Docker image
```

CI runs on GitLab (`.gitlab-ci.yml`): vet, race tests, linux cross-build.

## Architecture

Hexagonal (ports & adapters):
- `internal/domain/engram/` -- core domain types and port interfaces
- `internal/app/engramsvc/` -- service orchestration (add, search, get, delete)
- `internal/adapters/httpapi/` -- HTTP JSON API (:8280)
- `internal/adapters/httpapi/mem0compat/` -- Mem0 wire-compatible shim (:8281)
- `internal/adapters/mcp/` -- MCP stdio server (11 tools)
- `internal/adapters/embedder/` -- Ollama/MiniMax/chain embedders
- `internal/adapters/llm/` -- OpenAI-compatible LLM client (infer path)
- `internal/adapters/sqlite/` -- SQLite history journal
- `internal/adapters/vectorstore/` -- in-memory + Qdrant vector stores
- `cmd/engramd/` -- daemon entry point
- `cmd/engramcli/` -- CLI (doctor, add, search, get, delete, migrate, shadow-write)

## Config

All via `ENGRAM_*` environment variables. See `internal/config/config.go`.
Max 3 direct Go dependencies (SQLite, ULID, mcp-go).

## Coding Conventions

- Go, strict typing. `go vet` + `go test -race` before commit.
- No secrets in committed files. Use `.env.template` pattern.
- Conventional commits: `type(scope): message`.
- No AI attribution in any artifact.
- No fleet hostnames, personal paths, or internal IPs in tracked files (PUBLIC repo).

## Identity

- Personal repos: `nfsarch33`
- NEVER use Zendesk identity for this repo.
