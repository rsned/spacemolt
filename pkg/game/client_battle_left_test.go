package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// A battle_left push arrives when a combatant leaves a fight. Observed live
// 2026-08-27:
//
//	{"player_id":"crt_56cdf31ae2ce202c1d79057b75924563","reason":"fled","username":""}
//
// Note the crt_ prefix: the departing combatant is a CREATURE, and username is
// empty. Anything that assumes a player here reads a blank name.
//
// Crucially, one participant leaving is NOT the battle ending -- battle_ended
// is a separate frame. Clearing InBattle here would strand us: the worker would
// believe the fight was over while the remaining hostiles kept firing.
func TestHandleResponse_BattleLeft_DoesNotEndTheBattle(t *testing.T) {
	c := NewClient("wss://test.example.com", "u", "t", nil)
	c.SetDebugLogging(false)
	c.mu.Lock()
	c.state.InBattle = true
	c.state.InCombat = true
	c.mu.Unlock()

	c.handleResponse(protocol.Response{
		Type: protocol.TypeBattleLeft,
		Payload: map[string]any{
			"player_id": "crt_56cdf31ae2ce202c1d79057b75924563",
			"reason":    "fled",
			"username":  "",
		},
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.state.InBattle {
		t.Error("battle_left cleared InBattle; one combatant leaving is not the battle ending (battle_ended is)")
	}
}

// It carries no battle_id, so it must not disturb a remembered one -- the same
// rule battle_joined and battle_damage already follow.
func TestHandleResponse_BattleLeft_KeepsBattleID(t *testing.T) {
	c := NewClient("wss://test.example.com", "u", "t", nil)
	c.SetDebugLogging(false)
	c.mu.Lock()
	c.state.LastBattleID = "242b5fd8676d27c997f9dcd6b76a8cb7"
	c.mu.Unlock()

	c.handleResponse(protocol.Response{
		Type:    protocol.TypeBattleLeft,
		Payload: map[string]any{"player_id": "crt_56cdf31ae2ce202c1d79057b75924563", "reason": "fled"},
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.LastBattleID != "242b5fd8676d27c997f9dcd6b76a8cb7" {
		t.Errorf("LastBattleID = %q; battle_left carries no id and must not clear it", c.state.LastBattleID)
	}
}
