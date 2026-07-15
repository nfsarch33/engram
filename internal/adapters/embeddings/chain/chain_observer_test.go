package chain

import (
	"context"
	"errors"
	"testing"
)

// recordingObserver captures every ObserveFallthrough call for assertions.
type recordingObserver struct {
	calls []observerCall
}

type observerCall struct {
	from, to, reason string
}

func (r *recordingObserver) ObserveFallthrough(from, to, reason string) {
	r.calls = append(r.calls, observerCall{from, to, reason})
}

type successEmbedder struct{ name string }

func (s *successEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 2, 3}
	}
	return out, nil
}

type failEmbedder struct{ name string; err error }

func (f *failEmbedder) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, f.err
}

func TestEmbedder_FallsThrough_NotifiesObserver(t *testing.T) {
	rec := &recordingObserver{}
	c, err := New(
		WithProvider("ollama", &failEmbedder{name: "ollama", err: errors.New("connection refused")}),
		WithProvider("minimax", &successEmbedder{name: "minimax"}),
		WithObserver(rec),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.EmbedBatch(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(got))
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 fallthrough call, got %d", len(rec.calls))
	}
	c0 := rec.calls[0]
	if c0.from != "ollama" {
		t.Errorf("call[0].from = %q, want %q", c0.from, "ollama")
	}
	if c0.to != "minimax" {
		t.Errorf("call[0].to = %q, want %q", c0.to, "minimax")
	}
	if c0.reason != "connection refused" {
		t.Errorf("call[0].reason = %q, want %q", c0.reason, "connection refused")
	}
}

func TestEmbedder_AllProvidersFail_NotifiesObserver_NoneSentinel(t *testing.T) {
	rec := &recordingObserver{}
	c, err := New(
		WithProvider("primary", &failEmbedder{name: "primary", err: errors.New("503")}),
		WithProvider("fallback", &failEmbedder{name: "fallback", err: errors.New("429 rate-limited")}),
		WithObserver(rec),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.EmbedBatch(context.Background(), []string{"x"})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if len(rec.calls) != 2 {
		t.Fatalf("expected 2 fallthrough calls, got %d", len(rec.calls))
	}
	if rec.calls[1].to != "<none>" {
		t.Errorf("last call .to = %q, want %q", rec.calls[1].to, "<none>")
	}
}

func TestEmbedder_FirstProviderSucceeds_NoObserverCall(t *testing.T) {
	rec := &recordingObserver{}
	c, err := New(
		WithProvider("primary", &successEmbedder{name: "primary"}),
		WithProvider("fallback", &successEmbedder{name: "fallback"}),
		WithObserver(rec),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.EmbedBatch(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("expected 0 observer calls, got %d", len(rec.calls))
	}
}

func TestEmbedder_DefaultObserver_NoPanic(t *testing.T) {
	c, err := New(
		WithProvider("primary", &failEmbedder{name: "primary", err: errors.New("x")}),
		WithProvider("fallback", &successEmbedder{name: "fallback"}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.EmbedBatch(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
}

func TestEmbedder_NilObserver_FallsBackToNoop(t *testing.T) {
	c, err := New(
		WithProvider("primary", &failEmbedder{name: "primary", err: errors.New("x")}),
		WithProvider("fallback", &successEmbedder{name: "fallback"}),
		WithObserver(nil),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.EmbedBatch(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
}

func TestEmbedder_EmptyInputs_NoObserverCall(t *testing.T) {
	rec := &recordingObserver{}
	c, err := New(
		WithProvider("a", &successEmbedder{name: "a"}),
		WithObserver(rec),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out, err := c.EmbedBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty output, got %v", out)
	}
	if len(rec.calls) != 0 {
		t.Errorf("expected 0 observer calls on empty input, got %d", len(rec.calls))
	}
}

func TestPrometheusObserver_DoesNotPanicOnConstruction(t *testing.T) {
	obs := NewPrometheusObserver()
	if obs == nil {
		t.Fatal("nil observer")
	}
	obs.ObserveFallthrough("a", "b", "test")
}