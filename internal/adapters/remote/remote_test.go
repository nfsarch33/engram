package remote_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/adapters/history/sqlite"
	"github.com/nfsarch33/engram/internal/adapters/httpapi"
	mcpadapter "github.com/nfsarch33/engram/internal/adapters/mcp"
	"github.com/nfsarch33/engram/internal/adapters/remote"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

type stubEmbedder struct{ dim int }

func (e *stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dim)
		v[0] = 1 // every text embeds the same: search returns everything in scope
		out[i] = v
	}
	return out, nil
}

// daemon starts an in-process engramd HTTP API (real handler, real service).
func daemon(t *testing.T) *httptest.Server {
	t.Helper()
	hist, err := sqlite.NewStore(":memory:")
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { hist.Close() })
	vec, _ := inmem.NewStore()
	svc, err := engramsvc.NewService(vec, hist, nil, &stubEmbedder{dim: 4}, engramsvc.Config{CollectionName: "t", EmbeddingDim: 4})
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	srv := httptest.NewServer(httpapi.NewHandler(svc))
	t.Cleanup(srv.Close)
	return srv
}

func client(t *testing.T, base string) *remote.Client {
	t.Helper()
	c, err := remote.New(base, 10*time.Second)
	if err != nil {
		t.Fatalf("remote.New: %v", err)
	}
	return c
}

// TestClient_FullCycleAgainstRealDaemon drives every MemoryService method
// through the wire against the real HTTP handler: add (both message shapes),
// get, search (scoped), update, history, get_all, delete, not-found, delete_all.
func TestClient_FullCycleAgainstRealDaemon(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := daemon(t)
	c := client(t, srv.URL)

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	recs, err := c.Add(ctx, engramsvc.AddRequest{
		Messages: []string{"remote first", "remote second"}, UserID: "u1", WorkspaceID: "ws-a", AppID: "app",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(recs) != 2 || recs[0].Text != "remote first" || recs[0].WorkspaceID != "ws-a" {
		t.Fatalf("Add returned %+v", recs)
	}
	id := recs[0].ID

	got, err := c.Get(ctx, engramsvc.GetRequest{ID: id})
	if err != nil || got.ID != id || got.Text != "remote first" {
		t.Fatalf("Get: %+v err=%v", got, err)
	}

	hits, err := c.Search(ctx, engramsvc.SearchRequest{Query: "remote", UserID: "u1", WorkspaceID: "ws-a", TopK: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("Search in scope: want 2, got %d", len(hits))
	}
	other, err := c.Search(ctx, engramsvc.SearchRequest{Query: "remote", UserID: "u1", WorkspaceID: "ws-b", TopK: 5})
	if err != nil || len(other) != 0 {
		t.Fatalf("Search out of scope must be empty: %d err=%v", len(other), err)
	}

	upd, err := c.Update(ctx, engramsvc.UpdateRequest{ID: id, Text: "remote first, edited"})
	if err != nil || upd.Text != "remote first, edited" {
		t.Fatalf("Update: %+v err=%v", upd, err)
	}
	events, err := c.History(ctx, id)
	if err != nil || len(events) != 1 || events[0].Event != engram.EventUpdate {
		t.Fatalf("History: %+v err=%v", events, err)
	}

	all, err := c.GetAll(ctx, engram.HistoryFilter{UserID: "u1"})
	if err != nil || len(all) != 2 {
		t.Fatalf("GetAll: %d err=%v", len(all), err)
	}
	scoped, err := c.GetAll(ctx, engram.HistoryFilter{UserID: "u1", WorkspaceID: "ws-b"})
	if err != nil || len(scoped) != 0 {
		t.Fatalf("GetAll out of scope: %d err=%v", len(scoped), err)
	}

	if err := c.Delete(ctx, engramsvc.DeleteRequest{ID: id}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Get(ctx, engramsvc.GetRequest{ID: id}); !errors.Is(err, engram.ErrNotFound) {
		t.Fatalf("Get after delete: err=%v, want ErrNotFound", err)
	}
	if err := c.Delete(ctx, engramsvc.DeleteRequest{ID: id}); !errors.Is(err, engram.ErrNotFound) {
		t.Fatalf("double Delete: err=%v, want ErrNotFound", err)
	}

	n, err := c.DeleteAll(ctx, engram.HistoryFilter{UserID: "u1", WorkspaceID: "ws-a"})
	if err != nil || n != 1 {
		t.Fatalf("DeleteAll: n=%d err=%v, want 1", n, err)
	}

	health := c.HealthCheck(ctx)
	if health.Status != "ok" || health.Subsystem["daemon"] != "ok" || health.Subsystem["history_store"] != "ok" {
		t.Fatalf("HealthCheck: %+v", health)
	}
}

// TestClient_EmptyTextMapsToSentinel: the daemon's 400 becomes ErrEmptyText,
// as the in-process service would return.
func TestClient_EmptyTextMapsToSentinel(t *testing.T) {
	t.Parallel()
	srv := daemon(t)
	c := client(t, srv.URL)
	_, err := c.Add(context.Background(), engramsvc.AddRequest{Messages: []string{}, UserID: "u1"})
	if !errors.Is(err, engram.ErrEmptyText) {
		t.Fatalf("empty add: err=%v, want ErrEmptyText", err)
	}
}

// TestClient_UnreachableDaemon: a closed port is reported, not hung, and the
// health check says degraded instead of pretending.
func TestClient_UnreachableDaemon(t *testing.T) {
	t.Parallel()
	dead := httptest.NewServer(nil)
	base := dead.URL
	dead.Close()
	c := client(t, base)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Ping(ctx); !errors.Is(err, remote.ErrUnreachable) {
		t.Fatalf("Ping dead daemon: err=%v, want ErrUnreachable", err)
	}
	if h := c.HealthCheck(ctx); h.Status != "degraded" {
		t.Fatalf("HealthCheck dead daemon: %+v", h)
	}
}

func TestNew_RejectsBadURL(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "127.0.0.1:8280", "not a url", "http://"} {
		if _, err := remote.New(bad, time.Second); err == nil {
			t.Errorf("New(%q) accepted", bad)
		}
	}
}

// TestMCPAdapter_OverRemote is the fleet's write path end to end: the MCP
// adapter on a remote client, chat-style object messages, against the real
// daemon handler. This is exactly the call that failed fleet-wide.
func TestMCPAdapter_OverRemote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := daemon(t)
	a := mcpadapter.NewAdapter(client(t, srv.URL))

	res, err := a.HandleTool(ctx, "engram_add", map[string]any{
		"messages":     []any{map[string]any{"role": "user", "content": "fleet write over the proxy"}},
		"user_id":      "nfs",
		"workspace_id": "ws-fleet",
	})
	if err != nil {
		t.Fatalf("engram_add over remote: %v", err)
	}
	if res.(map[string]any)["count"] != 1 {
		t.Fatalf("engram_add over remote: %+v", res)
	}
	sr, err := a.HandleTool(ctx, "engram_search", map[string]any{"query": "proxy", "user_id": "nfs", "workspace_id": "ws-fleet", "top_k": float64(5)})
	if err != nil || sr.(map[string]any)["count"] != 1 {
		t.Fatalf("engram_search over remote: %+v err=%v", sr, err)
	}
	all, err := a.HandleTool(ctx, "mem0_get_all", map[string]any{"user_id": "nfs"})
	if err != nil || all.(map[string]any)["count"] != 1 {
		t.Fatalf("mem0_get_all over remote: %+v err=%v", all, err)
	}
	doc, err := a.HandleTool(ctx, "mem0_doctor", nil)
	if err != nil || doc.(engramsvc.HealthResult).Status != "ok" {
		t.Fatalf("mem0_doctor over remote: %+v err=%v", doc, err)
	}
}
