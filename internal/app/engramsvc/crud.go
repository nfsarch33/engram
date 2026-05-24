package engramsvc

import (
	"context"
	"fmt"
	"time"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// Get retrieves a single memory record by ID.
func (s *Service) Get(ctx context.Context, req GetRequest) (engram.MemoryRecord, error) {
	return s.hist.GetRecord(ctx, req.ID)
}

// GetAll retrieves all memories matching the scoping filter.
func (s *Service) GetAll(ctx context.Context, filter engram.HistoryFilter) ([]engram.MemoryRecord, error) {
	return s.hist.ListRecords(ctx, filter)
}

// Update replaces the text of an existing memory record.
func (s *Service) Update(ctx context.Context, req UpdateRequest) (engram.MemoryRecord, error) {
	if req.Text == "" {
		return engram.MemoryRecord{}, engram.ErrEmptyText
	}
	rec, err := s.hist.GetRecord(ctx, req.ID)
	if err != nil {
		return engram.MemoryRecord{}, fmt.Errorf("update: get: %w", err)
	}
	snapshot := rec
	rec.Text = req.Text
	rec.UpdatedAt = time.Now().UTC()
	if err := s.hist.UpdateRecord(ctx, rec); err != nil {
		return engram.MemoryRecord{}, fmt.Errorf("update: save: %w", err)
	}
	ev := []engram.MemoryEvent{{
		Event: engram.EventUpdate, ID: rec.ID, Text: rec.Text, OldMemory: &snapshot,
	}}
	if err := s.hist.SaveEvents(ctx, ev); err != nil {
		return engram.MemoryRecord{}, fmt.Errorf("update: save event: %w", err)
	}
	// Re-index updated text.
	if err := s.embedAndIndex(ctx, []engram.MemoryRecord{rec}); err != nil {
		return engram.MemoryRecord{}, err
	}
	return rec, nil
}

// Delete removes a memory record by ID.
func (s *Service) Delete(ctx context.Context, req DeleteRequest) error {
	if err := s.hist.DeleteRecord(ctx, req.ID); err != nil {
		return fmt.Errorf("delete: history: %w", err)
	}
	if err := s.vec.DeleteBatch(ctx, []engram.MemoryID{req.ID}); err != nil {
		return fmt.Errorf("delete: vector: %w", err)
	}
	ev := []engram.MemoryEvent{{Event: engram.EventDelete, ID: req.ID}}
	return s.hist.SaveEvents(ctx, ev)
}

// DeleteAll removes all memory records matching the scoping filter.
func (s *Service) DeleteAll(ctx context.Context, filter engram.HistoryFilter) (int, error) {
	recs, err := s.hist.ListRecords(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("delete_all: list: %w", err)
	}
	ids := make([]engram.MemoryID, len(recs))
	for i, r := range recs {
		ids[i] = r.ID
	}
	for _, id := range ids {
		if err := s.hist.DeleteRecord(ctx, id); err != nil {
			return 0, fmt.Errorf("delete_all: record %s: %w", id, err)
		}
	}
	if len(ids) > 0 {
		if err := s.vec.DeleteBatch(ctx, ids); err != nil {
			return 0, fmt.Errorf("delete_all: vectors: %w", err)
		}
	}
	return len(ids), nil
}

// History returns all mutation events for a given memory record.
func (s *Service) History(ctx context.Context, id engram.MemoryID) ([]engram.MemoryEvent, error) {
	return s.hist.ListEvents(ctx, id)
}
