package knowledge

import "testing"

func TestPublicFacilitiesTableExists(t *testing.T) {
	kb := newTestKB(t)
	var name string
	if err := kb.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='public_facilities'`,
	).Scan(&name); err != nil || name != "public_facilities" {
		t.Fatalf("public_facilities table missing: %v", err)
	}
	var idx string
	if err := kb.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_public_facilities_recipe'`,
	).Scan(&idx); err != nil || idx != "idx_public_facilities_recipe" {
		t.Fatalf("idx_public_facilities_recipe missing: %v", err)
	}
}

// TestEnsurePublicFacilitiesRentalCol_RenamesOldColumn simulates a DB that
// applied the original migration 48 (provisional labor_cost column) and
// verifies the self-healing fixup renames it to rental_fee_per_run and is
// idempotent — the exact live-DB drift that broke capture after the in-place
// migration edit.
func TestEnsurePublicFacilitiesRentalCol_RenamesOldColumn(t *testing.T) {
	kb := newTestKB(t)
	db := kb.db
	// Recreate the table in its OLD shape (labor_cost).
	if _, err := db.Exec(`DROP TABLE public_facilities`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE public_facilities (
		station_id TEXT NOT NULL, facility_id TEXT NOT NULL,
		labor_cost INTEGER DEFAULT 0,
		PRIMARY KEY (station_id, facility_id))`); err != nil {
		t.Fatalf("create old shape: %v", err)
	}

	if err := ensurePublicFacilitiesRentalCol(db); err != nil {
		t.Fatalf("fixup: %v", err)
	}

	var newCol, oldCol int
	_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('public_facilities') WHERE name='rental_fee_per_run'`).Scan(&newCol)
	_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('public_facilities') WHERE name='labor_cost'`).Scan(&oldCol)
	if newCol != 1 || oldCol != 0 {
		t.Fatalf("rename failed: rental_fee_per_run=%d labor_cost=%d", newCol, oldCol)
	}

	// Idempotent: a second run on the already-correct table must be a no-op.
	if err := ensurePublicFacilitiesRentalCol(db); err != nil {
		t.Fatalf("second run not idempotent: %v", err)
	}
}
