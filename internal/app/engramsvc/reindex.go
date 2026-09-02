package engramsvc

import (
	"context"
	"errors"
	"fmt"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// ErrIndexNotInspectable is returned when the vector store cannot report
// which IDs it holds, so the gap between history and index is unknowable.
var ErrIndexNotInspectable = errors.New("engramsvc: vector store cannot report indexed ids")

// reindexBatch bounds one embedder call during a reindex so a large gap is
// paid for in bounded, resumable steps rather than one giant request.
const reindexBatch = 32

// IndexStats is the gap between what history holds and what search serves.
type IndexStats struct {
	Records int `json:"records"` // history records
	Indexed int `json:"indexed"` // of those, present in the vector index
	Missing int `json:"missing"` // records with no vector: invisible to search
}

// IndexStats reports the history/index gap. It is what the metrics
// collector exposes, so a restart that emptied a non-durable index, a
// restore from backup, or an embedder change shows up as a number instead
// of as search quietly forgetting.
func (s *Service) IndexStats(ctx context.Context) (IndexStats, error) {
	missing, total, err := s.missingRecords(ctx)
	if err != nil {
		return IndexStats{}, err
	}
	return IndexStats{Records: total, Indexed: total - len(missing), Missing: len(missing)}, nil
}

// ReindexMissing embeds and indexes every history record whose ID is absent
// from the vector index, in batches, and returns how many it re-indexed. It
// spends embedding calls only on the gap: records already indexed cost
// nothing. Safe to run repeatedly; the second run finds nothing to do.
func (s *Service) ReindexMissing(ctx context.Context) (int, error) {
	missing, _, err := s.missingRecords(ctx)
	if err != nil {
		return 0, err
	}
	done := 0
	for start := 0; start < len(missing); start += reindexBatch {
		end := start + reindexBatch
		if end > len(missing) {
			end = len(missing)
		}
		if err := s.embedAndIndex(ctx, missing[start:end]); err != nil {
			return done, fmt.Errorf("reindex: batch at %d: %w", start, err)
		}
		done += end - start
	}
	return done, nil
}

// missingRecords lists history records absent from the index, in history
// order, plus the total record count.
func (s *Service) missingRecords(ctx context.Context) ([]engram.MemoryRecord, int, error) {
	inspector, ok := s.vec.(engram.IndexInspector)
	if !ok {
		return nil, 0, ErrIndexNotInspectable
	}
	indexed, err := inspector.IndexedIDs(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("reindex: indexed ids: %w", err)
	}
	have := make(map[engram.MemoryID]struct{}, len(indexed))
	for _, id := range indexed {
		have[id] = struct{}{}
	}
	all, err := s.hist.ListRecords(ctx, engram.HistoryFilter{})
	if err != nil {
		return nil, 0, fmt.Errorf("reindex: list records: %w", err)
	}
	var missing []engram.MemoryRecord
	for _, rec := range all {
		if _, ok := have[rec.ID]; !ok {
			missing = append(missing, rec)
		}
	}
	return missing, len(all), nil
}
