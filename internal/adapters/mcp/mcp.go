// Package mcp provides an MCP tool adapter for the Engram service.
package mcp

import (
	"context"
	"fmt"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/nfsarch33/engram/internal/app/engramsvc"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// Adapter exposes the Engram service as a set of MCP tools.
type Adapter struct {
	svc   *engramsvc.Service
	tools []mcplib.Tool
}

// NewAdapter creates an Adapter for the given service. It registers both the
// canonical engram_* tools and mem0_* aliases so Engram can serve as a drop-in
// replacement for mem0-mcp-go.
func NewAdapter(svc *engramsvc.Service) *Adapter {
	a := &Adapter{svc: svc}

	addOpts := []mcplib.ToolOption{
		mcplib.WithArray("messages",
			mcplib.Required(),
			mcplib.Description("List of message strings to store as memories"),
		),
		mcplib.WithString("user_id", mcplib.Description("Owner user ID")),
		mcplib.WithString("agent_id"),
		mcplib.WithString("run_id"),
		mcplib.WithString("app_id"),
		mcplib.WithString("workspace_id"),
		mcplib.WithBoolean("infer", mcplib.Description("Extract facts via LLM before storing")),
	}
	searchOpts := []mcplib.ToolOption{
		mcplib.WithString("query", mcplib.Required(), mcplib.Description("Search query text")),
		mcplib.WithString("user_id"),
		mcplib.WithString("agent_id"),
		mcplib.WithString("run_id"),
		mcplib.WithString("app_id"),
		mcplib.WithString("workspace_id"),
		mcplib.WithNumber("top_k", mcplib.Description("Maximum number of results to return")),
	}
	getOpts := []mcplib.ToolOption{
		mcplib.WithString("id", mcplib.Required(), mcplib.Description("Memory ID")),
	}
	deleteOpts := []mcplib.ToolOption{
		mcplib.WithString("id", mcplib.Required(), mcplib.Description("Memory ID")),
	}
	getAllOpts := []mcplib.ToolOption{
		mcplib.WithString("user_id", mcplib.Description("Filter by user ID")),
		mcplib.WithString("agent_id"),
		mcplib.WithString("app_id"),
		mcplib.WithString("run_id"),
		mcplib.WithString("workspace_id"),
	}

	a.tools = []mcplib.Tool{
		// Canonical engram_* tools.
		mcplib.NewTool("engram_add",
			append([]mcplib.ToolOption{mcplib.WithDescription("Add one or more memories for a user")}, addOpts...)...),
		mcplib.NewTool("engram_search",
			append([]mcplib.ToolOption{mcplib.WithDescription("Search memories by semantic similarity")}, searchOpts...)...),
		mcplib.NewTool("engram_get",
			append([]mcplib.ToolOption{mcplib.WithDescription("Retrieve a single memory by ID")}, getOpts...)...),
		mcplib.NewTool("engram_update",
			mcplib.WithDescription("Update the text content of an existing memory"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Memory ID")),
			mcplib.WithString("text", mcplib.Required(), mcplib.Description("New text content")),
		),
		mcplib.NewTool("engram_delete",
			append([]mcplib.ToolOption{mcplib.WithDescription("Delete a memory by ID")}, deleteOpts...)...),
		mcplib.NewTool("engram_history",
			mcplib.WithDescription("List change history events for a memory"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Memory ID")),
		),

		// Mem0-compatible aliases for drop-in replacement of mem0-mcp-go.
		mcplib.NewTool("mem0_add",
			append([]mcplib.ToolOption{mcplib.WithDescription("Add memories (Mem0-compatible alias)")}, addOpts...)...),
		mcplib.NewTool("mem0_search",
			append([]mcplib.ToolOption{mcplib.WithDescription("Search memories (Mem0-compatible alias)")}, searchOpts...)...),
		mcplib.NewTool("mem0_get_all",
			append([]mcplib.ToolOption{mcplib.WithDescription("List all memories (Mem0-compatible alias)")}, getAllOpts...)...),
		mcplib.NewTool("mem0_delete",
			append([]mcplib.ToolOption{mcplib.WithDescription("Delete a memory (Mem0-compatible alias)")}, deleteOpts...)...),
		mcplib.NewTool("mem0_doctor",
			mcplib.WithDescription("Health check (Mem0-compatible alias)"),
		),
	}
	return a
}

// Tools returns the MCP tool definitions for this adapter.
func (a *Adapter) Tools() []mcplib.Tool {
	return a.tools
}

// HandleTool dispatches a named tool call and returns the result.
func (a *Adapter) HandleTool(ctx context.Context, name string, params map[string]any) (any, error) {
	switch name {
	case "engram_add", "mem0_add":
		return a.handleAdd(ctx, params)
	case "engram_search", "mem0_search":
		return a.handleSearch(ctx, params)
	case "engram_get":
		return a.handleGet(ctx, params)
	case "engram_update":
		return a.handleUpdate(ctx, params)
	case "engram_delete", "mem0_delete":
		return a.handleDelete(ctx, params)
	case "engram_history":
		return a.handleHistory(ctx, params)
	case "mem0_get_all":
		return a.handleGetAll(ctx, params)
	case "mem0_doctor":
		return a.handleDoctor(ctx)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (a *Adapter) handleAdd(ctx context.Context, p map[string]any) (any, error) {
	raw, _ := p["messages"].([]any)
	messages := make([]string, 0, len(raw))
	for _, m := range raw {
		if s, ok := m.(string); ok {
			messages = append(messages, s)
		}
	}
	req := engramsvc.AddRequest{
		Messages:    messages,
		UserID:      strVal(p, "user_id"),
		AgentID:     strVal(p, "agent_id"),
		RunID:       strVal(p, "run_id"),
		AppID:       strVal(p, "app_id"),
		WorkspaceID: strVal(p, "workspace_id"),
	}
	if infer, ok := p["infer"].(bool); ok {
		req.Infer = infer
	}
	return a.svc.Add(ctx, req)
}

func (a *Adapter) handleSearch(ctx context.Context, p map[string]any) (any, error) {
	topK := 10
	if v, ok := p["top_k"].(float64); ok && v > 0 {
		topK = int(v)
	}
	req := engramsvc.SearchRequest{
		Query:       strVal(p, "query"),
		UserID:      strVal(p, "user_id"),
		AgentID:     strVal(p, "agent_id"),
		RunID:       strVal(p, "run_id"),
		AppID:       strVal(p, "app_id"),
		WorkspaceID: strVal(p, "workspace_id"),
		TopK:        topK,
	}
	return a.svc.Search(ctx, req)
}

func (a *Adapter) handleGet(ctx context.Context, p map[string]any) (any, error) {
	return a.svc.Get(ctx, engramsvc.GetRequest{ID: engram.MemoryID(strVal(p, "id"))})
}

func (a *Adapter) handleUpdate(ctx context.Context, p map[string]any) (any, error) {
	return a.svc.Update(ctx, engramsvc.UpdateRequest{
		ID:   engram.MemoryID(strVal(p, "id")),
		Text: strVal(p, "text"),
	})
}

func (a *Adapter) handleDelete(ctx context.Context, p map[string]any) (any, error) {
	if err := a.svc.Delete(ctx, engramsvc.DeleteRequest{ID: engram.MemoryID(strVal(p, "id"))}); err != nil {
		return nil, err
	}
	return map[string]string{"status": "deleted"}, nil
}

func (a *Adapter) handleHistory(ctx context.Context, p map[string]any) (any, error) {
	return a.svc.History(ctx, engram.MemoryID(strVal(p, "id")))
}

func (a *Adapter) handleGetAll(ctx context.Context, p map[string]any) (any, error) {
	return a.svc.GetAll(ctx, engram.HistoryFilter{
		UserID:      strVal(p, "user_id"),
		AgentID:     strVal(p, "agent_id"),
		RunID:       strVal(p, "run_id"),
		AppID:       strVal(p, "app_id"),
		WorkspaceID: strVal(p, "workspace_id"),
	})
}

func (a *Adapter) handleDoctor(ctx context.Context) (any, error) {
	return a.svc.HealthCheck(ctx), nil
}

func strVal(p map[string]any, key string) string {
	if v, ok := p[key].(string); ok {
		return v
	}
	return ""
}
