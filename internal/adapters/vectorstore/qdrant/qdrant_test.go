package qdrant_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nfsarch33/engram/internal/adapters/vectorstore/qdrant"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// mockQdrant is an in-memory Qdrant-like HTTP server for adapter tests.
type mockQdrant struct {
	mu          sync.Mutex
	collections map[string]int       // name -> dim
	points      map[string]mockPoint // string-ID -> point
	returnErr   bool                 // if true, return 500 on all requests
}

type mockPoint struct {
	id      string
	vector  []float32
	payload map[string]any
}

func newMock(t *testing.T) (*mockQdrant, *httptest.Server) {
	t.Helper()
	m := &mockQdrant{
		collections: make(map[string]int),
		points:      make(map[string]mockPoint),
	}
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	return m, srv
}

func (m *mockQdrant) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if m.returnErr {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": "mock forced error"}) //nolint:errcheck
		return
	}

	p := r.URL.Path

	switch {
	// PUT /collections/{name}   (create collection — no /points suffix)
	case r.Method == http.MethodPut && isCollectionRoot(p):
		var body struct {
			Vectors struct{ Size int `json:"size"` } `json:"vectors"`
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		name := p[len("/collections/"):]
		m.collections[name] = body.Vectors.Size
		json.NewEncoder(w).Encode(map[string]any{"result": true, "status": "ok"}) //nolint:errcheck

	// PUT /collections/{name}/points  (upsert)
	case r.Method == http.MethodPut && hasSuffix(p, "/points"):
		var body struct {
			Points []struct {
				ID      string         `json:"id"`
				Vector  []float32      `json:"vector"`
				Payload map[string]any `json:"payload"`
			} `json:"points"`
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		for _, pt := range body.Points {
			m.points[pt.ID] = mockPoint{id: pt.ID, vector: pt.Vector, payload: pt.Payload}
		}
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"result": map[string]any{"operation_id": 0, "status": "completed"},
			"status": "ok",
		})

	// POST /collections/{name}/points/search
	case r.Method == http.MethodPost && hasSuffix(p, "/points/search"):
		var body struct {
			Vector      []float32      `json:"vector"`
			Limit       int            `json:"limit"`
			WithPayload bool           `json:"with_payload"`
			Filter      map[string]any `json:"filter"`
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

		type hit struct {
			ID      string         `json:"id"`
			Score   float32        `json:"score"`
			Payload map[string]any `json:"payload,omitempty"`
		}
		var hits []hit
		for _, pt := range m.points {
			if !matchesFilter(pt.payload, body.Filter) {
				continue
			}
			score := mockCosineSim(body.Vector, pt.vector)
			var p map[string]any
			if body.WithPayload {
				p = pt.payload
			}
			hits = append(hits, hit{ID: pt.id, Score: score, Payload: p})
		}
		// Sort descending by score.
		for i := 1; i < len(hits); i++ {
			for j := i; j > 0 && hits[j].Score > hits[j-1].Score; j-- {
				hits[j], hits[j-1] = hits[j-1], hits[j]
			}
		}
		if body.Limit > 0 && len(hits) > body.Limit {
			hits = hits[:body.Limit]
		}
		json.NewEncoder(w).Encode(map[string]any{"result": hits, "status": "ok"}) //nolint:errcheck

	// POST /collections/{name}/points/delete
	case r.Method == http.MethodPost && hasSuffix(p, "/points/delete"):
		var body struct {
			Points []string `json:"points"`
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		for _, id := range body.Points {
			delete(m.points, id)
		}
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"result": map[string]any{"operation_id": 1, "status": "completed"},
			"status": "ok",
		})

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func isCollectionRoot(path string) bool {
	if len(path) <= len("/collections/") {
		return false
	}
	tail := path[len("/collections/"):]
	for _, c := range tail {
		if c == '/' {
			return false
		}
	}
	return true
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func matchesFilter(payload map[string]any, filter map[string]any) bool {
	if filter == nil {
		return true
	}
	must, ok := filter["must"].([]any)
	if !ok {
		return true
	}
	for _, clause := range must {
		m, ok := clause.(map[string]any)
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		matchMap, _ := m["match"].(map[string]any)
		if payload[key] != matchMap["value"] {
			return false
		}
	}
	return true
}

func mockCosineSim(a, b []float32) float32 {
	var dot, na, nb float64
	for i := range a {
		if i >= len(b) {
			break
		}
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(na) * math.Sqrt(nb)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}

func newStore(t *testing.T, srv *httptest.Server) *qdrant.Store {
	t.Helper()
	return qdrant.New(qdrant.Options{BaseURL: srv.URL})
}

// ---- tests ----

func TestEnsureCollection(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	if err := s.EnsureCollection(context.Background(), "test", 4); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}
}

func TestEnsureCollection_Idempotent(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	if err := s.EnsureCollection(ctx, "col", 4); err != nil {
		t.Fatalf("first EnsureCollection: %v", err)
	}
	if err := s.EnsureCollection(ctx, "col", 4); err != nil {
		t.Errorf("second EnsureCollection (idempotent): %v", err)
	}
}

func TestUpsertBatch_Empty(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 3) //nolint:errcheck
	if err := s.UpsertBatch(ctx, nil); err != nil {
		t.Errorf("empty UpsertBatch should be no-op: %v", err)
	}
}

func TestUpsertAndSearch(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "mem", 3) //nolint:errcheck

	id1 := engram.NewMemoryID()
	id2 := engram.NewMemoryID()
	id3 := engram.NewMemoryID()
	records := []engram.VectorRecord{
		{ID: id1, Vector: []float32{1, 0, 0}, Payload: map[string]any{"user_id": "alice", "text": "dogs"}},
		{ID: id2, Vector: []float32{0, 1, 0}, Payload: map[string]any{"user_id": "alice", "text": "cats"}},
		{ID: id3, Vector: []float32{0, 0, 1}, Payload: map[string]any{"user_id": "bob", "text": "fish"}},
	}
	if err := s.UpsertBatch(ctx, records); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}

	results, err := s.Search(ctx, engram.VectorQuery{
		Vector: []float32{1, 0, 0}, TopK: 1, WithPayload: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].ID != id1 {
		t.Errorf("top result should be id1, got %s", results[0].ID)
	}
}

func TestSearch_EmptyStore(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 3) //nolint:errcheck

	results, err := s.Search(ctx, engram.VectorQuery{
		Vector: []float32{1, 0, 0}, TopK: 5, WithPayload: true,
	})
	if err != nil {
		t.Fatalf("Search on empty store: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("want 0 results, got %d", len(results))
	}
}

func TestSearch_WithFilter(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "m", 2) //nolint:errcheck

	idAlice := engram.NewMemoryID()
	idBob := engram.NewMemoryID()
	s.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: idAlice, Vector: []float32{1, 0}, Payload: map[string]any{"user_id": "alice"}},
		{ID: idBob, Vector: []float32{1, 0}, Payload: map[string]any{"user_id": "bob"}},
	})

	results, err := s.Search(ctx, engram.VectorQuery{
		Vector:      []float32{1, 0},
		TopK:        5,
		WithPayload: true,
		Filters:     map[string]any{"user_id": "alice"},
	})
	if err != nil {
		t.Fatalf("Search with filter: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result for alice filter, got %d", len(results))
	}
	if results[0].ID != idAlice {
		t.Errorf("expected alice's record, got %s", results[0].ID)
	}
}

func TestUpsert_Overwrite(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck

	id := engram.NewMemoryID()
	s.UpsertBatch(ctx, []engram.VectorRecord{{ID: id, Vector: []float32{1, 0}}}) //nolint:errcheck
	s.UpsertBatch(ctx, []engram.VectorRecord{{ID: id, Vector: []float32{0, 1}}}) //nolint:errcheck

	results, err := s.Search(ctx, engram.VectorQuery{
		Vector: []float32{0, 1}, TopK: 1, WithPayload: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].ID != id {
		t.Errorf("overwritten record not found or wrong ID: %v", results)
	}
}

func TestDeleteBatch(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck

	idA := engram.NewMemoryID()
	idB := engram.NewMemoryID()
	s.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: idA, Vector: []float32{1, 0}},
		{ID: idB, Vector: []float32{0, 1}},
	})

	if err := s.DeleteBatch(ctx, []engram.MemoryID{idA}); err != nil {
		t.Fatalf("DeleteBatch: %v", err)
	}

	results, _ := s.Search(ctx, engram.VectorQuery{
		Vector: []float32{1, 0}, TopK: 5, WithPayload: true,
	})
	for _, r := range results {
		if r.ID == idA {
			t.Error("deleted record should not appear in search results")
		}
	}
}

func TestDeleteBatch_UnknownID_NoError(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck
	if err := s.DeleteBatch(ctx, []engram.MemoryID{engram.NewMemoryID()}); err != nil {
		t.Errorf("DeleteBatch with unknown ID should not error: %v", err)
	}
}

func TestConcurrentUpsert(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck

	const n = 10
	var wg sync.WaitGroup
	var errCount atomic.Int64
	wg.Add(n)
	for i := 0; i < n; i++ {
		id := engram.NewMemoryID()
		go func(id engram.MemoryID) {
			defer wg.Done()
			if err := s.UpsertBatch(ctx, []engram.VectorRecord{
				{ID: id, Vector: []float32{1, 0}},
			}); err != nil {
				errCount.Add(1)
			}
		}(id)
	}
	wg.Wait()
	if errCount.Load() > 0 {
		t.Errorf("concurrent upsert: %d errors", errCount.Load())
	}
}

func TestEnsureCollection_HTTPError(t *testing.T) {
	t.Parallel()
	m, srv := newMock(t)
	m.returnErr = true
	s := newStore(t, srv)
	if err := s.EnsureCollection(context.Background(), "fail", 4); err == nil {
		t.Error("expected error from HTTP 500, got nil")
	}
}

func TestUpsertBatch_HTTPError(t *testing.T) {
	t.Parallel()
	m, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck
	m.returnErr = true
	if err := s.UpsertBatch(ctx, []engram.VectorRecord{
		{ID: engram.NewMemoryID(), Vector: []float32{1, 0}},
	}); err == nil {
		t.Error("expected error from HTTP 500, got nil")
	}
}

func TestSearch_HTTPError(t *testing.T) {
	t.Parallel()
	m, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck
	m.returnErr = true
	if _, err := s.Search(ctx, engram.VectorQuery{
		Vector: []float32{1, 0}, TopK: 1,
	}); err == nil {
		t.Error("expected error from HTTP 500, got nil")
	}
}

func TestDeleteBatch_HTTPError(t *testing.T) {
	t.Parallel()
	m, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck
	m.returnErr = true
	if err := s.DeleteBatch(ctx, []engram.MemoryID{engram.NewMemoryID()}); err == nil {
		t.Error("expected error from HTTP 500, got nil")
	}
}

func TestSearch_InternalKeyStripped(t *testing.T) {
	t.Parallel()
	_, srv := newMock(t)
	s := newStore(t, srv)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck

	id := engram.NewMemoryID()
	s.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: id, Vector: []float32{1, 0}, Payload: map[string]any{"text": "hello"}},
	})

	results, err := s.Search(ctx, engram.VectorQuery{
		Vector: []float32{1, 0}, TopK: 1, WithPayload: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected 1 result")
	}
	// Internal _engram_id key must not appear in returned payload.
	if _, ok := results[0].Payload["_engram_id"]; ok {
		t.Error("internal _engram_id should be stripped from returned payload")
	}
	if results[0].Payload["text"] != "hello" {
		t.Errorf("user payload 'text' should be preserved, got %v", results[0].Payload)
	}
}
