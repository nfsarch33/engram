package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// TestNewStore_FileDatabaseCarriesThePragmas pins the write-path settings on a
// real file: WAL, synchronous=NORMAL (1) and a busy timeout. A store opened
// at SQLite's defaults would read journal_mode=delete and synchronous=2.
func TestNewStore_FileDatabaseCarriesThePragmas(t *testing.T) {
	t.Parallel()
	s, err := NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	var journal string
	var sync, busy int
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&sync); err != nil {
		t.Fatalf("synchronous: %v", err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if journal != "wal" || sync != 1 || busy != 5000 {
		t.Fatalf("pragmas: journal=%q synchronous=%d busy_timeout=%d; want wal/1/5000", journal, sync, busy)
	}

	// And the store still round-trips a record on that file.
	now := time.Now().UTC()
	rec := engram.MemoryRecord{ID: "p1", Text: "pragma pin", UserID: "u", CreatedAt: now, UpdatedAt: now}
	if err := s.SaveRecord(ctx, rec); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	got, err := s.GetRecord(ctx, "p1")
	if err != nil || got.Text != "pragma pin" {
		t.Fatalf("GetRecord: %+v err=%v", got, err)
	}
}

func TestDsnFor(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		":memory:":                    ":memory:",
		"/data/engram.db":             "/data/engram.db?" + dsnPragmas,
		"/data/engram.db?mode=rwc":    "/data/engram.db?mode=rwc&" + dsnPragmas,
		"x.db?_pragma=synchronous(2)": "x.db?_pragma=synchronous(2)",
	}
	for in, want := range cases {
		if got := dsnFor(in); got != want {
			t.Errorf("dsnFor(%q) = %q, want %q", in, got, want)
		}
	}
}
