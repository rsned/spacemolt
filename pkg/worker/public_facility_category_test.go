package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

type fakePublicFacilityStore struct {
	rows    []knowledge.PublicFacility
	station string
}

func (f *fakePublicFacilityStore) ReplacePublicFacilitiesAtStation(_ context.Context,
	stationID string, rows []knowledge.PublicFacility,
) (int, error) {
	f.station = stationID
	f.rows = rows

	return 0, nil
}

// OUR OWN faction's production facilities carry no "category" field. They
// identify as production by HAVING a production object -- see the live shape
// in facility_format_test.go's TestFormatFacilityFactionList_ProductionSplit:
//
//	{"active":true,"custom_name":"Bob's Iron Smeltery","recipe_id":"refine_steel",
//	 "type":"iron_refinery","production":{"public":true,"output_per_run":2,...}}
//
// The capture gated on `category == "production"` before ever reading the
// public flag, so every CRFT line was skipped. Observed live 2026-10-17: five
// PUBLIC faction production facilities at grand_exchange_station -- Polymer
// Extruder, Fiber Draw Tower, Carbon Arc Furnace, Bob's Iron Smeltery, H2 Fuel
// Combustor -- and public_facilities held ZERO rows for them, while all 2,516
// captured rows (other factions' and NPC lines, which do carry the field) read
// category=production.
//
// That is why "which contraband can we make" and "do we own a Polymer
// Extruder" were both unanswerable from our own data.
func TestUpsertPublicFromFacilityList_AcceptsEntriesWithNoCategory(t *testing.T) {
	store := &fakePublicFacilityStore{}
	raw := []byte(`{"base_id":"grand_exchange_station","faction_id":"e727",
	  "faction_facilities":[
	    {"active":true,"custom_name":"Polymer Extruder","facility_id":"b659c360",
	     "level":1,"name":"Polymer Extruder","recipe_id":"extrude_nanoplastic",
	     "rent_per_cycle":294,"type":"polymer_extruder",
	     "production":{"items_per_hour":3714,"output_per_run":3,"public":true,
	                   "recipe":"Extrude Nanoplastic","ticks_per_run":0.29}},
	    {"active":true,"custom_name":"Fiber Draw Tower","facility_id":"d0b5b0d4",
	     "level":1,"name":"Fiber Draw Tower","recipe_id":"spin_optical_fiber",
	     "rent_per_cycle":294,"type":"fiber_draw_tower",
	     "production":{"items_per_hour":210,"output_per_run":1,"public":true,
	                   "recipe":"Spin Optical Fiber","ticks_per_run":1.71}}
	  ]}`)

	n, _, err := upsertPublicFromFacilityList(context.Background(), store, raw, 2165000)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("captured %d, want 2 (neither entry carries a category field)", n)
	}
	got := map[string]string{}
	for _, r := range store.rows {
		got[r.FacilityName] = r.RecipeID
	}
	if got["Polymer Extruder"] != "extrude_nanoplastic" {
		t.Errorf("Polymer Extruder recipe = %q", got["Polymer Extruder"])
	}
	if got["Fiber Draw Tower"] != "spin_optical_fiber" {
		t.Errorf("Fiber Draw Tower recipe = %q", got["Fiber Draw Tower"])
	}
}

// A PRIVATE line still must not be captured: public_facilities is the public
// catalog, and production.public is absent-or-false for a private line -- the
// Red Room at voss_redoubt omits it rather than setting false.
func TestUpsertPublicFromFacilityList_StillSkipsPrivateAndNonProduction(t *testing.T) {
	store := &fakePublicFacilityStore{}
	raw := []byte(`{"base_id":"voss_redoubt_station",
	  "faction_facilities":[
	    {"name":"The Red Room","recipe_id":"load_red_mist","type":"red_room",
	     "production":{"output_per_run":2,"recipe":"Load Red Mist"}},
	    {"name":"Explicitly Private","recipe_id":"x","type":"y",
	     "production":{"public":false,"recipe":"X"}},
	    {"name":"Intel Terminal","faction_service":"faction_intel","type":"intel_terminal"}
	  ]}`)

	n, _, err := upsertPublicFromFacilityList(context.Background(), store, raw, 2165000)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("captured %d, want 0: omitted public, explicit false, and a service facility", n)
	}
}
