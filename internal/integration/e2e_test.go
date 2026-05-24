// Package integration contains full-stack end-to-end tests.
// They wire real adapters (SQLite, in-memory vector store, HTTP API) against
// a stub embedder so no external services are needed.
package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nfsarch33/engram/internal/adapters/history/sqlite"
	"github.com/nfsarch33/engram/internal/adapters/httpapi"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// --- stub embedder ----------------------------------------------------------

const testDim = 4

type deterministicEmbedder struct {
	mu    sync.Mutex
	calls int
}

func (e *deterministicEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	vecs := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, testDim)
		for j := range vec {
			vec[j] = float32(e.calls+i+1) * 0.1
		}
		vecs[i] = vec
		e.calls++
	}
	return vecs, nil
}

// --- test harness ------------------------------------------------------------

func buildStack(t *testing.T) *httptest.Server {
	t.Helper()
	hist, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("sqlite store: %v", err)
	}
	t.Cleanup(func() { hist.Close() })

	vec, err := inmem.NewStore()
	if err != nil {
		t.Fatalf("vector store: %v", err)
	}

	svc, err := engramsvc.NewService(vec, hist, nil, &deterministicEmbedder{}, engramsvc.Config{
		CollectionName: "test",
		EmbeddingDim:   testDim,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	handler := httpapi.NewHandler(svc)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// --- helpers ----------------------------------------------------------------

func post(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func get(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func del(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// addOne adds a single memory and returns its ID.
// POST /memories returns []MemoryRecord; we take the first.
func addOne(t *testing.T, srv *httptest.Server, userID, msg string) string {
	t.Helper()
	resp := post(t, srv, "/memories", map[string]any{
		"messages": []string{msg},
		"user_id":  userID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("add: want 201, got %d: %s", resp.StatusCode, body)
	}
	// response is []MemoryRecord -- take ID from first element
	var recs []map[string]any
	decodeJSON(t, resp, &recs)
	if len(recs) == 0 {
		t.Fatal("add: expected at least one record in response")
	}
	id, _ := recs[0]["id"].(string)
	if id == "" {
		t.Fatalf("add: expected id in first record, got %v", recs[0])
	}
	return id
}

// --- tests ------------------------------------------------------------------

// TestE2E_AddSearchRecall verifies the core happy path:
// add a memory, search for it, retrieve by ID.
func TestE2E_AddSearchRecall(t *testing.T) {
	srv := buildStack(t)

	id := addOne(t, srv, "alice", "user: I love Go programming")

	// Search -- returns []SearchResult{Record, Score}
	searchResp := post(t, srv, "/search", map[string]any{
		"query":   "Go programming",
		"user_id": "alice",
		"top_k":   5,
	})
	if searchResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(searchResp.Body)
		searchResp.Body.Close()
		t.Fatalf("search: want 200, got %d: %s", searchResp.StatusCode, body)
	}
	var searchOut []map[string]any
	decodeJSON(t, searchResp, &searchOut)
	if len(searchOut) == 0 {
		t.Fatal("search: expected at least one result")
	}

	// Get by ID -- returns MemoryRecord
	getResp := get(t, srv, "/memories/"+id)
	if getResp.StatusCode != http.StatusOK {
		getResp.Body.Close()
		t.Fatalf("get: want 200, got %d", getResp.StatusCode)
	}
	var rec map[string]any
	decodeJSON(t, getResp, &rec)
	if rec["id"] != id {
		t.Errorf("get: want id=%s, got %v", id, rec["id"])
	}
}

// TestE2E_UpdateDeleteHistory verifies the update/delete/history flows.
func TestE2E_UpdateDeleteHistory(t *testing.T) {
	srv := buildStack(t)
	id := addOne(t, srv, "bob", "user: I enjoy hiking")

	// Update
	updateReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/memories/"+id,
		bytes.NewBufferString(`{"text":"I enjoy trail running"}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("update request: %v", err)
	}
	if updateResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(updateResp.Body)
		updateResp.Body.Close()
		t.Fatalf("update: want 200, got %d: %s", updateResp.StatusCode, body)
	}
	updateResp.Body.Close()

	// History -- returns []MemoryEvent
	histResp := get(t, srv, fmt.Sprintf("/memories/%s/history", id))
	if histResp.StatusCode != http.StatusOK {
		t.Fatalf("history: want 200, got %d", histResp.StatusCode)
	}
	var events []map[string]any
	decodeJSON(t, histResp, &events)
	if len(events) == 0 {
		t.Error("history: expected at least one event")
	}

	// Delete
	delResp := del(t, srv, "/memories/"+id)
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d", delResp.StatusCode)
	}
	delResp.Body.Close()

	// Get after delete -> 404
	getResp := get(t, srv, "/memories/"+id)
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete: want 404, got %d", getResp.StatusCode)
	}
}

// TestE2E_MultiUserIsolation verifies memories can be added for multiple users.
func TestE2E_MultiUserIsolation(t *testing.T) {
	srv := buildStack(t)
	addOne(t, srv, "alice", "user: alice likes cats")
	addOne(t, srv, "bob", "user: bob likes dogs")

	// Search scoped to alice -- should succeed without error
	searchResp := post(t, srv, "/search", map[string]any{
		"query":   "pets",
		"user_id": "alice",
		"top_k":   10,
	})
	if searchResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(searchResp.Body)
		searchResp.Body.Close()
		t.Fatalf("search alice: want 200, got %d: %s", searchResp.StatusCode, body)
	}
	searchResp.Body.Close()
}

// TestE2E_Healthz confirms the health endpoint returns 200 with "ok".
func TestE2E_Healthz(t *testing.T) {
	srv := buildStack(t)
	resp := get(t, srv, "/healthz")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("healthz: want body=ok, got %q", body)
	}
}

// TestE2E_AddMultiple_ConcurrentWrites checks concurrent adds produce unique IDs.
func TestE2E_AddMultiple_ConcurrentWrites(t *testing.T) {
	srv := buildStack(t)
	const n = 10
	ids := make(chan string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp := post(t, srv, "/memories", map[string]any{
				"messages": []string{fmt.Sprintf("user: concurrent message %d", idx)},
				"user_id":  "concurrent-user",
			})
			if resp.StatusCode != http.StatusCreated {
				resp.Body.Close()
				t.Errorf("goroutine %d: want 201, got %d", idx, resp.StatusCode)
				return
			}
			var recs []map[string]any
			json.NewDecoder(resp.Body).Decode(&recs)
			resp.Body.Close()
			if len(recs) > 0 {
				if id, ok := recs[0]["id"].(string); ok {
					ids <- id
				}
			}
		}(i)
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]bool)
	for id := range ids {
		if seen[id] {
			t.Errorf("duplicate ID: %s", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Errorf("want %d unique IDs, got %d", n, len(seen))
	}
}

// TestE2E_GetNotFound returns 404 for a missing ID.
func TestE2E_GetNotFound(t *testing.T) {
	srv := buildStack(t)
	resp := get(t, srv, "/memories/"+string(engram.NewMemoryID()))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

// TestE2E_AddBadRequest returns 400 for empty messages.
func TestE2E_AddBadRequest(t *testing.T) {
	srv := buildStack(t)
	resp := post(t, srv, "/memories", map[string]any{
		"messages": []string{},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}
