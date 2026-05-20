package engramsvc_test

import (
	"context"
	"fmt"
	"sync"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// --- in-memory stubs for testing ----------------------------------------

type stubVectorStore struct {
	mu         sync.RWMutex
	collection string
	dim        int
	records    []engram.VectorRecord
}

func (s *stubVectorStore) EnsureCollection(_ context.Context, name string, dim int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.collection = name
	s.dim = dim
	return nil
}

func (s *stubVectorStore) UpsertBatch(_ context.Context, records []engram.VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, records...)
	return nil
}

func (s *stubVectorStore) Search(_ context.Context, q engram.VectorQuery) ([]engram.VectorResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := q.TopK
	if limit > len(s.records) {
		limit = len(s.records)
	}
	out := make([]engram.VectorResult, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, engram.VectorResult{
			ID:    s.records[i].ID,
			Score: 0.9,
		})
	}
	return out, nil
}

func (s *stubVectorStore) DeleteBatch(_ context.Context, ids []engram.MemoryID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := make(map[engram.MemoryID]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	filtered := s.records[:0]
	for _, r := range s.records {
		if _, ok := set[r.ID]; !ok {
			filtered = append(filtered, r)
		}
	}
	s.records = filtered
	return nil
}

// -------------------------------------------------------------------------

type stubHistoryStore struct {
	mu      sync.RWMutex
	records map[engram.MemoryID]engram.MemoryRecord
	events  []engram.MemoryEvent
}

func newStubHistory() *stubHistoryStore {
	return &stubHistoryStore{records: make(map[engram.MemoryID]engram.MemoryRecord)}
}

func (s *stubHistoryStore) SaveRecord(_ context.Context, rec engram.MemoryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[rec.ID] = rec
	return nil
}

func (s *stubHistoryStore) UpdateRecord(_ context.Context, rec engram.MemoryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[rec.ID]; !ok {
		return engram.ErrNotFound
	}
	s.records[rec.ID] = rec
	return nil
}

func (s *stubHistoryStore) DeleteRecord(_ context.Context, id engram.MemoryID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return engram.ErrNotFound
	}
	delete(s.records, id)
	return nil
}

func (s *stubHistoryStore) GetRecord(_ context.Context, id engram.MemoryID) (engram.MemoryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[id]
	if !ok {
		return engram.MemoryRecord{}, engram.ErrNotFound
	}
	return r, nil
}

func (s *stubHistoryStore) ListRecords(_ context.Context, f engram.HistoryFilter) ([]engram.MemoryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []engram.MemoryRecord
	for _, r := range s.records {
		if matches(r, f) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *stubHistoryStore) SaveEvents(_ context.Context, events []engram.MemoryEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, events...)
	return nil
}

func (s *stubHistoryStore) ListEvents(_ context.Context, id engram.MemoryID) ([]engram.MemoryEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []engram.MemoryEvent
	for _, ev := range s.events {
		if ev.ID == id {
			out = append(out, ev)
		}
	}
	return out, nil
}

func matches(r engram.MemoryRecord, f engram.HistoryFilter) bool {
	if f.UserID != "" && r.UserID != f.UserID {
		return false
	}
	if f.AgentID != "" && r.AgentID != f.AgentID {
		return false
	}
	if f.WorkspaceID != "" && r.WorkspaceID != f.WorkspaceID {
		return false
	}
	return true
}

// -------------------------------------------------------------------------

type stubEmbedder struct {
	dim int
}

func (s *stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, s.dim)
		for j := range vec {
			vec[j] = float32(i+1) * 0.1
		}
		out[i] = vec
	}
	return out, nil
}

// -------------------------------------------------------------------------

type stubLLMClient struct {
	// responses maps call index to JSON bytes to unmarshal into v
	mu        sync.Mutex
	responses []string
	callCount int
}

func (s *stubLLMClient) ChatJSON(_ context.Context, _ engram.ChatRequest, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.callCount >= len(s.responses) {
		return fmt.Errorf("stubLLMClient: no response for call %d", s.callCount)
	}
	raw := s.responses[s.callCount]
	s.callCount++
	// decode into target via json
	return decodeJSON(raw, v)
}

// decodeJSON is a simple helper so we don't need encoding/json in test helpers.
func decodeJSON(raw string, v any) error {
	// import is at the package level — handled in a separate file
	return jsonUnmarshal([]byte(raw), v)
}
