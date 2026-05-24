// Package agentrace emits structured NDJSON events for MCP tool calls.
package agentrace

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Event is the JSON shape written per MCP tool invocation.
type Event struct {
	Timestamp string `json:"ts"`
	EventType string `json:"event"`
	Server    string `json:"server"`
	Tool      string `json:"tool"`
	LatencyMS int64  `json:"latency_ms"`
	Success   bool   `json:"success"`
}

// Emitter writes agentrace events to an append-only NDJSON file.
// Safe for concurrent use.
type Emitter struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

// NewEmitter opens (or creates) the log file at path in append mode.
// Returns an error if the file cannot be opened for writing.
func NewEmitter(path string) (*Emitter, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Emitter{
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

// Emit writes a single agentrace event. It is safe for concurrent callers.
// A nil Emitter is a no-op (graceful degradation when logging is disabled).
func (e *Emitter) Emit(tool string, latency time.Duration, success bool) {
	if e == nil {
		return
	}
	ev := Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		EventType: "mcp_call",
		Server:    "engram",
		Tool:      tool,
		LatencyMS: latency.Milliseconds(),
		Success:   success,
	}
	e.mu.Lock()
	e.enc.Encode(ev) //nolint:errcheck
	e.mu.Unlock()
}

// Close flushes and closes the underlying file.
func (e *Emitter) Close() error {
	if e == nil || e.file == nil {
		return nil
	}
	return e.file.Close()
}
