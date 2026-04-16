package mbox

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const timeFormat = time.RFC3339Nano

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

// scanner is a common interface for sql.Row and sql.Rows to share scan logic.
type scanner interface {
	Scan(dest ...any) error
}

// Ingest stores a message in the database. It returns (true, nil) if the
// message was newly inserted, or (false, nil) if the ID already existed.
func (s *Store) Ingest(msg Message) (bool, error) {
	now := time.Now().UTC().Format(timeFormat)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO messages
			(id, channel, sender_id, sender, content, target_id, target_name, timestamp_utc, ingested_at, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID,
		msg.Channel,
		msg.SenderID,
		msg.Sender,
		msg.Content,
		nullableString(msg.TargetID),
		nullableString(msg.TargetName),
		msg.TimestampUTC.UTC().Format(timeFormat),
		now,
		msg.Source,
	)
	if err != nil {
		return false, fmt.Errorf("mbox: ingest: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mbox: ingest rows affected: %w", err)
	}
	return affected > 0, nil
}

// List returns messages matching the query, ordered newest-first.
// If q.Limit is 0 it defaults to 20.
func (s *Store) List(q Query) ([]Message, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	where, args := buildWhere(q)
	query := "SELECT id, channel, sender_id, sender, content, target_id, target_name, timestamp_utc, ingested_at, read_at, source FROM messages"
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY timestamp_utc DESC"
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, q.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("mbox: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, fmt.Errorf("mbox: list scan: %w", err)
	}
	return msgs, nil
}

// Get retrieves a single message by ID. Returns nil, nil if not found.
func (s *Store) Get(id string) (*Message, error) {
	row := s.db.QueryRow(
		"SELECT id, channel, sender_id, sender, content, target_id, target_name, timestamp_utc, ingested_at, read_at, source FROM messages WHERE id = ?",
		id,
	)
	msg, err := scanRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mbox: get: %w", err)
	}
	return &msg, nil
}

// buildWhere constructs a SQL WHERE clause and argument list from a Query.
func buildWhere(q Query) (string, []any) {
	var clauses []string
	var args []any

	if q.Channel != "" {
		clauses = append(clauses, "channel = ?")
		args = append(args, q.Channel)
	}
	if q.SenderID != "" {
		clauses = append(clauses, "sender_id = ?")
		args = append(args, q.SenderID)
	}
	if q.UnreadOnly {
		clauses = append(clauses, "read_at IS NULL")
	}
	if !q.Before.IsZero() {
		clauses = append(clauses, "timestamp_utc < ?")
		args = append(args, q.Before.UTC().Format(timeFormat))
	}
	if !q.After.IsZero() {
		clauses = append(clauses, "timestamp_utc > ?")
		args = append(args, q.After.UTC().Format(timeFormat))
	}

	return strings.Join(clauses, " AND "), args
}

// scanMessages scans all rows into a Message slice.
func scanMessages(rows *sql.Rows) ([]Message, error) {
	var msgs []Message
	for rows.Next() {
		msg, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	return msgs, rows.Err()
}

// scanRow scans a single row (from sql.Row or sql.Rows) into a Message.
func scanRow(s scanner) (Message, error) {
	var (
		msg        Message
		targetID   sql.NullString
		targetName sql.NullString
		readAt     sql.NullString
		tsStr      string
		ingestStr  string
	)

	if err := s.Scan(
		&msg.ID,
		&msg.Channel,
		&msg.SenderID,
		&msg.Sender,
		&msg.Content,
		&targetID,
		&targetName,
		&tsStr,
		&ingestStr,
		&readAt,
		&msg.Source,
	); err != nil {
		return Message{}, err
	}

	if targetID.Valid {
		msg.TargetID = targetID.String
	}
	if targetName.Valid {
		msg.TargetName = targetName.String
	}

	var err error
	msg.TimestampUTC, err = time.Parse(timeFormat, tsStr)
	if err != nil {
		return Message{}, fmt.Errorf("parse timestamp_utc %q: %w", tsStr, err)
	}
	msg.IngestedAt, err = time.Parse(timeFormat, ingestStr)
	if err != nil {
		return Message{}, fmt.Errorf("parse ingested_at %q: %w", ingestStr, err)
	}
	if readAt.Valid {
		t, err := time.Parse(timeFormat, readAt.String)
		if err != nil {
			return Message{}, fmt.Errorf("parse read_at %q: %w", readAt.String, err)
		}
		msg.ReadAt = &t
	}

	return msg, nil
}

// nullableString converts an empty string to a NULL sql value.
func nullableString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
