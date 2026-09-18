package knowledge

import (
	"testing"
)

// runMigrations decides what to apply from MAX(version), so replaying any
// migration means replaying every one above it -- which is exactly what
// TestMigration60_ReconcilesLiveShapes does when it clears version >= 60.
// A migration that adds columns therefore has to be idempotent, or it breaks
// every future test that replays an earlier one.
func TestMigration61_IsReplayable(t *testing.T) {
	kb := newTestSQLiteKB(t)

	for i := range 3 {
		if _, err := kb.db.Exec("DELETE FROM schema_migrations WHERE version >= 61"); err != nil {
			t.Fatalf("clear ledger: %v", err)
		}
		if err := runMigrations(kb.db); err != nil {
			t.Fatalf("replay %d: %v", i+1, err)
		}
	}

	for _, col := range []string{
		"exclusive_base_id", "exclusive_system_id",
		"exclusive_source", "exclusive_seen_tick", "exclusive_seen_at",
	} {
		var n int
		if err := kb.db.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('mission_templates') WHERE name=?`, col,
		).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("column %s: want exactly 1 after replays, got %d", col, n)
		}
	}
}
