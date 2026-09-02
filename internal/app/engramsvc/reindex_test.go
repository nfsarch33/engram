package engramsvc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// IndexedIDs makes the test vector store inspectable, as the durable and
// in-memory adapters are.
func (s *stubVectorStore) IndexedIDs(_ context.Context) ([]engram.MemoryID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]engram.MemoryID, 0, len(s.records))
	for _, r := range s.records {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// forget simulates the index being lost (a restart on a non-durable store)
// while history keeps every record.
func (s *stubVectorStore) forget() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = nil
}

// countingEmbedder counts calls so the test can prove a reindex spends
// embedding only on the gap.
type countingEmbedder struct {
	stubEmbedder
	calls int
	texts int
}

func (e *countingEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	e.calls++
	e.texts += len(texts)
	return e.stubEmbedder.EmbedBatch(ctx, texts)
}

func newReindexService(t *testing.T) (*engramsvc.Service, *stubVectorStore, *countingEmbedder) {
	t.Helper()
	vec := &stubVectorStore{}
	emb := &countingEmbedder{stubEmbedder: stubEmbedder{dim: 4}}
	svc, err := engramsvc.NewService(vec, newStubHistory(), nil, emb, engramsvc.Config{CollectionName: "t", EmbeddingDim: 4})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, vec, emb
}

// TestReindexMissing_RebuildsOnlyTheGap: after the index is lost, one
// ReindexMissing restores every record, a second one finds nothing, and the
// embedder was called for exactly the missing texts.
func TestReindexMissing_RebuildsOnlyTheGap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, vec, emb := newReindexService(t)

	for _, text := range []string{"alpha memory", "beta memory", "gamma memory"} {
		if _, err := svc.Add(ctx, engramsvc.AddRequest{Messages: []string{text}, UserID: "u"}); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	stats, err := svc.IndexStats(ctx)
	if err != nil {
		t.Fatalf("IndexStats: %v", err)
	}
	if stats != (engramsvc.IndexStats{Records: 3, Indexed: 3, Missing: 0}) {
		t.Fatalf("IndexStats after adds: %+v", stats)
	}

	vec.forget()
	stats, _ = svc.IndexStats(ctx)
	if stats.Missing != 3 || stats.Indexed != 0 {
		t.Fatalf("IndexStats after index loss: %+v, want missing=3", stats)
	}

	textsBefore := emb.texts
	n, err := svc.ReindexMissing(ctx)
	if err != nil {
		t.Fatalf("ReindexMissing: %v", err)
	}
	if n != 3 {
		t.Fatalf("ReindexMissing: want 3, got %d", n)
	}
	if emb.texts-textsBefore != 3 {
		t.Fatalf("embedder must be charged for exactly the gap: %d texts", emb.texts-textsBefore)
	}
	stats, _ = svc.IndexStats(ctx)
	if stats.Missing != 0 || stats.Indexed != 3 {
		t.Fatalf("IndexStats after reindex: %+v", stats)
	}

	// Idempotent: nothing left to do, nothing spent.
	textsBefore = emb.texts
	n, err = svc.ReindexMissing(ctx)
	if err != nil || n != 0 {
		t.Fatalf("second ReindexMissing: n=%d err=%v, want 0/nil", n, err)
	}
	if emb.texts != textsBefore {
		t.Fatalf("second ReindexMissing must not call the embedder")
	}
}

// TestReindexMissing_Batches: a gap larger than one batch is re-indexed in
// bounded embedder calls, all of it.
func TestReindexMissing_Batches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, vec, emb := newReindexService(t)

	const n = 70 // > 2 batches of 32
	for i := 0; i < n; i++ {
		if _, err := svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"m"}, UserID: "u"}); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	vec.forget()
	callsBefore := emb.calls
	got, err := svc.ReindexMissing(ctx)
	if err != nil {
		t.Fatalf("ReindexMissing: %v", err)
	}
	if got != n {
		t.Fatalf("ReindexMissing: want %d, got %d", n, got)
	}
	if calls := emb.calls - callsBefore; calls != 3 {
		t.Fatalf("want 3 embedder batches for %d records, got %d", n, calls)
	}
}

// plainStore hides the stub's IndexedIDs behind the bare port, so the
// service cannot inspect it and must say so rather than guess that nothing
// is missing.
type plainStore struct{ engram.VectorStore }

func TestReindexMissing_RequiresAnInspectableStore(t *testing.T) {
	t.Parallel()
	svc, err := engramsvc.NewService(plainStore{&stubVectorStore{}}, newStubHistory(), nil, &stubEmbedder{dim: 4}, engramsvc.Config{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.ReindexMissing(context.Background()); !errors.Is(err, engramsvc.ErrIndexNotInspectable) {
		t.Fatalf("ReindexMissing on a plain store: err=%v, want ErrIndexNotInspectable", err)
	}
	if _, err := svc.IndexStats(context.Background()); !errors.Is(err, engramsvc.ErrIndexNotInspectable) {
		t.Fatalf("IndexStats on a plain store: err=%v, want ErrIndexNotInspectable", err)
	}
}
