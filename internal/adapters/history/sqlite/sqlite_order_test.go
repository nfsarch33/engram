package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// TestListRecords_InsertionOrderNotWallClock: a record written later with an
// earlier created_at (the host clock stepped backwards) still lists after
// the one written before it. Ordering follows the insertion sequence.
func TestListRecords_InsertionOrderNotWallClock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	now := time.Now().UTC()
	first := engram.MemoryRecord{ID: "first", Text: "written first", UserID: "u", CreatedAt: now, UpdatedAt: now}
	second := engram.MemoryRecord{ID: "second", Text: "written second, clock went back", UserID: "u",
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	if err := s.SaveRecord(ctx, first); err != nil {
		t.Fatalf("SaveRecord first: %v", err)
	}
	if err := s.SaveRecord(ctx, second); err != nil {
		t.Fatalf("SaveRecord second: %v", err)
	}

	got, err := s.ListRecords(ctx, engram.HistoryFilter{UserID: "u"})
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(got) != 2 || got[0].ID != "first" || got[1].ID != "second" {
		t.Fatalf("order must follow insertion, got %v", ids(got))
	}
}

// TestListEvents_InsertionOrderNotWallClock does the same for the event
// log: events are stamped internally, so the clock step is simulated by
// rewinding the second event's stamp after the fact.
func TestListEvents_InsertionOrderNotWallClock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	id := engram.MemoryID("mem-1")
	if err := s.SaveEvents(ctx, []engram.MemoryEvent{{Event: engram.EventAdd, ID: id, Text: "v1"}}); err != nil {
		t.Fatalf("SaveEvents 1: %v", err)
	}
	if err := s.SaveEvents(ctx, []engram.MemoryEvent{{Event: engram.EventUpdate, ID: id, Text: "v2"}}); err != nil {
		t.Fatalf("SaveEvents 2: %v", err)
	}
	// The clock steps back an hour between the two writes.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE memory_events SET created_at = created_at - ? WHERE new_text = 'v2'`, int64(time.Hour)); err != nil {
		t.Fatalf("rewind: %v", err)
	}

	got, err := s.ListEvents(ctx, id)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 2 || got[0].Text != "v1" || got[1].Text != "v2" {
		t.Fatalf("event order must follow insertion, got %+v", got)
	}
}

func ids(recs []engram.MemoryRecord) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = string(r.ID)
	}
	return out
}
