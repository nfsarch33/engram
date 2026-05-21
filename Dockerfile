# syntax=docker/dockerfile:1.7

# Multi-stage build for the Engram daemon.
# - Stage 1: cross-compile a static linux/amd64 binary using the canonical
#   GVM go1.26.3 toolchain image.
# - Stage 2: distroless static-debian12-nonroot for minimal attack surface.
#
# Build:   docker build -t engramd:<version> .
# Run:     docker run --rm -p 8280:8280 \
#            -e ENGRAM_DB_PATH=/data/engram.db \
#            -v engram-data:/data engramd:<version>

FROM golang:1.26.3-bookworm AS builder

WORKDIR /src

# Copy module manifests first for cache-friendly dep download.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOFLAGS="-trimpath"

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/root/go/pkg/mod \
    go build \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/engramd \
        ./cmd/engramd && \
    go build \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/engramcli \
        ./cmd/engramcli

# ---- Runtime image ----
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Engram persists SQLite + cache to /data; mount a volume here in production.
USER nonroot:nonroot

COPY --from=builder --chown=nonroot:nonroot /out/engramd /app/engramd
COPY --from=builder --chown=nonroot:nonroot /out/engramcli /app/engramcli

ENV ENGRAM_ADDR=":8280" \
    ENGRAM_MEM0COMPAT_ADDR=":8281" \
    ENGRAM_DB_PATH="/data/engram.db" \
    ENGRAM_LOG_LEVEL="info"

EXPOSE 8280 8281

# Daemon respects SIGTERM/SIGINT for graceful shutdown.
ENTRYPOINT ["/app/engramd"]
