package game

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestSystemDecodesBaseType pins the v0.684.0 base_type field on get_system
// POIs: all three values decode and survive POIFromAPI, and a POI without a
// base leaves it empty.
func TestSystemDecodesBaseType(t *testing.T) {
	raw := `{"id":"sol","name":"Sol","pois":[
		{"id":"p_station","name":"Port","type":"station","has_base":true,"base_id":"b1","base_name":"Port","base_type":"station"},
		{"id":"p_outpost","name":"Depot","type":"asteroid_belt","has_base":true,"base_id":"b2","base_name":"Depot","base_type":"outpost"},
		{"id":"p_mobile","name":"Capital","type":"station","has_base":true,"base_id":"b3","base_name":"Capital","base_type":"mobile_base"},
		{"id":"p_empty","name":"Rock","type":"asteroid_belt"}]}`
	var sys serverapi.SystemData
	if err := json.Unmarshal([]byte(raw), &sys); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{BaseTypeStation, BaseTypeOutpost, BaseTypeMobileBase, ""}
	if len(sys.POIs) != len(want) {
		t.Fatalf("got %d POIs, want %d", len(sys.POIs), len(want))
	}
	for i, w := range want {
		if got := sys.POIs[i].BaseType; got != w {
			t.Errorf("serverapi POI %s base_type = %q, want %q", sys.POIs[i].ID, got, w)
		}
		if got := POIFromAPI(sys.POIs[i]).BaseType; got != w {
			t.Errorf("game POI %s BaseType = %q, want %q", sys.POIs[i].ID, got, w)
		}
	}
	if sys.POIs[3].HasBase {
		t.Error("baseless POI reports has_base")
	}

	var cur serverapi.CurrentPOI
	if err := json.Unmarshal([]byte(`{"id":"p_outpost","has_base":true,"base_type":"outpost"}`), &cur); err != nil {
		t.Fatal(err)
	}
	if cur.BaseType != BaseTypeOutpost {
		t.Errorf("CurrentPOI.BaseType = %q", cur.BaseType)
	}
}

// TestFitPreviewIsTypedAndImplemented pins fit_preview: both clients implement
// it and the documented reply decodes.
func TestFitPreviewIsTypedAndImplemented(t *testing.T) {
	var (
		_ GameClient = (*Client)(nil)
		_ GameClient = (*MCPGameClient)(nil)
	)
	if got, want := actionResponseTypes["fit_preview"], reflect.TypeOf(serverapi.FitPreviewResponse{}); got != want {
		t.Fatalf("actionResponseTypes[fit_preview] = %v, want %v", got, want)
	}
	raw := `{"ship_id":"s1","valid":false,
		"issues":[{"code":"cpu_exceeded","message":"CPU 12 > 10"}],
		"stats":{"max_hull":100,"cpu_capacity":10,"cpu_used":12,"weapon_slots":2,"weapon_slots_used":1},
		"steps":[{"action":"uninstall_mod","module_id":"m1","type_id":"pulse_laser_i","name":"Pulse Laser I","temporary":true},
			{"action":"install_mod","module_id":"pulse_laser_i","type_id":"pulse_laser_i","name":"Pulse Laser I"}]}`
	var resp serverapi.FitPreviewResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Valid || len(resp.Issues) != 1 || resp.Issues[0].Code != "cpu_exceeded" ||
		resp.Stats.CPUUsed != 12 || len(resp.Steps) != 2 || !resp.Steps[0].Temporary {
		t.Fatalf("decoded %+v", resp)
	}
}
