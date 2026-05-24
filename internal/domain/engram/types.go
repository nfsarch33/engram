package engram

import "time"

// MemoryID is a typed string identifier for a memory record (ULID format).
type MemoryID string

// MemoryRecord holds a single consolidated memory entry plus its scoping metadata.
type MemoryRecord struct {
	ID          MemoryID       `json:"id"`
	Text        string         `json:"text"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	UserID      string         `json:"user_id,omitempty"`
	AgentID     string         `json:"agent_id,omitempty"`
	RunID       string         `json:"run_id,omitempty"`
	AppID       string         `json:"app_id,omitempty"`
	WorkspaceID string         `json:"workspace_id,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Fact is an atomic piece of knowledge extracted from a conversation.
type Fact struct {
	Text   string
	Type   string // e.g. "identity", "preference"
	Source string // optional source message id
}

// MemoryEventType enumerates the LLM-driven memory mutation instructions.
type MemoryEventType string

const (
	EventAdd    MemoryEventType = "add"
	EventUpdate MemoryEventType = "update"
	EventDelete MemoryEventType = "delete"
	EventNone   MemoryEventType = "none"
)

// MemoryEvent represents one instruction from the LLM update prompt.
type MemoryEvent struct {
	Event     MemoryEventType
	ID        MemoryID      // existing record for update/delete; empty for add
	Text      string        // new text for add/update
	OldMemory *MemoryRecord // snapshot before update/delete
}
