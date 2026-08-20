package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestMetrics_PrometheusExposition pins the v18760 contract: GET /metrics
// serves Prometheus text exposition (not JSON), carrying both the
// engram-specific series and the default-registry series (Go runtime plus
// anything the embeddings chain registers via promauto).
func TestMetrics_PrometheusExposition(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)

	resp := postJSON(t, srv.URL+"/memories", map[string]any{
		"messages": []string{"prom exposition memory"},
		"user_id":  "prom-user",
	})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed memory: status %d", resp.StatusCode)
	}

	mresp := getJSON(t, srv.URL+"/metrics")
	defer mresp.Body.Close()
	if mresp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics status = %d", mresp.StatusCode)
	}
	ct := mresp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("/metrics content-type = %q, want Prometheus text exposition", ct)
	}
	body, _ := io.ReadAll(mresp.Body)
	text := string(body)
	for _, want := range []string{
		"# TYPE engram_memory_count gauge",
		"engram_memory_count 1",
		"engram_subsystem_up{subsystem=",
		"go_goroutines", // default gatherer must be included
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("/metrics missing %q\n----\n%s", want, text[:min(len(text), 800)])
		}
	}
	if strings.Contains(text, "\"memory_count\"") {
		t.Fatal("/metrics still serves the legacy JSON shape")
	}
}

// TestMetricsJSON_LegacyShapePreserved keeps the pre-v18760 JSON consumers
// working at /metrics.json (the SOPs reference the JSON shape).
func TestMetricsJSON_LegacyShapePreserved(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)

	resp := getJSON(t, srv.URL+"/metrics.json")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics.json status = %d", resp.StatusCode)
	}
	var payload struct {
		MemoryCount *int              `json:"memory_count"`
		Status      string            `json:"status"`
		Subsystems  map[string]string `json:"subsystems"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("/metrics.json not JSON: %v", err)
	}
	if payload.MemoryCount == nil || payload.Status == "" {
		t.Fatalf("legacy payload incomplete: %+v", payload)
	}
}

// TestMetrics_SubsystemGaugeReflectsHealth asserts healthy subsystems export
// as 1 so alerting can key on engram_subsystem_up == 0.
func TestMetrics_SubsystemGaugeReflectsHealth(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)

	resp := getJSON(t, srv.URL+"/metrics")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	found := false
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "engram_subsystem_up{") {
			found = true
			if !strings.HasSuffix(strings.TrimSpace(line), " 1") {
				t.Fatalf("healthy subsystem not exported as 1: %q", line)
			}
		}
	}
	if !found {
		t.Fatal("no engram_subsystem_up series exported")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
