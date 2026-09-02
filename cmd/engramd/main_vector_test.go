package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/adapters/vectorstore/sqlitevec"
	"github.com/nfsarch33/engram/internal/config"
	"github.com/nfsarch33/engram/internal/domain/engram"
)

// TestBuildVectorStore_DefaultIsDurable: an unset selector yields the
// SQLite-backed store, and what it writes survives a fresh build.
func TestBuildVectorStore_DefaultIsDurable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := config.Config{VectorDBPath: filepath.Join(t.TempDir(), "engram.vectors.db")}

	first, err := buildVectorStore(cfg, discardLogger())
	if err != nil {
		t.Fatalf("buildVectorStore: %v", err)
	}
	store, ok := first.(*sqlitevec.Store)
	if !ok {
		t.Fatalf("default store = %T, want *sqlitevec.Store", first)
	}
	store.EnsureCollection(ctx, "c", 2)                                                      //nolint:errcheck
	store.UpsertBatch(ctx, []engram.VectorRecord{{ID: "survives", Vector: []float32{1, 0}}}) //nolint:errcheck
	store.Close()

	second, err := buildVectorStore(cfg, discardLogger())
	if err != nil {
		t.Fatalf("buildVectorStore (reopen): %v", err)
	}
	defer second.(*sqlitevec.Store).Close()
	ids, _ := second.(*sqlitevec.Store).IndexedIDs(ctx)
	if len(ids) != 1 || ids[0] != "survives" {
		t.Fatalf("after reopen: want [survives], got %v", ids)
	}
}

func TestBuildVectorStore_InmemIsOptIn(t *testing.T) {
	t.Parallel()
	cfg := config.Config{VectorStore: config.VectorStoreInmem}
	store, err := buildVectorStore(cfg, discardLogger())
	if err != nil {
		t.Fatalf("buildVectorStore: %v", err)
	}
	if _, ok := store.(*inmem.Store); !ok {
		t.Fatalf("inmem selector = %T, want *inmem.Store", store)
	}
}

func TestBuildVectorStore_RejectsUnknownSelector(t *testing.T) {
	t.Parallel()
	_, err := buildVectorStore(config.Config{VectorStore: "redis"}, discardLogger())
	if err == nil || !strings.Contains(err.Error(), "ENGRAM_VECTOR_STORE") {
		t.Fatalf("unknown selector: err=%v, want a hint naming ENGRAM_VECTOR_STORE", err)
	}
}

func TestBuildVectorStore_QdrantWins(t *testing.T) {
	t.Parallel()
	cfg := config.Config{QdrantURL: "http://localhost:6333", VectorStore: config.VectorStoreInmem}
	store, err := buildVectorStore(cfg, discardLogger())
	if err != nil {
		t.Fatalf("buildVectorStore: %v", err)
	}
	if _, ok := store.(*inmem.Store); ok {
		t.Fatal("a configured Qdrant URL must override the selector")
	}
}
