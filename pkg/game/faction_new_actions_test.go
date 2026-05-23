package game

import "testing"

func TestNewActionFactionFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"faction_accept_invite":   {"faction", "faction_id", "message"},
		"faction_withdraw_invite": {"message", "player_id"},
		"faction_remove_enemy":    {"message", "removed", "target_faction_id", "target_name"},
	})
}
