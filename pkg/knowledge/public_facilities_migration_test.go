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
