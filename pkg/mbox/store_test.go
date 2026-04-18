package mbox

import (
	"fmt"
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

func TestGetByPrefix(t *testing.T) {
	s := newTestStore(t)

	ts := time.Now().UTC()
	for _, id := range []string{"abc123def", "abc456ghi", "zzz999"} {
		if _, err := s.Ingest(Message{
			ID:           id,
			Channel:      "system",
			SenderID:     "u1",
			Sender:       "A",
			Content:      "c",
			TimestampUTC: ts,
			Source:       "test",
		}); err != nil {
			t.Fatalf("Ingest %s: %v", id, err)
		}
	}

	unique, err := s.GetByPrefix("zzz")
	if err != nil {
		t.Fatalf("GetByPrefix zzz: %v", err)
	}
	if unique == nil || unique.ID != "zzz999" {
		t.Errorf("GetByPrefix zzz: got %+v", unique)
	}

	ambiguous, err := s.GetByPrefix("abc")
	if err == nil {
		t.Errorf("GetByPrefix abc: expected ambiguity error, got msg %+v", ambiguous)
	}

	none, err := s.GetByPrefix("xyz")
	if err != nil {
		t.Fatalf("GetByPrefix xyz: %v", err)
	}
	if none != nil {
		t.Errorf("GetByPrefix xyz: want nil, got %+v", none)
	}

	if _, err := s.GetByPrefix(""); err == nil {
		t.Errorf("GetByPrefix empty: expected error")
	}
}

func TestSoftDeleteAndRestore(t *testing.T) {
	s := newTestStore(t)

	ts := time.Now().UTC()
	for _, id := range []string{"alive", "trashed"} {
		if _, err := s.Ingest(Message{
			ID:           id,
			Channel:      "private",
			SenderID:     "u1",
			Sender:       "A",
			Content:      id,
			TimestampUTC: ts,
			Source:       "test",
		}); err != nil {
			t.Fatalf("Ingest %s: %v", id, err)
		}
	}

	if err := s.SoftDelete("trashed"); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	// Default List hides soft-deleted.
	msgs, err := s.List(Query{Channel: "private"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "alive" {
		ids := make([]string, len(msgs))
		for i, m := range msgs {
			ids[i] = m.ID
		}
		t.Errorf("default List: want [alive], got %v", ids)
	}

	// IncludeDeleted sees both.
	msgs, err = s.List(Query{Channel: "private", IncludeDeleted: true})
	if err != nil {
		t.Fatalf("List IncludeDeleted: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("IncludeDeleted: want 2, got %d", len(msgs))
	}

	// Get still returns deleted with DeletedAt set.
	got, err := s.Get("trashed")
	if err != nil || got == nil {
		t.Fatalf("Get trashed: %v", err)
	}
	if got.DeletedAt == nil {
		t.Errorf("trashed.DeletedAt: want non-nil, got nil")
	}

	// Restore puts it back.
	if err := s.Restore("trashed"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	msgs, err = s.List(Query{Channel: "private"})
	if err != nil {
		t.Fatalf("List after restore: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("after restore: want 2, got %d", len(msgs))
	}
	got, _ = s.Get("trashed")
	if got == nil || got.DeletedAt != nil {
		t.Errorf("after restore: DeletedAt should be nil, got %+v", got.DeletedAt)
	}
}

func TestMarkReadAndUnreadCounts(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	for i, ch := range []string{"system", "system", "faction"} {
		if _, err := s.Ingest(Message{
			ID: fmt.Sprintf("u%d", i), Channel: ch, SenderID: "p1", Sender: "A",
			Content: "x", TimestampUTC: now.Add(time.Duration(i) * time.Second), Source: "push",
		}); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
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

	if _, err := s.Ingest(Message{ID: "s1", Channel: "local", SenderID: "p1", Sender: "StationKeeper",
		Content: "Station Aldrin needs iron_ore and copper_ore", TimestampUTC: now, Source: "push"}); err != nil {
		t.Fatalf("Ingest s1: %v", err)
	}
	if _, err := s.Ingest(Message{ID: "s2", Channel: "local", SenderID: "p2", Sender: "Trader",
		Content: "Selling fuel cells", TimestampUTC: now.Add(time.Second), Source: "push"}); err != nil {
		t.Fatalf("Ingest s2: %v", err)
	}
	if _, err := s.Ingest(Message{ID: "s3", Channel: "faction", SenderID: "p1", Sender: "StationKeeper",
		Content: "Station Hoffman needs uranium", TimestampUTC: now.Add(2 * time.Second), Source: "push"}); err != nil {
		t.Fatalf("Ingest s3: %v", err)
	}

	// Search content
	msgs, err := s.Search("iron_ore", Query{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "s1" {
		t.Fatalf("search iron_ore: got %d results", len(msgs))
	}

	// Search sender
	msgs, err = s.Search("StationKeeper", Query{Limit: 10})
	if err != nil {
		t.Fatalf("Search StationKeeper: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("search StationKeeper: got %d results, want 2", len(msgs))
	}

	// Search with channel filter
	msgs, err = s.Search("needs", Query{Channel: "local", Limit: 10})
	if err != nil {
		t.Fatalf("Search needs+local: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "s1" {
		t.Fatalf("search needs+local: got %d results, want 1", len(msgs))
	}
}

func TestSourceCounts(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	for i, src := range []string{"push", "backfill", "push"} {
		if _, err := s.Ingest(Message{
			ID: fmt.Sprintf("sc%d", i+1), Channel: "system", SenderID: "p1", Sender: "A",
			Content: "x", TimestampUTC: now.Add(time.Duration(i) * time.Second), Source: src,
		}); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
	}

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
