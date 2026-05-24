// Package httpapi provides a minimal JSON HTTP API over the Engram service.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// Handler is an http.Handler that routes Engram API requests.
type Handler struct {
	svc    *engramsvc.Service
	mux    *http.ServeMux
	logger *slog.Logger
}

// NewHandler wires the service into HTTP routes and returns a ready Handler.
func NewHandler(svc *engramsvc.Service) *Handler {
	h := &Handler{
		svc:    svc,
		mux:    http.NewServeMux(),
		logger: slog.Default(),
	}
	h.mux.HandleFunc("POST /memories", h.addMemory)
	h.mux.HandleFunc("POST /search", h.search)
	h.mux.HandleFunc("GET /memories/{id}", h.getMemory)
	h.mux.HandleFunc("PUT /memories/{id}", h.updateMemory)
	h.mux.HandleFunc("DELETE /memories/{id}", h.deleteMemory)
	h.mux.HandleFunc("GET /memories/{id}/history", h.getHistory)
	h.mux.HandleFunc("GET /healthz", h.healthz)
	h.mux.HandleFunc("GET /metrics", h.metrics)
	h.mux.HandleFunc("DELETE /memories", h.deleteAllMemories)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// --- request / response types -----------------------------------------------

type addRequest struct {
	Messages    []string       `json:"messages"`
	UserID      string         `json:"user_id"`
	AgentID     string         `json:"agent_id"`
	RunID       string         `json:"run_id"`
	AppID       string         `json:"app_id"`
	WorkspaceID string         `json:"workspace_id"`
	Metadata    map[string]any `json:"metadata"`
	Infer       bool           `json:"infer"`
}

type searchRequest struct {
	Query       string `json:"query"`
	UserID      string `json:"user_id"`
	AgentID     string `json:"agent_id"`
	RunID       string `json:"run_id"`
	AppID       string `json:"app_id"`
	WorkspaceID string `json:"workspace_id"`
	TopK        int    `json:"top_k"`
}

type updateRequest struct {
	Text string `json:"text"`
}

// --- handlers ---------------------------------------------------------------

func (h *Handler) addMemory(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages must not be empty")
		return
	}

	recs, err := h.svc.Add(r.Context(), engramsvc.AddRequest{
		Messages:    req.Messages,
		UserID:      req.UserID,
		AgentID:     req.AgentID,
		RunID:       req.RunID,
		AppID:       req.AppID,
		WorkspaceID: req.WorkspaceID,
		Metadata:    req.Metadata,
		Infer:       req.Infer,
	})
	if err != nil {
		h.logAndRespond(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, recs)
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
	if req.TopK <= 0 {
		req.TopK = 10
	}

	results, err := h.svc.Search(r.Context(), engramsvc.SearchRequest{
		Query:       req.Query,
		UserID:      req.UserID,
		AgentID:     req.AgentID,
		RunID:       req.RunID,
		AppID:       req.AppID,
		WorkspaceID: req.WorkspaceID,
		TopK:        req.TopK,
	})
	if err != nil {
		h.logAndRespond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (h *Handler) getMemory(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	rec, err := h.svc.Get(r.Context(), engramsvc.GetRequest{ID: engram.MemoryID(id)})
	if err != nil {
		h.logAndRespond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *Handler) updateMemory(w http.ResponseWriter, r *http.Request) {
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
		h.logAndRespond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *Handler) deleteMemory(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := h.svc.Delete(r.Context(), engramsvc.DeleteRequest{ID: engram.MemoryID(id)}); err != nil {
		h.logAndRespond(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getHistory(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	events, err := h.svc.History(r.Context(), engram.MemoryID(id))
	if err != nil {
		h.logAndRespond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok")) //nolint:errcheck
}

// --- helpers ----------------------------------------------------------------

// pathID extracts the {id} segment from URL patterns like /memories/{id}.
// go1.22+ http.ServeMux provides r.PathValue("id") but we support fallback.
func pathID(r *http.Request) string {
	if id := r.PathValue("id"); id != "" {
		return id
	}
	// fallback: last path segment before optional trailing component
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "history" && parts[i] != "" {
			return parts[i]
		}
	}
	return ""
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	recs, err := h.svc.GetAll(r.Context(), engram.HistoryFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "metrics: "+err.Error())
		return
	}
	health := h.svc.HealthCheck(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"memory_count": len(recs),
		"status":       health.Status,
		"subsystems":   health.Subsystem,
	})
}

func (h *Handler) deleteAllMemories(w http.ResponseWriter, r *http.Request) {
	filter := engram.HistoryFilter{
		UserID:  r.URL.Query().Get("user_id"),
		AgentID: r.URL.Query().Get("agent_id"),
		AppID:   r.URL.Query().Get("app_id"),
	}
	count, err := h.svc.DeleteAll(r.Context(), filter)
	if err != nil {
		h.logAndRespond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "count": count})
}

func (h *Handler) logAndRespond(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, engram.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, engram.ErrEmptyText) || errors.Is(err, engram.ErrInvalidTopK) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logger.Error("handler error", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
