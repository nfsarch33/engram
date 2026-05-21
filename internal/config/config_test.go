package config_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	t.Parallel()
	cfg := config.Load()
	if cfg.Addr != ":8280" {
		t.Errorf("default Addr: want :8280, got %q", cfg.Addr)
	}
	if cfg.DBPath != "engram.db" {
		t.Errorf("default DBPath: want engram.db, got %q", cfg.DBPath)
	}
	if cfg.Collection != "engram" {
		t.Errorf("default Collection: want engram, got %q", cfg.Collection)
	}
	if cfg.EmbeddingDim != 768 {
		t.Errorf("default EmbeddingDim: want 768, got %d", cfg.EmbeddingDim)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("default Timeout: want 30s, got %v", cfg.Timeout)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("default LogLevel: want info, got %q", cfg.LogLevel)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("ENGRAM_ADDR", ":9999")
	t.Setenv("ENGRAM_DB_PATH", "/tmp/test.db")
	t.Setenv("ENGRAM_COLLECTION", "mycol")
	t.Setenv("ENGRAM_EMBEDDING_DIM", "768")
	t.Setenv("ENGRAM_TIMEOUT", "60s")
	t.Setenv("ENGRAM_LOG_LEVEL", "debug")
	t.Setenv("ENGRAM_LLM_URL", "http://localhost:11434/v1")
	t.Setenv("ENGRAM_LLM_KEY", "sk-test")
	t.Setenv("ENGRAM_LLM_MODEL", "gpt-4o-mini")
	t.Setenv("ENGRAM_EMBED_URL", "http://localhost:11434/v1")
	t.Setenv("ENGRAM_EMBED_MODEL", "text-embedding-3-small")

	cfg := config.Load()

	if cfg.Addr != ":9999" {
		t.Errorf("Addr: want :9999, got %q", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath: want /tmp/test.db, got %q", cfg.DBPath)
	}
	if cfg.Collection != "mycol" {
		t.Errorf("Collection: want mycol, got %q", cfg.Collection)
	}
	if cfg.EmbeddingDim != 768 {
		t.Errorf("EmbeddingDim: want 768, got %d", cfg.EmbeddingDim)
	}
	if cfg.Timeout != 60*time.Second {
		t.Errorf("Timeout: want 60s, got %v", cfg.Timeout)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: want debug, got %q", cfg.LogLevel)
	}
	if cfg.LLMBaseURL != "http://localhost:11434/v1" {
		t.Errorf("LLMBaseURL: want http://localhost:11434/v1, got %q", cfg.LLMBaseURL)
	}
	if cfg.LLMAPIKey != "sk-test" {
		t.Errorf("LLMAPIKey: want sk-test, got %q", cfg.LLMAPIKey)
	}
	if cfg.LLMModel != "gpt-4o-mini" {
		t.Errorf("LLMModel: want gpt-4o-mini, got %q", cfg.LLMModel)
	}
	if cfg.EmbedBaseURL != "http://localhost:11434/v1" {
		t.Errorf("EmbedBaseURL: want http://localhost:11434/v1, got %q", cfg.EmbedBaseURL)
	}
	if cfg.EmbedModel != "text-embedding-3-small" {
		t.Errorf("EmbedModel: want text-embedding-3-small, got %q", cfg.EmbedModel)
	}
}

func TestLoad_InvalidDuration_FallsBackToDefault(t *testing.T) {
	t.Setenv("ENGRAM_TIMEOUT", "notaduration")
	cfg := config.Load()
	if cfg.Timeout != 30*time.Second {
		t.Errorf("bad duration fallback: want 30s, got %v", cfg.Timeout)
	}
}

func TestLoad_InvalidInt_FallsBackToDefault(t *testing.T) {
	t.Setenv("ENGRAM_EMBEDDING_DIM", "notanint")
	cfg := config.Load()
	if cfg.EmbeddingDim != 768 {
		t.Errorf("bad int fallback: want 768, got %d", cfg.EmbeddingDim)
	}
}

func TestConfig_HasLLM(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url  string
		want bool
	}{
		{"", false},
		{"http://localhost:11434/v1", true},
	}
	for _, tc := range cases {
		cfg := config.Config{LLMBaseURL: tc.url}
		if got := cfg.HasLLM(); got != tc.want {
			t.Errorf("HasLLM(%q): want %v, got %v", tc.url, tc.want, got)
		}
	}
}

func TestConfig_HasEmbedder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url  string
		want bool
	}{
		{"", false},
		{"http://localhost:11434/v1", true},
	}
	for _, tc := range cases {
		cfg := config.Config{EmbedBaseURL: tc.url}
		if got := cfg.HasEmbedder(); got != tc.want {
			t.Errorf("HasEmbedder(%q): want %v, got %v", tc.url, tc.want, got)
		}
	}
}
