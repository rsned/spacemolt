package knowledge

import (
	"context"
	"testing"
)

// v0.572.0 gave every hull a crew complement and a capture policy. Those are
// what decide whether a hull can even be flown understaffed and whether a
// boarding party can take it intact, so the catalog must carry them.
func TestShipClassPersonnelColumnsRoundTrip(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	in := ShipClassDef{
		ID: "survey_vessel", Name: "Survey Vessel", BaseHull: 340, BaseSpeed: 3, BaseFuel: 1020,
		CrewCapacity: 60, MarineCapacity: 6, MinimumCrew: 12,
		CapturePolicy: "standard", CapturePolicyReason: "",
		LatchResistance: 3, BoardingDefenseBonusPct: 15,
	}
	if err := kb.StoreShipClasses(ctx, []ShipClassDef{in}); err != nil {
		t.Fatalf("StoreShipClasses: %v", err)
	}
	got, err := kb.GetShipClass(ctx, "survey_vessel")
	if err != nil {
		t.Fatalf("GetShipClass: %v", err)
	}
	if got.CrewCapacity != 60 || got.MarineCapacity != 6 || got.MinimumCrew != 12 ||
		got.CapturePolicy != "standard" || got.LatchResistance != 3 || got.BoardingDefenseBonusPct != 15 {
		t.Errorf("personnel columns = crew %d marines %d min %d policy %q latch %d defense %d",
			got.CrewCapacity, got.MarineCapacity, got.MinimumCrew, got.CapturePolicy,
			got.LatchResistance, got.BoardingDefenseBonusPct)
	}
}

// A DB that predates the 2026-04-15 collapse gets its ships table from
// ensureCollapseMissingTables, so the personnel columns must be reconciled
// after that step, never inside a numbered migration.
func TestEnsureShipClassPersonnelCols_AddsMissingColumns(t *testing.T) {
	kb := newTestKB(t)
	for _, col := range []string{"crew_capacity", "marine_capacity", "minimum_crew",
		"capture_policy", "capture_policy_reason", "latch_resistance", "boarding_defense_bonus_pct"} {
		var n int
		if err := kb.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('ships') WHERE name=?`, col).Scan(&n); err != nil {
			t.Fatalf("pragma: %v", err)
		}
		if n != 1 {
			t.Errorf("ships.%s missing", col)
		}
	}
	// Idempotent: a second pass over a table that already has them is a no-op.
	if err := ensureShipClassPersonnelCols(kb.db); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}
