package knowledge

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestWriteMigratedDB_IsSchemaCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clone.db")
	if err := WriteMigratedDB(path); err != nil {
		t.Fatalf("WriteMigratedDB: %v", err)
	}
	kb, err := NewSQLiteKB(Config{DBPath: path, WAL: false, MaxOpenConns: 1, MaxIdleConns: 1})
	if err != nil {
		t.Fatalf("open clone: %v", err)
	}
	defer func() { _ = kb.Close() }()

	var got int
	if err := kb.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&got); err != nil {
		t.Fatalf("read version: %v", err)
	}
	ms := migrations()
	want := ms[len(ms)-1].version
	if got != want {
		t.Fatalf("clone schema version = %d, want %d (latest migration)", got, want)
	}
	// The clone must be empty, not a snapshot of somebody's data.
	var systems int
	if err := kb.db.QueryRow("SELECT COUNT(*) FROM systems").Scan(&systems); err != nil {
		t.Fatalf("count systems: %v", err)
	}
	if systems != 0 {
		t.Fatalf("clone has %d systems, want 0", systems)
	}
}

func TestMigratedTemplate_IsCachedAcrossCalls(t *testing.T) {
	a, err := MigratedTemplate()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	b, err := MigratedTemplate()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(a) == 0 || !bytes.Equal(a, b) {
		t.Fatalf("template not cached: len %d vs %d", len(a), len(b))
	}
}
