package mem0compat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// contractStubService is a minimal implementation of Service that records
// inputs verbatim and returns canned MemoryRecords. We do not reuse the
// stubService from handlers_test.go because that one is structured for
// per-test customisation; the contract test exercises a single end-to-end
// scenario across all handlers and benefits from a tighter recorder.
type contractStubService struct {
	addReq    engramsvc.AddRequest
	addResp   []engram.MemoryRecord
	searchReq engramsvc.SearchRequest
	updateReq engramsvc.UpdateRequest
	getReq    engramsvc.GetRequest
	deleteReq engramsvc.DeleteRequest
	historyID engram.MemoryID
	getAllF   engram.HistoryFilter

	rec      engram.MemoryRecord
	searchHits []engramsvc.SearchResult
	events     []engram.MemoryEvent
}

func (s *contractStubService) Add(_ context.Context, req engramsvc.AddRequest) ([]engram.MemoryRecord, error) {
	s.addReq = req
	return s.addResp, nil
}
func (s *contractStubService) Search(_ context.Context, req engramsvc.SearchRequest) ([]engramsvc.SearchResult, error) {
	s.searchReq = req
	return s.searchHits, nil
}
func (s *contractStubService) Get(_ context.Context, req engramsvc.GetRequest) (engram.MemoryRecord, error) {
	s.getReq = req
	return s.rec, nil
}
func (s *contractStubService) GetAll(_ context.Context, f engram.HistoryFilter) ([]engram.MemoryRecord, error) {
	s.getAllF = f
	return []engram.MemoryRecord{s.rec}, nil
}
func (s *contractStubService) Update(_ context.Context, req engramsvc.UpdateRequest) (engram.MemoryRecord, error) {
	s.updateReq = req
	return s.rec, nil
}
func (s *contractStubService) Delete(_ context.Context, req engramsvc.DeleteRequest) error {
	s.deleteReq = req
	return nil
}
func (s *contractStubService) History(_ context.Context, id engram.MemoryID) ([]engram.MemoryEvent, error) {
	s.historyID = id
	return s.events, nil
}

// loadFixture reads a captured Mem0 OSS request body from testdata/.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

// TestContract_FullFlow exercises the shim with the exact byte sequences a
// Mem0 OSS client emits, then asserts every response matches the wire shape
// documented in testdata/README.md.
func TestContract_FullFlow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC)
	rec := engram.MemoryRecord{
		ID:        engram.MemoryID("01HZQQRMD8EXAMPLE0000000000"),
		Text:      "user prefers Go over Python for backend services",
		UserID:    "alice",
		AgentID:   "cursor-coding",
		AppID:     "cursor",
		CreatedAt: now,
		UpdatedAt: now,
	}
	svc := &contractStubService{
		rec:        rec,
		addResp:    []engram.MemoryRecord{rec},
		searchHits: []engramsvc.SearchResult{{Record: rec, Score: 0.91}},
		events: []engram.MemoryEvent{
			{Event: engram.EventAdd, ID: rec.ID, Text: rec.Text},
			{Event: engram.EventUpdate, ID: rec.ID, Text: "user prefers Go for backend services and TypeScript for frontend",
				OldMemory: &engram.MemoryRecord{ID: rec.ID, Text: rec.Text}},
		},
	}

	srv := httptest.NewServer(NewHandler(svc, ""))
	defer srv.Close()

	t.Run("POST /memories", func(t *testing.T) {
		body := loadFixture(t, "add_request.json")
		resp := httpDo(t, srv.URL, http.MethodPost, "/memories", body)
		assertStatus(t, resp, http.StatusOK)
		var out struct {
			Results []map[string]any `json:"results"`
		}
		decode(t, resp, &out)
		if len(out.Results) != 1 {
			t.Fatalf("results: want 1, got %d", len(out.Results))
		}
		if got := out.Results[0]["event"]; got != "ADD" {
			t.Errorf("event: want ADD, got %v", got)
		}
		if got := out.Results[0]["id"]; got != string(rec.ID) {
			t.Errorf("id: want %q, got %v", rec.ID, got)
		}
		// Verify the shim forwarded the documented fields untouched.
		if got := svc.addReq.UserID; got != "alice" {
			t.Errorf("UserID forwarded: want alice, got %q", got)
		}
		if got := svc.addReq.AgentID; got != "cursor-coding" {
			t.Errorf("AgentID forwarded: want cursor-coding, got %q", got)
		}
		if got := svc.addReq.AppID; got != "cursor" {
			t.Errorf("AppID forwarded: want cursor, got %q", got)
		}
		if !svc.addReq.Infer {
			t.Error("Infer forwarded: want true")
		}
		if got := svc.addReq.Metadata["session"]; got != "2026-05-21-morning" {
			t.Errorf("Metadata[session] forwarded: want 2026-05-21-morning, got %v", got)
		}
		// Two messages -> two normalised strings.
		if want := 2; len(svc.addReq.Messages) != want {
			t.Errorf("Messages count: want %d, got %d", want, len(svc.addReq.Messages))
		}
	})

	t.Run("POST /search", func(t *testing.T) {
		body := loadFixture(t, "search_request.json")
		resp := httpDo(t, srv.URL, http.MethodPost, "/search", body)
		assertStatus(t, resp, http.StatusOK)
		// Mem0 OSS returns a JSON array, not an object.
		var arr []map[string]any
		decode(t, resp, &arr)
		if len(arr) != 1 {
			t.Fatalf("len: want 1, got %d", len(arr))
		}
		if got := arr[0]["id"]; got != string(rec.ID) {
			t.Errorf("id: want %q, got %v", rec.ID, got)
		}
		if got, ok := arr[0]["score"].(float64); !ok || got < 0.9 {
			t.Errorf("score: want >=0.9 float, got %v", arr[0]["score"])
		}
		if got := svc.searchReq.UserID; got != "alice" {
			t.Errorf("UserID extracted from filters: want alice, got %q", got)
		}
		if got := svc.searchReq.TopK; got != 5 {
			t.Errorf("TopK: want 5, got %d", got)
		}
	})

	t.Run("GET /memories/{id}", func(t *testing.T) {
		resp := httpDo(t, srv.URL, http.MethodGet, "/memories/"+string(rec.ID), nil)
		assertStatus(t, resp, http.StatusOK)
		var out map[string]any
		decode(t, resp, &out)
		if got := out["id"]; got != string(rec.ID) {
			t.Errorf("id: want %q, got %v", rec.ID, got)
		}
		if got := out["memory"]; got != rec.Text {
			t.Errorf("memory: want %q, got %v", rec.Text, got)
		}
		if got := out["user_id"]; got != "alice" {
			t.Errorf("user_id: want alice, got %v", got)
		}
	})

	t.Run("GET /memories list", func(t *testing.T) {
		resp := httpDo(t, srv.URL, http.MethodGet, "/memories?user_id=alice&app_id=cursor&limit=10", nil)
		assertStatus(t, resp, http.StatusOK)
		var arr []map[string]any
		decode(t, resp, &arr)
		if len(arr) == 0 {
			t.Fatal("expected at least one result")
		}
		if got := svc.getAllF.UserID; got != "alice" {
			t.Errorf("UserID forwarded: want alice, got %q", got)
		}
	})

	t.Run("PUT /memories/{id}", func(t *testing.T) {
		body := loadFixture(t, "update_request.json")
		resp := httpDo(t, srv.URL, http.MethodPut, "/memories/"+string(rec.ID), body)
		assertStatus(t, resp, http.StatusOK)
		var out map[string]any
		decode(t, resp, &out)
		if _, ok := out["message"]; !ok {
			t.Fatalf("response missing message: %v", out)
		}
		if got := svc.updateReq.Text; got == "" {
			t.Error("Text not forwarded")
		}
	})

	t.Run("GET /memories/{id}/history", func(t *testing.T) {
		resp := httpDo(t, srv.URL, http.MethodGet, "/memories/"+string(rec.ID)+"/history", nil)
		assertStatus(t, resp, http.StatusOK)
		var arr []map[string]any
		decode(t, resp, &arr)
		if len(arr) != 2 {
			t.Fatalf("len: want 2, got %d", len(arr))
		}
		if got := arr[0]["event"]; got != "ADD" {
			t.Errorf("event[0]: want ADD, got %v", got)
		}
		if got := arr[1]["event"]; got != "UPDATE" {
			t.Errorf("event[1]: want UPDATE, got %v", got)
		}
		if got := arr[1]["old_memory"]; got != rec.Text {
			t.Errorf("old_memory: want %q, got %v", rec.Text, got)
		}
	})

	t.Run("DELETE /memories/{id}", func(t *testing.T) {
		resp := httpDo(t, srv.URL, http.MethodDelete, "/memories/"+string(rec.ID), nil)
		assertStatus(t, resp, http.StatusOK)
		var out map[string]any
		decode(t, resp, &out)
		if got := out["message"]; got != "Memory deleted successfully!" {
			t.Errorf("message: want exact Mem0 OSS string, got %v", got)
		}
		if got := svc.deleteReq.ID; got != rec.ID {
			t.Errorf("ID: want %q, got %q", rec.ID, got)
		}
	})

	t.Run("GET /healthz", func(t *testing.T) {
		resp := httpDo(t, srv.URL, http.MethodGet, "/healthz", nil)
		assertStatus(t, resp, http.StatusOK)
		if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
			t.Errorf("Content-Type: want text/plain, got %q", ct)
		}
	})

	t.Run("GET /auth/setup-status", func(t *testing.T) {
		resp := httpDo(t, srv.URL, http.MethodGet, "/auth/setup-status", nil)
		assertStatus(t, resp, http.StatusOK)
		var out map[string]any
		decode(t, resp, &out)
		if got, ok := out["is_configured"].(bool); !ok || !got {
			t.Errorf("is_configured: want true bool, got %v", out["is_configured"])
		}
	})
}

// TestContract_APIKeyHeader verifies the gate matches what Mem0 OSS clients
// expect: X-API-Key on every protected route, no gate on the two probes.
func TestContract_APIKeyHeader(t *testing.T) {
	t.Parallel()

	rec := engram.MemoryRecord{ID: "01H", Text: "x"}
	svc := &contractStubService{rec: rec, addResp: []engram.MemoryRecord{rec}}
	srv := httptest.NewServer(NewHandler(svc, "secret"))
	defer srv.Close()

	// /healthz bypass.
	resp := httpDo(t, srv.URL, http.MethodGet, "/healthz", nil)
	assertStatus(t, resp, http.StatusOK)

	// /auth/setup-status bypass.
	resp = httpDo(t, srv.URL, http.MethodGet, "/auth/setup-status", nil)
	assertStatus(t, resp, http.StatusOK)

	// /memories without header -> 401.
	resp = httpDo(t, srv.URL, http.MethodPost, "/memories", []byte(`{"messages":["x"],"user_id":"a"}`))
	assertStatus(t, resp, http.StatusUnauthorized)

	// /memories with header -> 200.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/memories", bytes.NewReader([]byte(`{"messages":["x"],"user_id":"a"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status with key: want 200, got %d", resp.StatusCode)
	}
}

// --- helpers ---------------------------------------------------------------

func httpDo(t *testing.T, base, method, path string, body []byte) *http.Response {
	t.Helper()
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, base+path, bytes.NewReader(body))
	} else {
		req, err = http.NewRequest(method, base+path, nil)
	}
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	return resp
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status: want %d, got %d", want, resp.StatusCode)
	}
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
