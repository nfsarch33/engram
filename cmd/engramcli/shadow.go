package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// runShadow implements the dual-run shadow mode: writes to both Engram and Mem0,
// reads from Engram, compares results, and logs discrepancies.
func runShadow(deps Deps, args []string) int {
	fs := flag.NewFlagSet("shadow", flag.ContinueOnError)
	engramAddr := fs.String("engram-addr", defaultAddr(), "Engram daemon address")
	mem0Addr := fs.String("mem0-addr", defaultMem0Addr(), "Mem0 OSS HTTP address")
	query := fs.String("query", "", "Search query for comparison")
	topK := fs.Int("top-k", 5, "Max results per system")
	userID := fs.String("user-id", "", "User ID for scoped queries")
	logFile := fs.String("log", defaultShadowLog(), "NDJSON log path for discrepancies")
	fs.SetOutput(deps.Stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *query == "" {
		fmt.Fprintln(deps.Stderr, "engramcli shadow: --query is required")
		return 1
	}

	searchBody := map[string]any{
		"query":   *query,
		"top_k":   *topK,
		"user_id": *userID,
	}
	data, _ := json.Marshal(searchBody)

	engramResults, engramErr := doSearch(*engramAddr, data)
	mem0Results, mem0Err := doMem0Search(*mem0Addr, data)

	entry := ShadowLogEntry{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Query:        *query,
		UserID:       *userID,
		TopK:         *topK,
		EngramCount:  len(engramResults),
		Mem0Count:    len(mem0Results),
		EngramError:  errStr(engramErr),
		Mem0Error:    errStr(mem0Err),
		Discrepancy:  false,
		EngramIDs:    extractIDs(engramResults),
		Mem0IDs:      extractIDs(mem0Results),
	}

	if engramErr != nil || mem0Err != nil {
		entry.Discrepancy = true
		entry.Reason = "error in one or both systems"
	} else if entry.EngramCount != entry.Mem0Count {
		entry.Discrepancy = true
		entry.Reason = fmt.Sprintf("count mismatch: engram=%d mem0=%d", entry.EngramCount, entry.Mem0Count)
	} else {
		overlap := computeOverlap(entry.EngramIDs, entry.Mem0IDs)
		entry.Overlap = overlap
		if overlap < 0.5 && entry.EngramCount > 0 {
			entry.Discrepancy = true
			entry.Reason = fmt.Sprintf("low overlap: %.0f%%", overlap*100)
		}
	}

	logEntry(*logFile, entry)

	fmt.Fprintf(deps.Stdout, "shadow: engram=%d mem0=%d overlap=%.0f%% discrepancy=%v\n",
		entry.EngramCount, entry.Mem0Count, entry.Overlap*100, entry.Discrepancy)
	if entry.Reason != "" {
		fmt.Fprintf(deps.Stdout, "  reason: %s\n", entry.Reason)
	}
	return 0
}

// ShadowLogEntry is one NDJSON record written per shadow comparison.
type ShadowLogEntry struct {
	Timestamp   string   `json:"ts"`
	Query       string   `json:"query"`
	UserID      string   `json:"user_id"`
	TopK        int      `json:"top_k"`
	EngramCount int      `json:"engram_count"`
	Mem0Count   int      `json:"mem0_count"`
	EngramError string   `json:"engram_error,omitempty"`
	Mem0Error   string   `json:"mem0_error,omitempty"`
	Discrepancy bool     `json:"discrepancy"`
	Reason      string   `json:"reason,omitempty"`
	Overlap     float64  `json:"overlap"`
	EngramIDs   []string `json:"engram_ids"`
	Mem0IDs     []string `json:"mem0_ids"`
}

func doSearch(addr string, body []byte) ([]map[string]any, error) {
	resp, err := http.Post(addr+"/search", "application/json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Results []map[string]any `json:"results"`
	}
	json.NewDecoder(resp.Body).Decode(&out) //nolint:errcheck
	return out.Results, nil
}

func doMem0Search(addr string, body []byte) ([]map[string]any, error) {
	resp, err := http.Post(addr+"/v1/memories/search/", "application/json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Results []map[string]any `json:"results"`
	}
	json.NewDecoder(resp.Body).Decode(&out) //nolint:errcheck
	return out.Results, nil
}

func extractIDs(results []map[string]any) []string {
	ids := make([]string, 0, len(results))
	for _, r := range results {
		if id, ok := r["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func computeOverlap(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	set := make(map[string]bool, len(b))
	for _, id := range b {
		set[id] = true
	}
	count := 0
	for _, id := range a {
		if set[id] {
			count++
		}
	}
	return float64(count) / float64(max(len(a), len(b)))
}

func logEntry(path string, entry ShadowLogEntry) {
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0o755) //nolint:errcheck
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	json.NewEncoder(f).Encode(entry) //nolint:errcheck
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func defaultMem0Addr() string {
	if v := os.Getenv("MEM0_ADDR"); v != "" {
		return v
	}
	return "http://127.0.0.1:8888"
}

func defaultShadowLog() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "logs", "runx", "engram-shadow.ndjson")
}
