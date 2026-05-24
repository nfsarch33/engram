package engramsvc

import (
	"context"
	"fmt"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// SearchResult combines a vector result with the full memory record.
type SearchResult struct {
	Record engram.MemoryRecord `json:"record"`
	Score  float32             `json:"score"`
}

// Search performs semantic search over stored memories.
func (s *Service) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if req.Query == "" {
		return nil, engram.ErrEmptyText
	}
	if req.TopK <= 0 {
		return nil, engram.ErrInvalidTopK
	}

	vecs, err := s.embedder.EmbedBatch(ctx, []string{req.Query})
	if err != nil {
		return nil, fmt.Errorf("search: embed query: %w", err)
	}

	filters := map[string]any{}
	if req.UserID != "" {
		filters["user_id"] = req.UserID
	}
	if req.AgentID != "" {
		filters["agent_id"] = req.AgentID
	}
	if req.WorkspaceID != "" {
		filters["workspace_id"] = req.WorkspaceID
	}

	hits, err := s.vec.Search(ctx, engram.VectorQuery{
		Vector:      vecs[0],
		TopK:        req.TopK,
		Filters:     filters,
		WithPayload: true,
	})
	if err != nil {
		return nil, fmt.Errorf("search: vector search: %w", err)
	}

	results := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		rec, err := s.hist.GetRecord(ctx, h.ID)
		if err != nil {
			// record may have been deleted; skip it
			continue
		}
		results = append(results, SearchResult{Record: rec, Score: h.Score})
	}
	return results, nil
}
