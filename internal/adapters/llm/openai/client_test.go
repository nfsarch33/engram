package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	llm "github.com/nfsarch33/engram/internal/adapters/llm/openai"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

func makeServer(t *testing.T, statusCode int, resp any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(resp)
	}))
}

func chatResponse(content string) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-abc",
		"object":  "chat.completion",
		"model":   "gpt-4o-mini",
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
	}
}

func TestChatJSON_Success(t *testing.T) {
	t.Parallel()
	type result struct {
		Answer string `json:"answer"`
	}
	payload := `{"answer":"hello"}`
	srv := makeServer(t, http.StatusOK, chatResponse(payload))
	defer srv.Close()

	c := llm.New(llm.Options{
		BaseURL: srv.URL,
		APIKey:  "sk-test",
		Model:   "gpt-4o-mini",
	})

	var out result
	err := c.ChatJSON(context.Background(), engram.ChatRequest{
		SystemPrompt: "you are helpful",
		UserPrompt:   "say hello",
	}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Answer != "hello" {
		t.Errorf("want hello, got %q", out.Answer)
	}
}

func TestChatJSON_StripMarkdownFence(t *testing.T) {
	t.Parallel()
	type result struct {
		Value int `json:"value"`
	}
	// LLMs often wrap JSON in markdown fences
	payload := "```json\n{\"value\":42}\n```"
	srv := makeServer(t, http.StatusOK, chatResponse(payload))
	defer srv.Close()

	c := llm.New(llm.Options{BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-4o-mini"})

	var out result
	err := c.ChatJSON(context.Background(), engram.ChatRequest{
		SystemPrompt: "sys",
		UserPrompt:   "user",
	}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Value != 42 {
		t.Errorf("want 42, got %d", out.Value)
	}
}

func TestChatJSON_ServerError(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusInternalServerError, map[string]any{
		"error": map[string]any{"message": "server error", "type": "server_error"},
	})
	defer srv.Close()

	c := llm.New(llm.Options{BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-4o-mini"})
	err := c.ChatJSON(context.Background(), engram.ChatRequest{}, new(map[string]any))
	if err == nil {
		t.Fatal("expected error on 5xx")
	}
}

func TestChatJSON_Unauthorized(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusUnauthorized, map[string]any{
		"error": map[string]any{"message": "invalid api key", "type": "invalid_request_error"},
	})
	defer srv.Close()

	c := llm.New(llm.Options{BaseURL: srv.URL, APIKey: "bad", Model: "gpt-4o-mini"})
	err := c.ChatJSON(context.Background(), engram.ChatRequest{}, new(map[string]any))
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestChatJSON_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusOK, chatResponse("not json at all"))
	defer srv.Close()

	c := llm.New(llm.Options{BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-4o-mini"})
	err := c.ChatJSON(context.Background(), engram.ChatRequest{}, new(map[string]any))
	if err == nil {
		t.Fatal("expected error on non-JSON content")
	}
}

func TestChatJSON_ContextCancelled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := llm.New(llm.Options{BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-4o-mini"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.ChatJSON(ctx, engram.ChatRequest{}, new(map[string]any))
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestNew_DefaultModel(t *testing.T) {
	t.Parallel()
	c := llm.New(llm.Options{BaseURL: "http://unused", APIKey: "sk-test"})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}
