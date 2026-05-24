package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// runShadowWrite implements the dual-write reconciler. The legacy memory
// service is the canonical (primary) path; Engram is the secondary mirror.
// The harness:
//
//  1. Sends the same payload to both endpoints in parallel.
//  2. Records {a, b, ts, app_id, payload_hash} to NDJSON for daily audit.
//  3. Returns non-zero only if the canonical path failed; secondary failures
//     are logged as `diverged=true` but never block the canonical write.
//
// The allow-list flag enforces SOP §3.1: only configured app_ids are mirrored;
// every other app_id skips the secondary write but still logs `skipped=true`
// so an audit can prove the safeguard is in effect.
func runShadowWrite(deps Deps, args []string) int {
	fs := flag.NewFlagSet("shadow-write", flag.ContinueOnError)
	engramAddr := fs.String("engram-addr", defaultAddr(), "Engram daemon address (secondary)")
	mem0Addr := fs.String("mem0-addr", defaultMem0Addr(), "Mem0 OSS address (canonical primary)")
	appID := fs.String("app-id", "default", "Calling app_id (Mem0 metadata)")
	allowApp := fs.String("allow-app", "default", "Comma-separated app_ids to dual-write; other values write Mem0 only")
	userID := fs.String("user-id", "", "Optional user_id metadata")
	agentID := fs.String("agent-id", "", "Optional agent_id metadata")
	message := fs.String("message", "", "Message in role:content form (required)")
	logPath := fs.String("log", defaultShadowWriteLog(), "NDJSON audit log path")
	timeout := fs.Duration("timeout", 10*time.Second, "Per-endpoint write timeout")
	apiKey := fs.String("engram-api-key", os.Getenv("ENGRAM_API_KEY"), "X-API-Key for Engram (defaults to $ENGRAM_API_KEY)")
	fs.SetOutput(deps.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *message == "" {
		fmt.Fprintln(deps.Stderr, "engramcli shadow-write: --message is required")
		return 2
	}
	role, content, ok := strings.Cut(*message, ":")
	if !ok {
		fmt.Fprintln(deps.Stderr, "engramcli shadow-write: --message must be role:content")
		return 2
	}

	body := map[string]any{
		"messages": []map[string]string{{"role": role, "content": content}},
	}
	if *userID != "" {
		body["user_id"] = *userID
	}
	if *agentID != "" {
		body["agent_id"] = *agentID
	}
	if *appID != "" {
		body["app_id"] = *appID
	}
	payload, _ := json.Marshal(body)
	hash := payloadHash(payload)

	allowed := false
	for _, a := range strings.Split(*allowApp, ",") {
		if strings.TrimSpace(a) == *appID {
			allowed = true
			break
		}
	}

	rec := ShadowWriteRecord{
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		AppID:       *appID,
		PayloadHash: hash,
	}

	client := &http.Client{Timeout: *timeout}

	// Canonical write first; capture id A.
	idA, mem0Err := postMemory(client, strings.TrimRight(*mem0Addr, "/")+"/v1/memories/", payload, "")
	rec.A = idA
	if mem0Err != nil {
		rec.Mem0Error = mem0Err.Error()
		rec.Diverged = true
	}

	if !allowed {
		rec.Skipped = true
		rec.Reason = "app_id not in --allow-app list"
		appendNDJSON(*logPath, rec)
		fmt.Fprintf(deps.Stdout, "shadow-write: skipped (app_id=%s not allow-listed); a=%s\n", *appID, idA)
		if mem0Err != nil {
			return 1
		}
		return 0
	}

	idB, engramErr := postMemory(client, strings.TrimRight(*engramAddr, "/")+"/memories", payload, *apiKey)
	rec.B = idB
	if engramErr != nil {
		rec.EngramError = engramErr.Error()
		rec.Diverged = true
	}

	appendNDJSON(*logPath, rec)
	fmt.Fprintf(deps.Stdout, "shadow-write: a=%s b=%s diverged=%v\n", rec.A, rec.B, rec.Diverged)
	if mem0Err != nil {
		// Canonical path failed -- caller must see non-zero so it can retry.
		fmt.Fprintf(deps.Stderr, "shadow-write: canonical (mem0) failed: %v\n", mem0Err)
		return 1
	}
	return 0
}

// ShadowWriteRecord is one NDJSON audit row appended per dual-write attempt.
// The {a, b, ts, app_id, payload_hash} schema matches SOP step 2 of §3.
type ShadowWriteRecord struct {
	Timestamp   string `json:"ts"`
	AppID       string `json:"app_id"`
	A           string `json:"a"`
	B           string `json:"b,omitempty"`
	PayloadHash string `json:"payload_hash"`
	Diverged    bool   `json:"diverged,omitempty"`
	Skipped     bool   `json:"skipped,omitempty"`
	Mem0Error   string `json:"mem0_error,omitempty"`
	EngramError string `json:"engram_error,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

func payloadHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func postMemory(c *http.Client, url string, payload []byte, apiKey string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode id: %w", err)
	}
	return out.ID, nil
}

var ndjsonMu sync.Mutex

func appendNDJSON(path string, rec ShadowWriteRecord) {
	ndjsonMu.Lock()
	defer ndjsonMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(rec)
}

func defaultShadowWriteLog() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".engram", "shadow.ndjson")
}
