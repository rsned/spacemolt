package main

import (
	"strings"
	"testing"
)

// Recorded `facility list` blocks (Grand Exchange, 2026-10-26; life support from
// Conjunction Junction, 2026-10-31), plus the shortfall fields the openapi spec
// documents but no recording has shown yet: power fuel_inputs/remediation,
// life_support starved/remediation, construction under_construction.
const stationBlocksHealthy = `{"base_id":"grand_exchange_station",
 "power":{"battery_capacity":180,"battery_stored":180,"current_draw":1,"efficiency":1,"supply":200},
 "life_support":{"demand":3,"plants":1,"maintenance_cycle_ticks":100,"supply":12,"maintenance":[
   {"item_id":"catalytic_scrubber_cartridge","name":"Catalytic Scrubber Cartridge","quantity_per_cycle":1},
   {"item_id":"oxygen_gas","name":"Oxygen Gas","quantity_per_cycle":2},
   {"item_id":"water_ice","name":"Water Ice","quantity_per_cycle":3}]},
 "construction":{"pending":[{"build_cost":2257000,"category":"production","definition_id":"synthesize_pan_galactic_matrix_factory",
   "name":"Synthesize Pan-Galactic Matrix Factory","reason":"production","status":"waiting_for_materials","materials":[
   {"item_id":"steel_plate","name":"Steel Plate","quantity_in_storage":3440,"quantity_required":3000},
   {"item_id":"control_node","name":"Control Node","quantity_in_storage":48,"quantity_missing":452,"quantity_required":500},
   {"item_id":"plasma_injector","name":"Plasma Injector","quantity_in_storage":78,"quantity_missing":522,"quantity_required":600}]}]},
 "service_pools":{
   "marine_training":{"capacity":512,"next_cycle_supply_required":64,"refill_per_cycle":128,"remaining":154,"supply_item":"protein_rations"},
   "medical":{"capacity":2048,"crew_treatment_price":59,"marine_treatment_price":61,"next_cycle_supply_required":4,"refill_per_cycle":512,"remaining":2028,"supply_item":"medical_supplies"},
   "personnel":{"capacity":2048,"next_cycle_supply_required":17,"refill_per_cycle":512,"remaining":1917,"supply_item":"protein_rations"}}}`

const stationBlocksShort = `{"base_id":"c976fa7df53c87f9616165d7d411b41a",
 "power":{"battery_capacity":0,"battery_stored":0,"current_draw":30,"efficiency":0.4,"supply":12,
   "fuel_inputs":[{"item_id":"fuel_cell","name":"Fuel Cell","quantity_per_cycle":2}],
   "remediation":"Build or supply more power generation."},
 "life_support":{"demand":20,"plants":1,"maintenance_cycle_ticks":100,"supply":12,
   "maintenance":[{"item_id":"water_ice","name":"Water Ice","quantity_per_cycle":3}],
   "starved":[{"item_id":"water_ice","name":"Water Ice","quantity_per_cycle":3}],
   "remediation":"Deposit water ice into station storage."},
 "construction":{"under_construction":[{"definition_id":"algae_cultivation_tank","name":"Algae Cultivation Tank","category":"production",
   "status":"under_construction","ticks_until_complete":12,"build_cost":90000}]}}`

func stationBlockLine(out, substr string) string {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}

// TestFormatFacilityList_StationBlocksHealthy guards that power, life support,
// service pools and pending construction all render from a real response.
func TestFormatFacilityList_StationBlocksHealthy(t *testing.T) {
	out := formatFacilityList([]byte(stationBlocksHealthy))

	for _, c := range []struct{ line, want string }{
		{"Power:", "supply 200"},
		{"Power:", "draw 1"},
		{"Power:", "battery 180/180"},
		{"Power:", "efficiency 100%"},
		{"Life support:", "supply 12 / demand 3"},
		{"Life support:", "1 plant"},
		{"Upkeep / 100 ticks:", "Oxygen Gas 2"},
		{"Upkeep / 100 ticks:", "Water Ice 3"},
		{"Personnel", "1917/2048"},
		{"Personnel", "refill 512/cycle"},
		{"Personnel", "protein_rations"},
		{"Personnel", "next 17"},
		{"Medical", "2028/2048"},
		{"Medical", "crew 59"},
		{"Medical", "marine 61"},
		{"Marine training", "154/512"},
		{"Synthesize Pan-Galactic Matrix Factory", "waiting_for_materials"},
		{"Synthesize Pan-Galactic Matrix Factory", "2,257,000"},
		{"Control Node", "missing 452"},
		{"Control Node", "48/500"},
		{"Plasma Injector", "missing 522"},
	} {
		if got := stationBlockLine(out, c.line); !strings.Contains(got, c.want) {
			t.Errorf("line with %q should contain %q, got %q\nfull output:\n%s", c.line, c.want, got, out)
		}
	}
	// A satisfied material is not a shortfall and must not be listed as one.
	if strings.Contains(out, "Steel Plate") {
		t.Errorf("fully stocked material listed as missing:\n%s", out)
	}
	for _, l := range []string{"Power:", "Life support:", "Personnel", "Medical", "Marine training"} {
		if strings.Contains(stationBlockLine(out, l), "⚠") {
			t.Errorf("healthy %s line should carry no warning: %s", l, stationBlockLine(out, l))
		}
	}
}

// TestFormatFacilityList_StationBlocksShortfalls guards the warnings: a
// throttled grid with its fuel inputs and remediation, starved life support
// with its remediation, and a build already under construction.
func TestFormatFacilityList_StationBlocksShortfalls(t *testing.T) {
	out := formatFacilityList([]byte(stationBlocksShort))

	for _, c := range []struct{ line, want string }{
		{"Power:", "efficiency 40%"},
		{"Power:", "⚠ throttled"},
		{"Fuel / cycle:", "Fuel Cell 2"},
		{"Build or supply more power generation.", "💡"},
		{"Life support:", "⚠ short 8"},
		{"Starved:", "Water Ice 3"},
		{"Deposit water ice into station storage.", "💡"},
		{"Algae Cultivation Tank", "under_construction"},
		{"Algae Cultivation Tank", "12 ticks left"},
	} {
		if got := stationBlockLine(out, c.line); !strings.Contains(got, c.want) {
			t.Errorf("line with %q should contain %q, got %q\nfull output:\n%s", c.line, c.want, got, out)
		}
	}
	// No battery capacity means no battery figure, not "0/0".
	if strings.Contains(stationBlockLine(out, "Power:"), "battery") {
		t.Errorf("battery shown with zero capacity: %s", stationBlockLine(out, "Power:"))
	}
}

// TestFormatFacilityList_NoStationBlocks guards that a response without any of
// the blocks prints no empty Station / Construction headings.
func TestFormatFacilityList_NoStationBlocks(t *testing.T) {
	out := formatFacilityList([]byte(`{"base_id":"x","player_facilities":[],"faction_facilities":[]}`))
	for _, unwanted := range []string{"Station status", "Power:", "Life support:", "Service pools", "Construction"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected %q with no blocks present:\n%s", unwanted, out)
		}
	}
}
