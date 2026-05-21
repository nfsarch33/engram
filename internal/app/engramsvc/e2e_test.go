package engramsvc_test

import (
	"context"
	"testing"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
)

func TestE2E_FullCRUDCycle(t *testing.T) {
	ctx := context.Background()

	svc, err := engramsvc.NewService(
		&stubVectorStore{},
		newStubHistory(),
		nil,
		&stubEmbedder{dim: 768},
		engramsvc.Config{CollectionName: "test-e2e", EmbeddingDim: 768},
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	recs, err := svc.Add(ctx, engramsvc.AddRequest{
		Messages: []string{"The Helixon platform uses EINO v0.8.13 for agent orchestration"},
		UserID:   "test-user",
		Infer:    false,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("Add returned 0 records")
	}
	id := recs[0].ID
	if id == "" {
		t.Fatal("Add returned empty ID")
	}

	results, err := svc.Search(ctx, engramsvc.SearchRequest{
		Query:  "EINO agent orchestration",
		UserID: "test-user",
		TopK:   5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search returned 0 results")
	}

	found := false
	for _, r := range results {
		if r.Record.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Search did not return the added memory %s", id)
	}

	events, err := svc.History(ctx, id)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	t.Logf("History returned %d events for %s (raw add may skip event logging)", len(events), id)

	err = svc.Delete(ctx, engramsvc.DeleteRequest{ID: id})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestE2E_HealthCheck(t *testing.T) {
	ctx := context.Background()

	svc, err := engramsvc.NewService(
		&stubVectorStore{},
		newStubHistory(),
		nil,
		&stubEmbedder{dim: 768},
		engramsvc.Config{CollectionName: "test-health", EmbeddingDim: 768},
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	health := svc.HealthCheck(ctx)
	if health.Status != "ok" {
		t.Errorf("HealthCheck status = %q, want ok", health.Status)
	}
	if health.Subsystem == nil {
		t.Error("HealthCheck returned nil subsystem map")
	}
}

func inferResponses(n int) []string {
	resp := `{"facts":[{"text":"test fact","event":"ADD"}]}`
	out := make([]string, n)
	for i := range out {
		out[i] = resp
	}
	return out
}
