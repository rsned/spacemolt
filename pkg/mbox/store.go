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
		`ALTER TABLE messages ADD COLUMN deleted_at TEXT;
		CREATE INDEX idx_messages_not_deleted ON messages(channel, timestamp_utc DESC) WHERE deleted_at IS NULL;`,
		`ALTER TABLE messages ADD COLUMN empire_official INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE messages ADD COLUMN spam_at TEXT;
		CREATE INDEX idx_messages_spam ON messages(timestamp_utc DESC) WHERE spam_at IS NOT NULL;`,
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
	DeletedAt    *time.Time
	// SpamAt is set when the message is in the spam folder — either because it
	// was ingested from a blocked sender or flagged via MarkSpamBySender.
	// Spam messages are excluded from default queries (List, Search,
	// UnreadCounts), like soft-deleted ones.
	SpamAt *time.Time
	Source string
	// EmpireOfficial is true when the server delivered this message through
	// the verified empire-leadership pipeline or from an empire NPC. When
	// set, SenderID is the empire's own ID. Use it to detect player
	// impersonation of empire officials. (server v0.294.0+)
	EmpireOfficial bool
}

// Query holds filter parameters for retrieving messages. By default,
// soft-deleted messages are excluded; set IncludeDeleted to see them.
type Query struct {
	Channel         string
	SenderID        string
	UnreadOnly      bool
	IncludeDeleted  bool
	// IncludeSpam includes spam-flagged messages in results (they are
	// excluded by default). SpamOnly restricts results to spam-flagged
	// messages — the "spam folder" view. SpamOnly takes precedence.
	IncludeSpam     bool
	SpamOnly        bool
	Before          time.Time
	After           time.Time
	Limit           int
	Offset          int
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
			(id, channel, sender_id, sender, content, target_id, target_name, timestamp_utc, ingested_at, source, empire_official, spam_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		boolToInt(msg.EmpireOfficial),
		nullableTime(msg.SpamAt),
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
	query := "SELECT id, channel, sender_id, sender, content, target_id, target_name, timestamp_utc, ingested_at, read_at, deleted_at, source, empire_official, spam_at FROM messages"
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
// Returns soft-deleted messages as well; callers that want to exclude
// them should check the returned Message.DeletedAt field.
func (s *Store) Get(id string) (*Message, error) {
	row := s.db.QueryRow(
		"SELECT id, channel, sender_id, sender, content, target_id, target_name, timestamp_utc, ingested_at, read_at, deleted_at, source, empire_official, spam_at FROM messages WHERE id = ?",
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

// GetByPrefix retrieves a single message whose ID starts with prefix.
// Returns nil, nil if no match. Returns an error if the prefix matches
// more than one message (caller must disambiguate by using a longer
// prefix or the full ID).
func (s *Store) GetByPrefix(prefix string) (*Message, error) {
	if prefix == "" {
		return nil, fmt.Errorf("mbox: empty prefix")
	}
	rows, err := s.db.Query(
		"SELECT id, channel, sender_id, sender, content, target_id, target_name, timestamp_utc, ingested_at, read_at, deleted_at, source, empire_official, spam_at FROM messages WHERE id LIKE ? || '%' LIMIT 2",
		prefix,
	)
	if err != nil {
		return nil, fmt.Errorf("mbox: get by prefix: %w", err)
	}
	defer func() { _ = rows.Close() }()

	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, fmt.Errorf("mbox: get by prefix scan: %w", err)
	}
	switch len(msgs) {
	case 0:
		return nil, nil
	case 1:
		return &msgs[0], nil
	default:
		return nil, fmt.Errorf("mbox: prefix %q is ambiguous (matched multiple messages)", prefix)
	}
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
	if !q.IncludeDeleted {
		clauses = append(clauses, "deleted_at IS NULL")
	}
	switch {
	case q.SpamOnly:
		clauses = append(clauses, "spam_at IS NOT NULL")
	case !q.IncludeSpam:
		clauses = append(clauses, "spam_at IS NULL")
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
		msg            Message
		targetID       sql.NullString
		targetName     sql.NullString
		readAt         sql.NullString
		deletedAt      sql.NullString
		spamAt         sql.NullString
		tsStr          string
		ingestStr      string
		empireOfficial int
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
		&deletedAt,
		&msg.Source,
		&empireOfficial,
		&spamAt,
	); err != nil {
		return Message{}, err
	}
	msg.EmpireOfficial = empireOfficial != 0

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
	if deletedAt.Valid {
		t, err := time.Parse(timeFormat, deletedAt.String)
		if err != nil {
			return Message{}, fmt.Errorf("parse deleted_at %q: %w", deletedAt.String, err)
		}
		msg.DeletedAt = &t
	}
	if spamAt.Valid {
		t, err := time.Parse(timeFormat, spamAt.String)
		if err != nil {
			return Message{}, fmt.Errorf("parse spam_at %q: %w", spamAt.String, err)
		}
		msg.SpamAt = &t
	}

	return msg, nil
}

// SoftDelete marks a message as deleted. It is hidden from default
// queries (List, Search) but remains in the store and can be undone
// with Restore. No-op if the message is already deleted.
func (s *Store) SoftDelete(id string) error {
	now := time.Now().UTC().Format(timeFormat)
	if _, err := s.db.Exec(
		"UPDATE messages SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL",
		now, id,
	); err != nil {
		return fmt.Errorf("mbox: soft delete %s: %w", id, err)
	}
	return nil
}

// Restore clears the soft-delete flag on a message, making it visible
// to default queries again. No-op if not deleted.
func (s *Store) Restore(id string) error {
	if _, err := s.db.Exec(
		"UPDATE messages SET deleted_at = NULL WHERE id = ?",
		id,
	); err != nil {
		return fmt.Errorf("mbox: restore %s: %w", id, err)
	}
	return nil
}

// MarkSpamBySender flags every non-spam message whose sender_id or sender
// display name matches idOrName (case-insensitive) as spam, moving it into the
// spam folder. It returns the number of messages newly flagged.
func (s *Store) MarkSpamBySender(idOrName string) (int, error) {
	now := time.Now().UTC().Format(timeFormat)
	res, err := s.db.Exec(
		`UPDATE messages SET spam_at = ?
			WHERE spam_at IS NULL
			  AND (LOWER(sender_id) = LOWER(?) OR LOWER(sender) = LOWER(?))`,
		now, idOrName, idOrName,
	)
	if err != nil {
		return 0, fmt.Errorf("mbox: mark spam %q: %w", idOrName, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mbox: mark spam rows affected: %w", err)
	}
	return int(n), nil
}

// UnmarkSpamBySender clears the spam flag on every spam message whose sender_id
// or sender display name matches idOrName (case-insensitive), restoring it to
// its original folder. It returns the number of messages restored.
func (s *Store) UnmarkSpamBySender(idOrName string) (int, error) {
	res, err := s.db.Exec(
		`UPDATE messages SET spam_at = NULL
			WHERE spam_at IS NOT NULL
			  AND (LOWER(sender_id) = LOWER(?) OR LOWER(sender) = LOWER(?))`,
		idOrName, idOrName,
	)
	if err != nil {
		return 0, fmt.Errorf("mbox: unmark spam %q: %w", idOrName, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mbox: unmark spam rows affected: %w", err)
	}
	return int(n), nil
}

// MarkRead marks the given message IDs as read if they are currently unread.
func (s *Store) MarkRead(ids ...string) error {
	now := time.Now().UTC().Format(timeFormat)
	for _, id := range ids {
		if _, err := s.db.Exec(
			"UPDATE messages SET read_at = ? WHERE id = ? AND read_at IS NULL",
			now, id,
		); err != nil {
			return fmt.Errorf("mbox: mark read %s: %w", id, err)
		}
	}
	return nil
}

// MarkChannelRead marks all unread messages in a channel as read.
func (s *Store) MarkChannelRead(channel string) error {
	now := time.Now().UTC().Format(timeFormat)
	if _, err := s.db.Exec(
		"UPDATE messages SET read_at = ? WHERE channel = ? AND read_at IS NULL",
		now, channel,
	); err != nil {
		return fmt.Errorf("mbox: mark channel read %s: %w", channel, err)
	}
	return nil
}

// UnreadCounts returns a map of channel → unread message count.
func (s *Store) UnreadCounts() (map[string]int, error) {
	rows, err := s.db.Query(
		"SELECT channel, COUNT(*) FROM messages WHERE read_at IS NULL AND spam_at IS NULL GROUP BY channel",
	)
	if err != nil {
		return nil, fmt.Errorf("mbox: unread counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int)
	for rows.Next() {
		var ch string
		var n int
		if err := rows.Scan(&ch, &n); err != nil {
			return nil, fmt.Errorf("mbox: unread counts scan: %w", err)
		}
		counts[ch] = n
	}
	return counts, rows.Err()
}

// Search performs a full-text search over messages, with optional Query filters.
// Default limit is 20 if q.Limit is 0.
func (s *Store) Search(text string, q Query) ([]Message, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	var clauses []string
	args := []any{text}

	if q.Channel != "" {
		clauses = append(clauses, "m.channel = ?")
		args = append(args, q.Channel)
	}
	if q.SenderID != "" {
		clauses = append(clauses, "m.sender_id = ?")
		args = append(args, q.SenderID)
	}
	if q.UnreadOnly {
		clauses = append(clauses, "m.read_at IS NULL")
	}
	if !q.IncludeDeleted {
		clauses = append(clauses, "m.deleted_at IS NULL")
	}
	switch {
	case q.SpamOnly:
		clauses = append(clauses, "m.spam_at IS NOT NULL")
	case !q.IncludeSpam:
		clauses = append(clauses, "m.spam_at IS NULL")
	}

	query := `SELECT m.id, m.channel, m.sender_id, m.sender, m.content, m.target_id, m.target_name, m.timestamp_utc, m.ingested_at, m.read_at, m.deleted_at, m.source, m.empire_official, m.spam_at
FROM messages m
JOIN messages_fts ON m.rowid = messages_fts.rowid
WHERE messages_fts MATCH ?`

	if len(clauses) > 0 {
		query += " AND " + strings.Join(clauses, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY m.timestamp_utc DESC LIMIT %d OFFSET %d", limit, q.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("mbox: search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, fmt.Errorf("mbox: search scan: %w", err)
	}
	return msgs, nil
}

// SearchCount returns the total number of messages matching the FTS query,
// applying the same filters (Channel, SenderID, UnreadOnly, IncludeDeleted)
// as Search but ignoring Limit/Offset.
func (s *Store) SearchCount(text string, q Query) (int, error) {
	var clauses []string
	args := []any{text}

	if q.Channel != "" {
		clauses = append(clauses, "m.channel = ?")
		args = append(args, q.Channel)
	}
	if q.SenderID != "" {
		clauses = append(clauses, "m.sender_id = ?")
		args = append(args, q.SenderID)
	}
	if q.UnreadOnly {
		clauses = append(clauses, "m.read_at IS NULL")
	}
	if !q.IncludeDeleted {
		clauses = append(clauses, "m.deleted_at IS NULL")
	}
	switch {
	case q.SpamOnly:
		clauses = append(clauses, "m.spam_at IS NOT NULL")
	case !q.IncludeSpam:
		clauses = append(clauses, "m.spam_at IS NULL")
	}

	query := `SELECT COUNT(*)
FROM messages m
JOIN messages_fts ON m.rowid = messages_fts.rowid
WHERE messages_fts MATCH ?`
	if len(clauses) > 0 {
		query += " AND " + strings.Join(clauses, " AND ")
	}

	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("mbox: search count: %w", err)
	}
	return n, nil
}

// SourceCounts returns a map of source → message count.
func (s *Store) SourceCounts() (map[string]int, error) {
	rows, err := s.db.Query(
		"SELECT source, COUNT(*) FROM messages GROUP BY source",
	)
	if err != nil {
		return nil, fmt.Errorf("mbox: source counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int)
	for rows.Next() {
		var src string
		var n int
		if err := rows.Scan(&src, &n); err != nil {
			return nil, fmt.Errorf("mbox: source counts scan: %w", err)
		}
		counts[src] = n
	}
	return counts, rows.Err()
}

// Cursor returns the oldest-seen timestamp for a channel's backfill cursor.
// Returns (time.Time{}, false, nil) if no cursor exists for the channel.
func (s *Store) Cursor(channel string) (time.Time, bool, error) {
	var tsStr string
	err := s.db.QueryRow(
		"SELECT oldest_seen_utc FROM channel_cursors WHERE channel = ?", channel,
	).Scan(&tsStr)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("mbox: cursor %s: %w", channel, err)
	}
	t, err := time.Parse(timeFormat, tsStr)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("mbox: cursor parse %q: %w", tsStr, err)
	}
	return t, true, nil
}

// SetCursor upserts the backfill cursor for a channel.
func (s *Store) SetCursor(channel string, oldest time.Time, capped bool) error {
	now := time.Now().UTC().Format(timeFormat)
	oldestStr := oldest.UTC().Format(timeFormat)
	cappedInt := 0
	if capped {
		cappedInt = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO channel_cursors (channel, oldest_seen_utc, newest_seen_utc, last_backfill_at, backfill_capped)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(channel) DO UPDATE SET oldest_seen_utc=excluded.oldest_seen_utc, last_backfill_at=excluded.last_backfill_at, backfill_capped=excluded.backfill_capped`,
		channel, oldestStr, oldestStr, now, cappedInt,
	)
	if err != nil {
		return fmt.Errorf("mbox: set cursor %s: %w", channel, err)
	}
	return nil
}

// ClearCursor removes the backfill cursor for a channel so the next
// Backfill starts from the most recent page (i.e. "from now") instead
// of resuming from the oldest previously-seen message.
func (s *Store) ClearCursor(channel string) error {
	if _, err := s.db.Exec("DELETE FROM channel_cursors WHERE channel = ?", channel); err != nil {
		return fmt.Errorf("mbox: clear cursor %s: %w", channel, err)
	}
	return nil
}

// nullableString converts an empty string to a NULL sql value.
func nullableString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// nullableTime converts a nil *time.Time to a NULL sql value, otherwise the
// UTC timestamp formatted with timeFormat.
func nullableTime(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(timeFormat), Valid: true}
}

// boolToInt maps a bool to the 0/1 integer SQLite uses for boolean columns.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
