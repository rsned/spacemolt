package serverapi

import (
	"encoding/json"
	"testing"
)

// Undocked, view_storage now answers with an INDEX of every station holding
// this player's storage instead of an empty result: items/ships are empty and
// the inventory is summarised per base in "locations". Captured live
// 2026-08-28. Dropping it silently cost us the one cheap fleet-wide answer to
// "where is our stuff", and ship_count is the only view we have ever had of
// where ships are parked -- agent_ships has never captured a row.
func TestViewStorageResponse_DecodesLocationIndex(t *testing.T) {
	const payload = `{
	  "action": "view_storage",
	  "base_id": "",
	  "hint": "3,239,823 items in storage at cargo_lanes_freight_depot, central_nexus\n\nDock, or pass station_id, to read one station's contents.",
	  "items": [],
	  "locations": [
	    {"base_id":"cargo_lanes_freight_depot","base_name":"Cargo Lanes Freight Depot","item_count":43,"ship_count":0,"system":"cargo_lanes","system_name":"Cargo Lanes"},
	    {"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","item_count":681378,"ship_count":13,"system":"haven","system_name":"Haven"}
	  ],
	  "ships": []
	}`

	var got ViewStorageResponse
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Locations) != 2 {
		t.Fatalf("Locations = %d entries, want 2 -- the per-station index must not be dropped", len(got.Locations))
	}

	ge := got.Locations[1]
	if ge.BaseID != "grand_exchange_station" {
		t.Errorf("BaseID = %q, want grand_exchange_station", ge.BaseID)
	}
	if ge.BaseName != "Grand Exchange Station" {
		t.Errorf("BaseName = %q, want Grand Exchange Station", ge.BaseName)
	}
	if ge.ItemCount != 681378 {
		t.Errorf("ItemCount = %d, want 681378", ge.ItemCount)
	}
	if ge.ShipCount != 13 {
		t.Errorf("ShipCount = %d, want 13", ge.ShipCount)
	}
	if ge.System != "haven" {
		t.Errorf("System = %q, want haven", ge.System)
	}
	if ge.SystemName != "Haven" {
		t.Errorf("SystemName = %q, want Haven", ge.SystemName)
	}
}

// The docked form still carries items for one base and no index. Adding
// Locations must not disturb it.
func TestViewStorageResponse_DockedFormStillDecodes(t *testing.T) {
	const payload = `{"action":"view_storage","base_id":"grand_exchange_station","credits":50,"items":[{"item_id":"iron_ore","quantity":7}]}`
	var got ViewStorageResponse
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.BaseID != "grand_exchange_station" {
		t.Errorf("BaseID = %q", got.BaseID)
	}
	if len(got.Items) != 1 {
		t.Fatalf("Items = %d, want 1", len(got.Items))
	}
	if len(got.Locations) != 0 {
		t.Errorf("Locations = %d, want 0 for the docked form", len(got.Locations))
	}
}
