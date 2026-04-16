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
