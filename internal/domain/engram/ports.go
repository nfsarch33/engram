package engram

import "context"

// VectorRecord is the unit sent to and from a vector database.
type VectorRecord struct {
	ID      MemoryID
	Vector  []float32
	Payload map[string]any
}

// VectorQuery describes a semantic search request.
type VectorQuery struct {
	Vector      []float32
	TopK        int
	Filters     map[string]any // scoping: user_id, workspace_id, etc.
	WithPayload bool
}

// VectorResult is one hit from a vector search.
type VectorResult struct {
	ID      MemoryID
	Score   float32
	Payload map[string]any
}

// VectorStore is the port for semantic vector operations.
type VectorStore interface {
	EnsureCollection(ctx context.Context, name string, dim int) error
	UpsertBatch(ctx context.Context, records []VectorRecord) error
	Search(ctx context.Context, q VectorQuery) ([]VectorResult, error)
	DeleteBatch(ctx context.Context, ids []MemoryID) error
}

// HistoryFilter scopes history queries to a particular agent/user/run context.
type HistoryFilter struct {
	UserID      string
	AgentID     string
	RunID       string
	AppID       string
	WorkspaceID string
}

// HistoryStore is the port for durable record and event persistence.
type HistoryStore interface {
	SaveRecord(ctx context.Context, rec MemoryRecord) error
	UpdateRecord(ctx context.Context, rec MemoryRecord) error
	DeleteRecord(ctx context.Context, id MemoryID) error
	GetRecord(ctx context.Context, id MemoryID) (MemoryRecord, error)
	ListRecords(ctx context.Context, filter HistoryFilter) ([]MemoryRecord, error)

	SaveEvents(ctx context.Context, events []MemoryEvent) error
	ListEvents(ctx context.Context, id MemoryID) ([]MemoryEvent, error)
}

// ChatRequest carries a system + user prompt pair to an LLM.
type ChatRequest struct {
	SystemPrompt string
	UserPrompt   string
}

// LLMClient is the port for structured LLM output (JSON mode).
type LLMClient interface {
	ChatJSON(ctx context.Context, req ChatRequest, v any) error
}

// Embedder is the port for turning text slices into vector slices.
type Embedder interface {
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}
