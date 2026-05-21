package chain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/engram/internal/adapters/embeddings/chain"
)

type mockEmbedder struct {
	result [][]float32
	err    error
	called bool
}

func (m *mockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestChain_FirstSucceeds(t *testing.T) {
	t.Parallel()
	first := &mockEmbedder{result: [][]float32{{0.1, 0.2}}}
	second := &mockEmbedder{result: [][]float32{{0.9, 0.8}}}

	c, err := chain.New(
		chain.WithProvider("first", first),
		chain.WithProvider("second", second),
	)
	if err != nil {
		t.Fatal(err)
	}

	vecs, err := c.EmbedBatch(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 1 || vecs[0][0] != 0.1 {
		t.Errorf("expected first provider result, got %v", vecs)
	}
	if !first.called {
		t.Error("first provider should have been called")
	}
	if second.called {
		t.Error("second provider should NOT have been called")
	}
}

func TestChain_FallbackOnError(t *testing.T) {
	t.Parallel()
	first := &mockEmbedder{err: errors.New("connection refused")}
	second := &mockEmbedder{result: [][]float32{{0.5, 0.5}}}

	c, err := chain.New(
		chain.WithProvider("ollama", first),
		chain.WithProvider("minimax", second),
	)
	if err != nil {
		t.Fatal(err)
	}

	vecs, err := c.EmbedBatch(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 1 || vecs[0][0] != 0.5 {
		t.Errorf("expected second provider result, got %v", vecs)
	}
	if !first.called || !second.called {
		t.Error("both providers should have been called")
	}
}

func TestChain_AllFail(t *testing.T) {
	t.Parallel()
	first := &mockEmbedder{err: errors.New("ollama down")}
	second := &mockEmbedder{err: errors.New("minimax rate limited")}

	c, err := chain.New(
		chain.WithProvider("ollama", first),
		chain.WithProvider("minimax", second),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.EmbedBatch(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if !first.called || !second.called {
		t.Error("both providers should have been tried")
	}
}

func TestChain_EmptyInput(t *testing.T) {
	t.Parallel()
	first := &mockEmbedder{result: [][]float32{}}

	c, err := chain.New(chain.WithProvider("first", first))
	if err != nil {
		t.Fatal(err)
	}

	vecs, err := c.EmbedBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("want 0 vectors, got %d", len(vecs))
	}
	if first.called {
		t.Error("provider should not be called for empty input")
	}
}

func TestChain_NoProviders(t *testing.T) {
	t.Parallel()
	_, err := chain.New()
	if err == nil {
		t.Fatal("expected error with no providers")
	}
}

func TestChain_ProviderCount(t *testing.T) {
	t.Parallel()
	c, err := chain.New(
		chain.WithProvider("a", &mockEmbedder{}),
		chain.WithProvider("b", &mockEmbedder{}),
		chain.WithProvider("c", &mockEmbedder{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if c.ProviderCount() != 3 {
		t.Errorf("want 3 providers, got %d", c.ProviderCount())
	}
}

func TestChain_ThreeProviders_MiddleSucceeds(t *testing.T) {
	t.Parallel()
	first := &mockEmbedder{err: errors.New("fail")}
	second := &mockEmbedder{result: [][]float32{{0.3}}}
	third := &mockEmbedder{result: [][]float32{{0.9}}}

	c, err := chain.New(
		chain.WithProvider("a", first),
		chain.WithProvider("b", second),
		chain.WithProvider("c", third),
	)
	if err != nil {
		t.Fatal(err)
	}

	vecs, err := c.EmbedBatch(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vecs[0][0] != 0.3 {
		t.Errorf("expected second provider result, got %v", vecs[0][0])
	}
	if !first.called || !second.called {
		t.Error("first and second should be called")
	}
	if third.called {
		t.Error("third should NOT be called")
	}
}
