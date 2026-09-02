package engramsvc_test

import (
	"reflect"
	"testing"

	"github.com/nfsarch33/engram/internal/app/engramsvc"
)

// TestMessageTexts_AcceptsBothWireShapes pins the write-path contract that
// was broken fleet-wide: strings and chat-style objects both store; empty
// strings, objects without text, and unknown types are dropped in place.
func TestMessageTexts_AcceptsBothWireShapes(t *testing.T) {
	t.Parallel()
	raw := []any{
		"plain string",
		map[string]any{"role": "user", "content": "object with content"},
		map[string]any{"text": "object with text"},
		map[string]any{"role": "assistant", "content": ""},
		map[string]any{"role": "user"},
		"",
		42,
		nil,
		map[string]any{"content": "second content", "text": "text loses to content"},
	}
	got := engramsvc.MessageTexts(raw)
	want := []string{"plain string", "object with content", "object with text", "second content"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MessageTexts:\n got %q\nwant %q", got, want)
	}
}

func TestMessageText_RejectsEmpty(t *testing.T) {
	t.Parallel()
	for _, v := range []any{"", map[string]any{}, map[string]any{"content": 7}, []any{"x"}, nil} {
		if s, ok := engramsvc.MessageText(v); ok {
			t.Errorf("MessageText(%#v) = %q, ok=true; want ok=false", v, s)
		}
	}
	if got := engramsvc.MessageTexts(nil); len(got) != 0 {
		t.Fatalf("MessageTexts(nil) = %q, want empty", got)
	}
}
