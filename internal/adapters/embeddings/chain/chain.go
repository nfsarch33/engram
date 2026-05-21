// Package chain implements a fallback-chain Embedder that tries multiple
// providers in order until one succeeds.
package chain

import (
	"context"
	"fmt"
	"strings"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// Embedder wraps multiple engram.Embedder implementations and tries each in
// order. The first successful response wins; if all fail, it returns an error
// aggregating all failures.
type Embedder struct {
	providers []namedProvider
}

type namedProvider struct {
	name     string
	embedder engram.Embedder
}

// Option configures the chain.
type Option func(*Embedder)

// WithProvider adds a named provider to the chain. Providers are tried in the
// order they are added.
func WithProvider(name string, e engram.Embedder) Option {
	return func(c *Embedder) {
		c.providers = append(c.providers, namedProvider{name: name, embedder: e})
	}
}

// New constructs a chain from the given options. At least one provider is required.
func New(opts ...Option) (*Embedder, error) {
	c := &Embedder{}
	for _, o := range opts {
		o(c)
	}
	if len(c.providers) == 0 {
		return nil, fmt.Errorf("chain embedder: at least one provider is required")
	}
	return c, nil
}

// EmbedBatch tries each provider in order. Returns the first successful result.
func (c *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	var errs []string
	for _, p := range c.providers {
		vecs, err := p.embedder.EmbedBatch(ctx, texts)
		if err == nil {
			return vecs, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", p.name, err))
	}
	return nil, fmt.Errorf("chain embedder: all providers failed: %s", strings.Join(errs, "; "))
}

// ProviderCount returns the number of configured providers.
func (c *Embedder) ProviderCount() int {
	return len(c.providers)
}
