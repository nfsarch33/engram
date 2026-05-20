package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/engram/internal/adapters/history/sqlite"
	"github.com/nfsarch33/engram/internal/adapters/httpapi"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// stubEmbedder returns fixed-dimension zero vectors.
type stubEmbedder struct{ dim int }

func (e *stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, e.dim)
		out[i][0] = float32(i + 1) // non-zero so cosine works
	}
	return out, nil
}

func makeServer(t *testing.T) *httptest.Server {
	t.Helper()
	hist, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	vec, _ := inmem.NewStore()
	emb := &stubEmbedder{dim: 8}
	svc, err := engramsvc.NewService(vec, hist, nil, emb, engramsvc.Config{
		CollectionName: "test", EmbeddingDim: 8,
	})
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	t.Cleanup(func() { hist.Close() })

	handler := httpapi.NewHandler(svc)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b)) //nolint:noctx
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func getJSON(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// --- POST /memories ---------------------------------------------------------

func TestAddMemory_Success(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)

	resp := postJSON(t, srv.URL+"/memories", map[string]any{
		"messages": []string{"user likes Go", "user dislikes PHP"},
		"user_id":  "u1",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("want 201, got %d", resp.StatusCode)
	}

	var result []engram.MemoryRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("want 2 records, got %d", len(result))
	}
}

func TestAddMemory_EmptyMessages(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	resp := postJSON(t, srv.URL+"/memories", map[string]any{"messages": []string{}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestAddMemory_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	resp, err := http.Post(srv.URL+"/memories", "application/json", bytes.NewReader([]byte("not json"))) //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// --- POST /search -----------------------------------------------------------

func TestSearch_Success(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)

	// Seed a memory first.
	postJSON(t, srv.URL+"/memories", map[string]any{
		"messages": []string{"python is great"},
		"user_id":  "u1",
	}).Body.Close()

	resp := postJSON(t, srv.URL+"/search", map[string]any{
		"query":   "python",
		"user_id": "u1",
		"top_k":   5,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	resp := postJSON(t, srv.URL+"/search", map[string]any{"top_k": 5})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// --- GET /memories/{id} -----------------------------------------------------

func TestGetMemory_Success(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)

	addResp := postJSON(t, srv.URL+"/memories", map[string]any{
		"messages": []string{"test memory"},
		"user_id":  "u1",
	})
	var records []engram.MemoryRecord
	json.NewDecoder(addResp.Body).Decode(&records) //nolint:errcheck
	addResp.Body.Close()

	if len(records) == 0 {
		t.Fatal("no records from add")
	}

	resp := getJSON(t, srv.URL+"/memories/"+string(records[0].ID))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

func TestGetMemory_NotFound(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	resp := getJSON(t, srv.URL+"/memories/01ARZ3NDEKTSV4RRFFQ69G5FAV")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// --- PUT /memories/{id} -----------------------------------------------------

func TestUpdateMemory_Success(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)

	addResp := postJSON(t, srv.URL+"/memories", map[string]any{
		"messages": []string{"old text"},
		"user_id":  "u1",
	})
	var records []engram.MemoryRecord
	json.NewDecoder(addResp.Body).Decode(&records) //nolint:errcheck
	addResp.Body.Close()

	b, _ := json.Marshal(map[string]any{"text": "new text"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/memories/"+string(records[0].ID), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

// --- DELETE /memories/{id} --------------------------------------------------

func TestDeleteMemory_Success(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)

	addResp := postJSON(t, srv.URL+"/memories", map[string]any{
		"messages": []string{"to delete"},
		"user_id":  "u1",
	})
	var records []engram.MemoryRecord
	json.NewDecoder(addResp.Body).Decode(&records) //nolint:errcheck
	addResp.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/memories/"+string(records[0].ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("want 204, got %d", resp.StatusCode)
	}

	// Second get should 404.
	resp2 := getJSON(t, srv.URL+"/memories/"+string(records[0].ID))
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("want 404 after delete, got %d", resp2.StatusCode)
	}
}

// --- GET /memories/{id}/history ---------------------------------------------

func TestGetHistory_Success(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)

	addResp := postJSON(t, srv.URL+"/memories", map[string]any{
		"messages": []string{"mem"},
		"user_id":  "u1",
	})
	var records []engram.MemoryRecord
	json.NewDecoder(addResp.Body).Decode(&records) //nolint:errcheck
	addResp.Body.Close()

	resp := getJSON(t, srv.URL+"/memories/"+string(records[0].ID)+"/history")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}
