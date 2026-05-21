//go:build integration

// Package integration contains live, environment-gated tests that require an
// external embedder service. The default `go test ./...` run skips this file
// because of the `integration` build tag.
//
// To run:
//
//	ENGRAM_LIVE_OLLAMA_URL=http://wsl1:11434 \
//	  go test -tags integration -race -count=1 \
//	    ./internal/integration/...
//
// The test exercises the full Engram stack against a real Ollama embedder
// reachable at $ENGRAM_LIVE_OLLAMA_URL. It seeds three deliberately-distinct
// memories and asserts the most-relevant memory is recalled at rank 1
// (recall@1 == 1) for an associated query.
package integration_test

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	embedopenai "github.com/nfsarch33/engram/internal/adapters/embeddings/openai"
	"github.com/nfsarch33/engram/internal/adapters/history/sqlite"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/app/engramsvc"
)

const (
	liveEmbedURLEnv   = "ENGRAM_LIVE_OLLAMA_URL"
	liveEmbedModelEnv = "ENGRAM_LIVE_OLLAMA_MODEL"
	liveEmbedDimEnv   = "ENGRAM_LIVE_OLLAMA_DIM"
)

// resolveOllamaBaseURL appends `/v1` if the env value is the bare Ollama
// host so callers can pass either form ergonomically.
func resolveOllamaBaseURL(raw string) string {
	if raw == "" {
		return ""
	}
	// trim trailing slash for normalisation.
	for len(raw) > 0 && raw[len(raw)-1] == '/' {
		raw = raw[:len(raw)-1]
	}
	if len(raw) >= 3 && raw[len(raw)-3:] == "/v1" {
		return raw
	}
	return raw + "/v1"
}

// preflight short-circuits the test cleanly when the operator's network does
// not reach the live embedder. We probe /v1/models with a tight deadline so
// CI on isolated runners reports SKIP instead of timing out for minutes.
func preflight(t *testing.T, baseURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		t.Skipf("live embedder URL %q invalid: %v", baseURL, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("live embedder %q not reachable: %v", baseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		t.Skipf("live embedder %q returned %d", baseURL, resp.StatusCode)
	}
}

func newLiveService(t *testing.T) *engramsvc.Service {
	t.Helper()
	rawURL := os.Getenv(liveEmbedURLEnv)
	if rawURL == "" {
		t.Skipf("%s not set; skipping live Ollama integration", liveEmbedURLEnv)
	}
	baseURL := resolveOllamaBaseURL(rawURL)
	preflight(t, baseURL)

	model := os.Getenv(liveEmbedModelEnv)
	if model == "" {
		model = "nomic-embed-text"
	}
	dim := 768
	if v := os.Getenv(liveEmbedDimEnv); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			t.Fatalf("invalid %s=%q: %v", liveEmbedDimEnv, v, err)
		}
		dim = n
	}

	hist, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("sqlite store: %v", err)
	}
	t.Cleanup(func() { hist.Close() })

	vec, err := inmem.NewStore()
	if err != nil {
		t.Fatalf("vector store: %v", err)
	}

	embedder := embedopenai.New(embedopenai.Options{
		BaseURL: baseURL,
		Model:   model,
		Dim:     dim,
		Timeout: 30 * time.Second,
	})

	svc, err := engramsvc.NewService(vec, hist, nil, embedder, engramsvc.Config{
		CollectionName: "engram-live-test",
		EmbeddingDim:   dim,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc
}

// TestLiveOllama_AddSearchRecallAtOne is the canonical acceptance test for
// the live embedder integration. It seeds three semantically-disjoint
// memories and asserts that searching for a synonym of one of them returns
// that memory at rank 1.
func TestLiveOllama_AddSearchRecallAtOne(t *testing.T) {
	svc := newLiveService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Three distinct topics so the closest match is unambiguous.
	memories := []string{
		"user: I love programming in Go and writing concurrent systems.",
		"user: My favourite cuisine is Japanese; I cook ramen at home.",
		"user: I trail-run on weekends and just bought new running shoes.",
	}

	for _, m := range memories {
		recs, err := svc.Add(ctx, engramsvc.AddRequest{
			Messages: []string{m},
			UserID:   "live-test-user",
		})
		if err != nil {
			t.Fatalf("add %q: %v", m, err)
		}
		if len(recs) == 0 {
			t.Fatalf("add %q: returned no records", m)
		}
	}

	// Query worded very differently from the seeded text but semantically
	// closest to the Go/programming entry.
	out, err := svc.Search(ctx, engramsvc.SearchRequest{
		Query:  "favourite programming language for backend services",
		UserID: "live-test-user",
		TopK:   3,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("search: expected at least one result")
	}

	top := out[0].Record.Text
	if top != memories[0] {
		t.Errorf("recall@1 mismatch: want %q, got %q (full results: %d items)",
			memories[0], top, len(out))
		for i, r := range out {
			t.Logf("  rank %d: score=%.4f text=%q", i+1, r.Score, r.Record.Text)
		}
	}
}
