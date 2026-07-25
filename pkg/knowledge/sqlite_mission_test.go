package knowledge

import (
	"database/sql"
	"testing"
)

func TestMissionTemplatesHasProceduralColumn(t *testing.T) {
	kb := newTestSQLiteKB(t)
	var n int
	if err := kb.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('mission_templates') WHERE name='procedural'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("mission_templates.procedural column missing (got %d)", n)
	}
}

func TestEnsureMissionTemplatesProceduralCol_AddsToLegacy(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Legacy table shape: no `procedural` column.
	if _, err := db.Exec(`CREATE TABLE mission_templates (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}

	if err := ensureMissionTemplatesProceduralCol(db); err != nil {
		t.Fatalf("ensure (add): %v", err)
	}
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('mission_templates') WHERE name='procedural'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("procedural column not added (got %d)", n)
	}
	// Idempotent: second run is a no-op, not an error.
	if err := ensureMissionTemplatesProceduralCol(db); err != nil {
		t.Fatalf("ensure (idempotent): %v", err)
	}
}
