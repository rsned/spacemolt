package knowledge

import (
	"context"
	"testing"
)

func TestUpsertAndQueryPublicFacilities(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	rows := []PublicFacility{
		{StationID: "grand_exchange", FacilityID: "f1", RecipeID: "ceramite_plating",
			Category: "production", Level: 2, RentalFeePerRun: 40, OwnerFaction: "CRFT", LastSeenTick: 100},
		{StationID: "war_citadel", FacilityID: "f2", RecipeID: "ceramite_plating",
			Category: "production", Level: 1, RentalFeePerRun: 60, OwnerFaction: "WAR", LastSeenTick: 100},
		{StationID: "war_citadel", FacilityID: "f9", RecipeID: "reactor_core",
			Category: "production", Level: 3, RentalFeePerRun: 200, OwnerFaction: "WAR", LastSeenTick: 100},
	}
	if err := kb.UpsertPublicFacilities(ctx, rows); err != nil {
		t.Fatal(err)
	}

	got, err := kb.FacilitiesForRecipe(ctx, "ceramite_plating")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 facilities for ceramite_plating, got %d", len(got))
	}

	// Upsert refresh: same PK, new level/fee overwrites.
	rows[0].Level = 4
	rows[0].RentalFeePerRun = 35
	if err := kb.UpsertPublicFacilities(ctx, rows[:1]); err != nil {
		t.Fatal(err)
	}
	got, _ = kb.FacilitiesForRecipe(ctx, "ceramite_plating")
	var f1 *PublicFacility
	for i := range got {
		if got[i].FacilityID == "f1" {
			f1 = &got[i]
		}
	}
	if f1 == nil || f1.Level != 4 || f1.RentalFeePerRun != 35 {
		t.Fatalf("upsert did not refresh f1: %+v", f1)
	}
}

func TestFacilitiesForRecipeUnknownReturnsEmpty(t *testing.T) {
	kb := newTestKB(t)
	got, err := kb.FacilitiesForRecipe(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %d", len(got))
	}
}
