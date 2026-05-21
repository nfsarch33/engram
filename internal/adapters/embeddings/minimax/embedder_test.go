package minimax_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/engram/internal/adapters/embeddings/minimax"
)

func makeServer(t *testing.T, statusCode int, resp any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if r.URL.Path != "/embeddings" {
			t.Errorf("want /embeddings, got %s", r.URL.Path)
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
			"index":     i,
			"embedding": floats,
		}
	}
	return map[string]any{"data": data}
}

func TestEmbedBatch_Single(t *testing.T) {
	t.Parallel()
	want := []float32{0.1, 0.2, 0.3}
	srv := makeServer(t, http.StatusOK, buildResponse([][]float32{want}))
	defer srv.Close()

	e := minimax.New(minimax.Options{
		BaseURL: srv.URL,
		APIKey:  "test-key",
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

	e := minimax.New(minimax.Options{BaseURL: srv.URL, APIKey: "test-key"})
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
	e := minimax.New(minimax.Options{BaseURL: "http://unused", APIKey: "test-key"})
	vecs, err := e.EmbedBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("want 0 vectors, got %d", len(vecs))
	}
}

func TestEmbedBatch_Unauthorized(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusUnauthorized, map[string]any{
		"base_resp": map[string]any{
			"status_code": 1004,
			"status_msg":  "invalid api key",
		},
	})
	defer srv.Close()

	e := minimax.New(minimax.Options{BaseURL: srv.URL, APIKey: "bad-key"})
	_, err := e.EmbedBatch(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error on 401 response")
	}
}

func TestEmbedBatch_ServerError(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusInternalServerError, map[string]any{
		"base_resp": map[string]any{
			"status_code": 5000,
			"status_msg":  "internal error",
		},
	})
	defer srv.Close()

	e := minimax.New(minimax.Options{BaseURL: srv.URL, APIKey: "test-key"})
	_, err := e.EmbedBatch(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestEmbedBatch_AuthHeader(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(buildResponse([][]float32{{0.1}}))
	}))
	defer srv.Close()

	e := minimax.New(minimax.Options{BaseURL: srv.URL, APIKey: "my-secret-key"})
	_, err := e.EmbedBatch(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer my-secret-key" {
		t.Errorf("want Authorization=Bearer my-secret-key, got %q", gotAuth)
	}
}

func TestEmbedBatch_RequestBody(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(buildResponse([][]float32{{0.1}}))
	}))
	defer srv.Close()

	e := minimax.New(minimax.Options{BaseURL: srv.URL, APIKey: "k", Model: "embo-01"})
	_, err := e.EmbedBatch(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["model"] != "embo-01" {
		t.Errorf("want model=embo-01, got %v", gotBody["model"])
	}
	if gotBody["type"] != "db" {
		t.Errorf("want type=db, got %v", gotBody["type"])
	}
}

func TestEmbedBatch_ContextCancelled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	e := minimax.New(minimax.Options{BaseURL: srv.URL, APIKey: "k"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.EmbedBatch(ctx, []string{"hello"})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestNew_Defaults(t *testing.T) {
	t.Parallel()
	e := minimax.New(minimax.Options{APIKey: "k"})
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
