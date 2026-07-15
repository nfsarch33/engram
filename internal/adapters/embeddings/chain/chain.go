// Package chain implements a fallback-chain Embedder that tries multiple
// providers in order until one succeeds.
package chain

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// Embedder wraps multiple engram.Embedder implementations and tries each in
// order. The first successful response wins; if all fail, it returns an error
// aggregating all failures.
type Embedder struct {
	providers []namedProvider
	observer  Observer // default NoopObserver when nil
}

type namedProvider struct {
	name     string
	embedder engram.Embedder
}

// Observer is the metric/log surface the chain reports fallthrough events to.
// Concrete impls live in this package (NoopObserver, PrometheusObserver).
// Keeping this an interface lets unit tests inject counters without depending
// on prometheus/client_golang.
type Observer interface {
	// ObserveFallthrough is called every time the chain skips a provider due
	// to an error. from is the failed provider name, to is the next one tried
	// (or "<none>" if the last provider also failed).
	ObserveFallthrough(from, to, reason string)
}

// NoopObserver discards all events. Default for tests / non-prom builds.
type NoopObserver struct{}

// ObserveFallthrough is a no-op.
func (NoopObserver) ObserveFallthrough(string, string, string) {}

// ObserverOption configures metrics reporting on the chain.
type ObserverOption = Option

// WithObserver attaches a metric/log observer.
func WithObserver(obs Observer) Option {
	return func(c *Embedder) {
		if obs != nil {
			c.observer = obs
		}
	}
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
	c := &Embedder{observer: NoopObserver{}}
	for _, o := range opts {
		o(c)
	}
	if c.observer == nil {
		c.observer = NoopObserver{}
	}
	if len(c.providers) == 0 {
		return nil, fmt.Errorf("chain embedder: at least one provider is required")
	}
	return c, nil
}

// EmbedBatch tries each provider in order. Returns the first successful result.
//
// Every fallthrough (provider failed -> next tried) is reported via the
// Observer. This is the cost-alerting hook: when the fallback is a paid
// provider (api.minimax.chat), Prometheus scrapes the fallthrough counter
// and Alertmanager fires when the daily rate exceeds the configured budget.
// See ops/prometheus/rules/engram-embedder-cost.yaml for the alert rule.
func (c *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	var errs []string
	for i, p := range c.providers {
		vecs, err := p.embedder.EmbedBatch(ctx, texts)
		if err == nil {
			return vecs, nil
		}
		reason := err.Error()
		if i+1 < len(c.providers) {
			next := c.providers[i+1].name
			c.observer.ObserveFallthrough(p.name, next, reason)
			slog.Warn("embedder provider fallthrough",
				"from", p.name, "to", next, "reason", reason)
		} else {
			c.observer.ObserveFallthrough(p.name, "<none>", reason)
			slog.Error("embedder all providers failed",
				"last_failed", p.name, "reason", reason)
		}
		errs = append(errs, fmt.Sprintf("%s: %v", p.name, err))
	}
	return nil, fmt.Errorf("chain embedder: all providers failed: %s", strings.Join(errs, "; "))
}

// ProviderCount returns the number of configured providers.
func (c *Embedder) ProviderCount() int {
	return len(c.providers)
}

// PrometheusObserver is the production observer. It increments a counter
// per (from, to, reason) tuple and is safe for concurrent use.
//
// The counter is registered against the default Prometheus registry on
// first construction via promauto. Tests should not construct this type
// directly - they should use NoopObserver.
type PrometheusObserver struct {
	counter *prometheus.CounterVec
	once    sync.Once
}

// NewPrometheusObserver returns an observer that increments the
// engram_embedder_fallback_total counter on every fallthrough. The counter
// is registered with promauto so a /metrics handler picks it up
// automatically.
func NewPrometheusObserver() *PrometheusObserver {
	obs := &PrometheusObserver{}
	obs.once.Do(func() {
		obs.counter = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "engram_embedder_fallback_total",
				Help: "Embedder provider fallthroughs (provider failed, next tried). " +
					"Cost-alerting hook: rate of to=<paid> indicates spend on paid fallback.",
			},
			[]string{"from", "to", "reason"},
		)
	})
	return obs
}

// ObserveFallthrough increments the counter.
func (p *PrometheusObserver) ObserveFallthrough(from, to, reason string) {
	p.counter.WithLabelValues(from, to, truncate(reason, 200)).Inc()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}