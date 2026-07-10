package knowledge

import (
	"context"
	"testing"
	"time"
)

// last_seen_utc existed in migration 48 but nothing ever wrote it, so every row
// carried ''. It is the only wall-clock freshness signal on the table:
// last_seen_tick is a GameClock value, and the GameClock only ever drifts
// forward, so tick deltas understate real age.
func TestUpsertPublicFacilities_PopulatesLastSeenUTC(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	// Caller omits LastSeenUTC → upsert stamps it, so existing callers heal
	// themselves on the next sweep without a backfill migration.
	before := time.Now().UTC().Add(-time.Second)
	if err := kb.UpsertPublicFacilities(ctx, []PublicFacility{
		{StationID: "voss_redoubt_station", FacilityID: "sf-steel-1", RecipeID: "refine_steel",
			Category: "production", Level: 2, RentalFeePerRun: 35, LastSeenTick: 100},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := kb.FacilitiesForRecipe(ctx, "refine_steel")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 facility, got %d", len(got))
	}
	if got[0].LastSeenUTC == "" {
		t.Fatal("LastSeenUTC is empty; upsert must stamp it when the caller omits it")
	}
	ts, err := time.Parse(time.RFC3339, got[0].LastSeenUTC)
	if err != nil {
		t.Fatalf("LastSeenUTC %q is not RFC3339: %v", got[0].LastSeenUTC, err)
	}
	if ts.Before(before) || ts.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("LastSeenUTC %v not within the upsert window", ts)
	}

	// An explicit value round-trips untouched, and a re-sweep refreshes it.
	explicit := "2026-07-09T17:18:39Z"
	if err := kb.UpsertPublicFacilities(ctx, []PublicFacility{
		{StationID: "voss_redoubt_station", FacilityID: "sf-steel-1", RecipeID: "refine_steel",
			Category: "production", Level: 2, RentalFeePerRun: 35, LastSeenTick: 200, LastSeenUTC: explicit},
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = kb.FacilitiesForRecipe(ctx, "refine_steel")
	if got[0].LastSeenUTC != explicit {
		t.Errorf("LastSeenUTC = %q, want %q (explicit value must round-trip and refresh)", got[0].LastSeenUTC, explicit)
	}
}

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
