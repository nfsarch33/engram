// Package minimax implements the engram Embedder port against the MiniMax
// embeddings API (embo-01, 1536-dim).
package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	defaultModel   = "embo-01"
	defaultBaseURL = "https://api.minimax.chat/v1"
	defaultTimeout = 30 * time.Second
)

// Options configures the MiniMax embedder.
type Options struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// Embedder calls the MiniMax embeddings API.
type Embedder struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// New returns an Embedder from opts. Zero-value fields use safe defaults.
func New(opts Options) *Embedder {
	base := opts.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	model := opts.Model
	if model == "" {
		model = defaultModel
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Embedder{
		baseURL:    strings.TrimRight(base, "/"),
		apiKey:     opts.APIKey,
		model:      model,
		httpClient: &http.Client{Timeout: timeout},
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
	Type  string   `json:"type"`
}

type embedObject struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type embedResponse struct {
	Data []embedObject `json:"data"`
}

type apiError struct {
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

// EmbedBatch sends texts to the MiniMax embeddings endpoint.
// embo-01 produces 1536-dim vectors.
func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	body, err := json.Marshal(embedRequest{
		Model: e.model,
		Input: texts,
		Type:  "db",
	})
	if err != nil {
		return nil, fmt.Errorf("minimax embedder: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("minimax embedder: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("minimax embedder: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp apiError
		json.NewDecoder(resp.Body).Decode(&errResp) //nolint:errcheck
		msg := errResp.BaseResp.StatusMsg
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("minimax embedder: api error %d: %s", resp.StatusCode, msg)
	}

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("minimax embedder: decode response: %w", err)
	}

	vecs := make([][]float32, len(texts))
	for _, obj := range result.Data {
		if obj.Index < len(vecs) {
			vecs[obj.Index] = obj.Embedding
		}
	}

	for i, v := range vecs {
		if v == nil {
			return nil, fmt.Errorf("minimax embedder: missing embedding at index %d", i)
		}
	}

	return vecs, nil
}
