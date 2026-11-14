package serverapi

import (
	"encoding/json"
	"testing"
)

// A POI scan lists the Frontier Repeaters there (openapi 2026-11-13).
func TestScanResponseDecodesRepeaters(t *testing.T) {
	raw := `{"poi_id":"p1","scan_power":40,"repeaters":[{"repeater_id":"r1","name":"Relay","hull":50,"max_hull":100,"in_battle":true}]}`
	var s ScanResponse
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Repeaters) != 1 || s.Repeaters[0].RepeaterID != "r1" || s.Repeaters[0].MaxHull != 100 || !s.Repeaters[0].InBattle {
		t.Fatalf("Repeaters = %+v", s.Repeaters)
	}
}
