package engramsvc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

type factExtractionResponse struct {
	Facts []struct {
		Text string `json:"text"`
		Type string `json:"type,omitempty"`
	} `json:"facts"`
}

type updateDecisionResponse struct {
	Events []struct {
		Event string `json:"event"`
		ID    string `json:"id,omitempty"`
		Text  string `json:"text,omitempty"`
	} `json:"events"`
}

// Add stores new memory records, with optional LLM-assisted fact extraction and dedup.
func (s *Service) Add(ctx context.Context, req AddRequest) ([]engram.MemoryRecord, error) {
	if len(req.Messages) == 0 {
		return nil, engram.ErrEmptyText
	}
	if !req.Infer {
		return s.addRaw(ctx, req)
	}
	return s.addWithInference(ctx, req)
}

func (s *Service) addRaw(ctx context.Context, req AddRequest) ([]engram.MemoryRecord, error) {
	now := time.Now().UTC()
	records := make([]engram.MemoryRecord, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if msg == "" {
			continue
		}
		rec := engram.MemoryRecord{
			ID:          engram.NewMemoryID(),
			Text:        msg,
			Metadata:    req.Metadata,
			UserID:      req.UserID,
			AgentID:     req.AgentID,
			RunID:       req.RunID,
			AppID:       req.AppID,
			WorkspaceID: req.WorkspaceID,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.hist.SaveRecord(ctx, rec); err != nil {
			return nil, fmt.Errorf("add: save record: %w", err)
		}
		records = append(records, rec)
	}
	if err := s.embedAndIndex(ctx, records); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Service) addWithInference(ctx context.Context, req AddRequest) ([]engram.MemoryRecord, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("engramsvc: LLM required for inferred add")
	}

	// Step 1: extract facts from messages.
	var factResp factExtractionResponse
	if err := s.llm.ChatJSON(ctx, engram.ChatRequest{
		SystemPrompt: factExtractionSystem,
		UserPrompt:   buildFactExtractionPrompt(req.Messages),
	}, &factResp); err != nil {
		return nil, fmt.Errorf("add: fact extraction: %w", err)
	}
	if len(factResp.Facts) == 0 {
		return nil, nil
	}

	// Step 2: load existing memories for the same scope.
	existing, err := s.hist.ListRecords(ctx, engram.HistoryFilter{
		UserID:      req.UserID,
		AgentID:     req.AgentID,
		WorkspaceID: req.WorkspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("add: list existing: %w", err)
	}

	// Step 3: ask LLM to decide events.
	existingJSON, _ := json.Marshal(existing)
	factsJSON, _ := json.Marshal(factResp.Facts)
	var updateResp updateDecisionResponse
	if err := s.llm.ChatJSON(ctx, engram.ChatRequest{
		SystemPrompt: updateDecisionSystem,
		UserPrompt:   buildUpdateDecisionPrompt(string(existingJSON), string(factsJSON)),
	}, &updateResp); err != nil {
		return nil, fmt.Errorf("add: update decision: %w", err)
	}

	// Step 4: execute events.
	now := time.Now().UTC()
	var mutated []engram.MemoryRecord
	var domainEvents []engram.MemoryEvent

	for _, ev := range updateResp.Events {
		switch ev.Event {
		case string(engram.EventAdd):
			rec := engram.MemoryRecord{
				ID:          engram.NewMemoryID(),
				Text:        ev.Text,
				Metadata:    req.Metadata,
				UserID:      req.UserID,
				AgentID:     req.AgentID,
				RunID:       req.RunID,
				AppID:       req.AppID,
				WorkspaceID: req.WorkspaceID,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if err := s.hist.SaveRecord(ctx, rec); err != nil {
				return nil, fmt.Errorf("add event: save: %w", err)
			}
			mutated = append(mutated, rec)
			domainEvents = append(domainEvents, engram.MemoryEvent{
				Event: engram.EventAdd, ID: rec.ID, Text: rec.Text,
			})

		case string(engram.EventUpdate):
			id := engram.MemoryID(ev.ID)
			existing, err := s.hist.GetRecord(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("add event: get for update: %w", err)
			}
			snapshot := existing
			existing.Text = ev.Text
			existing.UpdatedAt = now
			if err := s.hist.UpdateRecord(ctx, existing); err != nil {
				return nil, fmt.Errorf("add event: update: %w", err)
			}
			mutated = append(mutated, existing)
			domainEvents = append(domainEvents, engram.MemoryEvent{
				Event: engram.EventUpdate, ID: existing.ID, Text: existing.Text, OldMemory: &snapshot,
			})

		case string(engram.EventDelete):
			id := engram.MemoryID(ev.ID)
			if err := s.hist.DeleteRecord(ctx, id); err != nil {
				return nil, fmt.Errorf("add event: delete: %w", err)
			}
			if err := s.vec.DeleteBatch(ctx, []engram.MemoryID{id}); err != nil {
				return nil, fmt.Errorf("add event: vector delete: %w", err)
			}
			domainEvents = append(domainEvents, engram.MemoryEvent{Event: engram.EventDelete, ID: id})

		case string(engram.EventNone):
			// no-op
		}
	}

	if len(domainEvents) > 0 {
		if err := s.hist.SaveEvents(ctx, domainEvents); err != nil {
			return nil, fmt.Errorf("add: save events: %w", err)
		}
	}

	if len(mutated) > 0 {
		if err := s.embedAndIndex(ctx, mutated); err != nil {
			return nil, err
		}
	}
	return mutated, nil
}

// embedAndIndex embeds texts and upserts into the vector store.
func (s *Service) embedAndIndex(ctx context.Context, records []engram.MemoryRecord) error {
	if len(records) == 0 {
		return nil
	}
	texts := textsFromRecords(records)
	vecs, err := s.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("embedAndIndex: embed: %w", err)
	}
	vrecs := make([]engram.VectorRecord, 0, len(records))
	for i, rec := range records {
		vrecs = append(vrecs, engram.VectorRecord{
			ID:     rec.ID,
			Vector: vecs[i],
			Payload: map[string]any{
				"user_id":      rec.UserID,
				"agent_id":     rec.AgentID,
				"run_id":       rec.RunID,
				"app_id":       rec.AppID,
				"workspace_id": rec.WorkspaceID,
				"text":         rec.Text,
			},
		})
	}
	if err := s.vec.UpsertBatch(ctx, vrecs); err != nil {
		return fmt.Errorf("embedAndIndex: upsert: %w", err)
	}
	return nil
}

func textsFromRecords(recs []engram.MemoryRecord) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Text
	}
	return out
}
