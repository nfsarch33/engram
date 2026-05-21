// Package ollama implements the engram Embedder port against a local Ollama
// instance using its /api/embed endpoint (native format, not OpenAI compat).
package ollama

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
	defaultModel   = "nomic-embed-text"
	defaultBaseURL = "http://127.0.0.1:11434"
	defaultTimeout = 30 * time.Second
)

// Options configures the Ollama embedder.
type Options struct {
	BaseURL string
	Model   string
	Timeout time.Duration
}

// Embedder calls a local Ollama instance for embeddings via /api/embed.
type Embedder struct {
	baseURL    string
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
		model:      model,
		httpClient: &http.Client{Timeout: timeout},
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

type ollamaError struct {
	Error string `json:"error"`
}

// EmbedBatch sends texts to the Ollama /api/embed endpoint.
// Returns one vector per input text. nomic-embed-text produces 768-dim vectors.
func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	body, err := json.Marshal(embedRequest{
		Model: e.model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama embedder: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embedder: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embedder: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp ollamaError
		json.NewDecoder(resp.Body).Decode(&errResp) //nolint:errcheck
		msg := errResp.Error
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("ollama embedder: api error %d: %s", resp.StatusCode, msg)
	}

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embedder: decode response: %w", err)
	}

	if len(result.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embedder: expected %d embeddings, got %d", len(texts), len(result.Embeddings))
	}

	return result.Embeddings, nil
}
