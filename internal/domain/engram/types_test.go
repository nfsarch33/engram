package engram_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

func TestMemoryRecordFields(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	rec := engram.MemoryRecord{
		ID:          "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Text:        "user prefers dark mode",
		Metadata:    map[string]any{"source": "chat"},
		UserID:      "u1",
		AgentID:     "a1",
		RunID:       "r1",
		AppID:       "app1",
		WorkspaceID: "ws1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if rec.ID == "" {
		t.Error("ID should not be empty")
	}
	if rec.Text == "" {
		t.Error("Text should not be empty")
	}
	if rec.UserID != "u1" {
		t.Errorf("UserID: got %q, want %q", rec.UserID, "u1")
	}
	if rec.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestFactFields(t *testing.T) {
	t.Parallel()
	f := engram.Fact{
		Text:   "user is a software engineer",
		Type:   "identity",
		Source: "msg-001",
	}
	if f.Text == "" {
		t.Error("Fact.Text should not be empty")
	}
	if f.Type != "identity" {
		t.Errorf("Fact.Type: got %q, want identity", f.Type)
	}
}

func TestMemoryEventTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		et   engram.MemoryEventType
		want string
	}{
		{engram.EventAdd, "add"},
		{engram.EventUpdate, "update"},
		{engram.EventDelete, "delete"},
		{engram.EventNone, "none"},
	}
	for _, tc := range cases {
		if string(tc.et) != tc.want {
			t.Errorf("MemoryEventType: got %q, want %q", tc.et, tc.want)
		}
	}
}

func TestMemoryEvent(t *testing.T) {
	t.Parallel()
	old := &engram.MemoryRecord{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Text: "old text"}
	ev := engram.MemoryEvent{
		Event:     engram.EventUpdate,
		ID:        "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Text:      "new text",
		OldMemory: old,
	}
	if ev.OldMemory == nil {
		t.Error("OldMemory should be set on update event")
	}
	if ev.OldMemory.Text != "old text" {
		t.Errorf("OldMemory.Text: got %q, want %q", ev.OldMemory.Text, "old text")
	}
}
