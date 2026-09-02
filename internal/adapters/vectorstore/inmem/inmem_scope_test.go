package inmem_test

import (
	"context"
	"testing"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// TestSearch_FiltersScopeResults is the tenant-isolation contract of the
// VectorStore port pinned on the in-memory adapter: a query scoped to one
// workspace must never surface another workspace's record, however close its
// vector is. Before this test the adapter ignored q.Filters entirely.
func TestSearch_FiltersScopeResults(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck

	err := s.UpsertBatch(ctx, []engram.VectorRecord{
		{ID: "ws-a-1", Vector: vec(1, 0), Payload: map[string]any{"workspace_id": "a", "user_id": "u"}},
		{ID: "ws-b-1", Vector: vec(1, 0), Payload: map[string]any{"workspace_id": "b", "user_id": "u"}},
		{ID: "no-ws", Vector: vec(1, 0), Payload: map[string]any{"user_id": "u"}},
	})
	if err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}

	got, err := s.Search(ctx, engram.VectorQuery{
		Vector:  vec(1, 0),
		TopK:    10,
		Filters: map[string]any{"workspace_id": "a"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].ID != "ws-a-1" {
		t.Fatalf("workspace filter leaked: got %+v, want only ws-a-1", got)
	}

	// Two filters are ANDed; a record lacking a filtered key never matches.
	got, err = s.Search(ctx, engram.VectorQuery{
		Vector:  vec(1, 0),
		TopK:    10,
		Filters: map[string]any{"workspace_id": "b", "user_id": "u"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].ID != "ws-b-1" {
		t.Fatalf("ANDed filters: got %+v, want only ws-b-1", got)
	}

	// No filters: legacy callers still see everything.
	got, err = s.Search(ctx, engram.VectorQuery{Vector: vec(1, 0), TopK: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("unfiltered search: want 3, got %d", len(got))
	}
}

// TestSearch_FilterValueMismatchTypeDoesNotPanic: a filter carrying a
// non-comparable value must yield no match, not a panic in the goroutine.
func TestSearch_FilterValueMismatchTypeDoesNotPanic(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2)           //nolint:errcheck
	s.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: "x", Vector: vec(1, 0), Payload: map[string]any{"tags": []string{"a"}}},
	})
	got, err := s.Search(ctx, engram.VectorQuery{
		Vector:  vec(1, 0),
		TopK:    5,
		Filters: map[string]any{"tags": map[string]any{"k": "v"}},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("mismatched filter type must not match: got %+v", got)
	}
}

// TestIndexedIDs_ReportsWhatTheIndexHolds pins the IndexInspector contract
// the service's reindex path and the gap metric depend on.
func TestIndexedIDs_ReportsWhatTheIndexHolds(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck

	ids, err := s.IndexedIDs(ctx)
	if err != nil {
		t.Fatalf("IndexedIDs: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("empty index: want 0 ids, got %d", len(ids))
	}

	s.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: "a", Vector: vec(1, 0)},
		{ID: "b", Vector: vec(0, 1)},
	})
	s.DeleteBatch(ctx, []engram.MemoryID{"a"}) //nolint:errcheck

	ids, err = s.IndexedIDs(ctx)
	if err != nil {
		t.Fatalf("IndexedIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "b" {
		t.Fatalf("after upsert+delete: want [b], got %v", ids)
	}
	if s.Len() != 1 {
		t.Fatalf("Len: want 1, got %d", s.Len())
	}
}

// TestSearch_TiesAreDeterministic: equal scores order by ID so a fleet of
// callers replaying the same query see the same ranking every time.
func TestSearch_TiesAreDeterministic(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.EnsureCollection(ctx, "c", 2)           //nolint:errcheck
	s.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: "z", Vector: vec(1, 0)},
		{ID: "m", Vector: vec(1, 0)},
		{ID: "a", Vector: vec(1, 0)},
	})
	for i := 0; i < 5; i++ {
		got, err := s.Search(ctx, engram.VectorQuery{Vector: vec(1, 0), TopK: 3})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if got[0].ID != "a" || got[1].ID != "m" || got[2].ID != "z" {
			t.Fatalf("run %d: tie order must be by id, got %s %s %s", i, got[0].ID, got[1].ID, got[2].ID)
		}
	}
}
