// Package config loads Engram runtime configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Vector store selectors for ENGRAM_VECTOR_STORE. Qdrant is selected by
// ENGRAM_QDRANT_URL instead and overrides both.
const (
	// VectorStoreSQLite is the default: a durable index that restores itself
	// at boot from ENGRAM_VECTOR_DB_PATH without re-embedding anything.
	VectorStoreSQLite = "sqlite"
	// VectorStoreInmem keeps the index only in process memory; every restart
	// empties it. Development and tests only.
	VectorStoreInmem = "inmem"
)

// Config holds all runtime parameters for the Engram daemon.
type Config struct {
	// HTTP server
	Addr string

	// Mem0 OSS-compatible HTTP shim (separate listener, separate port).
	Mem0CompatAddr string

	// API key gate; applied to both the canonical API and the shim. Empty
	// disables the gate, matching the existing engramd convention.
	APIKey string

	// Storage
	DBPath     string
	Collection string

	// Vector index: VectorStoreSQLite (default) or VectorStoreInmem, and the
	// file the durable store lives in (derived from DBPath unless set). It is
	// a separate file from the history database on purpose - the two stores
	// never contend for one file's write lock - and must be backed up with it.
	VectorStore  string
	VectorDBPath string

	// Qdrant
	QdrantURL    string
	QdrantAPIKey string

	// Embedding (primary)
	EmbeddingDim int
	EmbedBaseURL string
	EmbedModel   string
	EmbedAPIKey  string

	// Embedding fallback (MiniMax)
	EmbedFallbackURL   string
	EmbedFallbackModel string
	EmbedFallbackKey   string

	// LLM (for infer=true path)
	LLMBaseURL string
	LLMAPIKey  string
	LLMModel   string

	// Runtime
	Timeout  time.Duration
	LogLevel string
}

// Load reads ENGRAM_* environment variables and returns a Config with defaults.
func Load() Config {
	dbPath := getenv("ENGRAM_DB_PATH", "engram.db")
	return Config{
		Addr:               getenv("ENGRAM_ADDR", ":8280"),
		Mem0CompatAddr:     getenv("ENGRAM_MEM0COMPAT_ADDR", ":8281"),
		APIKey:             os.Getenv("ENGRAM_API_KEY"),
		DBPath:             dbPath,
		Collection:         getenv("ENGRAM_COLLECTION", "engram"),
		VectorStore:        getenv("ENGRAM_VECTOR_STORE", VectorStoreSQLite),
		VectorDBPath:       getenv("ENGRAM_VECTOR_DB_PATH", DefaultVectorDBPath(dbPath)),
		QdrantURL:          getenv("ENGRAM_QDRANT_URL", ""),
		QdrantAPIKey:       os.Getenv("ENGRAM_QDRANT_KEY"),
		EmbeddingDim:       getenvInt("ENGRAM_EMBEDDING_DIM", 768),
		EmbedBaseURL:       getenv("ENGRAM_EMBED_URL", ""),
		EmbedModel:         getenv("ENGRAM_EMBED_MODEL", "nomic-embed-text"),
		EmbedAPIKey:        os.Getenv("ENGRAM_EMBED_KEY"),
		EmbedFallbackURL:   getenv("ENGRAM_EMBED_FALLBACK_URL", ""),
		EmbedFallbackModel: getenv("ENGRAM_EMBED_FALLBACK_MODEL", "embo-01"),
		EmbedFallbackKey:   os.Getenv("ENGRAM_EMBED_FALLBACK_KEY"),
		LLMBaseURL:         getenv("ENGRAM_LLM_URL", ""),
		LLMAPIKey:          os.Getenv("ENGRAM_LLM_KEY"),
		LLMModel:           getenv("ENGRAM_LLM_MODEL", "gpt-4o-mini"),
		Timeout:            getenvDuration("ENGRAM_TIMEOUT", 30*time.Second),
		LogLevel:           getenv("ENGRAM_LOG_LEVEL", "info"),
	}
}

// DefaultVectorDBPath derives the vectors file from the history database
// path: "engram.db" becomes "engram.vectors.db" beside it, and an in-memory
// history keeps an in-memory index.
func DefaultVectorDBPath(dbPath string) string {
	if dbPath == ":memory:" {
		return dbPath
	}
	return strings.TrimSuffix(dbPath, ".db") + ".vectors.db"
}

// HasLLM returns true when an LLM base URL is configured.
func (c Config) HasLLM() bool { return c.LLMBaseURL != "" }

// HasEmbedder returns true when an embedder base URL is configured.
func (c Config) HasEmbedder() bool { return c.EmbedBaseURL != "" }

// HasQdrant returns true when a Qdrant URL is configured.
func (c Config) HasQdrant() bool { return c.QdrantURL != "" }

// HasEmbedFallback returns true when a fallback embedder is configured.
func (c Config) HasEmbedFallback() bool { return c.EmbedFallbackURL != "" }

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
