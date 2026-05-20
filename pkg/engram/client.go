// Package engram provides the public Go SDK for the Engram memory engine.
// Consumers should use this package rather than importing internal packages directly.
package engram

import (
	"context"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// Client is the stable public API for the Engram memory engine.
type Client struct {
	svc *engramsvc.Service
}

// NewClient constructs a Client from a pre-configured Service.
// Use NewClientWithAdapters (future) to wire concrete adapters automatically.
func NewClient(svc *engramsvc.Service) *Client {
	return &Client{svc: svc}
}

// AddRequest is re-exported so callers don't import internal packages.
type AddRequest = engramsvc.AddRequest

// SearchRequest is re-exported for public use.
type SearchRequest = engramsvc.SearchRequest

// SearchResult is re-exported for public use.
type SearchResult = engramsvc.SearchResult

// MemoryRecord is re-exported for public use.
type MemoryRecord = engram.MemoryRecord

// MemoryID is re-exported for public use.
type MemoryID = engram.MemoryID

// HistoryFilter is re-exported for public use.
type HistoryFilter = engram.HistoryFilter

// Add stores new memories.
func (c *Client) Add(ctx context.Context, req AddRequest) ([]MemoryRecord, error) {
	return c.svc.Add(ctx, req)
}

// Search performs semantic search.
func (c *Client) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	return c.svc.Search(ctx, req)
}

// Get retrieves a single memory by ID.
func (c *Client) Get(ctx context.Context, id MemoryID) (MemoryRecord, error) {
	return c.svc.Get(ctx, engramsvc.GetRequest{ID: id})
}

// Update replaces the text of an existing memory.
func (c *Client) Update(ctx context.Context, id MemoryID, text string) (MemoryRecord, error) {
	return c.svc.Update(ctx, engramsvc.UpdateRequest{ID: id, Text: text})
}

// Delete removes a memory by ID.
func (c *Client) Delete(ctx context.Context, id MemoryID) error {
	return c.svc.Delete(ctx, engramsvc.DeleteRequest{ID: id})
}

// GetAll returns all memories matching the filter.
func (c *Client) GetAll(ctx context.Context, filter HistoryFilter) ([]MemoryRecord, error) {
	return c.svc.GetAll(ctx, filter)
}

// History returns mutation events for a memory record.
func (c *Client) History(ctx context.Context, id MemoryID) ([]engram.MemoryEvent, error) {
	return c.svc.History(ctx, id)
}
