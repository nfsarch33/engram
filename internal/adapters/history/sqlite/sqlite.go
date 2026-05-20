// Package sqlite implements engram.HistoryStore using a SQLite database.
// Uses modernc.org/sqlite (pure Go, no CGO). Supports :memory: for tests.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nfsarch33/engram/internal/domain/engram"
	_ "modernc.org/sqlite" // register sqlite3 driver
)

const schema = `
CREATE TABLE IF NOT EXISTS memories (
	id           TEXT PRIMARY KEY,
	text         TEXT    NOT NULL,
	metadata     TEXT    NOT NULL DEFAULT '{}',
	user_id      TEXT    NOT NULL DEFAULT '',
	agent_id     TEXT    NOT NULL DEFAULT '',
	run_id       TEXT    NOT NULL DEFAULT '',
	app_id       TEXT    NOT NULL DEFAULT '',
	workspace_id TEXT    NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memories_user_id ON memories(user_id);
CREATE INDEX IF NOT EXISTS idx_memories_agent_id ON memories(agent_id);
CREATE INDEX IF NOT EXISTS idx_memories_workspace ON memories(workspace_id);

CREATE TABLE IF NOT EXISTS memory_events (
	id         TEXT PRIMARY KEY,
	memory_id  TEXT    NOT NULL,
	event_type TEXT    NOT NULL,
	new_text   TEXT    NOT NULL DEFAULT '',
	old_text   TEXT    NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_memory_id ON memory_events(memory_id);
`

// Store implements engram.HistoryStore backed by SQLite.
type Store struct {
	mu sync.Mutex // serialise writes; reads use their own connections in WAL mode
	db *sql.DB
}

// NewStore opens (or creates) a SQLite database at dbPath.
// Use ":memory:" for an in-process test database.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", dbPath, err)
	}
	// Single connection ensures :memory: databases share state across goroutines
	// and prevents SQLITE_BUSY on WAL checkpoints. The store-level mutex
	// serialises writes so a single connection is sufficient.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: set WAL: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// --- HistoryStore impl ------------------------------------------------------

func (s *Store) SaveRecord(ctx context.Context, rec engram.MemoryRecord) error {
	meta, err := json.Marshal(rec.Metadata)
	if err != nil {
		return fmt.Errorf("sqlite: marshal metadata: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO memories
			(id,text,metadata,user_id,agent_id,run_id,app_id,workspace_id,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		string(rec.ID), rec.Text, string(meta),
		rec.UserID, rec.AgentID, rec.RunID, rec.AppID, rec.WorkspaceID,
		rec.CreatedAt.UnixNano(), rec.UpdatedAt.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("sqlite: SaveRecord: %w", err)
	}
	return nil
}

func (s *Store) UpdateRecord(ctx context.Context, rec engram.MemoryRecord) error {
	meta, err := json.Marshal(rec.Metadata)
	if err != nil {
		return fmt.Errorf("sqlite: marshal metadata: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx,
		`UPDATE memories SET text=?,metadata=?,updated_at=? WHERE id=?`,
		rec.Text, string(meta), rec.UpdatedAt.UnixNano(), string(rec.ID),
	)
	if err != nil {
		return fmt.Errorf("sqlite: UpdateRecord: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("sqlite: UpdateRecord %s: %w", rec.ID, engram.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteRecord(ctx context.Context, id engram.MemoryID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id=?`, string(id))
	if err != nil {
		return fmt.Errorf("sqlite: DeleteRecord: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("sqlite: DeleteRecord %s: %w", id, engram.ErrNotFound)
	}
	return nil
}

func (s *Store) GetRecord(ctx context.Context, id engram.MemoryID) (engram.MemoryRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,text,metadata,user_id,agent_id,run_id,app_id,workspace_id,created_at,updated_at
		 FROM memories WHERE id=?`, string(id))
	return scanRecord(row)
}

func (s *Store) ListRecords(ctx context.Context, f engram.HistoryFilter) ([]engram.MemoryRecord, error) {
	query := `SELECT id,text,metadata,user_id,agent_id,run_id,app_id,workspace_id,created_at,updated_at
	          FROM memories WHERE 1=1`
	args := make([]any, 0, 4)
	if f.UserID != "" {
		query += " AND user_id=?"
		args = append(args, f.UserID)
	}
	if f.AgentID != "" {
		query += " AND agent_id=?"
		args = append(args, f.AgentID)
	}
	if f.RunID != "" {
		query += " AND run_id=?"
		args = append(args, f.RunID)
	}
	if f.WorkspaceID != "" {
		query += " AND workspace_id=?"
		args = append(args, f.WorkspaceID)
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: ListRecords: %w", err)
	}
	defer rows.Close()

	var out []engram.MemoryRecord
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: ListRecords scan: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) SaveEvents(ctx context.Context, events []engram.MemoryEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: SaveEvents begin tx: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO memory_events (id,memory_id,event_type,new_text,old_text,created_at)
		 VALUES (?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return fmt.Errorf("sqlite: SaveEvents prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().UnixNano()
	for _, ev := range events {
		oldText := ""
		if ev.OldMemory != nil {
			oldText = ev.OldMemory.Text
		}
		evID := string(engram.NewMemoryID())
		if _, err := stmt.ExecContext(ctx, evID, string(ev.ID), string(ev.Event), ev.Text, oldText, now); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("sqlite: SaveEvents insert: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) ListEvents(ctx context.Context, id engram.MemoryID) ([]engram.MemoryEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT event_type, memory_id, new_text, old_text
		 FROM memory_events WHERE memory_id=? ORDER BY created_at ASC`, string(id))
	if err != nil {
		return nil, fmt.Errorf("sqlite: ListEvents: %w", err)
	}
	defer rows.Close()

	var out []engram.MemoryEvent
	for rows.Next() {
		var evType, memID, newText, oldText string
		if err := rows.Scan(&evType, &memID, &newText, &oldText); err != nil {
			return nil, fmt.Errorf("sqlite: ListEvents scan: %w", err)
		}
		ev := engram.MemoryEvent{
			Event: engram.MemoryEventType(evType),
			ID:    engram.MemoryID(memID),
			Text:  newText,
		}
		if oldText != "" {
			ev.OldMemory = &engram.MemoryRecord{Text: oldText}
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// --- helpers ----------------------------------------------------------------

type scanner interface {
	Scan(dest ...any) error
}

func scanRecord(row scanner) (engram.MemoryRecord, error) {
	var (
		id, text, meta                              string
		userID, agentID, runID, appID, workspaceID string
		createdNano, updatedNano                    int64
	)
	err := row.Scan(&id, &text, &meta, &userID, &agentID, &runID, &appID, &workspaceID, &createdNano, &updatedNano)
	if err == sql.ErrNoRows {
		return engram.MemoryRecord{}, engram.ErrNotFound
	}
	if err != nil {
		return engram.MemoryRecord{}, fmt.Errorf("sqlite: scan: %w", err)
	}

	var metadata map[string]any
	if err := json.Unmarshal([]byte(meta), &metadata); err != nil {
		metadata = map[string]any{}
	}

	return engram.MemoryRecord{
		ID:          engram.MemoryID(id),
		Text:        text,
		Metadata:    metadata,
		UserID:      userID,
		AgentID:     agentID,
		RunID:       runID,
		AppID:       appID,
		WorkspaceID: workspaceID,
		CreatedAt:   time.Unix(0, createdNano).UTC(),
		UpdatedAt:   time.Unix(0, updatedNano).UTC(),
	}, nil
}
