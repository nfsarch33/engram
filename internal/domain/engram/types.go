package engram

import "time"

// MemoryID is a typed string identifier for a memory record (ULID format).
type MemoryID string

// MemoryRecord holds a single consolidated memory entry plus its scoping metadata.
type MemoryRecord struct {
	ID          MemoryID
	Text        string
	Metadata    map[string]any
	UserID      string
	AgentID     string
	RunID       string
	AppID       string
	WorkspaceID string
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
