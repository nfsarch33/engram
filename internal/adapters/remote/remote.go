// Package remote implements the MCP adapter's MemoryService over the daemon's
// canonical HTTP API, so `engramd --mcp-stdio --remote <url>` is a thin proxy:
// no local stores, no embedder, one index shared by every client.
//
// Why: the fleet's MCP configuration launched a second engramd per editor
// session with `--no-http --mcp-stdio` and an ENGRAM_BASE_URL it never read.
// That process opened its own history file, had no embedder, and exited at
// startup - every agent write failed while the daemon stayed healthy. A proxy
// cannot diverge from the daemon: what it writes is what search serves.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// Client talks to a running engramd over HTTP.
type Client struct {
	base   string
	http   *http.Client
	apiKey string // sent as Authorization: Bearer on every request when set; never logged
}

// Option customises New.
type Option func(*Client)

// WithAPIKey sends "Authorization: Bearer <key>" on every request. The plane's
// bearer gate (engram.cylrl.dev) requires it; loopback daemons ignore it.
// The key is only ever attached to the request, never returned or logged.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// New returns a client for the daemon at baseURL (scheme://host:port, no
// trailing path). timeout bounds every request.
func New(baseURL string, timeout time.Duration, opts ...Option) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("remote: base URL %q must be scheme://host[:port]", baseURL)
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	c := &Client{base: u.String(), http: &http.Client{Timeout: timeout}}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c, nil
}

// --- MemoryService ------------------------------------------------------------

// Add stores messages through POST /memories.
func (c *Client) Add(ctx context.Context, req engramsvc.AddRequest) ([]engram.MemoryRecord, error) {
	body := map[string]any{
		"messages":     req.Messages,
		"user_id":      req.UserID,
		"agent_id":     req.AgentID,
		"run_id":       req.RunID,
		"app_id":       req.AppID,
		"workspace_id": req.WorkspaceID,
		"metadata":     req.Metadata,
		"infer":        req.Infer,
	}
	var recs []engram.MemoryRecord
	if err := c.do(ctx, http.MethodPost, "/memories", body, &recs); err != nil {
		return nil, err
	}
	return recs, nil
}

// Search runs POST /search.
func (c *Client) Search(ctx context.Context, req engramsvc.SearchRequest) ([]engramsvc.SearchResult, error) {
	body := map[string]any{
		"query":        req.Query,
		"user_id":      req.UserID,
		"agent_id":     req.AgentID,
		"run_id":       req.RunID,
		"app_id":       req.AppID,
		"workspace_id": req.WorkspaceID,
		"top_k":        req.TopK,
	}
	var out []engramsvc.SearchResult
	if err := c.do(ctx, http.MethodPost, "/search", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get fetches GET /memories/{id}.
func (c *Client) Get(ctx context.Context, req engramsvc.GetRequest) (engram.MemoryRecord, error) {
	var rec engram.MemoryRecord
	err := c.do(ctx, http.MethodGet, "/memories/"+url.PathEscape(string(req.ID)), nil, &rec)
	return rec, err
}

// Update sends PUT /memories/{id}.
func (c *Client) Update(ctx context.Context, req engramsvc.UpdateRequest) (engram.MemoryRecord, error) {
	var rec engram.MemoryRecord
	err := c.do(ctx, http.MethodPut, "/memories/"+url.PathEscape(string(req.ID)), map[string]any{"text": req.Text}, &rec)
	return rec, err
}

// Delete sends DELETE /memories/{id}.
func (c *Client) Delete(ctx context.Context, req engramsvc.DeleteRequest) error {
	return c.do(ctx, http.MethodDelete, "/memories/"+url.PathEscape(string(req.ID)), nil, nil)
}

// DeleteAll sends DELETE /memories?<filter>.
func (c *Client) DeleteAll(ctx context.Context, f engram.HistoryFilter) (int, error) {
	var out struct {
		Count int `json:"count"`
	}
	if err := c.do(ctx, http.MethodDelete, "/memories?"+filterQuery(f), nil, &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

// GetAll lists GET /memories?<filter>.
func (c *Client) GetAll(ctx context.Context, f engram.HistoryFilter) ([]engram.MemoryRecord, error) {
	var recs []engram.MemoryRecord
	if err := c.do(ctx, http.MethodGet, "/memories?"+filterQuery(f), nil, &recs); err != nil {
		return nil, err
	}
	return recs, nil
}

// History lists GET /memories/{id}/history.
func (c *Client) History(ctx context.Context, id engram.MemoryID) ([]engram.MemoryEvent, error) {
	var events []engram.MemoryEvent
	if err := c.do(ctx, http.MethodGet, "/memories/"+url.PathEscape(string(id))+"/history", nil, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// HealthCheck reads GET /metrics.json, which carries the daemon's own
// subsystem verdicts; an unreachable daemon reports as degraded rather than
// as an error, matching the in-process signature.
func (c *Client) HealthCheck(ctx context.Context) engramsvc.HealthResult {
	var out struct {
		Status     string            `json:"status"`
		Subsystems map[string]string `json:"subsystems"`
	}
	if err := c.do(ctx, http.MethodGet, "/metrics.json", nil, &out); err != nil {
		return engramsvc.HealthResult{
			Status:    "degraded",
			Service:   "engram",
			Subsystem: map[string]string{"daemon": "error: " + err.Error()},
		}
	}
	if out.Subsystems == nil {
		out.Subsystems = map[string]string{}
	}
	out.Subsystems["daemon"] = "ok"
	return engramsvc.HealthResult{Status: out.Status, Service: "engram", Subsystem: out.Subsystems}
}

// --- transport --------------------------------------------------------------

// do performs one request. 404 maps to engram.ErrNotFound and a 400 whose
// body names the empty-text rule maps to engram.ErrEmptyText, so callers see
// the same sentinels the in-process service returns.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("remote: encode: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return fmt.Errorf("remote: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("remote: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("remote: read response: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return engram.ErrNotFound
	case resp.StatusCode == http.StatusBadRequest && strings.Contains(string(data), "must not be empty"):
		return engram.ErrEmptyText
	case resp.StatusCode >= 300:
		return fmt.Errorf("remote: %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("remote: decode %s %s: %w", method, path, err)
	}
	return nil
}

func filterQuery(f engram.HistoryFilter) string {
	q := url.Values{}
	set := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	set("user_id", f.UserID)
	set("agent_id", f.AgentID)
	set("run_id", f.RunID)
	set("app_id", f.AppID)
	set("workspace_id", f.WorkspaceID)
	return q.Encode()
}

// ErrUnreachable is a convenience for callers probing the daemon at startup.
var ErrUnreachable = errors.New("remote: daemon unreachable")

// auth attaches the bearer when one is configured. The key is only ever put
// on the request; it is never returned in errors or logged.
func (c *Client) auth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// Ping verifies the daemon answers GET /healthz.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	if err != nil {
		return err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: /healthz status %d", ErrUnreachable, resp.StatusCode)
	}
	return nil
}
