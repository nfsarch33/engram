package mem0compat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// Service is the slice of engramsvc.Service used by the shim. Defining it
// as an interface here keeps the adapter testable without spinning up the
// full service graph; the cmd wire-up passes a real *engramsvc.Service.
type Service interface {
	Add(ctx context.Context, req engramsvc.AddRequest) ([]engram.MemoryRecord, error)
	Search(ctx context.Context, req engramsvc.SearchRequest) ([]engramsvc.SearchResult, error)
	Get(ctx context.Context, req engramsvc.GetRequest) (engram.MemoryRecord, error)
	GetAll(ctx context.Context, filter engram.HistoryFilter) ([]engram.MemoryRecord, error)
	Update(ctx context.Context, req engramsvc.UpdateRequest) (engram.MemoryRecord, error)
	Delete(ctx context.Context, req engramsvc.DeleteRequest) error
	History(ctx context.Context, id engram.MemoryID) ([]engram.MemoryEvent, error)
}

// Handler is an http.Handler that exposes the Mem0 OSS wire surface backed
// by the Engram service. It is mounted on its own port (default :8281) so
// existing /memories etc. routes on the canonical Engram API stay intact.
type Handler struct {
	svc    Service
	mux    *http.ServeMux
	logger *slog.Logger
	apiKey string
}

// NewHandler wires the routes and returns a ready Handler. Passing an empty
// apiKey disables the gate; this matches engramd's existing convention.
func NewHandler(svc Service, apiKey string) *Handler {
	h := &Handler{
		svc:    svc,
		mux:    http.NewServeMux(),
		logger: slog.Default(),
		apiKey: apiKey,
	}
	h.mux.HandleFunc("POST /memories", h.add)
	h.mux.HandleFunc("GET /memories", h.getAll)
	h.mux.HandleFunc("POST /search", h.search)
	h.mux.HandleFunc("GET /memories/{id}", h.get)
	h.mux.HandleFunc("PUT /memories/{id}", h.update)
	h.mux.HandleFunc("DELETE /memories/{id}", h.delete)
	h.mux.HandleFunc("GET /memories/{id}/history", h.history)
	h.mux.HandleFunc("GET /healthz", h.healthz)
	h.mux.HandleFunc("GET /auth/setup-status", h.setupStatus)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	APIKeyMiddleware(h.apiKey)(h.mux).ServeHTTP(w, r)
}

// --- request shapes ---------------------------------------------------------

// addRequest mirrors what mem0-mcp-go and the Mem0 OSS UI send. Mem0 accepts
// either a list of role/content message objects or a list of raw strings, so
// we decode messages as []json.RawMessage and normalise.
type addRequest struct {
	Messages []json.RawMessage `json:"messages"`
	UserID   string            `json:"user_id"`
	AgentID  string            `json:"agent_id"`
	RunID    string            `json:"run_id"`
	AppID    string            `json:"app_id"`
	Metadata map[string]any    `json:"metadata"`
	Infer    *bool             `json:"infer"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func normaliseMessages(raw []json.RawMessage) ([]string, error) {
	out := make([]string, 0, len(raw))
	for _, m := range raw {
		// Try as a JSON string first; if that fails, decode as a message object.
		var s string
		if err := json.Unmarshal(m, &s); err == nil {
			if s != "" {
				out = append(out, s)
			}
			continue
		}
		var msg message
		if err := json.Unmarshal(m, &msg); err != nil {
			return nil, err
		}
		if msg.Content != "" {
			out = append(out, msg.Content)
		}
	}
	return out, nil
}

type searchRequest struct {
	Query   string         `json:"query"`
	Limit   int            `json:"limit"`
	Filters map[string]any `json:"filters"`
	// top-level fields are tolerated even though Mem0 OSS rejects them.
	UserID string `json:"user_id"`
	AppID  string `json:"app_id"`
}

type updateRequest struct {
	Text string `json:"text"`
}

// --- handlers ---------------------------------------------------------------

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	msgs, err := normaliseMessages(req.Messages)
	if err != nil {
		writeError(w, http.StatusBadRequest, "messages must be strings or {role,content} objects")
		return
	}
	if len(msgs) == 0 {
		writeError(w, http.StatusBadRequest, "messages must not be empty")
		return
	}

	infer := false
	if req.Infer != nil {
		infer = *req.Infer
	}

	recs, svcErr := h.svc.Add(r.Context(), engramsvc.AddRequest{
		Messages: msgs,
		UserID:   req.UserID,
		AgentID:  req.AgentID,
		RunID:    req.RunID,
		AppID:    req.AppID,
		Metadata: req.Metadata,
		Infer:    infer,
	})
	if svcErr != nil {
		h.respondErr(w, r, svcErr)
		return
	}

	results := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		results = append(results, recordToAddResult(rec))
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query must not be empty")
		return
	}

	userID := req.UserID
	agentID := ""
	runID := ""
	appID := req.AppID
	workspaceID := ""
	if v, ok := stringFromFilters(req.Filters, "user_id"); ok {
		userID = v
	}
	if v, ok := stringFromFilters(req.Filters, "agent_id"); ok {
		agentID = v
	}
	if v, ok := stringFromFilters(req.Filters, "run_id"); ok {
		runID = v
	}
	if v, ok := stringFromFilters(req.Filters, "app_id"); ok {
		appID = v
	}
	if v, ok := stringFromFilters(req.Filters, "workspace_id"); ok {
		workspaceID = v
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	results, err := h.svc.Search(r.Context(), engramsvc.SearchRequest{
		Query:       req.Query,
		UserID:      userID,
		AgentID:     agentID,
		RunID:       runID,
		AppID:       appID,
		WorkspaceID: workspaceID,
		TopK:        limit,
	})
	if err != nil {
		h.respondErr(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(results))
	for _, res := range results {
		obj := recordToReadResult(res.Record)
		obj["score"] = res.Score
		out = append(out, obj)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getAll(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := engram.HistoryFilter{
		UserID:      q.Get("user_id"),
		AgentID:     q.Get("agent_id"),
		RunID:       q.Get("run_id"),
		AppID:       q.Get("app_id"),
		WorkspaceID: q.Get("workspace_id"),
	}
	limit := 0
	if raw := q.Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			limit = v
		}
	}

	recs, err := h.svc.GetAll(r.Context(), filter)
	if err != nil {
		h.respondErr(w, r, err)
		return
	}
	if limit > 0 && len(recs) > limit {
		recs = recs[:limit]
	}
	out := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToReadResult(rec))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	rec, err := h.svc.Get(r.Context(), engramsvc.GetRequest{ID: engram.MemoryID(id)})
	if err != nil {
		h.respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, recordToReadResult(rec))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text must not be empty")
		return
	}
	rec, err := h.svc.Update(r.Context(), engramsvc.UpdateRequest{
		ID:   engram.MemoryID(id),
		Text: req.Text,
	})
	if err != nil {
		h.respondErr(w, r, err)
		return
	}
	out := recordToReadResult(rec)
	out["message"] = "Memory updated successfully!"
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := h.svc.Delete(r.Context(), engramsvc.DeleteRequest{ID: engram.MemoryID(id)}); err != nil {
		h.respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "Memory deleted successfully!"})
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	events, err := h.svc.History(r.Context(), engram.MemoryID(id))
	if err != nil {
		h.respondErr(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		out = append(out, eventToHistoryEntry(ev))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// setupStatus is the unauthenticated probe the Mem0 OSS UI hits before
// injecting credentials. We always claim "configured" because Engram uses
// the API key gate, not a database-backed setup ceremony.
func (h *Handler) setupStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"is_configured": true})
}

// --- shaping helpers --------------------------------------------------------

func recordToReadResult(rec engram.MemoryRecord) map[string]any {
	out := map[string]any{
		"id":     string(rec.ID),
		"memory": rec.Text,
	}
	if !rec.CreatedAt.IsZero() {
		out["created_at"] = rec.CreatedAt
	}
	if !rec.UpdatedAt.IsZero() {
		out["updated_at"] = rec.UpdatedAt
	}
	if rec.UserID != "" {
		out["user_id"] = rec.UserID
	}
	if rec.AgentID != "" {
		out["agent_id"] = rec.AgentID
	}
	if rec.RunID != "" {
		out["run_id"] = rec.RunID
	}
	if rec.AppID != "" {
		out["app_id"] = rec.AppID
	}
	if rec.WorkspaceID != "" {
		out["workspace_id"] = rec.WorkspaceID
	}
	if rec.Metadata != nil {
		out["metadata"] = rec.Metadata
	}
	return out
}

// recordToAddResult mirrors Mem0 OSS POST /memories: each result includes
// {id, memory, event} where event is uppercase ADD/UPDATE/DELETE.
func recordToAddResult(rec engram.MemoryRecord) map[string]any {
	out := recordToReadResult(rec)
	out["event"] = "ADD"
	return out
}

func eventToHistoryEntry(ev engram.MemoryEvent) map[string]any {
	out := map[string]any{
		"id":    string(ev.ID),
		"event": strings.ToUpper(string(ev.Event)),
	}
	if ev.Text != "" {
		out["memory"] = ev.Text
	}
	if ev.OldMemory != nil {
		out["old_memory"] = ev.OldMemory.Text
	}
	return out
}

func stringFromFilters(filters map[string]any, key string) (string, bool) {
	if filters == nil {
		return "", false
	}
	v, ok := filters[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, s != ""
}

// pathID extracts the {id} segment using go1.22+ http.ServeMux PathValue.
func pathID(r *http.Request) string {
	if id := r.PathValue("id"); id != "" {
		return id
	}
	return ""
}

// respondErr maps domain errors to Mem0-shaped JSON responses.
func (h *Handler) respondErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, engram.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, engram.ErrEmptyText), errors.Is(err, engram.ErrInvalidTopK), errors.Is(err, engram.ErrInvalidID):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.logger.Error("mem0compat handler error", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
