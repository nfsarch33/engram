package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"
)

func runMigrate(deps Deps, args []string) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fromMem0 := fs.Bool("from-mem0", false, "Import memories from a Mem0 OSS instance")
	endpoint := fs.String("endpoint", "", "Mem0 OSS HTTP endpoint (e.g. http://127.0.0.1:18888)")
	addr := fs.String("addr", defaultAddr(), "Engram daemon address to import into")
	dryRun := fs.Bool("dry-run", false, "Print what would be imported without writing")
	apiKey := fs.String("api-key", "", "Mem0 API key (if required)")
	fs.SetOutput(deps.Stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if !*fromMem0 {
		fmt.Fprintln(deps.Stderr, "engramcli migrate: --from-mem0 is the only supported source")
		return 1
	}
	if *endpoint == "" {
		fmt.Fprintln(deps.Stderr, "engramcli migrate: --endpoint is required")
		return 1
	}

	fmt.Fprintf(deps.Stdout, "migrating from Mem0 at %s -> Engram at %s\n", *endpoint, *addr)

	memories, err := fetchMem0Memories(*endpoint, *apiKey)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "engramcli migrate: fetch from Mem0: %v\n", err)
		return 1
	}

	fmt.Fprintf(deps.Stdout, "found %d memories in Mem0\n", len(memories))
	if len(memories) == 0 {
		fmt.Fprintln(deps.Stdout, "nothing to migrate")
		return 0
	}

	if *dryRun {
		for i, m := range memories {
			text := m.Memory
			if len(text) > 80 {
				text = text[:80] + "..."
			}
			fmt.Fprintf(deps.Stdout, "  [%d] user=%s text=%q\n", i+1, m.UserID, text)
		}
		fmt.Fprintln(deps.Stdout, "dry run complete; no memories written")
		return 0
	}

	imported, skipped := 0, 0
	for _, m := range memories {
		if m.Memory == "" {
			skipped++
			continue
		}
		if err := importToEngram(*addr, m); err != nil {
			fmt.Fprintf(deps.Stderr, "  WARN: failed to import %s: %v\n", m.ID, err)
			skipped++
			continue
		}
		imported++
	}

	fmt.Fprintf(deps.Stdout, "migration complete: %d imported, %d skipped\n", imported, skipped)
	return 0
}

// mem0Memory is the shape returned by Mem0 OSS GET /memories.
type mem0Memory struct {
	ID          string         `json:"id"`
	Memory      string         `json:"memory"`
	UserID      string         `json:"user_id"`
	AgentID     string         `json:"agent_id"`
	RunID       string         `json:"run_id"`
	AppID       string         `json:"app_id"`
	WorkspaceID string         `json:"workspace_id"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func fetchMem0Memories(endpoint, apiKey string) ([]mem0Memory, error) {
	client := &http.Client{Timeout: 60 * time.Second}

	req, err := http.NewRequest(http.MethodGet, endpoint+"/memories", nil) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s/memories: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Mem0 returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var memories []mem0Memory
	if err := json.NewDecoder(resp.Body).Decode(&memories); err != nil {
		return nil, fmt.Errorf("decode Mem0 response: %w", err)
	}
	return memories, nil
}

func importToEngram(engramAddr string, m mem0Memory) error {
	client := &http.Client{Timeout: 30 * time.Second}

	body := map[string]any{
		"messages": []string{m.Memory},
	}
	if m.UserID != "" {
		body["user_id"] = m.UserID
	}
	if m.AgentID != "" {
		body["agent_id"] = m.AgentID
	}
	if m.RunID != "" {
		body["run_id"] = m.RunID
	}
	if m.AppID != "" {
		body["app_id"] = m.AppID
	}
	if m.Metadata != nil {
		body["metadata"] = m.Metadata
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	resp, err := client.Post(engramAddr+"/memories", "application/json", bytes.NewReader(data)) //nolint:noctx
	if err != nil {
		return fmt.Errorf("POST to Engram: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Engram returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
