package mbox

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store is a SQLite-backed message store for a single agent's inbox.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at dbPath, enables WAL mode,
// and runs any pending schema migrations.
func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("mbox: create dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("mbox: open db: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mbox: enable WAL: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mbox: migrate: %w", err)
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec("CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)"); err != nil {
		return err
	}

	var current int
	row := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&current); err != nil {
		return err
	}

	migrations := []string{
		`CREATE TABLE messages (
			id            TEXT PRIMARY KEY,
			channel       TEXT NOT NULL,
			sender_id     TEXT NOT NULL,
			sender        TEXT NOT NULL,
			content       TEXT NOT NULL,
			target_id     TEXT,
			target_name   TEXT,
			timestamp_utc TEXT NOT NULL,
			ingested_at   TEXT NOT NULL,
			read_at       TEXT,
			source        TEXT NOT NULL
		);
		CREATE INDEX idx_messages_channel_ts ON messages(channel, timestamp_utc DESC);
		CREATE INDEX idx_messages_unread ON messages(channel, read_at) WHERE read_at IS NULL;
		CREATE INDEX idx_messages_sender ON messages(sender_id, timestamp_utc DESC);

		CREATE TABLE channel_cursors (
			channel          TEXT PRIMARY KEY,
			oldest_seen_utc  TEXT NOT NULL,
			newest_seen_utc  TEXT NOT NULL,
			last_backfill_at TEXT NOT NULL,
			backfill_capped  INTEGER NOT NULL DEFAULT 0
		);

		CREATE VIRTUAL TABLE messages_fts USING fts5(
			content, sender, content='messages', content_rowid='rowid',
			tokenize='porter unicode61'
		);

		CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(rowid, content, sender) VALUES (new.rowid, new.content, new.sender);
		END;
		CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, content, sender) VALUES('delete', old.rowid, old.content, old.sender);
		END;`,
	}

	for i, ddl := range migrations {
		ver := i + 1
		if ver <= current {
			continue
		}
		if _, err := s.db.Exec(ddl); err != nil {
			return fmt.Errorf("migration %d: %w", ver, err)
		}
		if _, err := s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", ver); err != nil {
			return fmt.Errorf("record version %d: %w", ver, err)
		}
	}
	return nil
}

// Message represents a single chat message stored in the mbox.
type Message struct {
	ID           string
	Channel      string
	SenderID     string
	Sender       string
	Content      string
	TargetID     string
	TargetName   string
	TimestampUTC time.Time
	IngestedAt   time.Time
	ReadAt       *time.Time
	Source       string
}

// Query holds filter parameters for retrieving messages.
type Query struct {
	Channel    string
	SenderID   string
	UnreadOnly bool
	Before     time.Time
	After      time.Time
	Limit      int
	Offset     int
}
