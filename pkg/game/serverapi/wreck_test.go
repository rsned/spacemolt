package serverapi

import (
	"encoding/json"
	"testing"
)

// A ship wreck lists its salvageable modules as objects (LootedModule in the
// spec: id, name, type, type_id), not as id strings. Decoding them as strings
// failed the WHOLE get_wrecks reply — seen live 2026-08-30 in alula, where a
// player wreck beside a carcass broke play_as's wildlife capture; the worker's
// hunt loop parses the same struct.
func TestWreck_DecodesModuleObjects(t *testing.T) {
	raw := `{"count":1,"wrecks":[{"id":"91c0","type":"ship","ship_class":"underwriter","victim_id":"p1",
		"cargo":[{"item_id":"gold_ore","quantity":10}],
		"modules":[{"id":"m1","name":"Pulse Laser III","type":"weapon","type_id":"pulse_laser_iii"}]}]}`
	var resp GetWrecksResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Wrecks) != 1 || len(resp.Wrecks[0].Modules) != 1 {
		t.Fatalf("wrecks = %+v", resp.Wrecks)
	}
	if m := resp.Wrecks[0].Modules[0]; m.TypeID != "pulse_laser_iii" || m.Name != "Pulse Laser III" || m.Type != "weapon" {
		t.Errorf("module = %+v", m)
	}
}
