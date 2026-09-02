package config

import "testing"

// TestLoad_VectorStoreDefaultsToDurable: with nothing set, the daemon persists
// its index beside the history database. The in-memory store is opt-in.
func TestLoad_VectorStoreDefaultsToDurable(t *testing.T) {
	t.Setenv("ENGRAM_DB_PATH", "/var/lib/engram/engram.db")
	t.Setenv("ENGRAM_VECTOR_STORE", "")
	t.Setenv("ENGRAM_VECTOR_DB_PATH", "")

	cfg := Load()
	if cfg.VectorStore != VectorStoreSQLite {
		t.Fatalf("VectorStore = %q, want %q", cfg.VectorStore, VectorStoreSQLite)
	}
	if want := "/var/lib/engram/engram.vectors.db"; cfg.VectorDBPath != want {
		t.Fatalf("VectorDBPath = %q, want %q", cfg.VectorDBPath, want)
	}
}

func TestLoad_VectorStoreOverrides(t *testing.T) {
	t.Setenv("ENGRAM_DB_PATH", "engram.db")
	t.Setenv("ENGRAM_VECTOR_STORE", VectorStoreInmem)
	t.Setenv("ENGRAM_VECTOR_DB_PATH", "/elsewhere/vectors.db")

	cfg := Load()
	if cfg.VectorStore != VectorStoreInmem {
		t.Fatalf("VectorStore = %q, want %q", cfg.VectorStore, VectorStoreInmem)
	}
	if cfg.VectorDBPath != "/elsewhere/vectors.db" {
		t.Fatalf("VectorDBPath = %q, want the explicit override", cfg.VectorDBPath)
	}
}

func TestDefaultVectorDBPath(t *testing.T) {
	cases := map[string]string{
		"engram.db":          "engram.vectors.db",
		"/data/engram.db":    "/data/engram.vectors.db",
		"/data/history":      "/data/history.vectors.db",
		":memory:":           ":memory:",
		"/data/engram.db.db": "/data/engram.db.vectors.db",
	}
	for in, want := range cases {
		if got := DefaultVectorDBPath(in); got != want {
			t.Errorf("DefaultVectorDBPath(%q) = %q, want %q", in, got, want)
		}
	}
}
