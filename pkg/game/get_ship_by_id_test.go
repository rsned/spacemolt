package game

import (
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// v0.568.0: list_ships reports module_type_ids per ship, so "which hull carries
// a survey scanner" is one call instead of a visit to every station.
//
// Before this, OwnedShip.Modules was a bare COUNT and ship_modules had never
// captured a row, so what was fitted to the fleet's 170 idle hulls was simply
// unknown. That blocked the mining gate outright: mining succeeds only while
// summed module power stays below a resource's supported_power, and the summed
// power of a stored hull could not be computed at all.
func TestOwnedShip_CarriesModuleTypeIDs(t *testing.T) {
	const payload = `{"ships":[{
		"ship_id":"74aeb79e","class_id":"survey_vessel","is_active":false,
		"location":"stored at Grand Exchange Station","modules":3,
		"module_type_ids":["survey_scanner_ii","mining_laser_iv","ice_harvester_iv"]
	}],"count":1}`

	var got serverapi.ListShipsResponse
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Ships) != 1 {
		t.Fatalf("ships = %d", len(got.Ships))
	}
	ids := got.Ships[0].ModuleTypeIDs
	if len(ids) != 3 {
		t.Fatalf("ModuleTypeIDs = %v, want 3 entries", ids)
	}
	if ids[0] != "survey_scanner_ii" {
		t.Errorf("ModuleTypeIDs[0] = %q", ids[0])
	}
	// The count field remains, and must agree.
	if got.Ships[0].Modules != len(ids) {
		t.Errorf("Modules count %d disagrees with %d type ids", got.Ships[0].Modules, len(ids))
	}
}

// get_ship now takes an optional ship_id: the full fit of any ship you own,
// from anywhere, no docking and no travel. Without an id it still reads the
// active ship.
func TestGetShipByID_SendsTheShipID(t *testing.T) {
	c := &Client{}
	if got := getShipPayload(""); got != nil {
		t.Errorf("payload for the active ship = %v, want nil", got)
	}
	got := getShipPayload("74aeb79e")
	if got == nil || got["ship_id"] != "74aeb79e" {
		t.Errorf("payload = %v, want ship_id set", got)
	}
	_ = c
}
