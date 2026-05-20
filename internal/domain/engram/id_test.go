package engram_test

import (
	"testing"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

func TestNewMemoryID(t *testing.T) {
	t.Parallel()
	id1 := engram.NewMemoryID()
	id2 := engram.NewMemoryID()
	if id1 == "" {
		t.Error("NewMemoryID returned empty string")
	}
	if id1 == id2 {
		t.Error("Two consecutive IDs must not be equal")
	}
	if len(string(id1)) != 26 {
		t.Errorf("ULID should be 26 chars, got %d", len(string(id1)))
	}
}

func TestParseMemoryID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid ulid", input: "01ARZ3NDEKTSV4RRFFQ69G5FAV", wantErr: false},
		{name: "empty string", input: "", wantErr: true},
		{name: "garbage", input: "not-a-ulid", wantErr: true},
		{name: "too short", input: "01ARZ3NDE", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, err := engram.ParseMemoryID(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
			}
			if string(id) != tc.input {
				t.Errorf("ParseMemoryID: got %q, want %q", id, tc.input)
			}
		})
	}
}

func TestParseMemoryIDRoundTrip(t *testing.T) {
	t.Parallel()
	id := engram.NewMemoryID()
	parsed, err := engram.ParseMemoryID(string(id))
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}
	if parsed != id {
		t.Errorf("round-trip mismatch: got %q, want %q", parsed, id)
	}
}
