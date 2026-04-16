# Agent Mbox Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per-agent SQLite message store with push-driven ingestion, bounded backfill, FTS5 search, and CLI browsing.

**Architecture:** New `pkg/mbox` package (store, ingester, CLI helpers). `Client` gains an `OnChatMessage` callback following the existing `onStorageUpdate` pattern. `play_as` REPL wires up the mbox at startup and adds `mbox` subcommands.

**Tech Stack:** Go 1.24, `modernc.org/sqlite` (same driver as `pkg/knowledge`), FTS5.

**Spec:** `docs/superpowers/specs/2026-04-15-agent-mbox-design.md`

---

### Task 1: Store — Schema, Open, Migrate

**Files:**
- Create: `pkg/mbox/store.go`
- Create: `pkg/mbox/store_test.go`

This task builds the `Store` type with SQLite init, WAL mode, schema creation, and the migration framework.

- [ ] **Step 1: Write the failing test for Store.Open**

Create `pkg/mbox/store_test.go`:

```go
package mbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "mbox.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q): %v", dbPath, err)
	}
	defer s.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestOpenRunsMigrations(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Verify tables exist by querying them
	var count int
	err = s.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	if err != nil {
		t.Fatalf("messages table not created: %v", err)
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM channel_cursors").Scan(&count)
	if err != nil {
		t.Fatalf("channel_cursors table not created: %v", err)
	}
	err = s.db.QueryRow("SELECT version FROM schema_version ORDER BY version DESC LIMIT 1").Scan(&count)
	if err != nil {
		t.Fatalf("schema_version not populated: %v", err)
	}
	if count < 1 {
		t.Fatalf("schema_version = %d, want >= 1", count)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/mbox/ -run TestOpen -v`
Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Write minimal implementation**

Create `pkg/mbox/store.go`:

```go
package mbox

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store is a per-agent SQLite message store for chat messages.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// Open creates or opens an mbox database at the given path.
// It enables WAL mode and runs schema migrations.
func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("mbox: create dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("mbox: open db: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("mbox: enable WAL: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("mbox: migrate: %w", err)
	}
	return s, nil
}

// Close closes the database connection.
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
		// Version 1: core tables
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

// Message represents a stored chat message.
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

// Query filters for listing messages.
type Query struct {
	Channel    string
	SenderID   string
	UnreadOnly bool
	Before     time.Time
	After      time.Time
	Limit      int
	Offset     int
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/mbox/ -run TestOpen -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/mbox/store.go pkg/mbox/store_test.go
git commit -m "feat(mbox): add Store with schema, migrations, and Open/Close"
```

---

### Task 2: Store — Ingest, List, Get

**Files:**
- Modify: `pkg/mbox/store.go`
- Modify: `pkg/mbox/store_test.go`

Adds `Ingest`, `List`, and `Get` methods.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/mbox/store_test.go`:

```go
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestIngestAndDedupe(t *testing.T) {
	s := newTestStore(t)

	msg := Message{
		ID:           "msg-001",
		Channel:      "system",
		SenderID:     "player-1",
		Sender:       "Alice",
		Content:      "Hello world",
		TimestampUTC: time.Now().UTC(),
		Source:       "push",
	}

	inserted, err := s.Ingest(msg)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !inserted {
		t.Fatal("first Ingest should return inserted=true")
	}

	// Duplicate should not insert
	inserted, err = s.Ingest(msg)
	if err != nil {
		t.Fatalf("Ingest duplicate: %v", err)
	}
	if inserted {
		t.Fatal("duplicate Ingest should return inserted=false")
	}
}

func TestListByChannel(t *testing.T) {
	s := newTestStore(t)

	base := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	for i, ch := range []string{"system", "faction", "system"} {
		s.Ingest(Message{
			ID:           fmt.Sprintf("msg-%03d", i),
			Channel:      ch,
			SenderID:     "p1",
			Sender:       "Alice",
			Content:      fmt.Sprintf("msg %d", i),
			TimestampUTC: base.Add(time.Duration(i) * time.Minute),
			Source:       "push",
		})
	}

	msgs, err := s.List(Query{Channel: "system", Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	// Newest first
	if msgs[0].ID != "msg-002" {
		t.Fatalf("expected newest first, got %s", msgs[0].ID)
	}
}

func TestListUnreadOnly(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	s.Ingest(Message{ID: "r1", Channel: "system", SenderID: "p1", Sender: "A", Content: "x", TimestampUTC: now, Source: "push"})
	s.Ingest(Message{ID: "r2", Channel: "system", SenderID: "p1", Sender: "A", Content: "y", TimestampUTC: now.Add(time.Second), Source: "push"})
	s.MarkRead("r1")

	msgs, err := s.List(Query{Channel: "system", UnreadOnly: true, Limit: 10})
	if err != nil {
		t.Fatalf("List unread: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "r2" {
		t.Fatalf("expected only r2 unread, got %v", msgs)
	}
}

func TestGet(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()
	s.Ingest(Message{ID: "g1", Channel: "local", SenderID: "p1", Sender: "Bob", Content: "hi", TimestampUTC: now, Source: "push"})

	msg, err := s.Get("g1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if msg == nil || msg.Sender != "Bob" {
		t.Fatalf("unexpected message: %+v", msg)
	}

	msg, err = s.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get nonexistent: %v", err)
	}
	if msg != nil {
		t.Fatal("expected nil for nonexistent ID")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/mbox/ -run "TestIngest|TestList|TestGet" -v`
Expected: FAIL — methods not defined.

- [ ] **Step 3: Implement Ingest, List, Get**

Add to `pkg/mbox/store.go`:

```go
const timeFormat = time.RFC3339Nano

// Ingest stores a message. Returns (true, nil) if inserted, (false, nil) if duplicate.
func (s *Store) Ingest(msg Message) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(timeFormat)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO messages (id, channel, sender_id, sender, content, target_id, target_name, timestamp_utc, ingested_at, read_at, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)`,
		msg.ID, msg.Channel, msg.SenderID, msg.Sender, msg.Content,
		msg.TargetID, msg.TargetName,
		msg.TimestampUTC.Format(timeFormat), now, msg.Source,
	)
	if err != nil {
		return false, fmt.Errorf("mbox: ingest: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// List returns messages matching the query, newest first.
func (s *Store) List(q Query) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	where, args := buildWhere(q)
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	query := fmt.Sprintf(
		"SELECT id, channel, sender_id, sender, content, target_id, target_name, timestamp_utc, ingested_at, read_at, source FROM messages %s ORDER BY timestamp_utc DESC LIMIT ? OFFSET ?",
		where,
	)
	args = append(args, limit, q.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("mbox: list: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// Get returns a single message by ID, or nil if not found.
func (s *Store) Get(id string) (*Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(
		"SELECT id, channel, sender_id, sender, content, target_id, target_name, timestamp_utc, ingested_at, read_at, source FROM messages WHERE id = ?",
		id,
	)
	msg, err := scanMessage(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mbox: get: %w", err)
	}
	return &msg, nil
}

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
		args = append(args, q.Before.Format(timeFormat))
	}
	if !q.After.IsZero() {
		clauses = append(clauses, "timestamp_utc > ?")
		args = append(args, q.After.Format(timeFormat))
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

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

type scanner interface {
	Scan(dest ...any) error
}

func scanRow(row scanner) (Message, error) {
	var m Message
	var tsUTC, ingestedAt string
	var readAt, targetID, targetName sql.NullString
	err := row.Scan(&m.ID, &m.Channel, &m.SenderID, &m.Sender, &m.Content,
		&targetID, &targetName, &tsUTC, &ingestedAt, &readAt, &m.Source)
	if err != nil {
		return m, err
	}
	m.TargetID = targetID.String
	m.TargetName = targetName.String
	m.TimestampUTC, _ = time.Parse(timeFormat, tsUTC)
	m.IngestedAt, _ = time.Parse(timeFormat, ingestedAt)
	if readAt.Valid {
		t, _ := time.Parse(timeFormat, readAt.String)
		m.ReadAt = &t
	}
	return m, nil
}

func scanMessage(row *sql.Row) (Message, error) {
	return scanRow(row)
}
```

Add `"strings"` to imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/mbox/ -run "TestIngest|TestList|TestGet" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/mbox/store.go pkg/mbox/store_test.go
git commit -m "feat(mbox): add Ingest, List, Get methods"
```

---

### Task 3: Store — MarkRead, UnreadCounts, Search, SourceCounts, Cursors

**Files:**
- Modify: `pkg/mbox/store.go`
- Modify: `pkg/mbox/store_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `pkg/mbox/store_test.go`:

```go
func TestMarkReadAndUnreadCounts(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	for i, ch := range []string{"system", "system", "faction"} {
		s.Ingest(Message{
			ID: fmt.Sprintf("u%d", i), Channel: ch, SenderID: "p1", Sender: "A",
			Content: "x", TimestampUTC: now.Add(time.Duration(i) * time.Second), Source: "push",
		})
	}

	counts, err := s.UnreadCounts()
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if counts["system"] != 2 || counts["faction"] != 1 {
		t.Fatalf("unexpected counts: %v", counts)
	}

	if err := s.MarkRead("u0"); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	counts, _ = s.UnreadCounts()
	if counts["system"] != 1 {
		t.Fatalf("after MarkRead, system=%d want 1", counts["system"])
	}

	if err := s.MarkChannelRead("system"); err != nil {
		t.Fatalf("MarkChannelRead: %v", err)
	}
	counts, _ = s.UnreadCounts()
	if counts["system"] != 0 {
		t.Fatalf("after MarkChannelRead, system=%d want 0", counts["system"])
	}
}

func TestSearch(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	s.Ingest(Message{ID: "s1", Channel: "local", SenderID: "p1", Sender: "StationKeeper",
		Content: "Station Aldrin needs iron_ore and copper_ore", TimestampUTC: now, Source: "push"})
	s.Ingest(Message{ID: "s2", Channel: "local", SenderID: "p2", Sender: "Trader",
		Content: "Selling fuel cells", TimestampUTC: now.Add(time.Second), Source: "push"})
	s.Ingest(Message{ID: "s3", Channel: "faction", SenderID: "p1", Sender: "StationKeeper",
		Content: "Station Hoffman needs uranium", TimestampUTC: now.Add(2 * time.Second), Source: "push"})

	// Search content
	msgs, err := s.Search("iron_ore", Query{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "s1" {
		t.Fatalf("search iron_ore: got %d results", len(msgs))
	}

	// Search sender
	msgs, _ = s.Search("StationKeeper", Query{Limit: 10})
	if len(msgs) != 2 {
		t.Fatalf("search StationKeeper: got %d results, want 2", len(msgs))
	}

	// Search with channel filter
	msgs, _ = s.Search("needs", Query{Channel: "local", Limit: 10})
	if len(msgs) != 1 || msgs[0].ID != "s1" {
		t.Fatalf("search needs+local: got %d results, want 1", len(msgs))
	}
}

func TestSourceCounts(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	s.Ingest(Message{ID: "sc1", Channel: "system", SenderID: "p1", Sender: "A", Content: "x", TimestampUTC: now, Source: "push"})
	s.Ingest(Message{ID: "sc2", Channel: "system", SenderID: "p1", Sender: "A", Content: "y", TimestampUTC: now.Add(time.Second), Source: "backfill"})
	s.Ingest(Message{ID: "sc3", Channel: "system", SenderID: "p1", Sender: "A", Content: "z", TimestampUTC: now.Add(2 * time.Second), Source: "push"})

	counts, err := s.SourceCounts()
	if err != nil {
		t.Fatalf("SourceCounts: %v", err)
	}
	if counts["push"] != 2 || counts["backfill"] != 1 {
		t.Fatalf("unexpected source counts: %v", counts)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	s := newTestStore(t)

	_, exists, err := s.Cursor("faction")
	if err != nil {
		t.Fatalf("Cursor: %v", err)
	}
	if exists {
		t.Fatal("cursor should not exist yet")
	}

	oldest := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)
	if err := s.SetCursor("faction", oldest, true); err != nil {
		t.Fatalf("SetCursor: %v", err)
	}

	got, exists, err := s.Cursor("faction")
	if err != nil {
		t.Fatalf("Cursor after set: %v", err)
	}
	if !exists {
		t.Fatal("cursor should exist after SetCursor")
	}
	if !got.Equal(oldest) {
		t.Fatalf("got %v, want %v", got, oldest)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/mbox/ -run "TestMarkRead|TestSearch|TestSourceCounts|TestCursor" -v`
Expected: FAIL — methods not defined.

- [ ] **Step 3: Implement the methods**

Add to `pkg/mbox/store.go`:

```go
// MarkRead marks specific messages as read.
func (s *Store) MarkRead(ids ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(timeFormat)
	for _, id := range ids {
		if _, err := s.db.Exec("UPDATE messages SET read_at = ? WHERE id = ? AND read_at IS NULL", now, id); err != nil {
			return fmt.Errorf("mbox: mark read %s: %w", id, err)
		}
	}
	return nil
}

// MarkChannelRead marks all unread messages in a channel as read.
func (s *Store) MarkChannelRead(channel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(timeFormat)
	_, err := s.db.Exec("UPDATE messages SET read_at = ? WHERE channel = ? AND read_at IS NULL", now, channel)
	if err != nil {
		return fmt.Errorf("mbox: mark channel read %s: %w", channel, err)
	}
	return nil
}

// UnreadCounts returns the count of unread messages per channel.
func (s *Store) UnreadCounts() (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT channel, COUNT(*) FROM messages WHERE read_at IS NULL GROUP BY channel")
	if err != nil {
		return nil, fmt.Errorf("mbox: unread counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var ch string
		var n int
		if err := rows.Scan(&ch, &n); err != nil {
			return nil, err
		}
		counts[ch] = n
	}
	return counts, rows.Err()
}

// Search performs FTS5 full-text search over message content and sender.
func (s *Store) Search(text string, q Query) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	var clauses []string
	var args []any

	clauses = append(clauses, "messages_fts MATCH ?")
	args = append(args, text)

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

	where := "WHERE " + strings.Join(clauses, " AND ")
	query := fmt.Sprintf(
		`SELECT m.id, m.channel, m.sender_id, m.sender, m.content, m.target_id, m.target_name, m.timestamp_utc, m.ingested_at, m.read_at, m.source
		 FROM messages m
		 JOIN messages_fts ON m.rowid = messages_fts.rowid
		 %s
		 ORDER BY m.timestamp_utc DESC LIMIT ? OFFSET ?`,
		where,
	)
	args = append(args, limit, q.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("mbox: search: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// SourceCounts returns the count of messages grouped by source (push/backfill/reconcile).
func (s *Store) SourceCounts() (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT source, COUNT(*) FROM messages GROUP BY source")
	if err != nil {
		return nil, fmt.Errorf("mbox: source counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var src string
		var n int
		if err := rows.Scan(&src, &n); err != nil {
			return nil, err
		}
		counts[src] = n
	}
	return counts, rows.Err()
}

// Cursor returns the oldest backfilled timestamp for a channel.
func (s *Store) Cursor(channel string) (time.Time, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var oldest string
	err := s.db.QueryRow("SELECT oldest_seen_utc FROM channel_cursors WHERE channel = ?", channel).Scan(&oldest)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("mbox: cursor: %w", err)
	}
	t, _ := time.Parse(timeFormat, oldest)
	return t, true, nil
}

// SetCursor upserts the backfill cursor for a channel.
func (s *Store) SetCursor(channel string, oldest time.Time, capped bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(timeFormat)
	cappedInt := 0
	if capped {
		cappedInt = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO channel_cursors (channel, oldest_seen_utc, newest_seen_utc, last_backfill_at, backfill_capped)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(channel) DO UPDATE SET oldest_seen_utc=excluded.oldest_seen_utc, last_backfill_at=excluded.last_backfill_at, backfill_capped=excluded.backfill_capped`,
		channel, oldest.Format(timeFormat), now, now, cappedInt,
	)
	if err != nil {
		return fmt.Errorf("mbox: set cursor: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/mbox/ -v`
Expected: ALL PASS

- [ ] **Step 5: Run golangci-lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/mbox/`
Expected: No new findings

- [ ] **Step 6: Commit**

```bash
git add pkg/mbox/store.go pkg/mbox/store_test.go
git commit -m "feat(mbox): add MarkRead, UnreadCounts, Search, SourceCounts, Cursor methods"
```

---

### Task 4: Ingester — Push Handler

**Files:**
- Create: `pkg/mbox/ingest.go`
- Create: `pkg/mbox/ingest_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/mbox/ingest_test.go`:

```go
package mbox

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestHandlePush(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ing := NewIngester(s)

	msg := serverapi.ChatMessage{
		ID:           "push-001",
		Channel:      "local",
		SenderID:     "player-42",
		Sender:       "GunnyDraper",
		Content:      "Anyone selling tritanium?",
		TimestampUTC: time.Now().UTC().Format(time.RFC3339),
	}

	ing.HandlePush(msg)

	// Verify it was stored
	got, err := s.Get("push-001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("message not found after HandlePush")
	}
	if got.Source != "push" {
		t.Fatalf("source = %q, want push", got.Source)
	}
	if got.Sender != "GunnyDraper" {
		t.Fatalf("sender = %q, want GunnyDraper", got.Sender)
	}
}

func TestHandlePushDedupe(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ing := NewIngester(s)

	msg := serverapi.ChatMessage{
		ID:           "dup-001",
		Channel:      "system",
		SenderID:     "p1",
		Sender:       "Alice",
		Content:      "hello",
		TimestampUTC: time.Now().UTC().Format(time.RFC3339),
	}

	ing.HandlePush(msg)
	ing.HandlePush(msg) // duplicate — should not error

	msgs, _ := s.List(Query{Channel: "system", Limit: 10})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/mbox/ -run TestHandlePush -v`
Expected: FAIL — `NewIngester` not defined.

- [ ] **Step 3: Implement Ingester and HandlePush**

Create `pkg/mbox/ingest.go`:

```go
package mbox

import (
	"log"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Ingester writes chat messages into a Store from push events and backfill crawls.
type Ingester struct {
	store  *Store
	logger *log.Logger
}

// NewIngester creates an Ingester for the given Store.
func NewIngester(store *Store) *Ingester {
	return &Ingester{
		store:  store,
		logger: log.New(log.Default().Writer(), "[mbox] ", log.LstdFlags),
	}
}

// HandlePush ingests a single chat message received via server push.
func (ing *Ingester) HandlePush(msg serverapi.ChatMessage) {
	ts, err := time.Parse(time.RFC3339, msg.TimestampUTC)
	if err != nil {
		ts, err = time.Parse(time.RFC3339Nano, msg.TimestampUTC)
		if err != nil {
			ts = time.Now().UTC()
		}
	}

	m := Message{
		ID:           msg.ID,
		Channel:      msg.Channel,
		SenderID:     msg.SenderID,
		Sender:       msg.Sender,
		Content:      msg.Content,
		TargetID:     msg.TargetID,
		TargetName:   msg.TargetName,
		TimestampUTC: ts,
		Source:       "push",
	}
	if _, err := ing.store.Ingest(m); err != nil {
		ing.logger.Printf("push ingest error: %v", err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/mbox/ -run TestHandlePush -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/mbox/ingest.go pkg/mbox/ingest_test.go
git commit -m "feat(mbox): add Ingester with HandlePush for server push events"
```

---

### Task 5: Ingester — Backfill

**Files:**
- Modify: `pkg/mbox/ingest.go`
- Modify: `pkg/mbox/ingest_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/mbox/ingest_test.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
)

// fakeGameClient simulates GetChatHistory for backfill tests.
type fakeGameClient struct {
	pages   map[string][][]serverapi.ChatMessage // channel -> pages (each page = one call, newest first)
	callIdx map[string]*atomic.Int32
	rawJSON []byte
}

func newFakeClient() *fakeGameClient {
	return &fakeGameClient{
		pages:   make(map[string][][]serverapi.ChatMessage),
		callIdx: make(map[string]*atomic.Int32),
	}
}

func (f *fakeGameClient) addPage(channel string, msgs []serverapi.ChatMessage) {
	f.pages[channel] = append(f.pages[channel], msgs)
	if _, ok := f.callIdx[channel]; !ok {
		f.callIdx[channel] = &atomic.Int32{}
	}
}

func (f *fakeGameClient) GetChatHistory(ctx context.Context, channel string, payload map[string]any) error {
	idx, ok := f.callIdx[channel]
	if !ok {
		f.rawJSON = []byte(`{"messages":[]}`)
		return nil
	}
	i := int(idx.Add(1) - 1)
	pages := f.pages[channel]
	if i >= len(pages) {
		f.rawJSON = []byte(`{"messages":[]}`)
		return nil
	}
	data, _ := json.Marshal(map[string]any{"messages": pages[i]})
	f.rawJSON = data
	return nil
}

func (f *fakeGameClient) GetRawJSON(key string) []byte {
	return f.rawJSON
}

func TestBackfillBasic(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ing := NewIngester(s)
	fc := newFakeClient()

	now := time.Now().UTC()
	var page []serverapi.ChatMessage
	for i := range 5 {
		page = append(page, serverapi.ChatMessage{
			ID:           fmt.Sprintf("bf-%03d", i),
			Channel:      "system",
			SenderID:     "p1",
			Sender:       "Alice",
			Content:      fmt.Sprintf("msg %d", i),
			TimestampUTC: now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
		})
	}
	fc.addPage("system", page)

	report, err := ing.Backfill(context.Background(), fc, BackfillOptions{
		Channels:        []string{"system"},
		MaxPerChannel:   500,
		RequestInterval: 0, // no delay in tests
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if report.Channels["system"].Fetched != 5 {
		t.Fatalf("fetched = %d, want 5", report.Channels["system"].Fetched)
	}

	msgs, _ := s.List(Query{Channel: "system", Limit: 10})
	if len(msgs) != 5 {
		t.Fatalf("stored %d messages, want 5", len(msgs))
	}
}

func TestBackfillStopsOnKnownID(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ing := NewIngester(s)

	// Pre-seed a message
	now := time.Now().UTC()
	s.Ingest(Message{
		ID: "existing-001", Channel: "faction", SenderID: "p1", Sender: "A",
		Content: "old", TimestampUTC: now.Add(-10 * time.Minute), Source: "push",
	})

	fc := newFakeClient()
	fc.addPage("faction", []serverapi.ChatMessage{
		{ID: "new-001", Channel: "faction", SenderID: "p1", Sender: "A", Content: "new", TimestampUTC: now.Format(time.RFC3339)},
		{ID: "existing-001", Channel: "faction", SenderID: "p1", Sender: "A", Content: "old", TimestampUTC: now.Add(-10 * time.Minute).Format(time.RFC3339)},
		{ID: "older-001", Channel: "faction", SenderID: "p1", Sender: "A", Content: "older", TimestampUTC: now.Add(-20 * time.Minute).Format(time.RFC3339)},
	})

	report, _ := ing.Backfill(context.Background(), fc, BackfillOptions{
		Channels:        []string{"faction"},
		MaxPerChannel:   500,
		RequestInterval: 0,
	})

	// Should have only inserted 1 new message (stopped at existing-001)
	if report.Channels["faction"].Fetched != 1 {
		t.Fatalf("fetched = %d, want 1", report.Channels["faction"].Fetched)
	}
}

func TestBackfillCap(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ing := NewIngester(s)
	fc := newFakeClient()

	now := time.Now().UTC()
	// Two pages of 3 messages each
	for p := range 2 {
		var page []serverapi.ChatMessage
		for i := range 3 {
			idx := p*3 + i
			page = append(page, serverapi.ChatMessage{
				ID:           fmt.Sprintf("cap-%03d", idx),
				Channel:      "local",
				SenderID:     "p1",
				Sender:       "A",
				Content:      "x",
				TimestampUTC: now.Add(-time.Duration(idx) * time.Minute).Format(time.RFC3339),
			})
		}
		fc.addPage("local", page)
	}

	report, _ := ing.Backfill(context.Background(), fc, BackfillOptions{
		Channels:        []string{"local"},
		MaxPerChannel:   4,
		RequestInterval: 0,
	})

	if !report.Channels["local"].Capped {
		t.Fatal("expected capped=true when hitting MaxPerChannel")
	}
	if report.Channels["local"].Fetched != 4 {
		t.Fatalf("fetched = %d, want 4 (capped)", report.Channels["local"].Fetched)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/mbox/ -run "TestBackfill" -v`
Expected: FAIL — `Backfill`, `BackfillOptions`, `BackfillReport` not defined.

- [ ] **Step 3: Implement Backfill**

Add to `pkg/mbox/ingest.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
)

// BackfillClient is the subset of game.GameClient needed for backfill.
type BackfillClient interface {
	GetChatHistory(ctx context.Context, channel string, payload map[string]any) error
	GetRawJSON(key string) []byte
}

// BackfillOptions configures the backfill crawl.
type BackfillOptions struct {
	Channels        []string
	MaxPerChannel   int
	RequestInterval time.Duration
}

// BackfillReport summarizes what backfill fetched.
type BackfillReport struct {
	Channels map[string]ChannelReport
}

// ChannelReport summarizes backfill results for one channel.
type ChannelReport struct {
	Fetched int
	Capped  bool
}

// Backfill crawls get_chat_history newest-first per channel until it hits
// a known message or reaches MaxPerChannel.
func (ing *Ingester) Backfill(ctx context.Context, client BackfillClient, opts BackfillOptions) (BackfillReport, error) {
	if opts.MaxPerChannel <= 0 {
		opts.MaxPerChannel = 500
	}

	report := BackfillReport{Channels: make(map[string]ChannelReport)}

	for _, ch := range opts.Channels {
		cr, err := ing.backfillChannel(ctx, client, ch, opts)
		if err != nil {
			ing.logger.Printf("backfill %s error: %v", ch, err)
		}
		report.Channels[ch] = cr
	}
	return report, nil
}

func (ing *Ingester) backfillChannel(ctx context.Context, client BackfillClient, channel string, opts BackfillOptions) (ChannelReport, error) {
	var cr ChannelReport
	var before string
	var oldestSeen time.Time

	for {
		if ctx.Err() != nil {
			return cr, ctx.Err()
		}
		if cr.Fetched >= opts.MaxPerChannel {
			cr.Capped = true
			break
		}

		payload := map[string]any{"limit": 100}
		if before != "" {
			payload["before"] = before
		}

		if err := client.GetChatHistory(ctx, channel, payload); err != nil {
			return cr, fmt.Errorf("get_chat_history(%s): %w", channel, err)
		}

		raw := client.GetRawJSON("_last")
		if len(raw) == 0 {
			break
		}

		var resp struct {
			Messages []serverapi.ChatMessage `json:"messages"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return cr, fmt.Errorf("unmarshal %s: %w", channel, err)
		}
		if len(resp.Messages) == 0 {
			break
		}

		hitKnown := false
		for _, m := range resp.Messages {
			if cr.Fetched >= opts.MaxPerChannel {
				cr.Capped = true
				hitKnown = true
				break
			}

			ts, err := time.Parse(time.RFC3339, m.TimestampUTC)
			if err != nil {
				ts, err = time.Parse(time.RFC3339Nano, m.TimestampUTC)
				if err != nil {
					ts = time.Now().UTC()
				}
			}

			msg := Message{
				ID:           m.ID,
				Channel:      m.Channel,
				SenderID:     m.SenderID,
				Sender:       m.Sender,
				Content:      m.Content,
				TargetID:     m.TargetID,
				TargetName:   m.TargetName,
				TimestampUTC: ts,
				Source:       "backfill",
			}
			if msg.Channel == "" {
				msg.Channel = channel
			}

			inserted, err := ing.store.Ingest(msg)
			if err != nil {
				return cr, fmt.Errorf("ingest: %w", err)
			}
			if !inserted {
				hitKnown = true
				break
			}
			cr.Fetched++

			if oldestSeen.IsZero() || ts.Before(oldestSeen) {
				oldestSeen = ts
			}
			before = m.TimestampUTC
		}

		if hitKnown {
			break
		}

		if opts.RequestInterval > 0 {
			time.Sleep(opts.RequestInterval)
		}
	}

	if !oldestSeen.IsZero() {
		if err := ing.store.SetCursor(channel, oldestSeen, cr.Capped); err != nil {
			ing.logger.Printf("set cursor %s: %v", channel, err)
		}
	}

	return cr, nil
}
```

- [ ] **Step 4: Run all tests**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/mbox/ -v`
Expected: ALL PASS

- [ ] **Step 5: Run golangci-lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/mbox/`
Expected: No new findings

- [ ] **Step 6: Commit**

```bash
git add pkg/mbox/ingest.go pkg/mbox/ingest_test.go
git commit -m "feat(mbox): add Backfill with bounded crawl, early exit, and cap"
```

---

### Task 6: Client OnChatMessage Callback

**Files:**
- Modify: `pkg/game/client.go`

Wire the `OnChatMessage` callback on `Client` following the `onStorageUpdate` pattern.

- [ ] **Step 1: Add the callback field and setter**

In `pkg/game/client.go`, add to the `Client` struct (near line 100, after `onStorageUpdate`):

```go
	// Chat message callback — fired when a chat_message push is received
	onChatMessage func(msg serverapi.ChatMessage)
	onChatMu      sync.RWMutex
```

Add setter method (after `SetOnStorageUpdate` around line 283):

```go
// SetOnChatMessage registers a callback that fires when a real-time
// chat_message push event is received from the server.
func (c *Client) SetOnChatMessage(fn func(msg serverapi.ChatMessage)) {
	c.onChatMu.Lock()
	defer c.onChatMu.Unlock()
	c.onChatMessage = fn
}
```

- [ ] **Step 2: Fire the callback in handleResponse**

In `pkg/game/client.go`, replace the `case protocol.TypeChatMessage:` block (around line 1845):

```go
	case protocol.TypeChatMessage:
		var chatMsg serverapi.ChatMessage
		if data, err := json.Marshal(resp.Payload); err == nil {
			if err := json.Unmarshal(data, &chatMsg); err == nil {
				c.onChatMu.RLock()
				cb := c.onChatMessage
				c.onChatMu.RUnlock()
				if cb != nil {
					cb(chatMsg)
				}
			}
		}
		if sender, ok := resp.Payload["sender"].(string); ok {
			if channel, ok := resp.Payload["channel"].(string); ok {
				c.debugLogger.Printf("[CHAT] %s (%s): %v", sender, channel, resp.Payload["content"])
			}
		} else {
			c.debugLogger.Printf("[CHAT] %v", resp.Payload)
		}
```

- [ ] **Step 3: Add SetOnChatMessage to the GameClient interface**

In `pkg/game/interface.go`, add near the other setter methods:

```go
	SetOnChatMessage(fn func(msg serverapi.ChatMessage))
```

- [ ] **Step 4: Add stub to MCPGameClient**

In `pkg/game/mcp_game_client.go`, add:

```go
func (m *MCPGameClient) SetOnChatMessage(fn func(msg serverapi.ChatMessage)) {
	// MCP bridge does not receive push events
}
```

- [ ] **Step 5: Update mock in runner_test.go**

In `pkg/agent/runner_test.go`, add to the `mockGameClient` struct:

```go
func (m *mockGameClient) SetOnChatMessage(fn func(msg serverapi.ChatMessage)) {}
```

- [ ] **Step 6: Update mock in client_dispatcher_test.go**

In `pkg/skills/client_dispatcher_test.go`, check if `mockGameClient` needs the same stub. Add if present:

```go
func (m *mockGameClient) SetOnChatMessage(fn func(msg serverapi.ChatMessage)) {}
```

- [ ] **Step 7: Build and test**

Run: `cd /home/robert/spacemolt/spacemolt && go build ./... && go test ./pkg/game/ ./pkg/agent/ ./pkg/skills/ -v -count=1 2>&1 | tail -30`
Expected: PASS, no compilation errors

- [ ] **Step 8: Run golangci-lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/game/ ./pkg/agent/ ./pkg/skills/`
Expected: No new findings

- [ ] **Step 9: Commit**

```bash
git add pkg/game/client.go pkg/game/interface.go pkg/game/mcp_game_client.go pkg/agent/runner_test.go pkg/skills/client_dispatcher_test.go
git commit -m "feat(game): add OnChatMessage callback for real-time chat push events"
```

---

### Task 7: Play-as REPL — Mbox Wiring and CLI Commands

**Files:**
- Modify: `cmd/tools/play_as/main.go`

Wire mbox `Store` + `Ingester` at startup, add `mbox` REPL subcommand.

- [ ] **Step 1: Add mbox initialization at REPL startup**

In `cmd/tools/play_as/main.go`, after the chat poller setup (~line 222), add mbox initialization:

```go
	// Initialize mbox store for persistent message storage.
	mboxDBPath := filepath.Join("data", "agents", agentID, "mbox.db")
	mboxStore, err := mbox.Open(mboxDBPath)
	if err != nil {
		log.Printf("[mbox] warning: could not open mbox: %v", err)
	} else {
		defer mboxStore.Close()

		mboxIng := mbox.NewIngester(mboxStore)

		// Wire push handler
		client.SetOnChatMessage(func(msg serverapi.ChatMessage) {
			mboxIng.HandlePush(msg)
		})

		// Backfill in background after login
		go func() {
			report, err := mboxIng.Backfill(ctx, client, mbox.BackfillOptions{
				Channels:        []string{"system", "local", "faction"},
				MaxPerChannel:   500,
				RequestInterval: game.SleepQuick,
			})
			if err != nil {
				log.Printf("[mbox] backfill error: %v", err)
				return
			}
			for ch, cr := range report.Channels {
				if cr.Fetched > 0 {
					suffix := ""
					if cr.Capped {
						suffix = " (capped)"
					}
					log.Printf("[mbox] backfill %s: %d messages%s", ch, cr.Fetched, suffix)
				}
			}
		}()
	}
```

Add import for `"github.com/rsned/spacemolt/pkg/mbox"`.

- [ ] **Step 2: Add mbox command dispatch**

In the REPL command dispatch section (before the default `executeCommand` call), add `mbox` handling. The cleanest place is after the `history` handling block (~line 287), as another special-cased REPL command:

```go
		if command == "mbox" {
			if mboxStore == nil {
				fmt.Println("mbox not available (database not initialized)")
				fmt.Println()
				continue
			}
			handleMboxCommand(mboxStore, mboxIng, client, ctx, parts[1:])
			fmt.Println()
			continue
		}
```

Note: `mboxIng` and `mboxStore` need to be accessible in this scope. Since they're declared inside an `if err == nil` block, restructure so that `mboxStore` is declared before the block (initialized to `nil`) and `mboxIng` is a `*mbox.Ingester` also declared at the top scope.

Move declarations before the `if` block:

```go
	var mboxStore *mbox.Store
	var mboxIng *mbox.Ingester

	mboxDBPath := filepath.Join("data", "agents", agentID, "mbox.db")
	if s, err := mbox.Open(mboxDBPath); err != nil {
		log.Printf("[mbox] warning: could not open mbox: %v", err)
	} else {
		mboxStore = s
		defer mboxStore.Close()
		mboxIng = mbox.NewIngester(mboxStore)
		client.SetOnChatMessage(func(msg serverapi.ChatMessage) {
			mboxIng.HandlePush(msg)
		})
		go func() { /* backfill as shown in step 1 */ }()
	}
```

- [ ] **Step 3: Implement handleMboxCommand**

Add the handler function to `cmd/tools/play_as/main.go`:

```go
func handleMboxCommand(store *mbox.Store, ing *mbox.Ingester, client game.GameClient, ctx context.Context, args []string) {
	if len(args) == 0 {
		// Show unread counts
		counts, err := store.UnreadCounts()
		if err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}
		for _, ch := range []string{"system", "local", "faction", "private"} {
			n := counts[ch]
			color := channelColors[ch]
			reset := "\033[0m"
			if n > 0 {
				fmt.Printf("  %s%-8s%s %d unread\n", color, ch, reset, n)
			} else {
				fmt.Printf("  %-8s 0 unread\n", ch)
			}
		}
		return
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "list":
		mboxList(store, args[1:])
	case "show":
		if len(args) < 2 {
			fmt.Println("usage: mbox show <id>")
			return
		}
		mboxShow(store, args[1])
	case "search":
		if len(args) < 2 {
			fmt.Println("usage: mbox search <query> [--channel <ch>]")
			return
		}
		mboxSearch(store, args[1:])
	case "read":
		mboxRead(store, args[1:])
	case "backfill":
		mboxBackfill(ing, client, ctx, args[1:])
	case "sources":
		mboxSources(store)
	default:
		fmt.Println("mbox commands: list, show, search, read, backfill, sources")
		fmt.Println("  mbox                                  show unread counts")
		fmt.Println("  mbox list [channel] [--unread] [-n N]  list messages")
		fmt.Println("  mbox show <id>                         show message detail")
		fmt.Println("  mbox search <query> [--channel <ch>]   full-text search")
		fmt.Println("  mbox read <id>|--all|--channel <ch>    mark read")
		fmt.Println("  mbox backfill [--channel <ch>] [--limit N]")
		fmt.Println("  mbox sources                           push/backfill/reconcile counts")
	}
}

func mboxList(store *mbox.Store, args []string) {
	q := mbox.Query{Limit: 20}
	for i := 0; i < len(args); i++ {
		switch strings.ToLower(args[i]) {
		case "--unread":
			q.UnreadOnly = true
		case "-n":
			if i+1 < len(args) {
				i++
				if n, err := strconv.Atoi(args[i]); err == nil {
					q.Limit = n
				}
			}
		default:
			if q.Channel == "" {
				q.Channel = strings.ToLower(args[i])
			}
		}
	}

	msgs, err := store.List(q)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if len(msgs) == 0 {
		fmt.Println("  (no messages)")
		return
	}
	for _, m := range msgs {
		printMboxMessage(m)
	}
}

func mboxShow(store *mbox.Store, id string) {
	msg, err := store.Get(id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if msg == nil {
		fmt.Printf("message %q not found\n", id)
		return
	}
	color := channelColors[msg.Channel]
	reset := "\033[0m"
	fmt.Printf("  ID:        %s\n", msg.ID)
	fmt.Printf("  Channel:   %s%s%s\n", color, msg.Channel, reset)
	fmt.Printf("  Sender:    %s (%s)\n", msg.Sender, msg.SenderID)
	fmt.Printf("  Time:      %s (%s)\n", msg.TimestampUTC.Format(time.RFC3339), relativeTime(msg.TimestampUTC))
	if msg.TargetID != "" {
		fmt.Printf("  Target:    %s", msg.TargetID)
		if msg.TargetName != "" {
			fmt.Printf(" (%s)", msg.TargetName)
		}
		fmt.Println()
	}
	fmt.Printf("  Source:    %s\n", msg.Source)
	read := "unread"
	if msg.ReadAt != nil {
		read = msg.ReadAt.Format(time.RFC3339)
	}
	fmt.Printf("  Read:      %s\n", read)
	fmt.Printf("  Content:\n    %s\n", msg.Content)
}

func mboxSearch(store *mbox.Store, args []string) {
	if len(args) == 0 {
		return
	}
	text := args[0]
	q := mbox.Query{Limit: 20}
	for i := 1; i < len(args); i++ {
		if strings.ToLower(args[i]) == "--channel" && i+1 < len(args) {
			i++
			q.Channel = strings.ToLower(args[i])
		}
	}

	msgs, err := store.Search(text, q)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if len(msgs) == 0 {
		fmt.Println("  (no results)")
		return
	}
	for _, m := range msgs {
		printMboxMessage(m)
	}
}

func mboxRead(store *mbox.Store, args []string) {
	if len(args) == 0 {
		fmt.Println("usage: mbox read <id> | --all | --channel <ch>")
		return
	}
	switch strings.ToLower(args[0]) {
	case "--all":
		for _, ch := range []string{"system", "local", "faction", "private"} {
			store.MarkChannelRead(ch)
		}
		fmt.Println("  marked all messages read")
	case "--channel":
		if len(args) < 2 {
			fmt.Println("usage: mbox read --channel <ch>")
			return
		}
		ch := strings.ToLower(args[1])
		if err := store.MarkChannelRead(ch); err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}
		fmt.Printf("  marked %s messages read\n", ch)
	default:
		if err := store.MarkRead(args[0]); err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}
		fmt.Printf("  marked %s read\n", args[0])
	}
}

func mboxBackfill(ing *mbox.Ingester, client game.GameClient, ctx context.Context, args []string) {
	opts := mbox.BackfillOptions{
		Channels:        []string{"system", "local", "faction"},
		MaxPerChannel:   500,
		RequestInterval: game.SleepQuick,
	}
	for i := 0; i < len(args); i++ {
		switch strings.ToLower(args[i]) {
		case "--channel":
			if i+1 < len(args) {
				i++
				opts.Channels = []string{strings.ToLower(args[i])}
			}
		case "--limit":
			if i+1 < len(args) {
				i++
				if n, err := strconv.Atoi(args[i]); err == nil {
					opts.MaxPerChannel = n
				}
			}
		}
	}

	fmt.Printf("  backfilling %v (max %d per channel)...\n", opts.Channels, opts.MaxPerChannel)
	report, err := ing.Backfill(ctx, client, opts)
	if err != nil {
		fmt.Printf("  error: %v\n", err)
		return
	}
	for ch, cr := range report.Channels {
		suffix := ""
		if cr.Capped {
			suffix = " (more available)"
		}
		fmt.Printf("  %s: %d messages%s\n", ch, cr.Fetched, suffix)
	}
}

func mboxSources(store *mbox.Store) {
	counts, err := store.SourceCounts()
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if len(counts) == 0 {
		fmt.Println("  (no messages)")
		return
	}
	for _, src := range []string{"push", "backfill", "reconcile"} {
		if n, ok := counts[src]; ok {
			fmt.Printf("  %-12s %d\n", src, n)
		}
	}
}

func printMboxMessage(m mbox.Message) {
	color := channelColors[m.Channel]
	reset := "\033[0m"
	bold := "\033[1m"

	unreadMarker := "  "
	senderFmt := m.Sender
	if m.ReadAt == nil {
		unreadMarker = "* "
		senderFmt = bold + m.Sender + reset
	}

	fmt.Printf("%s%s[%-7s]%s %6s  %s  %s\n",
		unreadMarker, color, m.Channel, reset,
		relativeTime(m.TimestampUTC), senderFmt, truncate(m.Content, 60))
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
```

- [ ] **Step 4: Add mbox tab completion**

In the `makeCompleter` function or wherever tab completions are defined, add `"mbox"` to the list of known REPL commands. Add subcommands `list`, `show`, `search`, `read`, `backfill`, `sources` as completions when the first word is `mbox`.

- [ ] **Step 5: Build and test**

Run: `cd /home/robert/spacemolt/spacemolt && go build ./cmd/tools/play_as/`
Expected: PASS, no compilation errors

- [ ] **Step 6: Run golangci-lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./cmd/tools/play_as/`
Expected: No new findings

- [ ] **Step 7: Commit**

```bash
git add cmd/tools/play_as/main.go
git commit -m "feat(play_as): add mbox REPL commands with backfill and push wiring"
```

---

### Task 8: Agent-side Helpers on BaseAgent

**Files:**
- Modify: `pkg/agent/base.go`
- Modify: `pkg/agent/base_test.go` (create if needed)

- [ ] **Step 1: Write the failing test**

Create or append to `pkg/agent/base_test.go`:

```go
package agent

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/mbox"
)

func TestBaseAgentMboxHelpers(t *testing.T) {
	dir := t.TempDir()
	store, err := mbox.Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open mbox: %v", err)
	}
	defer store.Close()

	agent := NewBaseAgent("test-1", Personality{Name: "Test", Role: "miner"}, nil, nil)
	agent.SetMbox(store)

	now := time.Now().UTC()
	store.Ingest(mbox.Message{ID: "m1", Channel: "local", SenderID: "npc-station", Sender: "StationKeeper",
		Content: "Station Aldrin needs iron_ore", TimestampUTC: now, Source: "push"})
	store.Ingest(mbox.Message{ID: "m2", Channel: "faction", SenderID: "player-42", Sender: "GunnyDraper",
		Content: "Anyone near Kepler?", TimestampUTC: now.Add(time.Second), Source: "push"})
	store.Ingest(mbox.Message{ID: "m3", Channel: "local", SenderID: "npc-station", Sender: "StationKeeper",
		Content: "Station Hoffman needs uranium", TimestampUTC: now.Add(2 * time.Second), Source: "push"})

	// RecentFromSender
	msgs := agent.RecentFromSender("npc-station", 5)
	if len(msgs) != 2 {
		t.Fatalf("RecentFromSender: got %d, want 2", len(msgs))
	}

	// UnreadMatching
	msgs = agent.UnreadMatching([]string{"iron_ore"})
	if len(msgs) != 1 || msgs[0].ID != "m1" {
		t.Fatalf("UnreadMatching iron_ore: got %v", msgs)
	}

	// MarkProcessed
	agent.MarkProcessed("m1", "m2")
	msgs = agent.UnreadMatching([]string{"iron_ore"})
	if len(msgs) != 0 {
		t.Fatal("expected no unread after MarkProcessed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/agent/ -run TestBaseAgentMbox -v`
Expected: FAIL — `SetMbox`, `RecentFromSender`, `UnreadMatching`, `MarkProcessed` not defined.

- [ ] **Step 3: Implement helpers**

In `pkg/agent/base.go`, add the `mbox` field to `BaseAgent` struct:

```go
	mboxStore *mbox.Store
```

Add import: `"github.com/rsned/spacemolt/pkg/mbox"`

Add methods:

```go
// SetMbox sets the mbox store for this agent.
func (a *BaseAgent) SetMbox(store *mbox.Store) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mboxStore = store
}

// RecentFromSender returns the most recent n messages from a sender.
func (a *BaseAgent) RecentFromSender(senderID string, n int) []mbox.Message {
	a.mu.RLock()
	s := a.mboxStore
	a.mu.RUnlock()
	if s == nil {
		return nil
	}
	msgs, _ := s.List(mbox.Query{SenderID: senderID, Limit: n})
	return msgs
}

// UnreadMatching searches unread messages matching any of the given keywords.
func (a *BaseAgent) UnreadMatching(keywords []string) []mbox.Message {
	a.mu.RLock()
	s := a.mboxStore
	a.mu.RUnlock()
	if s == nil {
		return nil
	}
	var results []mbox.Message
	seen := make(map[string]bool)
	for _, kw := range keywords {
		msgs, _ := s.Search(kw, mbox.Query{UnreadOnly: true, Limit: 50})
		for _, m := range msgs {
			if !seen[m.ID] {
				seen[m.ID] = true
				results = append(results, m)
			}
		}
	}
	return results
}

// MarkProcessed marks messages as read after the agent has incorporated them.
func (a *BaseAgent) MarkProcessed(ids ...string) {
	a.mu.RLock()
	s := a.mboxStore
	a.mu.RUnlock()
	if s == nil {
		return
	}
	s.MarkRead(ids...)
}
```

- [ ] **Step 4: Run tests**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/agent/ -run TestBaseAgentMbox -v`
Expected: PASS

- [ ] **Step 5: Run full build and lint**

Run: `cd /home/robert/spacemolt/spacemolt && go build ./... && golangci-lint run ./pkg/agent/ ./pkg/mbox/`
Expected: PASS, no new findings

- [ ] **Step 6: Commit**

```bash
git add pkg/agent/base.go pkg/agent/base_test.go
git commit -m "feat(agent): add mbox helpers on BaseAgent (RecentFromSender, UnreadMatching, MarkProcessed)"
```

---

### Task 9: Final Integration — Build, Test, Push

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./... 2>&1 | tail -40`
Expected: ALL PASS

- [ ] **Step 2: Run golangci-lint on everything**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./...`
Expected: No new findings

- [ ] **Step 3: Push**

```bash
git push
```
