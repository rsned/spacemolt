// Package checkpoint persists a single worker's resumable state to SQLite.
package checkpoint

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const timeFormat = time.RFC3339Nano

// Intent is the worker's current standing behavior and active task position.
type Intent struct {
	StandingBehavior string
	ActiveTaskID     string
	StepIndex        int
}

// KnownState is the last game-state snapshot used for restart reconciliation.
type KnownState struct {
	System    string
	POI       string
	Docked    bool
	Credits   float64
	CargoJSON string
	Tick      int
}

// JournalEntry is one assigned-task outcome.
type JournalEntry struct {
	TaskID  string
	Outcome string
	At      time.Time
}

// Store is a per-worker SQLite checkpoint.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the checkpoint DB, enables WAL, and migrates.
func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("checkpoint: create dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("checkpoint: open db: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checkpoint: enable WAL: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checkpoint: migrate: %w", err)
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	if _, err := s.db.Exec("CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)"); err != nil {
		return err
	}
	var current int
	if err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&current); err != nil {
		return err
	}
	migrations := []string{
		`CREATE TABLE intent (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			standing_behavior TEXT NOT NULL,
			active_task_id    TEXT NOT NULL,
			step_index        INTEGER NOT NULL
		);
		CREATE TABLE known_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			system TEXT NOT NULL, poi TEXT NOT NULL, docked INTEGER NOT NULL,
			credits REAL NOT NULL, cargo_json TEXT NOT NULL, tick INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE task_journal (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL, outcome TEXT NOT NULL, at TEXT NOT NULL
		);
		CREATE TABLE cursors (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
	}
	for i := current; i < len(migrations); i++ {
		if _, err := s.db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", i+1); err != nil {
			return err
		}
	}
	return nil
}

// SaveIntent upserts the single intent row.
func (s *Store) SaveIntent(i Intent) error {
	_, err := s.db.Exec(
		`INSERT INTO intent (id, standing_behavior, active_task_id, step_index)
		 VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   standing_behavior=excluded.standing_behavior,
		   active_task_id=excluded.active_task_id,
		   step_index=excluded.step_index`,
		i.StandingBehavior, i.ActiveTaskID, i.StepIndex)
	if err != nil {
		return fmt.Errorf("checkpoint: save intent: %w", err)
	}
	return nil
}

// LoadIntent returns the intent row and whether one exists.
func (s *Store) LoadIntent() (Intent, bool, error) {
	var i Intent
	err := s.db.QueryRow(
		`SELECT standing_behavior, active_task_id, step_index FROM intent WHERE id=1`).
		Scan(&i.StandingBehavior, &i.ActiveTaskID, &i.StepIndex)
	if err == sql.ErrNoRows {
		return Intent{}, false, nil
	}
	if err != nil {
		return Intent{}, false, fmt.Errorf("checkpoint: load intent: %w", err)
	}
	return i, true, nil
}

// SaveKnownState upserts the single known-state row.
func (s *Store) SaveKnownState(k KnownState) error {
	_, err := s.db.Exec(
		`INSERT INTO known_state (id, system, poi, docked, credits, cargo_json, tick, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   system=excluded.system, poi=excluded.poi, docked=excluded.docked,
		   credits=excluded.credits, cargo_json=excluded.cargo_json,
		   tick=excluded.tick, updated_at=excluded.updated_at`,
		k.System, k.POI, boolToInt(k.Docked), k.Credits, k.CargoJSON, k.Tick,
		time.Now().Format(timeFormat))
	if err != nil {
		return fmt.Errorf("checkpoint: save known_state: %w", err)
	}
	return nil
}

// LoadKnownState returns the known-state row and whether one exists.
func (s *Store) LoadKnownState() (KnownState, bool, error) {
	var k KnownState
	var docked int
	err := s.db.QueryRow(
		`SELECT system, poi, docked, credits, cargo_json, tick FROM known_state WHERE id=1`).
		Scan(&k.System, &k.POI, &docked, &k.Credits, &k.CargoJSON, &k.Tick)
	if err == sql.ErrNoRows {
		return KnownState{}, false, nil
	}
	if err != nil {
		return KnownState{}, false, fmt.Errorf("checkpoint: load known_state: %w", err)
	}
	k.Docked = docked != 0
	return k, true, nil
}

// AppendJournal records one task outcome.
func (s *Store) AppendJournal(taskID, outcome string, at time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO task_journal (task_id, outcome, at) VALUES (?, ?, ?)`,
		taskID, outcome, at.Format(timeFormat))
	if err != nil {
		return fmt.Errorf("checkpoint: append journal: %w", err)
	}
	return nil
}

// Journal returns up to limit entries, newest first.
func (s *Store) Journal(limit int) ([]JournalEntry, error) {
	rows, err := s.db.Query(
		`SELECT task_id, outcome, at FROM task_journal ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: query journal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []JournalEntry
	for rows.Next() {
		var e JournalEntry
		var at string
		if err := rows.Scan(&e.TaskID, &e.Outcome, &at); err != nil {
			return nil, fmt.Errorf("checkpoint: scan journal: %w", err)
		}
		e.At, _ = time.Parse(timeFormat, at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetCursor upserts a named progress cursor.
func (s *Store) SetCursor(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO cursors (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("checkpoint: set cursor: %w", err)
	}
	return nil
}

// Cursor returns a named cursor and whether it exists.
func (s *Store) Cursor(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM cursors WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("checkpoint: get cursor: %w", err)
	}
	return v, true, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
