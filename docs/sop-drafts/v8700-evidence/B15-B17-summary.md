# Engram v8700 B15-B17 Evidence Summary

Sprint: v8700 Block 2 (overnight 2026-05-23)
Branch: `feat/v8700-engram-race-doctor`
Base: `origin/master @ 27704bc`

## B15: `go test -race -count=1 ./...` clean

19 packages, all pass under `-race`:

```
ok  github.com/nfsarch33/engram/cmd/engramcli                       2.373s
ok  github.com/nfsarch33/engram/cmd/engramd                         2.883s
ok  github.com/nfsarch33/engram/internal/adapters/embeddings/chain  3.121s
ok  github.com/nfsarch33/engram/internal/adapters/embeddings/minimax  8.317s
ok  github.com/nfsarch33/engram/internal/adapters/embeddings/ollama 2.065s
ok  github.com/nfsarch33/engram/internal/adapters/embeddings/openai 6.116s
ok  github.com/nfsarch33/engram/internal/adapters/history/sqlite    7.901s
ok  github.com/nfsarch33/engram/internal/adapters/httpapi           4.087s
ok  github.com/nfsarch33/engram/internal/adapters/httpapi/mem0compat 4.997s
ok  github.com/nfsarch33/engram/internal/adapters/httputil          4.430s
ok  github.com/nfsarch33/engram/internal/adapters/llm/openai        6.680s
ok  github.com/nfsarch33/engram/internal/adapters/mcp               9.157s
ok  github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem 7.019s
ok  github.com/nfsarch33/engram/internal/adapters/vectorstore/qdrant 5.597s
ok  github.com/nfsarch33/engram/internal/app/engramsvc              8.966s
ok  github.com/nfsarch33/engram/internal/cache                      7.766s
ok  github.com/nfsarch33/engram/internal/config                     7.403s
ok  github.com/nfsarch33/engram/internal/domain/engram              7.663s
ok  github.com/nfsarch33/engram/internal/integration                7.721s
?   github.com/nfsarch33/engram/pkg/engram                          [no test files]
```

Full log: `B15-engram-race-clean.log`.

## B16: `engramcli doctor`

Doctor command already implemented at `cmd/engramcli/doctor.go` with three
checks (healthz, search via embedder, /memories write+cleanup) and an
exit-code-driven summary. `cmd/engramcli/doctor_test.go` covers the
table-driven success / partial-failure paths. No changes required for
v8700-B16.

## B17: cross-compile

Built with `CGO_ENABLED=0`, `-trimpath`, `-ldflags="-s -w -X main.version=v8700"`.

Outputs (under `~/runs/v8700/`):

```
6ff2858e4e03c0fb1c980fd980f4a626a90bd5b049beb2a2ed3d68b930eb4c83  engram-darwin-arm64/engramcli
44a6186691d8452c493e857bc8fb31a0a50c94d63ce0906d5d247702ccb37218  engram-darwin-arm64/engramd
0b13f4519f07424440e61caea2da9c664f7c4b6b3d2f21f079b7e46ff82b0297  engram-windows-amd64/engramcli.exe
c6c80fa4503c591d9c5ed3f7262ab85eae2d342836a958f3835a9a46f4edef63  engram-windows-amd64/engramd.exe
```

Toolchain: `GOTOOLCHAIN=auto`, host `go1.25.6 darwin/arm64` (engram go.mod
requires >= 1.26.3, fetched on demand).
