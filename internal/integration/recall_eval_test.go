//go:build integration

// Live recall evaluation against a running daemon (canonical HTTP API).
//
// Gated on ENGRAM_LIVE_URL; skips when unset or when /healthz does not
// answer. Loads testdata/recall/corpus-v1.json, stores its memories under a
// unique user id, runs every query, and scores recall@k and MRR@k. The
// corpus carries its own rubric_version / corpus_version / floor, so a
// number from one version is never compared with a number from another.
//
//	ENGRAM_LIVE_URL=http://127.0.0.1:8280 ENGRAM_RECALL_OUT=/tmp/recall.json \
//	  go test -tags integration -race -count=1 -run TestLiveRecall ./internal/integration/...
//
// The envelope written to ENGRAM_RECALL_OUT (when set) is the sprint's
// measured claim; the test's own assertion is only the floor.
package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	liveURLEnv   = "ENGRAM_LIVE_URL"
	recallOutEnv = "ENGRAM_RECALL_OUT"
)

type recallCorpus struct {
	RubricVersion string  `json:"rubric_version"`
	CorpusVersion string  `json:"corpus_version"`
	TopK          int     `json:"top_k"`
	MinRecallAtK  float64 `json:"min_recall_at_k"`
	Memories      []struct {
		Key  string `json:"key"`
		Text string `json:"text"`
	} `json:"memories"`
	Queries []struct {
		Text   string `json:"text"`
		Expect string `json:"expect"`
	} `json:"queries"`
}

type recallQueryResult struct {
	Query  string   `json:"query"`
	Expect string   `json:"expect"`
	Rank   int      `json:"rank"` // 1-based; 0 = not in top k
	TopK   []string `json:"top_k"`
}

// recallEnvelope is the NDJSON-friendly record a scoreboard consumes.
type recallEnvelope struct {
	RubricVersion string              `json:"rubric_version"`
	CorpusVersion string              `json:"corpus_version"`
	GeneratedAt   time.Time           `json:"generated_at"`
	TopK          int                 `json:"top_k"`
	Memories      int                 `json:"n_memories"`
	Queries       int                 `json:"n_queries"`
	RecallAtK     float64             `json:"recall_at_k"`
	MRRAtK        float64             `json:"mrr_at_k"`
	Floor         float64             `json:"min_recall_at_k"`
	Results       []recallQueryResult `json:"results"`
}

type liveClient struct {
	base string
	http *http.Client
}

func (c *liveClient) postJSON(ctx context.Context, path string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", http.MethodPost, path, resp.StatusCode, data)
	}
	return json.Unmarshal(data, out)
}

func (c *liveClient) deleteAll(ctx context.Context, userID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"/memories?user_id="+userID, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("DELETE /memories: status %d", resp.StatusCode)
	}
	return nil
}

func newLiveClient(t *testing.T) *liveClient {
	t.Helper()
	base := os.Getenv(liveURLEnv)
	if base == "" {
		t.Skipf("%s not set; skipping live recall evaluation", liveURLEnv)
	}
	c := &liveClient{base: base, http: &http.Client{Timeout: 60 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Skipf("daemon at %s not reachable: %v", base, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("daemon at %s unhealthy: %d", base, resp.StatusCode)
	}
	return c
}

func loadCorpus(t *testing.T) recallCorpus {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "recall", "corpus-v1.json"))
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var c recallCorpus
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("decode corpus: %v", err)
	}
	if c.TopK <= 0 || len(c.Memories) == 0 || len(c.Queries) == 0 || c.RubricVersion == "" || c.CorpusVersion == "" {
		t.Fatalf("corpus is incomplete: %+v", c)
	}
	keys := map[string]bool{}
	for _, m := range c.Memories {
		if keys[m.Key] {
			t.Fatalf("duplicate memory key %q", m.Key)
		}
		keys[m.Key] = true
	}
	for _, q := range c.Queries {
		if !keys[q.Expect] {
			t.Fatalf("query %q expects unknown key %q", q.Text, q.Expect)
		}
	}
	return c
}

// TestLiveRecall_CorpusV1 stores the corpus, runs its queries, scores them,
// writes the envelope, and holds the floor.
func TestLiveRecall_CorpusV1(t *testing.T) {
	client := newLiveClient(t)
	corpus := loadCorpus(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	userID := fmt.Sprintf("recall-eval-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := client.deleteAll(cleanupCtx, userID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	idToKey := map[string]string{}
	for _, m := range corpus.Memories {
		var recs []struct {
			ID string `json:"id"`
		}
		err := client.postJSON(ctx, "/memories", map[string]any{
			"messages": []string{m.Text}, "user_id": userID, "app_id": "recall-eval",
		}, &recs)
		if err != nil {
			t.Fatalf("store %q: %v", m.Key, err)
		}
		if len(recs) != 1 || recs[0].ID == "" {
			t.Fatalf("store %q: unexpected response %+v", m.Key, recs)
		}
		idToKey[recs[0].ID] = m.Key
	}

	env := recallEnvelope{
		RubricVersion: corpus.RubricVersion,
		CorpusVersion: corpus.CorpusVersion,
		GeneratedAt:   time.Now().UTC(),
		TopK:          corpus.TopK,
		Memories:      len(corpus.Memories),
		Queries:       len(corpus.Queries),
		Floor:         corpus.MinRecallAtK,
	}
	var hits int
	var mrr float64
	for _, q := range corpus.Queries {
		var out []struct {
			Record struct {
				ID string `json:"id"`
			} `json:"record"`
		}
		err := client.postJSON(ctx, "/search", map[string]any{
			"query": q.Text, "user_id": userID, "top_k": corpus.TopK,
		}, &out)
		if err != nil {
			t.Fatalf("search %q: %v", q.Text, err)
		}
		res := recallQueryResult{Query: q.Text, Expect: q.Expect}
		for i, hit := range out {
			key := idToKey[hit.Record.ID]
			res.TopK = append(res.TopK, key)
			if key == q.Expect && res.Rank == 0 {
				res.Rank = i + 1
			}
		}
		if res.Rank > 0 {
			hits++
			mrr += 1 / float64(res.Rank)
		}
		env.Results = append(env.Results, res)
	}
	env.RecallAtK = float64(hits) / float64(len(corpus.Queries))
	env.MRRAtK = mrr / float64(len(corpus.Queries))

	t.Logf("recall@%d=%.3f mrr@%d=%.3f (%d memories, %d queries, rubric %s / corpus %s)",
		corpus.TopK, env.RecallAtK, corpus.TopK, env.MRRAtK, env.Memories, env.Queries, env.RubricVersion, env.CorpusVersion)
	for _, r := range env.Results {
		if r.Rank != 1 {
			t.Logf("  rank %d for %q (want %s): top=%v", r.Rank, r.Query, r.Expect, r.TopK)
		}
	}

	if out := os.Getenv(recallOutEnv); out != "" {
		data, _ := json.MarshalIndent(env, "", "  ")
		if err := os.WriteFile(out, data, 0o644); err != nil { //nolint:gosec // operator-chosen report path
			t.Fatalf("write %s: %v", out, err)
		}
	}

	if env.RecallAtK < corpus.MinRecallAtK {
		t.Fatalf("recall@%d = %.3f is below the corpus floor %.2f", corpus.TopK, env.RecallAtK, corpus.MinRecallAtK)
	}
}
