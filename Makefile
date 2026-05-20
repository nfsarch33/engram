MODULE := github.com/nfsarch33/engram
GO     := go
GOFLAGS :=

BIN_DIR := bin
DAEMON  := $(BIN_DIR)/engramd
CLI     := $(BIN_DIR)/engramcli

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: all build daemon cli test test-race lint vet fmt clean install

all: build

build: daemon cli

daemon:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DAEMON) ./cmd/engramd

cli:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(CLI) ./cmd/engramcli

test:
	$(GO) test -count=1 ./...

test-race:
	$(GO) test -race -count=1 ./...

test-race3:
	$(GO) test -race -count=3 ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

lint: vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed, skipping"

# Cross-compile for linux/amd64 (CI/Docker target)
build-linux:
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/engramd-linux-amd64 ./cmd/engramd
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/engramcli-linux-amd64 ./cmd/engramcli

install:
	$(GO) install $(GOFLAGS) -ldflags "$(LDFLAGS)" ./cmd/engramd
	$(GO) install $(GOFLAGS) -ldflags "$(LDFLAGS)" ./cmd/engramcli

clean:
	rm -rf $(BIN_DIR)

# Docker: build + run the daemon in a container
docker-build:
	docker build -t engramd:$(VERSION) .

docker-run:
	docker run --rm -p 8280:8280 \
		-e ENGRAM_DB_PATH=/data/engram.db \
		-e ENGRAM_ADDR=:8280 \
		-v engram-data:/data \
		engramd:$(VERSION)

# check = fmt + vet + test (mirrors CI gate)
check: fmt vet test-race
