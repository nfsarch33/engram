package ollama_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/engram/internal/adapters/embeddings/ollama"
)

func makeServer(t *testing.T, statusCode int, resp any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/embed" {
			t.Errorf("want /api/embed, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestEmbedBatch_Single(t *testing.T) {
	t.Parallel()
	want := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	srv := makeServer(t, http.StatusOK, map[string]any{
		"embeddings": [][]float32{want},
	})
	defer srv.Close()

	e := ollama.New(ollama.Options{
		BaseURL: srv.URL,
		Model:   "nomic-embed-text",
	})

	vecs, err := e.EmbedBatch(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("want 1 vector, got %d", len(vecs))
	}
	if len(vecs[0]) != 5 {
		t.Fatalf("want dim=5, got %d", len(vecs[0]))
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
		{1.0, 0.0, 0.5},
		{0.0, 1.0, 0.5},
	}
	srv := makeServer(t, http.StatusOK, map[string]any{
		"embeddings": wants,
	})
	defer srv.Close()

	e := ollama.New(ollama.Options{BaseURL: srv.URL})
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
	e := ollama.New(ollama.Options{BaseURL: "http://unused"})
	vecs, err := e.EmbedBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("want 0 vectors, got %d", len(vecs))
	}
}

func TestEmbedBatch_ModelNotFound(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusNotFound, map[string]any{
		"error": "model 'nomic-embed-text' not found",
	})
	defer srv.Close()

	e := ollama.New(ollama.Options{BaseURL: srv.URL})
	_, err := e.EmbedBatch(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error on 404 response")
	}
}

func TestEmbedBatch_ServerError(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusInternalServerError, map[string]any{
		"error": "internal server error",
	})
	defer srv.Close()

	e := ollama.New(ollama.Options{BaseURL: srv.URL})
	_, err := e.EmbedBatch(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestEmbedBatch_ContextCancelled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	e := ollama.New(ollama.Options{BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.EmbedBatch(ctx, []string{"hello"})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestEmbedBatch_CountMismatch(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusOK, map[string]any{
		"embeddings": [][]float32{{0.1, 0.2}},
	})
	defer srv.Close()

	e := ollama.New(ollama.Options{BaseURL: srv.URL})
	_, err := e.EmbedBatch(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error on count mismatch")
	}
}

func TestEmbedBatch_RequestBody(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1}},
		})
	}))
	defer srv.Close()

	e := ollama.New(ollama.Options{BaseURL: srv.URL, Model: "nomic-embed-text"})
	_, err := e.EmbedBatch(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["model"] != "nomic-embed-text" {
		t.Errorf("want model=nomic-embed-text, got %v", gotBody["model"])
	}
}

func TestNew_Defaults(t *testing.T) {
	t.Parallel()
	e := ollama.New(ollama.Options{})
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
