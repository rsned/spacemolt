package worker

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/customs"
)

func customsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() }) //nolint:errcheck
	if _, err := db.Exec(`CREATE TABLE action_log_events (player_id TEXT, event_id INTEGER, event_type TEXT,
		category TEXT DEFAULT '', created_at TEXT, data_json TEXT)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRefreshCustomsMapWritesFile(t *testing.T) {
	db := customsTestDB(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	at := now.Add(-time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO action_log_events VALUES
		('p', 1, 'combat.customs_inspection', '', ?, '{"system_id":"grumium","empire_id":"solarian","outcome":"clear"}')`, at); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	overrides := filepath.Join(dir, "overrides.json")
	if err := os.WriteFile(overrides, []byte(`{"clear":{"windmere":"v0.734.1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mapPath := filepath.Join(dir, "map.json")
	if _, err := RefreshCustomsMap(context.Background(), db, nil, mapPath, overrides, now); err != nil {
		t.Fatal(err)
	}
	m, err := customs.Load(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	if m.Systems["grumium"].Class != customs.Monitored || m.Systems["windmere"].Class != customs.Clear || m.WindowDays != 14 {
		t.Errorf("map = %+v, want grumium monitored, windmere clear, 14-day window", m)
	}
}
