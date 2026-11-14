package main

import (
	"strings"
	"testing"
)

// get_poi was the one query of its family with no formatter at all: the prompt
// dumped the raw JSON while get_nearby and get_location rendered properly.
// The reply carries the things that decide whether to mine, refuel or route
// here, so they have to be visible without reading JSON by eye.
func TestFormatPOIRendersTheDecisionFields(t *testing.T) {
	raw := []byte(`{
      "action":"get_poi","kind":"normal",
      "poi":{"id":"nashira_belt","system_id":"nashira","type":"asteroid_belt",
             "name":"Nashira Belt","description":"A worked-over iron field.",
             "position":{"x":1.5,"y":0.2}},
      "resources":[
        {"resource_id":"iron_ore","name":"Iron Ore","richness":40,"remaining_display":"abundant"},
        {"resource_id":"carbon_ore","name":"Carbon Ore","richness":4,"remaining_display":"depleted"}],
      "services":["refuel","repair"],
      "fuel_price":6,"fuel_price_all_in":7,"fuel_tax_per_unit":1,
      "police_drones":2
    }`)
	got := formatPOI(raw)
	if got == "" {
		t.Fatal("formatPOI returned empty; a registered formatter must render something")
	}
	for _, want := range []string{
		"Nashira Belt", "nashira_belt", "asteroid_belt", // identity
		"Iron Ore", "abundant", // what is minable, and whether it is worth it
		"40",                        // richness is a 2-90 server scale, printed raw as explore.go does
		"refuel",                    // why you would stop here
		"7",                         // all-in fuel price, the number that decides refuelling
		"A worked-over iron field.", // v0.623 gave every location a description
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatPOI output missing %q\n--- got ---\n%s", want, got)
		}
	}
}

// A wormhole POI is the one case where omitting a field strands a ship on the
// far side, so the destination and expiry must survive into the output.
func TestFormatPOIShowsWormholeExpiry(t *testing.T) {
	raw := []byte(`{"poi":{"id":"wh_1","type":"wormhole_entrance","name":"Faint Tear"},
      "wormhole_destination":"Kochab","wormhole_destination_id":"kochab",
      "wormhole_expires_in":"42 ticks"}`)
	got := formatPOI(raw)
	for _, want := range []string{"Faint Tear", "Kochab", "42 ticks"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatPOI output missing %q\n--- got ---\n%s", want, got)
		}
	}
}

// The transit variant describes a journey and carries no POI; it must not
// render an empty shell. Its from_system/to_system/ticks_remaining/transit_type
// were absent from GetPOIResponse until 2026-10-13 and decoded to nothing, so
// the ETA -- the only field saying when the ship can act again -- was lost.
func TestFormatPOIInTransitShowsTheLegAndETA(t *testing.T) {
	got := formatPOI([]byte(`{"kind":"transit","in_transit":true,"poi":{},
      "from_system":"the_telescope","to_system":"unknown_edge",
      "ticks_remaining":3,"transit_type":"jump",
      "message":"You are in hyperspace, jumping between systems."}`))
	if !strings.Contains(strings.ToLower(got), "transit") {
		t.Errorf("formatPOI = %q, want it to say the ship is in transit", got)
	}
	for _, want := range []string{"the_telescope", "unknown_edge", "3", "jump"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatPOI transit output missing %q\n--- got ---\n%s", want, got)
		}
	}
}

// Richness arrives on the server's own 2-90 scale (avg ~34 across every belt
// in the knowledge base), NOT as a 0-1 fraction. Scaling it by 100 printed
// Caph Disk's iron as "4000%", which both overstates the field and breaks
// ranking by eye against explore.go, which prints the value raw.
func TestFormatPOIPrintsRichnessOnTheServerScale(t *testing.T) {
	raw := []byte(`{"poi":{"id":"caph_disk","type":"asteroid_belt","name":"Caph Disk"},
      "resources":[{"resource_id":"iron_ore","name":"Iron Ore","richness":40,"remaining_display":"566 units"}]}`)
	got := formatPOI(raw)
	if strings.Contains(got, "4000") {
		t.Errorf("richness scaled by 100; the server scale is 2-90\n--- got ---\n%s", got)
	}
	if !strings.Contains(got, "40") {
		t.Errorf("richness 40 not rendered\n--- got ---\n%s", got)
	}
}

// v0.692.0: get_poi names the POI it is attached to.
func TestFormatPOIShowsParent(t *testing.T) {
	raw := `{"poi":{"id":"sol_central","system_id":"sol","name":"Sol Central","type":"station","parent_id":"earth"}}`
	if got := formatPOI([]byte(raw)); !strings.Contains(got, "Attached to: earth") {
		t.Errorf("missing parent in:\n%s", got)
	}
}

// Deep Range's get_system POIs carry parent_id; the system table shows it.
func TestFormatSystemShowsPOIParent(t *testing.T) {
	raw := `{"system":{"id":"deep_range","name":"Deep Range","pois":[{"id":"deep_range_outpost","name":"Deep Range Outpost","type":"station","parent_id":"deep_range_mineral_fields","position":{"x":1,"y":2}}]}}`
	if got := formatSystem([]byte(raw)); !strings.Contains(got, "attached to deep_range_mineral_fields") {
		t.Errorf("missing parent in:\n%s", got)
	}
}

// v0.684.0 base_type distinguishes a full station from a mobile base or
// outpost that the POI type ("station") does not.
func TestFormatSystemShowsBaseType(t *testing.T) {
	raw := `{"system":{"id":"irlc","name":"Ironlight","pois":[` +
		`{"base_id":"b1","base_name":"Ironlight Refuge","base_type":"station","has_base":true,"id":"c834","name":"Ironlight Refuge","position":{"x":-0.1,"y":0.4},"type":"station"},` +
		`{"base_id":"irlc_civitas","base_name":"Civitas","base_type":"mobile_base","has_base":true,"id":"irlc_civitas","name":"Civitas","position":{"x":-0.7,"y":5},"type":"station"},` +
		`{"id":"belt","name":"Rock Belt","position":{"x":3,"y":1},"type":"asteroid_belt"}]}}`
	got := formatSystem([]byte(raw))
	var civ, belt string
	for _, l := range strings.Split(got, "\n") {
		switch {
		case strings.HasPrefix(l, "Civitas"):
			civ = l
		case strings.HasPrefix(l, "Rock Belt"):
			belt = l
		}
	}
	if !strings.Contains(got, "| Base") || !strings.Contains(civ, "mobile_base") {
		t.Fatalf("no base type column / Civitas row %q in:\n%s", civ, got)
	}
	if strings.Contains(belt, "station") || strings.Contains(belt, "mobile_base") {
		t.Errorf("belt row %q shows a base type", belt)
	}
}
