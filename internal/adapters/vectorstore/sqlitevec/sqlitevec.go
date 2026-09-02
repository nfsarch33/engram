// Package sqlitevec implements engram.VectorStore as a durable in-memory
// index: brute-force cosine search over vectors held in process memory (it
// composes the inmem adapter), with every upsert and delete written through
// to a SQLite table so the index survives a daemon restart without spending
// a single embedding call.
//
// Why this exists: the daemon's default index was purely in-memory and
// nothing rebuilt it at boot, so every restart silently emptied semantic
// search for all pre-existing records while the history store still held
// them. The vectors file is separate from the history database on purpose -
// the two stores never contend for one file's write lock - and it must be
// backed up alongside it.
//
// Uses modernc.org/sqlite (pure Go, no CGO), the module's existing dependency.
package sqlitevec

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/nfsarch33/engram/internal/adapters/vectorstore/inmem"
	"github.com/nfsarch33/engram/internal/domain/engram"
	_ "modernc.org/sqlite" // register sqlite driver
)

const schema = `
CREATE TABLE IF NOT EXISTS vectors (
	id      TEXT    PRIMARY KEY,
	dim     INTEGER NOT NULL,
	vec     BLOB    NOT NULL,
	payload TEXT    NOT NULL DEFAULT '{}'
);
`

// dsnPragmas are carried in the DSN so every pooled connection gets them;
// a PRAGMA statement after Open reaches only the connection that ran it.
// WAL + synchronous=NORMAL keeps the fsync off the write path (an application
// crash is fully safe; an OS crash can lose the last transactions, which the
// service's reindex path recovers); busy_timeout avoids SQLITE_BUSY while a
// fresh WAL database is being initialised.
const dsnPragmas = "_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"

// MemoryPath opens an in-process database that lives only as long as the
// store; it exists so tests and --no-persist style setups can use the same
// code path.
const MemoryPath = ":memory:"

// Store is a durable vector store: reads are served from memory, writes go
// to SQLite first and to memory only once the transaction committed.
type Store struct {
	db  *sql.DB
	mem *inmem.Store

	mu      sync.Mutex // serialises write-through; mem has its own lock
	rowDims map[engram.MemoryID]int

	loaded  int // rows restored from disk at Open
	evicted int // rows dropped from the index at EnsureCollection (dim mismatch)
}

// Compile-time checks.
var (
	_ engram.VectorStore    = (*Store)(nil)
	_ engram.IndexInspector = (*Store)(nil)
)

// Open creates or opens the vectors database at path and restores every
// stored vector into memory. Pass MemoryPath for a throwaway store.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlitevec: path must not be empty")
	}
	dsn := path
	if path != MemoryPath {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		dsn = path + sep + dsnPragmas
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlitevec: open %q: %w", path, err)
	}
	// One connection: writes are serialised by s.mu anyway, reads come from
	// memory, and a single connection is what keeps ":memory:" coherent.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlitevec: migrate: %w", err)
	}
	mem, err := inmem.NewStore()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db, mem: mem, rowDims: make(map[engram.MemoryID]int)}
	if err := s.load(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// load restores every row into the in-memory index.
func (s *Store) load(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, dim, vec, payload FROM vectors`)
	if err != nil {
		return fmt.Errorf("sqlitevec: load: %w", err)
	}
	defer rows.Close()

	var batch []engram.VectorRecord
	for rows.Next() {
		var id string
		var dim int
		var blob []byte
		var payloadJSON string
		if err := rows.Scan(&id, &dim, &blob, &payloadJSON); err != nil {
			return fmt.Errorf("sqlitevec: load scan: %w", err)
		}
		vec, err := decodeVector(blob)
		if err != nil {
			return fmt.Errorf("sqlitevec: load %s: %w", id, err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			payload = map[string]any{}
		}
		batch = append(batch, engram.VectorRecord{ID: engram.MemoryID(id), Vector: vec, Payload: payload})
		s.rowDims[engram.MemoryID(id)] = dim
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sqlitevec: load rows: %w", err)
	}
	if err := s.mem.UpsertBatch(ctx, batch); err != nil {
		return err
	}
	s.loaded = len(batch)
	return nil
}

// EnsureCollection fixes the vector dimension. Rows restored from disk with
// a different dimension (an embedder or model change since they were
// written) are evicted from the in-memory index so cosine never runs over
// mismatched lengths; they stay on disk and the service's reindex path
// re-embeds them because they are no longer reported by IndexedIDs.
func (s *Store) EnsureCollection(ctx context.Context, name string, dim int) error {
	if err := s.mem.EnsureCollection(ctx, name, dim); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var evict []engram.MemoryID
	for id, d := range s.rowDims {
		if d != dim {
			evict = append(evict, id)
		}
	}
	if len(evict) == 0 {
		return nil
	}
	if err := s.mem.DeleteBatch(ctx, evict); err != nil {
		return err
	}
	for _, id := range evict {
		delete(s.rowDims, id)
	}
	s.evicted += len(evict)
	return nil
}

// UpsertBatch writes the records to SQLite in one transaction and only then
// into memory, so a crash between the two leaves the durable copy ahead of
// the served copy, never behind it.
func (s *Store) UpsertBatch(ctx context.Context, records []engram.VectorRecord) error {
	if len(records) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlitevec: upsert begin: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO vectors (id, dim, vec, payload) VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET dim=excluded.dim, vec=excluded.vec, payload=excluded.payload`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("sqlitevec: upsert prepare: %w", err)
	}
	defer stmt.Close()
	for _, r := range records {
		payloadJSON, err := json.Marshal(r.Payload)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlitevec: upsert %s payload: %w", r.ID, err)
		}
		if _, err := stmt.ExecContext(ctx, string(r.ID), len(r.Vector), encodeVector(r.Vector), string(payloadJSON)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlitevec: upsert %s: %w", r.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlitevec: upsert commit: %w", err)
	}
	if err := s.mem.UpsertBatch(ctx, records); err != nil {
		return err
	}
	for _, r := range records {
		s.rowDims[r.ID] = len(r.Vector)
	}
	return nil
}

// Search serves from memory; filters and ranking are the inmem adapter's.
func (s *Store) Search(ctx context.Context, q engram.VectorQuery) ([]engram.VectorResult, error) {
	return s.mem.Search(ctx, q)
}

// DeleteBatch removes the rows durably, then from memory. Unknown IDs are
// silently ignored, matching the port's contract.
func (s *Store) DeleteBatch(ctx context.Context, ids []engram.MemoryID) error {
	if len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlitevec: delete begin: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM vectors WHERE id = ?`, string(id)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlitevec: delete %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlitevec: delete commit: %w", err)
	}
	if err := s.mem.DeleteBatch(ctx, ids); err != nil {
		return err
	}
	for _, id := range ids {
		delete(s.rowDims, id)
	}
	return nil
}

// IndexedIDs reports the IDs served from memory. Implements
// engram.IndexInspector.
func (s *Store) IndexedIDs(ctx context.Context) ([]engram.MemoryID, error) {
	return s.mem.IndexedIDs(ctx)
}

// Len reports how many vectors the in-memory index serves.
func (s *Store) Len() int { return s.mem.Len() }

// LoadStats reports how many rows Open restored and how many
// EnsureCollection evicted for a dimension mismatch; the daemon logs both.
func (s *Store) LoadStats() (loaded, evicted int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loaded, s.evicted
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// --- encoding ----------------------------------------------------------------

// encodeVector serialises float32s as little-endian IEEE-754, 4 bytes each.
func encodeVector(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

func decodeVector(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("vector blob length %d is not a multiple of 4", len(b))
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out, nil
}
