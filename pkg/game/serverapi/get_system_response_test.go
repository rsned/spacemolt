package serverapi

import (
	"encoding/json"
	"testing"
)

func TestDecodeGetSystemActiveBattle(t *testing.T) {
	raw := `{
		"action": "get_system",
		"security_status": "high",
		"system": {"id": "sol", "name": "Sol"},
		"active_battle": {
			"battle_id": "batt_123",
			"sides": [
				{"side_id": 0, "player_count": 2, "faction_id": "fac_a"},
				{"side_id": 1, "player_count": 1}
			],
			"participants": [
				{"player_id": "p1", "username": "Alice", "side_id": 0, "faction_id": "fac_a", "ship_class": "frigate", "is_npc": false},
				{"player_id": "npc1", "username": "Raider", "side_id": 1, "is_npc": true}
			]
		}
	}`
	var r GetSystemResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.ActiveBattle == nil {
		t.Fatal("expected ActiveBattle to be populated")
	}
	ab := r.ActiveBattle
	if ab.BattleID != "batt_123" {
		t.Errorf("battle_id: got %q", ab.BattleID)
	}
	if len(ab.Sides) != 2 || ab.Sides[0].SideID != 0 || ab.Sides[0].PlayerCount != 2 || ab.Sides[0].FactionID != "fac_a" {
		t.Errorf("sides: got %+v", ab.Sides)
	}
	if ab.Sides[1].SideID != 1 || ab.Sides[1].FactionID != "" {
		t.Errorf("side[1] (no faction): got %+v", ab.Sides[1])
	}
	if len(ab.Participants) != 2 {
		t.Fatalf("participants: got %d", len(ab.Participants))
	}
	if p := ab.Participants[0]; p.PlayerID != "p1" || p.Username != "Alice" || p.SideID != 0 || p.ShipClass != "frigate" || p.IsNPC {
		t.Errorf("participant[0]: got %+v", p)
	}
	if p := ab.Participants[1]; p.SideID != 1 || !p.IsNPC {
		t.Errorf("participant[1] (npc): got %+v", p)
	}
}

func TestDecodeGetSystemNoActiveBattle(t *testing.T) {
	raw := `{"action":"get_system","security_status":"low","system":{"id":"sol","name":"Sol"}}`
	var r GetSystemResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.ActiveBattle != nil {
		t.Errorf("expected nil ActiveBattle when absent, got %+v", r.ActiveBattle)
	}
}
