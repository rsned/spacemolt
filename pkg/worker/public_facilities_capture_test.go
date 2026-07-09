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
