package sqlitevec_test

import (
	"context"
	"math"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/nfsarch33/engram/internal/adapters/vectorstore/sqlitevec"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

func vec(vals ...float32) []float32 { return vals }

func open(t *testing.T, path string) *sqlitevec.Store {
	t.Helper()
	s, err := sqlitevec.Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func ids(results []engram.VectorResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, string(r.ID))
	}
	sort.Strings(out)
	return out
}

// TestReopen_RestoresTheIndexWithoutReembedding is the reason the adapter
// exists: what was upserted before a restart is searchable after it, with
// the same score, and LoadStats says how many rows came back.
func TestReopen_RestoresTheIndexWithoutReembedding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vectors.db")

	first, err := sqlitevec.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first.EnsureCollection(ctx, "c", 3) //nolint:errcheck
	err = first.UpsertBatch(ctx, []engram.VectorRecord{
		{ID: "dogs", Vector: vec(1, 0, 0), Payload: map[string]any{"workspace_id": "w", "text": "about dogs"}},
		{ID: "cats", Vector: vec(0, 1, 0), Payload: map[string]any{"workspace_id": "w", "text": "about cats"}},
	})
	if err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := open(t, path)
	second.EnsureCollection(ctx, "c", 3) //nolint:errcheck
	loaded, evicted := second.LoadStats()
	if loaded != 2 || evicted != 0 {
		t.Fatalf("LoadStats after reopen: loaded=%d evicted=%d, want 2/0", loaded, evicted)
	}

	got, err := second.Search(ctx, engram.VectorQuery{Vector: vec(1, 0, 0), TopK: 1, Filters: map[string]any{"workspace_id": "w"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].ID != "dogs" {
		t.Fatalf("after reopen: want dogs, got %+v", got)
	}
	if math.Abs(float64(got[0].Score-1.0)) > 0.001 {
		t.Fatalf("restored vector must score ~1.0 on itself, got %f", got[0].Score)
	}
	if got[0].Payload["text"] != "about dogs" {
		t.Fatalf("payload must survive the restart, got %+v", got[0].Payload)
	}
}

// TestDelete_IsDurable: a deleted record does not come back after reopen.
func TestDelete_IsDurable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vectors.db")

	first, _ := sqlitevec.Open(path)
	first.EnsureCollection(ctx, "c", 2)           //nolint:errcheck
	first.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: "keep", Vector: vec(1, 0)},
		{ID: "drop", Vector: vec(0, 1)},
	})
	if err := first.DeleteBatch(ctx, []engram.MemoryID{"drop", "never-existed"}); err != nil {
		t.Fatalf("DeleteBatch: %v", err)
	}
	first.Close()

	second := open(t, path)
	second.EnsureCollection(ctx, "c", 2) //nolint:errcheck
	got, _ := second.IndexedIDs(ctx)
	if len(got) != 1 || got[0] != "keep" {
		t.Fatalf("after reopen: want [keep], got %v", got)
	}
}

// TestUpsert_OverwriteIsDurable: the second write wins on disk too.
func TestUpsert_OverwriteIsDurable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vectors.db")

	first, _ := sqlitevec.Open(path)
	first.EnsureCollection(ctx, "c", 2)                                                       //nolint:errcheck
	first.UpsertBatch(ctx, []engram.VectorRecord{{ID: "x", Vector: vec(1, 0)}})               //nolint:errcheck
	first.UpsertBatch(ctx, []engram.VectorRecord{{ID: "x", Vector: vec(0, 1), Payload: nil}}) //nolint:errcheck
	first.Close()

	second := open(t, path)
	second.EnsureCollection(ctx, "c", 2) //nolint:errcheck
	got, _ := second.Search(ctx, engram.VectorQuery{Vector: vec(0, 1), TopK: 1})
	if len(got) != 1 || got[0].ID != "x" || math.Abs(float64(got[0].Score-1.0)) > 0.001 {
		t.Fatalf("overwritten vector must be the durable one: got %+v", got)
	}
	if second.Len() != 1 {
		t.Fatalf("Len: want 1, got %d", second.Len())
	}
}

// TestEnsureCollection_EvictsDimensionMismatches: rows written by a
// different embedder dimension are dropped from the served index (and
// counted) rather than scored over mismatched lengths, and they are no
// longer reported as indexed so a reindex re-embeds them.
func TestEnsureCollection_EvictsDimensionMismatches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vectors.db")

	first, _ := sqlitevec.Open(path)
	first.EnsureCollection(ctx, "c", 3)           //nolint:errcheck
	first.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: "old-model", Vector: vec(1, 0, 0)},
	})
	first.Close()

	second := open(t, path)
	second.EnsureCollection(ctx, "c", 4) //nolint:errcheck
	loaded, evicted := second.LoadStats()
	if loaded != 1 || evicted != 1 {
		t.Fatalf("LoadStats: loaded=%d evicted=%d, want 1/1", loaded, evicted)
	}
	got, _ := second.IndexedIDs(ctx)
	if len(got) != 0 {
		t.Fatalf("evicted row must not be reported as indexed: %v", got)
	}
	// Re-embedding at the new dimension brings it back, durably.
	second.UpsertBatch(ctx, []engram.VectorRecord{{ID: "old-model", Vector: vec(1, 0, 0, 0)}}) //nolint:errcheck
	second.Close()

	third := open(t, path)
	third.EnsureCollection(ctx, "c", 4) //nolint:errcheck
	loaded, evicted = third.LoadStats()
	if loaded != 1 || evicted != 0 {
		t.Fatalf("after re-embed: loaded=%d evicted=%d, want 1/0", loaded, evicted)
	}
}

// TestFilters_DelegateToTheIndex: scoping is enforced through this adapter
// exactly as on the in-memory one.
func TestFilters_DelegateToTheIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t, sqlitevec.MemoryPath)
	s.EnsureCollection(ctx, "c", 2)           //nolint:errcheck
	s.UpsertBatch(ctx, []engram.VectorRecord{ //nolint:errcheck
		{ID: "a", Vector: vec(1, 0), Payload: map[string]any{"workspace_id": "a"}},
		{ID: "b", Vector: vec(1, 0), Payload: map[string]any{"workspace_id": "b"}},
	})
	got, err := s.Search(ctx, engram.VectorQuery{Vector: vec(1, 0), TopK: 5, Filters: map[string]any{"workspace_id": "b"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if want := []string{"b"}; !equal(ids(got), want) {
		t.Fatalf("filter leaked: got %v want %v", ids(got), want)
	}
}

// TestMemoryPath_IsNotDurable documents the escape hatch: ":memory:" stores
// vanish with the handle, which is what tests want and production must not.
func TestMemoryPath_IsNotDurable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t, sqlitevec.MemoryPath)
	s.EnsureCollection(ctx, "c", 2)                                                 //nolint:errcheck
	s.UpsertBatch(ctx, []engram.VectorRecord{{ID: "ephemeral", Vector: vec(1, 0)}}) //nolint:errcheck
	if s.Len() != 1 {
		t.Fatalf("Len: want 1, got %d", s.Len())
	}
	loaded, _ := s.LoadStats()
	if loaded != 0 {
		t.Fatalf("a fresh memory store must load 0 rows, got %d", loaded)
	}
}

// TestOpen_RejectsEmptyPath and an unopenable directory fail at Open, not
// on the first write.
func TestOpen_RejectsBadPaths(t *testing.T) {
	t.Parallel()
	if _, err := sqlitevec.Open(""); err == nil {
		t.Fatal("empty path must be rejected")
	}
	if s, err := sqlitevec.Open(filepath.Join(t.TempDir(), "missing", "dir", "v.db")); err == nil {
		s.Close()
		t.Fatal("a path inside a missing directory must be rejected")
	}
}

// TestConcurrentUpsertAndSearch: write-through serialises writers while
// readers keep being served; nothing is lost and the race detector is happy.
func TestConcurrentUpsertAndSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "vectors.db"))
	s.EnsureCollection(ctx, "c", 2) //nolint:errcheck

	const n = 40
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := engram.MemoryID(engram.NewMemoryID())
			if err := s.UpsertBatch(ctx, []engram.VectorRecord{{ID: id, Vector: vec(1, float32(i))}}); err != nil {
				t.Errorf("UpsertBatch: %v", err)
			}
			if _, err := s.Search(ctx, engram.VectorQuery{Vector: vec(1, 0), TopK: 3}); err != nil {
				t.Errorf("Search: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if s.Len() != n {
		t.Fatalf("Len: want %d, got %d", n, s.Len())
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
