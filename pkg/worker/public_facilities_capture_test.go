package worker

import (
	"context"
	"os"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestUpsertPublicFromFacilityList(t *testing.T) {
	raw, err := os.ReadFile("testdata/facility_list_public.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create in-memory KB: %v", err)
	}

	ctx := context.Background()

	n, err := upsertPublicFromFacilityList(ctx, kb, raw, 100)
	if err != nil {
		t.Fatalf("upsertPublicFromFacilityList returned error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 upserted row (private facility filtered out), got %d", n)
	}

	ceramite, err := kb.FacilitiesForRecipe(ctx, "ceramite_plating")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe(ceramite_plating) error: %v", err)
	}
	if len(ceramite) != 1 {
		t.Fatalf("expected 1 facility for ceramite_plating, got %d", len(ceramite))
	}
	f := ceramite[0]
	if f.StationID != "grand_exchange_station" {
		t.Errorf("StationID = %q, want grand_exchange_station", f.StationID)
	}
	if f.Level != 2 {
		t.Errorf("Level = %d, want 2", f.Level)
	}
	if f.RentalFeePerRun != 50 {
		t.Errorf("RentalFeePerRun = %d, want 50", f.RentalFeePerRun)
	}
	if f.OwnerFaction != "CRFT" {
		t.Errorf("OwnerFaction = %q, want CRFT", f.OwnerFaction)
	}

	reactor, err := kb.FacilitiesForRecipe(ctx, "reactor_core")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe(reactor_core) error: %v", err)
	}
	if len(reactor) != 0 {
		t.Fatalf("expected 0 facilities for reactor_core (private, not captured), got %d", len(reactor))
	}
}

// Live payloads (e.g. voss_redoubt) often carry NO public_facilities section at
// all: station-owned public production lines arrive under station_facilities and
// our own faction's rent-out lines under faction_facilities. Both must reach the
// catalog. Those sections also mix public and private lines, and a private line
// (The Red Room) omits production.public entirely rather than setting it false —
// so an explicit true is required, not merely a non-false.
func TestUpsertPublicFromFacilityList_AllSections(t *testing.T) {
	raw, err := os.ReadFile("testdata/facility_list_sections.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create in-memory KB: %v", err)
	}

	ctx := context.Background()

	n, err := upsertPublicFromFacilityList(ctx, kb, raw, 100)
	if err != nil {
		t.Fatalf("upsertPublicFromFacilityList returned error: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 upserted rows (station + faction public production), got %d", n)
	}

	steel, err := kb.FacilitiesForRecipe(ctx, "refine_steel")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe(refine_steel) error: %v", err)
	}
	if len(steel) != 1 {
		t.Fatalf("expected 1 facility for refine_steel (station_facilities), got %d", len(steel))
	}
	if got := steel[0].StationID; got != "voss_redoubt_station" {
		t.Errorf("StationID = %q, want voss_redoubt_station", got)
	}
	if got := steel[0].Level; got != 2 {
		t.Errorf("Level = %d, want 2", got)
	}
	if got := steel[0].RentalFeePerRun; got != 35 {
		t.Errorf("RentalFeePerRun = %d, want 35", got)
	}

	cells, err := kb.FacilitiesForRecipe(ctx, "crack_hot_cells")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe(crack_hot_cells) error: %v", err)
	}
	if len(cells) != 1 {
		t.Fatalf("expected 1 facility for crack_hot_cells (faction_facilities), got %d", len(cells))
	}
	if got := cells[0].OwnerFaction; got != "VOSS" {
		t.Errorf("OwnerFaction = %q, want VOSS", got)
	}

	// The Red Room: category=production but production.public is ABSENT. It is
	// private and must never be offered as a craft-for-hire site.
	redMist, err := kb.FacilitiesForRecipe(ctx, "load_red_mist")
	if err != nil {
		t.Fatalf("FacilitiesForRecipe(load_red_mist) error: %v", err)
	}
	if len(redMist) != 0 {
		t.Fatalf("expected 0 facilities for load_red_mist (public key absent = private), got %d", len(redMist))
	}
}
