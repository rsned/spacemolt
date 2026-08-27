package game

import (
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// A battle_joined push arrives when another player enters a fight already in
// progress. Observed live 2026-08-26 during the Dheneb station battle:
//
//	{"player_id":"32309e...","side_id":2,"username":"Munawar"}
//
// The client had no case for it, so handleResponse logged it as an unhandled
// response type and the arrival was invisible to anything reading state.
func TestHandleResponse_BattleJoined_MarksInBattle(t *testing.T) {
	c := NewClient("wss://test.example.com", "u", "t", nil)
	c.SetDebugLogging(false)
	c.handleResponse(protocol.Response{
		Type: protocol.TypeBattleJoined,
		Payload: map[string]any{
			"player_id": "32309e6505bd3f68e3a513f3372487b1",
			"side_id":   float64(2),
			"username":  "Munawar",
		},
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.state.InBattle {
		t.Error("battle_joined did not set InBattle; a reinforcement arriving means a fight is live")
	}
}

// battle_joined carries no battle_id, so it must NOT clear a remembered one —
// the same rule rememberBattleIDLocked already enforces for battle_damage.
func TestHandleResponse_BattleJoined_KeepsBattleID(t *testing.T) {
	c := NewClient("wss://test.example.com", "u", "t", nil)
	c.SetDebugLogging(false)
	c.mu.Lock()
	c.state.LastBattleID = "242b5fd8676d27c997f9dcd6b76a8cb7"
	c.mu.Unlock()

	c.handleResponse(protocol.Response{
		Type:    protocol.TypeBattleJoined,
		Payload: map[string]any{"player_id": "abc", "side_id": float64(2), "username": "Munawar"},
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.LastBattleID != "242b5fd8676d27c997f9dcd6b76a8cb7" {
		t.Errorf("LastBattleID = %q, want it preserved: battle_joined carries no battle_id",
			c.state.LastBattleID)
	}
}

// The attack reply gained battle_id/system/your_side/your_zone in the v0.5xx
// server. battle_id is the one that matters operationally: it is the handle for
// get_battle_log, and before this it had to be copied out of the console by
// hand at the moment the fight started.
func TestAttackResponse_CarriesBattleFields(t *testing.T) {
	var r serverapi.AttackResponse
	raw := `{
		"action": "attack",
		"battle_id": "242b5fd8676d27c997f9dcd6b76a8cb7",
		"kind": "player",
		"message": "Battle engaged with AetherWraith!",
		"system": "dheneb",
		"target": "AetherWraith",
		"your_side": 5,
		"your_zone": "outer"
	}`
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unmarshal attack reply: %v", err)
	}

	if r.BattleID != "242b5fd8676d27c997f9dcd6b76a8cb7" {
		t.Errorf("BattleID = %q, want the id from the reply", r.BattleID)
	}
	if r.System != "dheneb" {
		t.Errorf("System = %q, want %q", r.System, "dheneb")
	}
	if r.YourSide != 5 {
		t.Errorf("YourSide = %d, want 5", r.YourSide)
	}
	if r.YourZone != "outer" {
		t.Errorf("YourZone = %q, want %q — the zone is why weapons could not reach", r.YourZone, "outer")
	}
}
