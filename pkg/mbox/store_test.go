package mbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCreatesDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "mbox.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q): %v", dbPath, err)
	}
	defer func() { _ = s.Close() }()

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
	defer func() { _ = s.Close() }()

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

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "mbox.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestIngestAndDedupe(t *testing.T) {
	s := newTestStore(t)

	msg := Message{
		ID:           "msg-1",
		Channel:      "general",
		SenderID:     "user-1",
		Sender:       "Alice",
		Content:      "Hello, world!",
		TargetID:     "",
		TargetName:   "",
		TimestampUTC: time.Now().UTC().Add(-time.Minute),
		Source:       "chat",
	}

	inserted, err := s.Ingest(msg)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !inserted {
		t.Fatal("first Ingest: want inserted=true, got false")
	}

	inserted, err = s.Ingest(msg)
	if err != nil {
		t.Fatalf("Ingest duplicate: %v", err)
	}
	if inserted {
		t.Fatal("duplicate Ingest: want inserted=false, got true")
	}
}

func TestListByChannel(t *testing.T) {
	s := newTestStore(t)

	base := time.Now().UTC().Truncate(time.Second)
	msgs := []Message{
		{ID: "a1", Channel: "alpha", SenderID: "u1", Sender: "Alice", Content: "first", TimestampUTC: base.Add(-2 * time.Minute), Source: "chat"},
		{ID: "a2", Channel: "alpha", SenderID: "u2", Sender: "Bob", Content: "second", TimestampUTC: base.Add(-time.Minute), Source: "chat"},
		{ID: "b1", Channel: "beta", SenderID: "u1", Sender: "Alice", Content: "other", TimestampUTC: base, Source: "chat"},
	}
	for _, m := range msgs {
		if _, err := s.Ingest(m); err != nil {
			t.Fatalf("Ingest %s: %v", m.ID, err)
		}
	}

	results, err := s.List(Query{Channel: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("List alpha: want 2, got %d", len(results))
	}
	// newest-first: a2 before a1
	if results[0].ID != "a2" {
		t.Errorf("List alpha [0]: want a2, got %s", results[0].ID)
	}
	if results[1].ID != "a1" {
		t.Errorf("List alpha [1]: want a1, got %s", results[1].ID)
	}
}

func TestListUnreadOnly(t *testing.T) {
	s := newTestStore(t)

	base := time.Now().UTC().Truncate(time.Second)
	msgs := []Message{
		{ID: "r1", Channel: "ch", SenderID: "u1", Sender: "Alice", Content: "read me", TimestampUTC: base.Add(-2 * time.Minute), Source: "chat"},
		{ID: "r2", Channel: "ch", SenderID: "u1", Sender: "Alice", Content: "unread", TimestampUTC: base.Add(-time.Minute), Source: "chat"},
	}
	for _, m := range msgs {
		if _, err := s.Ingest(m); err != nil {
			t.Fatalf("Ingest %s: %v", m.ID, err)
		}
	}

	// Mark r1 as read directly via DB (MarkRead doesn't exist yet)
	_, err := s.db.Exec("UPDATE messages SET read_at = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339Nano), "r1")
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}

	results, err := s.List(Query{Channel: "ch", UnreadOnly: true, Limit: 10})
	if err != nil {
		t.Fatalf("List UnreadOnly: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("List UnreadOnly: want 1, got %d", len(results))
	}
	if results[0].ID != "r2" {
		t.Errorf("List UnreadOnly [0]: want r2, got %s", results[0].ID)
	}
}

func TestGet(t *testing.T) {
	s := newTestStore(t)

	ts := time.Now().UTC().Truncate(time.Second)
	msg := Message{
		ID:           "get-1",
		Channel:      "general",
		SenderID:     "u1",
		Sender:       "Alice",
		Content:      "hello",
		TargetID:     "u2",
		TargetName:   "Bob",
		TimestampUTC: ts,
		Source:       "chat",
	}
	if _, err := s.Ingest(msg); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	got, err := s.Get("get-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get: want non-nil, got nil")
	}
	if got.ID != msg.ID {
		t.Errorf("ID: want %s, got %s", msg.ID, got.ID)
	}
	if got.Channel != msg.Channel {
		t.Errorf("Channel: want %s, got %s", msg.Channel, got.Channel)
	}
	if got.Content != msg.Content {
		t.Errorf("Content: want %s, got %s", msg.Content, got.Content)
	}
	if got.TargetID != msg.TargetID {
		t.Errorf("TargetID: want %s, got %s", msg.TargetID, got.TargetID)
	}

	missing, err := s.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get nonexistent: %v", err)
	}
	if missing != nil {
		t.Errorf("Get nonexistent: want nil, got %+v", missing)
	}
}
