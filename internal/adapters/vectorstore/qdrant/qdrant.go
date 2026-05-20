// Package qdrant implements engram.VectorStore against a Qdrant HTTP API
// (v1.x). It uses only net/http -- no Qdrant Go SDK -- to stay within the
// 3-direct-deps constraint.
package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

const defaultTimeout = 30 * time.Second

// Options configures the Qdrant HTTP client.
type Options struct {
	BaseURL    string
	APIKey     string        // optional; sent as "api-key" header
	Timeout    time.Duration // defaults to 30s
	Collection string        // default collection name (optional, can be set per-call)
}

// Store implements engram.VectorStore via the Qdrant HTTP REST API.
type Store struct {
	baseURL    string
	apiKey     string
	collection string
	httpClient *http.Client
}

// New returns a Store from opts.
func New(opts Options) *Store {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Store{
		baseURL:    strings.TrimRight(opts.BaseURL, "/"),
		apiKey:     opts.APIKey,
		collection: opts.Collection,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// EnsureCollection creates the collection with cosine distance if it does not
// already exist. Idempotent: a 400 "already exists" response is treated as OK.
func (s *Store) EnsureCollection(ctx context.Context, name string, dim int) error {
	body, err := json.Marshal(map[string]any{
		"vectors": map[string]any{
			"size":     dim,
			"distance": "Cosine",
		},
	})
	if err != nil {
		return fmt.Errorf("qdrant: marshal create-collection: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		s.baseURL+"/collections/"+name, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("qdrant: build create-collection request: %w", err)
	}
	s.setHeaders(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: create-collection: %w", err)
	}
	defer resp.Body.Close()

	// 200 OK = created; 400 with "already exists" = idempotent success.
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode == http.StatusBadRequest {
		var r qdrantResult
		json.NewDecoder(resp.Body).Decode(&r) //nolint:errcheck
		if strings.Contains(r.Status.Error, "already exists") {
			return nil
		}
	}

	return fmt.Errorf("qdrant: create-collection %q: HTTP %d", name, resp.StatusCode)
}

// UpsertBatch inserts or replaces records. Empty input is a no-op.
func (s *Store) UpsertBatch(ctx context.Context, records []engram.VectorRecord) error {
	if len(records) == 0 {
		return nil
	}

	type point struct {
		ID      string         `json:"id"`
		Vector  []float32      `json:"vector"`
		Payload map[string]any `json:"payload,omitempty"`
	}

	points := make([]point, 0, len(records))
	for _, r := range records {
		// Store the engram MemoryID string inside the payload so we can
		// reconstruct it on the way back (Qdrant IDs are uint64 or UUID).
		payload := make(map[string]any, len(r.Payload)+1)
		for k, v := range r.Payload {
			payload[k] = v
		}
		payload["_engram_id"] = string(r.ID)

		points = append(points, point{
			ID:      string(r.ID),
			Vector:  r.Vector,
			Payload: payload,
		})
	}

	body, err := json.Marshal(map[string]any{"points": points})
	if err != nil {
		return fmt.Errorf("qdrant: marshal upsert: %w", err)
	}

	collection := s.collection
	if collection == "" {
		collection = "engram"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		s.baseURL+"/collections/"+collection+"/points",
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("qdrant: build upsert request: %w", err)
	}
	s.setHeaders(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: upsert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant: upsert HTTP %d", resp.StatusCode)
	}
	return nil
}

// Search performs an ANN search in the collection and returns the top-k hits.
// VectorQuery.Filters is translated to a Qdrant `must` filter (equality only).
func (s *Store) Search(ctx context.Context, q engram.VectorQuery) ([]engram.VectorResult, error) {
	if q.TopK <= 0 {
		return nil, fmt.Errorf("qdrant: top-k must be positive")
	}

	reqBody := map[string]any{
		"vector":       q.Vector,
		"limit":        q.TopK,
		"with_payload": q.WithPayload,
	}
	if len(q.Filters) > 0 {
		reqBody["filter"] = buildFilter(q.Filters)
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("qdrant: marshal search: %w", err)
	}

	collection := s.collection
	if collection == "" {
		collection = "engram"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/collections/"+collection+"/points/search",
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("qdrant: build search request: %w", err)
	}
	s.setHeaders(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qdrant: search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qdrant: search HTTP %d", resp.StatusCode)
	}

	var result struct {
		Result []struct {
			ID      string         `json:"id"`
			Score   float32        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("qdrant: decode search response: %w", err)
	}

	out := make([]engram.VectorResult, 0, len(result.Result))
	for _, hit := range result.Result {
		// Prefer the stored _engram_id over the raw Qdrant point ID.
		id := engram.MemoryID(hit.ID)
		if eid, ok := hit.Payload["_engram_id"].(string); ok {
			id = engram.MemoryID(eid)
		}

		// Strip internal key from the returned payload.
		payload := make(map[string]any, len(hit.Payload))
		for k, v := range hit.Payload {
			if k == "_engram_id" {
				continue
			}
			payload[k] = v
		}

		out = append(out, engram.VectorResult{
			ID:      id,
			Score:   hit.Score,
			Payload: payload,
		})
	}
	return out, nil
}

// DeleteBatch removes records by ID. Unknown IDs are silently ignored.
func (s *Store) DeleteBatch(ctx context.Context, ids []engram.MemoryID) error {
	if len(ids) == 0 {
		return nil
	}

	strIDs := make([]string, len(ids))
	for i, id := range ids {
		strIDs[i] = string(id)
	}

	body, err := json.Marshal(map[string]any{"points": strIDs})
	if err != nil {
		return fmt.Errorf("qdrant: marshal delete: %w", err)
	}

	collection := s.collection
	if collection == "" {
		collection = "engram"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/collections/"+collection+"/points/delete",
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("qdrant: build delete request: %w", err)
	}
	s.setHeaders(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: delete: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant: delete HTTP %d", resp.StatusCode)
	}
	return nil
}

// --- helpers ----------------------------------------------------------------

func (s *Store) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("api-key", s.apiKey)
	}
}

// buildFilter converts a flat key=value map to a Qdrant `must` filter.
func buildFilter(filters map[string]any) map[string]any {
	must := make([]map[string]any, 0, len(filters))
	for k, v := range filters {
		must = append(must, map[string]any{
			"key":   k,
			"match": map[string]any{"value": v},
		})
	}
	return map[string]any{"must": must}
}

// qdrantResult is the minimal error wrapper returned by Qdrant on failure.
type qdrantResult struct {
	Status struct {
		Error string `json:"error"`
	} `json:"status"`
}

// Ensure Store satisfies the VectorStore port at compile time.
var _ engram.VectorStore = (*Store)(nil)
