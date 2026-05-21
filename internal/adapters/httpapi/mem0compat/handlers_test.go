package mem0compat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// stubService captures Service calls and returns canned responses.
// It's the minimal surface the mem0compat handlers exercise; we keep it
// here (not in engramsvc) so the shim package owns its own test contract.
type stubService struct {
	addReq    engramsvc.AddRequest
	addResp   []engram.MemoryRecord
	addErr    error
	addCalls  int

	searchReq   engramsvc.SearchRequest
	searchResp  []engramsvc.SearchResult
	searchErr   error
	searchCalls int

	getReq    engramsvc.GetRequest
	getResp   engram.MemoryRecord
	getErr    error
	getCalls  int

	getAllFilter engram.HistoryFilter
	getAllResp   []engram.MemoryRecord
	getAllErr    error
	getAllCalls  int

	updateReq    engramsvc.UpdateRequest
	updateResp   engram.MemoryRecord
	updateErr    error
	updateCalls  int

	deleteReq   engramsvc.DeleteRequest
	deleteErr   error
	deleteCalls int

	historyID    engram.MemoryID
	historyResp  []engram.MemoryEvent
	historyErr   error
	historyCalls int
}

func (s *stubService) Add(_ context.Context, req engramsvc.AddRequest) ([]engram.MemoryRecord, error) {
	s.addCalls++
	s.addReq = req
	return s.addResp, s.addErr
}

func (s *stubService) Search(_ context.Context, req engramsvc.SearchRequest) ([]engramsvc.SearchResult, error) {
	s.searchCalls++
	s.searchReq = req
	return s.searchResp, s.searchErr
}

func (s *stubService) Get(_ context.Context, req engramsvc.GetRequest) (engram.MemoryRecord, error) {
	s.getCalls++
	s.getReq = req
	return s.getResp, s.getErr
}

func (s *stubService) GetAll(_ context.Context, filter engram.HistoryFilter) ([]engram.MemoryRecord, error) {
	s.getAllCalls++
	s.getAllFilter = filter
	return s.getAllResp, s.getAllErr
}

func (s *stubService) Update(_ context.Context, req engramsvc.UpdateRequest) (engram.MemoryRecord, error) {
	s.updateCalls++
	s.updateReq = req
	return s.updateResp, s.updateErr
}

func (s *stubService) Delete(_ context.Context, req engramsvc.DeleteRequest) error {
	s.deleteCalls++
	s.deleteReq = req
	return s.deleteErr
}

func (s *stubService) History(_ context.Context, id engram.MemoryID) ([]engram.MemoryEvent, error) {
	s.historyCalls++
	s.historyID = id
	return s.historyResp, s.historyErr
}

func newTestHandler(svc Service) http.Handler {
	return NewHandler(svc, "")
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// --- /healthz and /auth/setup-status ----------------------------------------

func TestHealthz_ReturnsOK(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&stubService{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("body: want ok, got %q", rec.Body.String())
	}
}

func TestSetupStatus_ReturnsConfigured(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&stubService{})
	req := httptest.NewRequest(http.MethodGet, "/auth/setup-status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Mem0 OSS uses {"is_configured": bool}; mem0-mcp-go probes the path
	// purely to confirm the bridge is alive, so any 200/JSON suffices.
	if _, ok := out["is_configured"]; !ok {
		t.Fatalf("body missing is_configured key: %v", out)
	}
}

// --- POST /memories ---------------------------------------------------------

func TestAddMemory_Mem0WireShape(t *testing.T) {
	t.Parallel()

	rec := engram.MemoryRecord{
		ID:     engram.MemoryID("01H8ZZZZZZZZZZZZZZZZZZZZZZ"),
		Text:   "user prefers Go over Python",
		UserID: "alice",
		AppID:  "cursor",
	}
	svc := &stubService{addResp: []engram.MemoryRecord{rec}}
	h := newTestHandler(svc)

	body := map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "I prefer Go"},
		},
		"user_id": "alice",
		"app_id":  "cursor",
		"infer":   true,
	}
	resp := doJSON(t, h, http.MethodPost, "/memories", body)

	if resp.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if svc.addCalls != 1 {
		t.Fatalf("Add calls: want 1, got %d", svc.addCalls)
	}
	if got := svc.addReq.UserID; got != "alice" {
		t.Errorf("UserID: want alice, got %q", got)
	}
	if got := svc.addReq.AppID; got != "cursor" {
		t.Errorf("AppID: want cursor, got %q", got)
	}
	if !svc.addReq.Infer {
		t.Error("Infer: want true")
	}
	if want, got := []string{"I prefer Go"}, svc.addReq.Messages; !equalStrings(want, got) {
		t.Errorf("Messages: want %v, got %v", want, got)
	}

	// Mem0 OSS returns {"results": [{id, memory, event, ...}]}.
	var out struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Results) != 1 {
		t.Fatalf("results: want 1, got %d", len(out.Results))
	}
	if got := out.Results[0]["id"]; got != string(rec.ID) {
		t.Errorf("id: want %q, got %v", rec.ID, got)
	}
	if got := out.Results[0]["memory"]; got != rec.Text {
		t.Errorf("memory: want %q, got %v", rec.Text, got)
	}
	if got := out.Results[0]["event"]; got != "ADD" {
		t.Errorf("event: want ADD, got %v", got)
	}
}

func TestAddMemory_StringMessages(t *testing.T) {
	t.Parallel()

	svc := &stubService{addResp: []engram.MemoryRecord{}}
	h := newTestHandler(svc)
	body := map[string]any{
		"messages": []string{"I prefer Go"},
		"user_id":  "bob",
	}
	resp := doJSON(t, h, http.MethodPost, "/memories", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if want, got := []string{"I prefer Go"}, svc.addReq.Messages; !equalStrings(want, got) {
		t.Errorf("Messages: want %v, got %v", want, got)
	}
}

func TestAddMemory_BadJSON(t *testing.T) {
	t.Parallel()
	h := newTestHandler(&stubService{})
	req := httptest.NewRequest(http.MethodPost, "/memories", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
}

func TestAddMemory_MissingMessages(t *testing.T) {
	t.Parallel()
	h := newTestHandler(&stubService{})
	resp := doJSON(t, h, http.MethodPost, "/memories", map[string]any{"user_id": "alice"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", resp.Code)
	}
}

// --- POST /search -----------------------------------------------------------

func TestSearch_FiltersOnUserID(t *testing.T) {
	t.Parallel()

	rec := engram.MemoryRecord{ID: "01H8ZZZZZZZZZZZZZZZZZZZZZZ", Text: "alice likes Go"}
	svc := &stubService{searchResp: []engramsvc.SearchResult{{Record: rec, Score: 0.91}}}
	h := newTestHandler(svc)

	body := map[string]any{
		"query": "Go preferences",
		"limit": 5,
		"filters": map[string]any{
			"user_id": "alice",
			"app_id":  "cursor",
		},
	}
	resp := doJSON(t, h, http.MethodPost, "/search", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := svc.searchReq.UserID; got != "alice" {
		t.Errorf("UserID: want alice, got %q", got)
	}
	if got := svc.searchReq.AppID; got != "cursor" {
		t.Errorf("AppID: want cursor, got %q", got)
	}
	if got := svc.searchReq.TopK; got != 5 {
		t.Errorf("TopK: want 5, got %d", got)
	}
	if got := svc.searchReq.Query; got != "Go preferences" {
		t.Errorf("Query: want %q, got %q", "Go preferences", got)
	}

	var arr []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &arr); err != nil {
		t.Fatalf("Mem0 OSS /search returns a JSON array; unmarshal: %v body=%s", err, resp.Body.String())
	}
	if len(arr) != 1 {
		t.Fatalf("results: want 1, got %d", len(arr))
	}
	if got := arr[0]["id"]; got != string(rec.ID) {
		t.Errorf("id: want %q, got %v", rec.ID, got)
	}
	if got := arr[0]["memory"]; got != rec.Text {
		t.Errorf("memory: want %q, got %v", rec.Text, got)
	}
	if score, ok := arr[0]["score"].(float64); !ok || score < 0.9 {
		t.Errorf("score: want >=0.9 float, got %v", arr[0]["score"])
	}
}

func TestSearch_DefaultLimit(t *testing.T) {
	t.Parallel()
	svc := &stubService{}
	h := newTestHandler(svc)
	body := map[string]any{"query": "anything", "filters": map[string]any{"user_id": "bob"}}
	resp := doJSON(t, h, http.MethodPost, "/search", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.Code)
	}
	if got := svc.searchReq.TopK; got != 10 {
		t.Errorf("TopK default: want 10, got %d", got)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	t.Parallel()
	h := newTestHandler(&stubService{})
	resp := doJSON(t, h, http.MethodPost, "/search", map[string]any{"query": ""})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", resp.Code)
	}
}

// --- GET /memories ----------------------------------------------------------

func TestGetAllMemories_FiltersOnQuery(t *testing.T) {
	t.Parallel()
	svc := &stubService{getAllResp: []engram.MemoryRecord{{ID: "01H", Text: "x", UserID: "alice"}}}
	h := newTestHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/memories?user_id=alice&app_id=cursor&limit=20", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	if got := svc.getAllFilter.UserID; got != "alice" {
		t.Errorf("UserID: want alice, got %q", got)
	}
	if got := svc.getAllFilter.AppID; got != "cursor" {
		t.Errorf("AppID: want cursor, got %q", got)
	}
	var arr []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	if len(arr) != 1 {
		t.Fatalf("len: want 1, got %d", len(arr))
	}
}

// --- GET /memories/{id} -----------------------------------------------------

func TestGetMemory_ReturnsRecord(t *testing.T) {
	t.Parallel()
	svc := &stubService{getResp: engram.MemoryRecord{ID: "01H", Text: "hi"}}
	h := newTestHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/memories/01H", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := svc.getReq.ID; got != engram.MemoryID("01H") {
		t.Errorf("ID: want 01H, got %q", got)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := out["id"]; got != "01H" {
		t.Errorf("id: want 01H, got %v", got)
	}
	if got := out["memory"]; got != "hi" {
		t.Errorf("memory: want hi, got %v", got)
	}
}

func TestGetMemory_NotFound(t *testing.T) {
	t.Parallel()
	svc := &stubService{getErr: engram.ErrNotFound}
	h := newTestHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/memories/missing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", rec.Code)
	}
}

// --- PUT /memories/{id} -----------------------------------------------------

func TestUpdateMemory_PutTextField(t *testing.T) {
	t.Parallel()
	svc := &stubService{updateResp: engram.MemoryRecord{ID: "01H", Text: "new text"}}
	h := newTestHandler(svc)
	resp := doJSON(t, h, http.MethodPut, "/memories/01H", map[string]any{"text": "new text"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := svc.updateReq.ID; got != engram.MemoryID("01H") {
		t.Errorf("ID: want 01H, got %q", got)
	}
	if got := svc.updateReq.Text; got != "new text" {
		t.Errorf("Text: want new text, got %q", got)
	}
}

func TestUpdateMemory_NotFound(t *testing.T) {
	t.Parallel()
	svc := &stubService{updateErr: engram.ErrNotFound}
	h := newTestHandler(svc)
	resp := doJSON(t, h, http.MethodPut, "/memories/missing", map[string]any{"text": "x"})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", resp.Code)
	}
}

// --- DELETE /memories/{id} --------------------------------------------------

func TestDeleteMemory_Success(t *testing.T) {
	t.Parallel()
	svc := &stubService{}
	h := newTestHandler(svc)
	req := httptest.NewRequest(http.MethodDelete, "/memories/01H", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	if got := svc.deleteReq.ID; got != engram.MemoryID("01H") {
		t.Errorf("ID: want 01H, got %q", got)
	}
	// Mem0 OSS returns {"message": "Memory deleted successfully!"}.
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := out["message"]; !ok {
		t.Fatalf("response missing message key: %v", out)
	}
}

func TestDeleteMemory_NotFound(t *testing.T) {
	t.Parallel()
	svc := &stubService{deleteErr: engram.ErrNotFound}
	h := newTestHandler(svc)
	req := httptest.NewRequest(http.MethodDelete, "/memories/missing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", rec.Code)
	}
}

// --- GET /memories/{id}/history --------------------------------------------

func TestHistory_ReturnsEvents(t *testing.T) {
	t.Parallel()
	svc := &stubService{historyResp: []engram.MemoryEvent{
		{Event: engram.EventAdd, ID: "01H", Text: "hi"},
		{Event: engram.EventUpdate, ID: "01H", Text: "hi v2", OldMemory: &engram.MemoryRecord{ID: "01H", Text: "hi"}},
	}}
	h := newTestHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/memories/01H/history", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := svc.historyID; got != engram.MemoryID("01H") {
		t.Errorf("historyID: want 01H, got %q", got)
	}
	var arr []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("len: want 2, got %d", len(arr))
	}
	if got := arr[0]["event"]; got != "ADD" {
		t.Errorf("event[0]: want ADD, got %v", got)
	}
	if got := arr[1]["event"]; got != "UPDATE" {
		t.Errorf("event[1]: want UPDATE, got %v", got)
	}
}

// --- internal-error path ----------------------------------------------------

func TestAddMemory_InternalError(t *testing.T) {
	t.Parallel()
	svc := &stubService{addErr: errors.New("boom")}
	h := newTestHandler(svc)
	resp := doJSON(t, h, http.MethodPost, "/memories", map[string]any{
		"messages": []string{"x"},
		"user_id":  "alice",
	})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status: want 500, got %d", resp.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := out["detail"]; !ok {
		t.Fatalf("error response missing detail: %v", out)
	}
}

// --- API-key gate end-to-end ------------------------------------------------

func TestHandler_APIKeyGateRejectsMissing(t *testing.T) {
	t.Parallel()
	svc := &stubService{}
	h := NewHandler(svc, "secret")

	resp := doJSON(t, h, http.MethodPost, "/memories", map[string]any{
		"messages": []string{"x"},
		"user_id":  "alice",
	})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", resp.Code)
	}
	if svc.addCalls != 0 {
		t.Fatalf("Add calls: want 0 (gate should reject), got %d", svc.addCalls)
	}
}

func TestHandler_APIKeyGateAcceptsCorrect(t *testing.T) {
	t.Parallel()
	svc := &stubService{addResp: []engram.MemoryRecord{{ID: "01H", Text: "hi"}}}
	h := NewHandler(svc, "secret")

	body, _ := json.Marshal(map[string]any{
		"messages": []string{"x"},
		"user_id":  "alice",
	})
	req := httptest.NewRequest(http.MethodPost, "/memories", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if svc.addCalls != 1 {
		t.Fatalf("Add calls: want 1, got %d", svc.addCalls)
	}
}

// --- helpers ---------------------------------------------------------------

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
