// Package openai implements the engram Embedder port against any
// OpenAI-compatible embeddings endpoint.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultModel = "text-embedding-3-small"
const defaultTimeout = 30 * time.Second

// Options configures the OpenAI embedder.
type Options struct {
	BaseURL string
	APIKey  string
	Model   string
	Dim     int
	Timeout time.Duration
}

// Embedder calls an OpenAI-compatible embeddings API.
type Embedder struct {
	baseURL    string
	apiKey     string
	model      string
	dim        int
	httpClient *http.Client
}

// New returns an Embedder from opts. Zero-value fields use safe defaults.
func New(opts Options) *Embedder {
	model := opts.Model
	if model == "" {
		model = defaultModel
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Embedder{
		baseURL:    strings.TrimRight(opts.BaseURL, "/"),
		apiKey:     opts.APIKey,
		model:      model,
		dim:        opts.Dim,
		httpClient: &http.Client{Timeout: timeout},
	}
}

type embedRequest struct {
	Input          []string `json:"input"`
	Model          string   `json:"model"`
	EncodingFormat string   `json:"encoding_format"`
}

type embedObject struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type embedResponse struct {
	Data []embedObject `json:"data"`
}

type apiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// EmbedBatch sends texts to the embeddings endpoint and returns one vector per text.
func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	body, err := json.Marshal(embedRequest{
		Input:          texts,
		Model:          e.model,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, fmt.Errorf("embedder: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedder: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedder: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var apiErr apiError
		json.NewDecoder(resp.Body).Decode(&apiErr) //nolint:errcheck
		msg := apiErr.Error.Message
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("embedder: api error %d: %s", resp.StatusCode, msg)
	}

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embedder: decode response: %w", err)
	}

	// API may return data out of order; sort by index.
	vecs := make([][]float32, len(texts))
	for _, obj := range result.Data {
		if obj.Index < len(vecs) {
			vecs[obj.Index] = obj.Embedding
		}
	}
	return vecs, nil
}
