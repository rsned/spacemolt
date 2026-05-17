package knowledge

import (
	"testing"
)

// newTestKB returns an in-memory SQLiteKB for use in seen_players tests.
func newTestKB(t *testing.T) *SQLiteKB {
	t.Helper()
	kb, err := NewSQLiteKB(Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	return kb
}

func TestSeenPlayersMigrationCreatesTables(t *testing.T) {
	kb := newTestKB(t)

	tables := []string{"seen_players", "seen_player_ships", "seen_player_sightings"}
	for _, tbl := range tables {
		var count int
		err := kb.db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tbl,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query for %s: %v", tbl, err)
		}
		if count != 1 {
			t.Errorf("table %s not created (count=%d)", tbl, count)
		}
	}
}
