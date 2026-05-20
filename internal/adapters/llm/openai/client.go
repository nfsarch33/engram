// Package openai implements the engram LLMClient port against any
// OpenAI-compatible chat completions endpoint.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

const defaultModel = "gpt-4o-mini"
const defaultTimeout = 30 * time.Second

// Options configures the OpenAI LLM client.
type Options struct {
	BaseURL     string
	APIKey      string
	Model       string
	Timeout     time.Duration
	MaxTokens   int
	Temperature float32
}

// Client calls an OpenAI-compatible chat completions API.
type Client struct {
	baseURL     string
	apiKey      string
	model       string
	maxTokens   int
	temperature float32
	httpClient  *http.Client
}

// New returns a Client from opts. Zero-value fields use safe defaults.
func New(opts Options) *Client {
	model := opts.Model
	if model == "" {
		model = defaultModel
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	temp := opts.Temperature
	if temp == 0 {
		temp = 0.0
	}
	return &Client{
		baseURL:     strings.TrimRight(opts.BaseURL, "/"),
		apiKey:      opts.APIKey,
		model:       model,
		maxTokens:   maxTokens,
		temperature: temp,
		httpClient:  &http.Client{Timeout: timeout},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float32       `json:"temperature"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
}

type apiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ChatJSON sends req to the chat completions endpoint and JSON-decodes the
// assistant's response content into v. It strips markdown code fences that
// some models wrap around JSON output.
func (c *Client) ChatJSON(ctx context.Context, req engram.ChatRequest, v any) error {
	messages := []chatMessage{}
	if req.SystemPrompt != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.UserPrompt})

	body, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
	})
	if err != nil {
		return fmt.Errorf("llm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("llm: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var apiErr apiError
		json.NewDecoder(resp.Body).Decode(&apiErr) //nolint:errcheck
		msg := apiErr.Error.Message
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("llm: api error %d: %s", resp.StatusCode, msg)
	}

	var chat chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return fmt.Errorf("llm: decode response: %w", err)
	}
	if len(chat.Choices) == 0 {
		return fmt.Errorf("llm: empty choices in response")
	}

	content := stripFence(chat.Choices[0].Message.Content)
	if err := json.Unmarshal([]byte(content), v); err != nil {
		return fmt.Errorf("llm: unmarshal content as JSON: %w (content=%q)", err, content)
	}
	return nil
}

// stripFence removes leading/trailing markdown code fences from LLM JSON output.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"```json", "```"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			s = strings.TrimSuffix(s, "```")
			s = strings.TrimSpace(s)
			break
		}
	}
	return s
}
