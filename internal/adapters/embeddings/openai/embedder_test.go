package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	embed "github.com/nfsarch33/engram/internal/adapters/embeddings/openai"
)

func makeServer(t *testing.T, statusCode int, resp any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(resp)
	}))
}

func buildResponse(embeddings [][]float32) map[string]any {
	data := make([]map[string]any, len(embeddings))
	for i, vec := range embeddings {
		floats := make([]any, len(vec))
		for j, f := range vec {
			floats[j] = float64(f)
		}
		data[i] = map[string]any{
			"object":    "embedding",
			"index":     i,
			"embedding": floats,
		}
	}
	return map[string]any{
		"object": "list",
		"data":   data,
		"model":  "text-embedding-3-small",
		"usage":  map[string]any{"prompt_tokens": 10, "total_tokens": 10},
	}
}

func TestEmbedBatch_Single(t *testing.T) {
	t.Parallel()
	want := []float32{0.1, 0.2, 0.3}
	srv := makeServer(t, http.StatusOK, buildResponse([][]float32{want}))
	defer srv.Close()

	e := embed.New(embed.Options{
		BaseURL: srv.URL,
		APIKey:  "sk-test",
		Model:   "text-embedding-3-small",
		Dim:     3,
	})

	vecs, err := e.EmbedBatch(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("want 1 vector, got %d", len(vecs))
	}
	for i, v := range vecs[0] {
		if abs32(v-want[i]) > 1e-5 {
			t.Errorf("vec[%d]: want %f, got %f", i, want[i], v)
		}
	}
}

func TestEmbedBatch_Multiple(t *testing.T) {
	t.Parallel()
	wants := [][]float32{
		{1.0, 0.0},
		{0.0, 1.0},
	}
	srv := makeServer(t, http.StatusOK, buildResponse(wants))
	defer srv.Close()

	e := embed.New(embed.Options{
		BaseURL: srv.URL,
		APIKey:  "sk-test",
		Model:   "text-embedding-3-small",
		Dim:     2,
	})

	vecs, err := e.EmbedBatch(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("want 2 vectors, got %d", len(vecs))
	}
}

func TestEmbedBatch_Empty(t *testing.T) {
	t.Parallel()
	e := embed.New(embed.Options{
		BaseURL: "http://unused",
		APIKey:  "sk-test",
		Model:   "text-embedding-3-small",
		Dim:     3,
	})
	vecs, err := e.EmbedBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("want 0 vectors, got %d", len(vecs))
	}
}

func TestEmbedBatch_ServerError(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusInternalServerError, map[string]any{
		"error": map[string]any{"message": "internal server error", "type": "server_error"},
	})
	defer srv.Close()

	e := embed.New(embed.Options{
		BaseURL: srv.URL,
		APIKey:  "sk-test",
		Model:   "text-embedding-3-small",
		Dim:     3,
	})
	_, err := e.EmbedBatch(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error on 5xx response")
	}
}

func TestEmbedBatch_Unauthorized(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusUnauthorized, map[string]any{
		"error": map[string]any{"message": "invalid api key", "type": "invalid_request_error"},
	})
	defer srv.Close()

	e := embed.New(embed.Options{
		BaseURL: srv.URL,
		APIKey:  "bad-key",
		Model:   "text-embedding-3-small",
		Dim:     3,
	})
	_, err := e.EmbedBatch(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error on 401 response")
	}
}

func TestEmbedBatch_ContextCancelled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// never respond
		<-r.Context().Done()
	}))
	defer srv.Close()

	e := embed.New(embed.Options{
		BaseURL: srv.URL,
		APIKey:  "sk-test",
		Model:   "text-embedding-3-small",
		Dim:     3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.EmbedBatch(ctx, []string{"hello"})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestNew_DefaultModel(t *testing.T) {
	t.Parallel()
	// New with zero Model should not panic and should use a default
	e := embed.New(embed.Options{
		BaseURL: "http://unused",
		APIKey:  "sk-test",
	})
	if e == nil {
		t.Fatal("expected non-nil embedder")
	}
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
