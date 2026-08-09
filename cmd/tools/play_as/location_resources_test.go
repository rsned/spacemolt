package main

import (
	"strings"
	"testing"
)

// liveLocationWithResources is a real get_location reply captured from
// craftsman-1 at Bunda Belt on 2026-08-08, trimmed only by dropping the
// nearby_players / nearby_empire_npcs entries — no resource field was renamed,
// added or invented.
//
// get_poi was retired server-side in 2026-06-24 and `poi` now transparently
// runs get_location, so this reply is the ONLY source of per-POI ore data.
// The formatter reported "Resources: 6 listed" and threw the rest away.
const liveLocationWithResources = `{"message":"Current location","location":{
"poi_id":"bunda_belt","poi_name":"Bunda Belt","poi_type":"asteroid_belt",
"system_id":"bunda","system_name":"Bunda","empire":"nebula",
"security_status":"Frontier (minimal police presence)",
"connections":["gold_run","cargo_lanes"],
"nearby_player_count":0,"nearby_empire_npc_count":0,"nearby_pirate_count":0,
"nearby_players":[],"nearby_empire_npcs":[],"nearby_pirates":[],
"resources":[
{"item_id":"carbon_ore","item_name":"Carbon Ore","remaining":30000,"richness":52,"supported_power":1500},
{"item_id":"vanadium_ore","item_name":"Vanadium Ore","remaining":25000,"richness":48,"supported_power":1250},
{"item_id":"tungsten_ore","item_name":"Tungsten Ore","remaining":25000,"richness":29,"supported_power":1250},
{"item_id":"platinum_ore","item_name":"Platinum Ore","remaining":21952,"richness":12,"supported_power":1097},
{"item_id":"thorium_ore","item_name":"Thorium Ore","remaining":3996,"richness":15,"supported_power":199},
{"item_id":"prismatic_nebulite","item_name":"Prismatic Nebulite","remaining":250,"richness":4,"supported_power":12}]}}`

// The whole point of asking a belt what it holds is the ore table. A count is
// not an answer.
func TestFormatLocationRendersResourceTable(t *testing.T) {
	out := formatGetLocation([]byte(liveLocationWithResources))
	if out == "" {
		t.Fatal("formatLocation returned empty")
	}

	// The bare count line is what this replaces; if it survives alongside the
	// table the output has two answers to the same question.
	if strings.Contains(out, "6 listed") {
		t.Error("the placeholder count line is still present")
	}

	for _, want := range []string{
		"Carbon Ore",
		// The id is what `mine` and every KB query take, so it must appear in
		// full alongside the display name.
		"carbon_ore",
		"prismatic_nebulite",
		// Richness and remaining are the two numbers that decide whether a belt
		// is worth working — the mission board's own advice is that quarry
		// gathers where ore is rich and the belt is un-worked.
		"52",
		"30000",
		"3996",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

// Richest first: a belt's headline fact is its best ore, and scanning a sorted
// column is the whole reason to render a table rather than echo the payload.
//
// This asserts on thorium-before-platinum and nothing else, because that is the
// ONLY pair the server sends out of order (richness 15 after 12); every other
// pair in the captured reply already descends. An earlier version compared
// carbon/vanadium/nebulite and passed with the sort deleted entirely — the
// payload's own order satisfied it. A sort test has to name a pair the input
// gets wrong, or it proves the fixture rather than the code.
func TestFormatLocationSortsResourcesByRichness(t *testing.T) {
	out := formatGetLocation([]byte(liveLocationWithResources))
	thorium := strings.Index(out, "thorium_ore")   // richness 15
	platinum := strings.Index(out, "platinum_ore") // richness 12, but sent FIRST
	if thorium < 0 || platinum < 0 {
		t.Fatalf("expected both resources in:\n%s", out)
	}
	if thorium > platinum {
		t.Errorf("thorium (richness 15) must sort above platinum (12); the server "+
			"sends them the other way round, so this is unsorted output:\n%s", out)
	}
}

// A POI with no resources must not print an empty table header — a station
// legitimately has none, and a bare heading reads as a rendering failure.
func TestFormatLocationWithoutResources(t *testing.T) {
	const body = `{"location":{"poi_id":"sol_station","poi_name":"Sol Station",
"poi_type":"station","system_id":"sol","system_name":"Sol",
"nearby_players":[],"nearby_empire_npcs":[],"nearby_pirates":[]}}`
	out := formatGetLocation([]byte(body))
	if out == "" {
		t.Fatal("formatLocation returned empty")
	}
	if strings.Contains(out, "Resources") {
		t.Errorf("a POI with no resources must not print a resource section, got:\n%s", out)
	}
}

// A server that omits supported_power (or sends a resource with no display
// name) must still render the row rather than dropping the ore entirely.
func TestFormatLocationResourceWithMissingFields(t *testing.T) {
	const body = `{"location":{"poi_id":"p","poi_name":"P","poi_type":"asteroid_belt",
"system_id":"s","system_name":"S",
"nearby_players":[],"nearby_empire_npcs":[],"nearby_pirates":[],
"resources":[{"item_id":"iron_ore","remaining":100,"richness":7}]}}`
	out := formatGetLocation([]byte(body))
	if !strings.Contains(out, "iron_ore") {
		t.Errorf("a resource missing item_name/supported_power must still render, got:\n%s", out)
	}
}
