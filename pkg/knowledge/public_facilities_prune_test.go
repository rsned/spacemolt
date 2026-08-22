package knowledge

import (
	"context"
	"testing"
)

// seedFacilities puts a known catalog in place for the prune tests: two public
// production lines at alpha, one at beta.
func seedFacilities(t *testing.T, kb *SQLiteKB) {
	t.Helper()
	ctx := context.Background()
	if err := kb.UpsertPublicFacilities(ctx, []PublicFacility{
		{StationID: "alpha", FacilityID: "a1", RecipeID: "refine_steel", Category: "production", LastSeenTick: 10},
		{StationID: "alpha", FacilityID: "a2", RecipeID: "ceramite_plating", Category: "production", LastSeenTick: 10},
		{StationID: "beta", FacilityID: "b1", RecipeID: "refine_steel", Category: "production", LastSeenTick: 10},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func stationFacilityIDs(t *testing.T, kb *SQLiteKB, station string) []string {
	t.Helper()
	rows, err := kb.db.Query(`SELECT facility_id FROM public_facilities WHERE station_id = ? ORDER BY facility_id`, station)
	if err != nil {
		t.Fatalf("query %s: %v", station, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	return out
}

// A facility that was dismantled (or made private) between two visits must not
// keep answering "you can build this here". Before this method existed the row
// was upserted on sight and never deleted, so the catalog over-reported
// coverage forever.
func TestReplacePublicFacilitiesAtStation_PrunesVanishedRows(t *testing.T) {
	kb, err := NewSQLiteKB(Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	seedFacilities(t, kb)

	ctx := context.Background()
	pruned, err := kb.ReplacePublicFacilitiesAtStation(ctx, "alpha", []PublicFacility{
		{StationID: "alpha", FacilityID: "a1", RecipeID: "refine_steel", Category: "production", LastSeenTick: 20},
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if got := stationFacilityIDs(t, kb, "alpha"); len(got) != 1 || got[0] != "a1" {
		t.Errorf("alpha facilities = %v, want [a1]", got)
	}
	// The survivor must be refreshed, not merely left alone.
	facs, err := kb.FacilitiesForRecipe(ctx, "refine_steel")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe: %v", err)
	}
	var alphaTick int
	for _, f := range facs {
		if f.StationID == "alpha" {
			alphaTick = f.LastSeenTick
		}
	}
	if alphaTick != 20 {
		t.Errorf("alpha/a1 last_seen_tick = %d, want 20 (refreshed by the replace)", alphaTick)
	}
	// Another station's catalog is none of this scrape's business.
	if got := stationFacilityIDs(t, kb, "beta"); len(got) != 1 || got[0] != "b1" {
		t.Errorf("beta facilities = %v, want [b1] untouched", got)
	}
}

// The case that matters most: a station whose LAST public line is gone. An
// empty row set is a real observation, not a no-op.
func TestReplacePublicFacilitiesAtStation_EmptyRowsClearsStation(t *testing.T) {
	kb, err := NewSQLiteKB(Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	seedFacilities(t, kb)

	pruned, err := kb.ReplacePublicFacilitiesAtStation(context.Background(), "alpha", nil)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if pruned != 2 {
		t.Errorf("pruned = %d, want 2", pruned)
	}
	if got := stationFacilityIDs(t, kb, "alpha"); len(got) != 0 {
		t.Errorf("alpha facilities = %v, want none", got)
	}
	if got := stationFacilityIDs(t, kb, "beta"); len(got) != 1 {
		t.Errorf("beta facilities = %v, want [b1] untouched", got)
	}
}

// A row for a different station in the same call would make the delete scope
// and the insert scope disagree — the caller has mixed two scrapes together.
// Refuse the whole call rather than half-apply it.
func TestReplacePublicFacilitiesAtStation_RejectsForeignRow(t *testing.T) {
	kb, err := NewSQLiteKB(Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	seedFacilities(t, kb)

	_, err = kb.ReplacePublicFacilitiesAtStation(context.Background(), "alpha", []PublicFacility{
		{StationID: "beta", FacilityID: "b9", Category: "production"},
	})
	if err == nil {
		t.Fatal("expected an error for a row belonging to another station")
	}
	if got := stationFacilityIDs(t, kb, "alpha"); len(got) != 2 {
		t.Errorf("alpha facilities = %v, want both still present (call rejected)", got)
	}
	if got := stationFacilityIDs(t, kb, "beta"); len(got) != 1 || got[0] != "b1" {
		t.Errorf("beta facilities = %v, want [b1] (foreign row not written)", got)
	}
}

// An empty station id would scope the delete to nothing and silently do
// nothing useful; it means the caller could not identify the station.
func TestReplacePublicFacilitiesAtStation_RejectsEmptyStation(t *testing.T) {
	kb, err := NewSQLiteKB(Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	seedFacilities(t, kb)

	if _, err := kb.ReplacePublicFacilitiesAtStation(context.Background(), "", nil); err == nil {
		t.Fatal("expected an error for an empty station id")
	}
	if got := stationFacilityIDs(t, kb, "alpha"); len(got) != 2 {
		t.Errorf("alpha facilities = %v, want untouched", got)
	}
}
