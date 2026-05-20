package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/adapters/history/sqlite"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func makeRecord(userID, text string) engram.MemoryRecord {
	now := time.Now().UTC()
	return engram.MemoryRecord{
		ID:        engram.NewMemoryID(),
		Text:      text,
		Metadata:  map[string]any{"source": "test"},
		UserID:    userID,
		AgentID:   "agent1",
		AppID:     "app1",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// --- SaveRecord / GetRecord -------------------------------------------------

func TestSaveAndGet(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	rec := makeRecord("u1", "user likes Go")

	if err := s.SaveRecord(ctx, rec); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	got, err := s.GetRecord(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if got.Text != rec.Text {
		t.Errorf("text: got %q, want %q", got.Text, rec.Text)
	}
	if got.UserID != rec.UserID {
		t.Errorf("user_id: got %q, want %q", got.UserID, rec.UserID)
	}
	if got.Metadata["source"] != "test" {
		t.Errorf("metadata source: got %v", got.Metadata["source"])
	}
}

func TestGetRecord_NotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	_, err := s.GetRecord(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err == nil {
		t.Error("expected ErrNotFound for unknown ID")
	}
}

func TestSaveRecord_DuplicateID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	rec := makeRecord("u1", "first")
	s.SaveRecord(ctx, rec) //nolint:errcheck
	err := s.SaveRecord(ctx, rec)
	if err == nil {
		t.Error("expected error on duplicate ID insert")
	}
}

// --- UpdateRecord -----------------------------------------------------------

func TestUpdateRecord(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	rec := makeRecord("u1", "old text")
	s.SaveRecord(ctx, rec) //nolint:errcheck

	rec.Text = "new text"
	rec.UpdatedAt = time.Now().UTC()
	if err := s.UpdateRecord(ctx, rec); err != nil {
		t.Fatalf("UpdateRecord: %v", err)
	}

	got, _ := s.GetRecord(ctx, rec.ID)
	if got.Text != "new text" {
		t.Errorf("UpdateRecord: got %q, want %q", got.Text, "new text")
	}
}

func TestUpdateRecord_NotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	err := s.UpdateRecord(context.Background(), makeRecord("u1", "ghost"))
	if err == nil {
		t.Error("expected error updating non-existent record")
	}
}

// --- DeleteRecord -----------------------------------------------------------

func TestDeleteRecord(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	rec := makeRecord("u1", "to delete")
	s.SaveRecord(ctx, rec) //nolint:errcheck

	if err := s.DeleteRecord(ctx, rec.ID); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	_, err := s.GetRecord(ctx, rec.ID)
	if err == nil {
		t.Error("record should be gone after delete")
	}
}

func TestDeleteRecord_NotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	err := s.DeleteRecord(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err == nil {
		t.Error("expected error deleting non-existent record")
	}
}

// --- ListRecords ------------------------------------------------------------

func TestListRecords_FilterByUser(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	s.SaveRecord(ctx, makeRecord("u1", "m1")) //nolint:errcheck
	s.SaveRecord(ctx, makeRecord("u1", "m2")) //nolint:errcheck
	s.SaveRecord(ctx, makeRecord("u2", "m3")) //nolint:errcheck

	recs, err := s.ListRecords(ctx, engram.HistoryFilter{UserID: "u1"})
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("want 2 records for u1, got %d", len(recs))
	}
}

func TestListRecords_NoFilter(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		s.SaveRecord(ctx, makeRecord("u1", "mem")) //nolint:errcheck
	}
	recs, err := s.ListRecords(ctx, engram.HistoryFilter{})
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(recs) != 5 {
		t.Errorf("want 5, got %d", len(recs))
	}
}

// --- SaveEvents / ListEvents ------------------------------------------------

func TestSaveAndListEvents(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	rec := makeRecord("u1", "mem")
	s.SaveRecord(ctx, rec) //nolint:errcheck

	events := []engram.MemoryEvent{
		{Event: engram.EventAdd, ID: rec.ID, Text: "mem"},
		{Event: engram.EventUpdate, ID: rec.ID, Text: "mem updated"},
	}
	if err := s.SaveEvents(ctx, events); err != nil {
		t.Fatalf("SaveEvents: %v", err)
	}

	got, err := s.ListEvents(ctx, rec.ID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 events, got %d", len(got))
	}
}

func TestListEvents_EmptyForUnknownID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	events, err := s.ListEvents(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("want 0 events for unknown ID, got %d", len(events))
	}
}

// --- Concurrency ------------------------------------------------------------

func TestConcurrentWrites(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	done := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		go func() {
			if err := s.SaveRecord(ctx, makeRecord("u1", "concurrent")); err != nil {
				t.Errorf("SaveRecord: %v", err)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
	recs, _ := s.ListRecords(ctx, engram.HistoryFilter{UserID: "u1"})
	if len(recs) != 20 {
		t.Errorf("concurrent inserts: want 20, got %d", len(recs))
	}
}
