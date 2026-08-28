package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// get_location and get_poi each see fields the other cannot. Rather than
// special-casing which POIs are worth a second query -- the kind of
// discriminator-trusting that dropped twelve harvesters from item_mining --
// both are captured everywhere and merged.
func TestMergePOIDetail_FillsWhatGetLocationCannotSee(t *testing.T) {
	// What get_location yields: identity, type, live resources, no capacity.
	loc := game.POI{
		ID: "frostmarket_flats", SystemID: "haven", Name: "Frostmarket Flats", Type: "ice_field",
		Resources: []game.POIResource{
			{ResourceID: "water_ice", Richness: 75, Remaining: 30141},
			{ResourceID: "carbon_dioxide_ice", Richness: 18, Remaining: 8046},
		},
	}
	// What get_poi adds.
	detail := game.POI{
		ID: "frostmarket_flats", Class: "kuiper", Description: "Icy bodies harvested for water.",
		Position: game.Position{X: 5.6, Y: 0.2},
		Hidden:   true, RevealDifficulty: 40, ExpiresAt: "2026-08-29T00:00:00Z",
		Resources: []game.POIResource{
			{ResourceID: "water_ice", Remaining: 30141, MaxRemaining: 50000},
			{ResourceID: "carbon_dioxide_ice", Remaining: 8046, MaxRemaining: 9000},
		},
	}

	mergePOIDetail(&loc, detail)

	if loc.Description == "" {
		t.Error("Description not merged; only get_poi and survey carry it, so ours go stale without this")
	}
	if loc.Class != "kuiper" {
		t.Errorf("Class = %q, want kuiper", loc.Class)
	}
	if !loc.Hidden || loc.RevealDifficulty != 40 {
		t.Errorf("Hidden=%v RevealDifficulty=%d, want true/40", loc.Hidden, loc.RevealDifficulty)
	}
	if loc.ExpiresAt != "2026-08-29T00:00:00Z" {
		t.Errorf("ExpiresAt = %q; with 9 live wormholes held and 0 expiries, this is the field that matters", loc.ExpiresAt)
	}
	if loc.Position.X != 5.6 {
		t.Errorf("Position = %+v", loc.Position)
	}

	caps := map[string]float64{}
	for _, r := range loc.Resources {
		caps[r.ResourceID] = r.MaxRemaining
	}
	if caps["water_ice"] != 50000 || caps["carbon_dioxide_ice"] != 9000 {
		t.Errorf("MaxRemaining not merged per resource: %+v", caps)
	}
	// The live numbers stay whatever get_location reported.
	if loc.Resources[0].Richness != 75 || loc.Resources[0].Remaining != 30141 {
		t.Errorf("live values disturbed: %+v", loc.Resources[0])
	}
}

// An empty or failed detail read must leave the base capture untouched, so a
// get_poi failure degrades to today's behaviour rather than blanking a row.
func TestMergePOIDetail_EmptyDetailChangesNothing(t *testing.T) {
	loc := game.POI{
		ID: "haven_star", Name: "Haven Star", Type: "sun", Class: "G5V",
		Description: "Stable golden star.",
		Resources:   []game.POIResource{{ResourceID: "x", Richness: 1, Remaining: 2, MaxRemaining: 3}},
	}
	before := loc
	mergePOIDetail(&loc, game.POI{})

	if loc.Class != before.Class || loc.Description != before.Description {
		t.Errorf("empty detail overwrote fields: %+v", loc)
	}
	if loc.Resources[0].MaxRemaining != 3 {
		t.Errorf("empty detail zeroed a known capacity: %+v", loc.Resources[0])
	}
}

// A resource the detail does not mention keeps what it had.
func TestMergePOIDetail_UnmentionedResourcesKeepTheirCapacity(t *testing.T) {
	loc := game.POI{
		ID: "belt",
		Resources: []game.POIResource{
			{ResourceID: "iron_ore", Remaining: 10, MaxRemaining: 900},
			{ResourceID: "silicon_ore", Remaining: 20},
		},
	}
	mergePOIDetail(&loc, game.POI{
		ID:        "belt",
		Resources: []game.POIResource{{ResourceID: "silicon_ore", MaxRemaining: 500}},
	})
	if loc.Resources[0].MaxRemaining != 900 {
		t.Errorf("iron_ore capacity lost: %+v", loc.Resources[0])
	}
	if loc.Resources[1].MaxRemaining != 500 {
		t.Errorf("silicon_ore capacity not merged: %+v", loc.Resources[1])
	}
}
