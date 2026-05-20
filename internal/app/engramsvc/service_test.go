package engramsvc_test

import (
	"context"
	"testing"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

func makeService(t *testing.T) (*engramsvc.Service, *stubHistoryStore, *stubVectorStore) {
	t.Helper()
	hist := newStubHistory()
	vec := &stubVectorStore{}
	emb := &stubEmbedder{dim: 8}
	svc, err := engramsvc.NewService(vec, hist, nil, emb, engramsvc.Config{
		CollectionName: "test",
		EmbeddingDim:   8,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, hist, vec
}

// ---- NewService -----------------------------------------------------------

func TestNewService_MissingEmbedder(t *testing.T) {
	t.Parallel()
	hist := newStubHistory()
	vec := &stubVectorStore{}
	_, err := engramsvc.NewService(vec, hist, nil, nil, engramsvc.Config{})
	if err == nil {
		t.Error("expected error when embedder is nil")
	}
}

func TestNewService_DefaultConfig(t *testing.T) {
	t.Parallel()
	hist := newStubHistory()
	vec := &stubVectorStore{}
	emb := &stubEmbedder{dim: 1536}
	svc, err := engramsvc.NewService(vec, hist, nil, emb, engramsvc.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Error("service should not be nil")
	}
	// EnsureCollection must have been called.
	if vec.collection == "" {
		t.Error("EnsureCollection should have been called")
	}
}

// ---- Add (infer=false) ----------------------------------------------------

func TestAdd_Raw_StoresRecords(t *testing.T) {
	t.Parallel()
	svc, hist, vec := makeService(t)
	ctx := context.Background()

	recs, err := svc.Add(ctx, engramsvc.AddRequest{
		Messages: []string{"user likes coffee", "user dislikes spam"},
		UserID:   "u1",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("want 2 records, got %d", len(recs))
	}
	// Both should be in history.
	all, _ := hist.ListRecords(ctx, engram.HistoryFilter{UserID: "u1"})
	if len(all) != 2 {
		t.Errorf("want 2 history records, got %d", len(all))
	}
	// Both should be in vector store.
	vec.mu.RLock()
	defer vec.mu.RUnlock()
	if len(vec.records) != 2 {
		t.Errorf("want 2 vector records, got %d", len(vec.records))
	}
}

func TestAdd_Raw_EmptyMessages_Error(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	_, err := svc.Add(context.Background(), engramsvc.AddRequest{Messages: nil})
	if err == nil {
		t.Error("expected error for empty messages")
	}
}

func TestAdd_Raw_RecordIDs_Unique(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	recs, err := svc.Add(context.Background(), engramsvc.AddRequest{
		Messages: []string{"msg1", "msg2", "msg3"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	seen := make(map[engram.MemoryID]struct{})
	for _, r := range recs {
		if _, dup := seen[r.ID]; dup {
			t.Errorf("duplicate ID: %s", r.ID)
		}
		seen[r.ID] = struct{}{}
	}
}

func TestAdd_Raw_Scoping(t *testing.T) {
	t.Parallel()
	svc, hist, _ := makeService(t)
	ctx := context.Background()
	svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"mem-u1"}, UserID: "u1"}) //nolint:errcheck
	svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"mem-u2"}, UserID: "u2"}) //nolint:errcheck

	u1recs, _ := hist.ListRecords(ctx, engram.HistoryFilter{UserID: "u1"})
	u2recs, _ := hist.ListRecords(ctx, engram.HistoryFilter{UserID: "u2"})
	if len(u1recs) != 1 || len(u2recs) != 1 {
		t.Errorf("scoping: u1=%d u2=%d, want 1 each", len(u1recs), len(u2recs))
	}
}

// ---- Add (infer=true) -----------------------------------------------------

func TestAdd_Infer_AddEvent(t *testing.T) {
	t.Parallel()
	hist := newStubHistory()
	vec := &stubVectorStore{}
	emb := &stubEmbedder{dim: 8}
	llm := &stubLLMClient{
		responses: []string{
			`{"facts":[{"text":"user prefers dark mode","type":"preference"}]}`,
			`{"events":[{"event":"add","text":"user prefers dark mode"}]}`,
		},
	}
	svc, err := engramsvc.NewService(vec, hist, llm, emb, engramsvc.Config{
		CollectionName: "test", EmbeddingDim: 8,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	recs, err := svc.Add(context.Background(), engramsvc.AddRequest{
		Messages: []string{"I prefer dark mode"},
		UserID:   "u1",
		Infer:    true,
	})
	if err != nil {
		t.Fatalf("Add infer: %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("want 1 record from add event, got %d", len(recs))
	}
	if recs[0].Text != "user prefers dark mode" {
		t.Errorf("wrong text: %q", recs[0].Text)
	}
}

func TestAdd_Infer_DeleteEvent(t *testing.T) {
	t.Parallel()
	hist := newStubHistory()
	vec := &stubVectorStore{}
	emb := &stubEmbedder{dim: 8}

	// Pre-seed a record to delete.
	svc1, _ := engramsvc.NewService(vec, hist, nil, emb, engramsvc.Config{CollectionName: "t", EmbeddingDim: 8})
	recs, _ := svc1.Add(context.Background(), engramsvc.AddRequest{
		Messages: []string{"user likes PHP"},
		UserID:   "u1",
	})
	idToDelete := string(recs[0].ID)

	llm := &stubLLMClient{
		responses: []string{
			`{"facts":[{"text":"user no longer likes PHP"}]}`,
			`{"events":[{"event":"delete","id":"` + idToDelete + `"}]}`,
		},
	}
	svc2, err := engramsvc.NewService(vec, hist, llm, emb, engramsvc.Config{CollectionName: "t", EmbeddingDim: 8})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, err = svc2.Add(context.Background(), engramsvc.AddRequest{
		Messages: []string{"I stopped liking PHP"},
		UserID:   "u1",
		Infer:    true,
	})
	if err != nil {
		t.Fatalf("Add delete event: %v", err)
	}

	_, err = hist.GetRecord(context.Background(), recs[0].ID)
	if err == nil {
		t.Error("record should have been deleted")
	}
}

func TestAdd_Infer_NoneEvent(t *testing.T) {
	t.Parallel()
	hist := newStubHistory()
	vec := &stubVectorStore{}
	emb := &stubEmbedder{dim: 8}
	llm := &stubLLMClient{
		responses: []string{
			`{"facts":[{"text":"hello world"}]}`,
			`{"events":[{"event":"none"}]}`,
		},
	}
	svc, err := engramsvc.NewService(vec, hist, llm, emb, engramsvc.Config{CollectionName: "t", EmbeddingDim: 8})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	recs, err := svc.Add(context.Background(), engramsvc.AddRequest{
		Messages: []string{"nothing important"},
		UserID:   "u1",
		Infer:    true,
	})
	if err != nil {
		t.Fatalf("Add none event: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("none event should produce 0 records, got %d", len(recs))
	}
}

// ---- Search ---------------------------------------------------------------

func TestSearch_ReturnsHits(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	ctx := context.Background()

	svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"python is great"}, UserID: "u1"}) //nolint:errcheck

	results, err := svc.Search(ctx, engramsvc.SearchRequest{
		Query:  "python",
		UserID: "u1",
		TopK:   5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one search result")
	}
}

func TestSearch_EmptyQuery_Error(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	_, err := svc.Search(context.Background(), engramsvc.SearchRequest{TopK: 5})
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestSearch_ZeroTopK_Error(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	_, err := svc.Search(context.Background(), engramsvc.SearchRequest{Query: "x", TopK: 0})
	if err == nil {
		t.Error("expected error for topK=0")
	}
}

// ---- CRUD -----------------------------------------------------------------

func TestGet_ExistingRecord(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	ctx := context.Background()
	recs, _ := svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"test memory"}, UserID: "u1"})

	got, err := svc.Get(ctx, engramsvc.GetRequest{ID: recs[0].ID})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Text != "test memory" {
		t.Errorf("Get text: got %q, want %q", got.Text, "test memory")
	}
}

func TestGet_NotFound(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	_, err := svc.Get(context.Background(), engramsvc.GetRequest{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV"})
	if err == nil {
		t.Error("expected ErrNotFound for unknown ID")
	}
}

func TestUpdate_ChangesText(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	ctx := context.Background()
	recs, _ := svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"old text"}, UserID: "u1"})

	updated, err := svc.Update(ctx, engramsvc.UpdateRequest{ID: recs[0].ID, Text: "new text"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Text != "new text" {
		t.Errorf("Update text: got %q, want %q", updated.Text, "new text")
	}
}

func TestUpdate_EmptyText_Error(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	ctx := context.Background()
	recs, _ := svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"text"}, UserID: "u1"})
	_, err := svc.Update(ctx, engramsvc.UpdateRequest{ID: recs[0].ID, Text: ""})
	if err == nil {
		t.Error("expected error for empty update text")
	}
}

func TestDelete_RemovesRecord(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	ctx := context.Background()
	recs, _ := svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"to delete"}, UserID: "u1"})

	if err := svc.Delete(ctx, engramsvc.DeleteRequest{ID: recs[0].ID}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := svc.Get(ctx, engramsvc.GetRequest{ID: recs[0].ID})
	if err == nil {
		t.Error("record should be gone after delete")
	}
}

func TestHistory_RecordsEvents(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	ctx := context.Background()
	recs, _ := svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"mem"}, UserID: "u1"})
	svc.Update(ctx, engramsvc.UpdateRequest{ID: recs[0].ID, Text: "updated"}) //nolint:errcheck

	events, err := svc.History(ctx, recs[0].ID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	// should have at least one update event
	hasUpdate := false
	for _, ev := range events {
		if ev.Event == engram.EventUpdate {
			hasUpdate = true
		}
	}
	if !hasUpdate {
		t.Error("expected update event in history")
	}
}

func TestGetAll_Scoped(t *testing.T) {
	t.Parallel()
	svc, _, _ := makeService(t)
	ctx := context.Background()
	svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"m1", "m2"}, UserID: "u1"}) //nolint:errcheck
	svc.Add(ctx, engramsvc.AddRequest{Messages: []string{"m3"}, UserID: "u2"})       //nolint:errcheck

	all, err := svc.GetAll(ctx, engram.HistoryFilter{UserID: "u1"})
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("GetAll u1: want 2, got %d", len(all))
	}
}
