package inmem_test

import (
	"context"
	"math"
	"testing"

	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

func newStore(t *testing.T) *inmem.Store {
	t.Helper()
	s, err := inmem.NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func vec(vals ...float32) []float32 { return vals }

// --- EnsureCollection -------------------------------------------------------

func TestEnsureCollection(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.EnsureCollection(context.Background(), "test", 3); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}
}

func TestEnsureCollection_Idempotent(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c1", 4) //nolint:errcheck
	if err := s.EnsureCollection(ctx, "c1", 4); err != nil {
		t.Error("second EnsureCollection call should be a no-op")
	}
}

// --- UpsertBatch / Search ---------------------------------------------------

func TestUpsertAndSearch(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 3) //nolint:errcheck

	records := []engram.VectorRecord{
		{ID: "id1", Vector: vec(1, 0, 0), Payload: map[string]any{"text": "about dogs"}},
		{ID: "id2", Vector: vec(0, 1, 0), Payload: map[string]any{"text": "about cats"}},
		{ID: "id3", Vector: vec(0, 0, 1), Payload: map[string]any{"text": "about fish"}},
	}
	if err := s.UpsertBatch(ctx, records); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}

	// Query close to "dogs"
	results, err := s.Search(ctx, engram.VectorQuery{
		Vector: vec(1, 0, 0),
		TopK:   2,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].ID != "id1" {
		t.Errorf("top result should be id1 (exact match), got %s", results[0].ID)
	}
	if math.Abs(float64(results[0].Score-1.0)) > 0.001 {
		t.Errorf("exact match should score ~1.0, got %f", results[0].Score)
	}
}

func TestSearch_TopKLargerThanStore(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck
	s.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: "a", Vector: vec(1, 0)},
	})
	results, err := s.Search(ctx, engram.VectorQuery{Vector: vec(1, 0), TopK: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("want 1, got %d", len(results))
	}
}

func TestSearch_EmptyStore(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 3) //nolint:errcheck
	results, err := s.Search(ctx, engram.VectorQuery{Vector: vec(1, 0, 0), TopK: 5})
	if err != nil {
		t.Fatalf("Search on empty store: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("want 0 results, got %d", len(results))
	}
}

// --- UpsertBatch overwrites --------------------------------------------------

func TestUpsert_Overwrite(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck
	s.UpsertBatch(ctx, []engram.VectorRecord{{ID: "x", Vector: vec(1, 0)}}) //nolint:errcheck
	// Overwrite with different vector.
	s.UpsertBatch(ctx, []engram.VectorRecord{{ID: "x", Vector: vec(0, 1)}}) //nolint:errcheck

	results, _ := s.Search(ctx, engram.VectorQuery{Vector: vec(0, 1), TopK: 1})
	if len(results) == 0 || results[0].ID != "x" {
		t.Error("upsert should overwrite existing record")
	}
	if math.Abs(float64(results[0].Score-1.0)) > 0.001 {
		t.Errorf("overwritten vector score: want ~1.0, got %f", results[0].Score)
	}
}

// --- DeleteBatch ------------------------------------------------------------

func TestDeleteBatch(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck
	s.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: "a", Vector: vec(1, 0)},
		{ID: "b", Vector: vec(0, 1)},
	})
	if err := s.DeleteBatch(ctx, []engram.MemoryID{"a"}); err != nil {
		t.Fatalf("DeleteBatch: %v", err)
	}
	results, _ := s.Search(ctx, engram.VectorQuery{Vector: vec(1, 0), TopK: 5})
	for _, r := range results {
		if r.ID == "a" {
			t.Error("deleted record should not appear in search results")
		}
	}
}

func TestDeleteBatch_UnknownID_NoError(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck
	// Deleting non-existent IDs should succeed silently.
	if err := s.DeleteBatch(ctx, []engram.MemoryID{"nope"}); err != nil {
		t.Errorf("DeleteBatch with unknown ID should not error: %v", err)
	}
}

// --- Concurrency ------------------------------------------------------------

func TestConcurrentUpsert(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 3) //nolint:errcheck

	done := make(chan struct{}, 30)
	for i := 0; i < 30; i++ {
		id := engram.NewMemoryID()
		go func(id engram.MemoryID) {
			s.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
				{ID: id, Vector: vec(1, 0, 0)},
			})
			done <- struct{}{}
		}(id)
	}
	for i := 0; i < 30; i++ {
		<-done
	}
	results, _ := s.Search(ctx, engram.VectorQuery{Vector: vec(1, 0, 0), TopK: 50})
	if len(results) != 30 {
		t.Errorf("concurrent upsert: want 30, got %d", len(results))
	}
}
