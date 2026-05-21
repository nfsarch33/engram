# Mem0 OSS Wire-Format Fixtures

JSON request bodies captured from the Mem0 OSS reference (`mem0/server`) and
the `mem0-mcp-go` Go client's `internal/mem0/client.go`. These are the exact
shapes a downstream caller (Mem0 OSS UI, mem0-mcp-go MCP wrapper, or curl
script) sends; the shim must accept them byte-for-byte and respond with the
shapes documented here.

## Request Shapes

| File | Path | Method | Notes |
|---|---|---|---|
| `add_request.json` | `/memories` | POST | message list of `{role, content}` |
| `search_request.json` | `/search` | POST | filters dict, top-level `user_id`/`app_id` rejected by Mem0 OSS but tolerated here |
| `update_request.json` | `/memories/{id}` | PUT | text-only payload |

## Response Shapes (asserted in `contract_test.go`)

- `POST /memories` -> `{"results": [{"id": "...", "memory": "...", "event": "ADD"}, ...]}`
- `POST /search` -> array of `{"id": "...", "memory": "...", "score": float}`
- `GET /memories/{id}` -> `{"id": "...", "memory": "...", "user_id": "...", ...}`
- `PUT /memories/{id}` -> `{"message": "Memory updated successfully!", ...}`
- `DELETE /memories/{id}` -> `{"message": "Memory deleted successfully!"}`
- `GET /memories/{id}/history` -> array of `{"id": "...", "event": "ADD|UPDATE|DELETE", "memory": "..."}`
- `GET /healthz` -> `ok` (text/plain)
- `GET /auth/setup-status` -> `{"is_configured": true}`

## Source Notes

- mem0-mcp-go client decodes search responses as both array (Mem0 OSS) and
  `{"results": [...]}` (managed Mem0). The shim emits the array form; the
  reuse audit at `docs/mem0-mcp-go-reuse-audit.md` documents this.
- The X-API-Key header is required when the daemon is started with
  `ENGRAM_API_KEY` set; `/healthz` and `/auth/setup-status` bypass the gate.
