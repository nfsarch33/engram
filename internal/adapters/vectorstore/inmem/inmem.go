// Package inmem implements engram.VectorStore with brute-force cosine similarity.
// Suitable for development, testing, and small deployments (<10k vectors).
package inmem

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// entry holds one stored vector.
type entry struct {
	id      engram.MemoryID
	vector  []float32
	norm    float32 // precomputed L2 norm for cosine efficiency
	payload map[string]any
}

// Store is a thread-safe in-memory vector store.
type Store struct {
	mu      sync.RWMutex
	entries map[engram.MemoryID]entry
	dim     int // set by EnsureCollection
}

// NewStore creates an empty in-memory vector store.
func NewStore() (*Store, error) {
	return &Store{entries: make(map[engram.MemoryID]entry)}, nil
}

// EnsureCollection configures the vector dimension. Idempotent.
func (s *Store) EnsureCollection(_ context.Context, _ string, dim int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dim == 0 {
		s.dim = dim
	}
	return nil
}

// UpsertBatch inserts or replaces records by ID.
func (s *Store) UpsertBatch(_ context.Context, records []engram.VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range records {
		payload := make(map[string]any, len(r.Payload))
		for k, v := range r.Payload {
			payload[k] = v
		}
		s.entries[r.ID] = entry{
			id:      r.ID,
			vector:  r.Vector,
			norm:    l2norm(r.Vector),
			payload: payload,
		}
	}
	return nil
}

// Search returns the top-k records closest to the query vector (cosine similarity).
func (s *Store) Search(_ context.Context, q engram.VectorQuery) ([]engram.VectorResult, error) {
	if q.TopK <= 0 {
		return nil, fmt.Errorf("inmem: top-k must be positive")
	}

	queryNorm := l2norm(q.Vector)

	type scored struct {
		id    engram.MemoryID
		score float32
		p     map[string]any
	}

	s.mu.RLock()
	candidates := make([]scored, 0, len(s.entries))
	for _, e := range s.entries {
		sim := cosineSimilarity(q.Vector, queryNorm, e.vector, e.norm)
		candidates = append(candidates, scored{id: e.id, score: sim, p: e.payload})
	}
	s.mu.RUnlock()

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	limit := q.TopK
	if limit > len(candidates) {
		limit = len(candidates)
	}
	results := make([]engram.VectorResult, 0, limit)
	for _, c := range candidates[:limit] {
		results = append(results, engram.VectorResult{
			ID:      c.id,
			Score:   c.score,
			Payload: c.p,
		})
	}
	return results, nil
}

// DeleteBatch removes records by ID. Unknown IDs are silently ignored.
func (s *Store) DeleteBatch(_ context.Context, ids []engram.MemoryID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.entries, id)
	}
	return nil
}

// --- math helpers -----------------------------------------------------------

func l2norm(v []float32) float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return float32(math.Sqrt(sum))
}

func cosineSimilarity(a []float32, normA float32, b []float32, normB float32) float32 {
	if normA == 0 || normB == 0 {
		return 0
	}
	var dot float64
	for i := range a {
		if i >= len(b) {
			break
		}
		dot += float64(a[i]) * float64(b[i])
	}
	return float32(dot / (float64(normA) * float64(normB)))
}
