package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// The server is the authority on whether we are in a battle. Observed live on
// pirate-5, 2026-11-17: a hunt disengaged ("You have disengaged from battle."),
// the follow-up poll answered "No active battle. Use attack to engage a
// target.", and no battle_ended ever arrived for the fight we had left. The
// cached flag stayed true, every later `battle stance` was refused with "You
// are not in a battle.", and the hunter ended each pass before engaging.
func TestHandleResponse_BattleRefusalClearsInBattle(t *testing.T) {
	messages := []string{
		"You are not in a battle. Use attack to engage a target.",
		"No active battle. Use attack to engage a target.",
		"You have disengaged from battle.",
	}
	for _, typ := range []string{protocol.TypeError, protocol.TypeActionError} {
		for _, msg := range messages {
			c := NewClient("wss://test.example.com", "u", "t", nil)
			c.SetDebugLogging(false)
			c.mu.Lock()
			c.state.InBattle = true
			c.state.BattleState = &BattleState{IsParticipant: true}
			c.mu.Unlock()

			c.handleResponse(protocol.Response{Type: typ, Payload: map[string]any{"message": msg}})

			c.mu.Lock()
			if c.state.InBattle {
				t.Errorf("%s %q left InBattle set", typ, msg)
			}
			if c.state.BattleState.IsParticipant {
				t.Errorf("%s %q left BattleState.IsParticipant set", typ, msg)
			}
			c.mu.Unlock()
		}
	}
}

func TestHandleResponse_BattleRefusalByCodeClearsInBattle(t *testing.T) {
	c := NewClient("wss://test.example.com", "u", "t", nil)
	c.SetDebugLogging(false)
	c.mu.Lock()
	c.state.InBattle = true
	c.mu.Unlock()

	c.handleResponse(protocol.Response{Type: protocol.TypeError, Payload: map[string]any{"code": "not_in_battle"}})

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.InBattle {
		t.Error("code not_in_battle left InBattle set")
	}
}

// An error that merely mentions a battle — the server refusing a command
// because we ARE in one — must not clear the flag.
func TestHandleResponse_InBattleErrorKeepsInBattle(t *testing.T) {
	c := NewClient("wss://test.example.com", "u", "t", nil)
	c.SetDebugLogging(false)
	c.mu.Lock()
	c.state.InBattle = true
	c.mu.Unlock()

	c.handleResponse(protocol.Response{
		Type:    protocol.TypeError,
		Payload: map[string]any{"code": "in_battle", "message": "You cannot dock while in a battle."},
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.state.InBattle {
		t.Error("an in_battle refusal cleared InBattle")
	}
}
