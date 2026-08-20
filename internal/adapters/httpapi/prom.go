// prom.go — Prometheus exposition for the Engram HTTP API (v18760).
//
// GET /metrics previously returned an ad-hoc JSON document, which meant the
// fleet's Prometheus could only black-box probe /healthz: Engram had liveness
// monitoring but zero native metrics. /metrics now serves real exposition
// from a handler-scoped registry PLUS the process default registry (Go
// runtime and the embeddings chain's promauto series), while the legacy JSON
// document stays available at /metrics.json for existing SOP consumers.
package httpapi

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// collectTimeout bounds the scrape-time service calls so a wedged store
// cannot stall the whole scrape (Prometheus default scrape timeout is 10s).
const collectTimeout = 5 * time.Second

var (
	memoryCountDesc = prometheus.NewDesc(
		"engram_memory_count",
		"Number of memory records currently stored.",
		nil, nil,
	)
	subsystemUpDesc = prometheus.NewDesc(
		"engram_subsystem_up",
		"Per-subsystem health from the service health check (1 healthy, 0 degraded).",
		[]string{"subsystem"}, nil,
	)
	scrapeErrorsDesc = prometheus.NewDesc(
		"engram_metrics_scrape_errors_total",
		"Count of scrape-time service errors (memory enumeration failures).",
		nil, nil,
	)
)

// svcCollector derives gauges from the live service at scrape time. It is
// registered on a per-handler registry so tests constructing many handlers
// never collide on duplicate registration.
type svcCollector struct {
	h            *Handler
	scrapeErrors atomic.Int64
}

func (c *svcCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- memoryCountDesc
	ch <- subsystemUpDesc
	ch <- scrapeErrorsDesc
}

func (c *svcCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()

	if recs, err := c.h.svc.GetAll(ctx, engram.HistoryFilter{}); err == nil {
		ch <- prometheus.MustNewConstMetric(memoryCountDesc, prometheus.GaugeValue, float64(len(recs)))
	} else {
		c.scrapeErrors.Add(1)
	}

	health := c.h.svc.HealthCheck(ctx)
	for name, state := range health.Subsystem {
		up := 0.0
		if state == "ok" {
			up = 1.0
		}
		ch <- prometheus.MustNewConstMetric(subsystemUpDesc, prometheus.GaugeValue, up, name)
	}

	ch <- prometheus.MustNewConstMetric(scrapeErrorsDesc, prometheus.CounterValue, float64(c.scrapeErrors.Load()))
}

// promHandler builds the exposition endpoint: handler-scoped registry (the
// service collector) merged with the process default gatherer (Go runtime +
// promauto users such as the embeddings chain).
func (h *Handler) promHandler() (http.Handler, error) {
	reg := prometheus.NewRegistry()
	if err := reg.Register(&svcCollector{h: h}); err != nil {
		return nil, err
	}
	gatherers := prometheus.Gatherers{reg, prometheus.DefaultGatherer}
	return promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{}), nil
}
