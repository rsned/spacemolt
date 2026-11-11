package serverapi

import (
	"encoding/json"
	"testing"
)

// Payloads captured live on 2026-11-11 (Haven -> Market Prime, bearing 51).
func TestPathfinderRepliesDecode(t *testing.T) {
	var j PathfinderJumpResponse
	if err := json.Unmarshal([]byte(`{"action":"pathfinder_jump","bearing":51,"message":"Pathfinder Drive engaged."}`), &j); err != nil || j.Bearing != 51 {
		t.Fatalf("jump = %+v err %v", j, err)
	}
	var a PathfinderArrivalResponse
	raw := `{"action":"pathfinder_arrival","from_system":"Haven","navigation_xp":3,"piloting_xp":12,"poi":"warehouse_world","system":"Market Prime","system_id":"market_prime"}`
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatal(err)
	}
	if a.SystemID != "market_prime" || a.POI != "warehouse_world" || a.FromSystem != "Haven" || a.PilotingXP != 12 || a.NavigationXP != 3 {
		t.Fatalf("arrival = %+v", a)
	}
}
