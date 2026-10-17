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

// This test was written from the SIMPLIFIED fixture below and its premise is
// false for the live payload: a real member reply carries
// "category":"production" on our own entries too. It is kept because the
// behaviour it pins is still correct -- a production line is one that HAS a
// production object, and nothing should depend on a sibling string field -- but
// it does NOT describe a bug we ever had. The actual cause of CRFT's lines
// being uncaptured was the missing ally_facilities section; see
// TestUpsertPublicFromFacilityList_MergesAllyFacilities.
//
// The fixture shape, from facility_format_test.go's
// TestFormatFacilityFactionList_ProductionSplit:
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

// A fifth facility section exists: ally_facilities. The capture merged only
// station/player/faction/public, so an ALLY's lines were invisible.
//
// Confirmed live 2026-10-17 by removing marketbot_haven from the mb fleet and
// running `facility list` as it: the drift monitor reported
//
//	New fields in "list" response not in FacilityListResponse: [ally_facilities]
//
// and its rendered output showed 9 public facilities at grand_exchange_station
// — ENDL x3, HEXC x2, BEAT x1, three player-owned — and NONE of CRFT's five,
// despite all five being public. marketbot_haven is in Data Bots [DB], and
// CRFT lists DB as an ally, so the server files our lines under ally rather
// than public for it. Ally use is fee-free, which is why our lines advertise a
// Fee/hr while every captured public row carries rental_fee_per_run.
//
// The sections are viewer-relative and symmetric: a faction's OWN lines are
// always in faction_facilities, never in ally_facilities, so a CRFT member
// never sees CRFT lines here. The gap therefore only ever affected viewers
// OUTSIDE the owning faction — which is every capture we run at
// grand_exchange_station, because the only resident there is a DB marketbot.
//
// The section's exact contents were never logged, so this pins only that the
// section is MERGED. Whether a given entry is stored still depends on its own
// production.public flag, exactly as for the other four sections — a
// conservative default that cannot invent a public line.
func TestUpsertPublicFromFacilityList_MergesAllyFacilities(t *testing.T) {
	store := &fakePublicFacilityStore{}
	raw := []byte(`{"base_id":"grand_exchange_station",
	  "ally_facilities":[
	    {"category":"production","name":"Polymer Extruder","facility_id":"b659c360",
	     "faction_id":"e727c0e918d994c72db2978fe5b18edc","recipe_id":"extrude_nanoplastic",
	     "production":{"public":true,"output_per_run":3,"recipe":"Extrude Nanoplastic",
	                   "rental_fee_per_run":60}}
	  ]}`)

	n, _, err := upsertPublicFromFacilityList(context.Background(), store, raw, 2165000)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("captured %d from ally_facilities, want 1", n)
	}
	if got := store.rows[0].RecipeID; got != "extrude_nanoplastic" {
		t.Errorf("recipe = %q", got)
	}
}

// An ally line that is NOT public must still be skipped: the ally section is
// merged, not trusted wholesale.
func TestUpsertPublicFromFacilityList_AllySectionStillHonoursThePublicFlag(t *testing.T) {
	store := &fakePublicFacilityStore{}
	raw := []byte(`{"base_id":"grand_exchange_station",
	  "ally_facilities":[{"category":"production","name":"Private Ally Line","recipe_id":"x",
	                      "production":{"recipe":"X"}}],
	  "station_facilities":[]}`)

	n, _, err := upsertPublicFromFacilityList(context.Background(), store, raw, 2165000)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("captured %d, want 0: the ally line omits production.public", n)
	}
}
