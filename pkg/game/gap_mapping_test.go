package game

import "testing"

func TestGapMappingRegistered(t *testing.T) {
	actions := []string{
		// existing structs
		"faction_list_missions", "faction_rooms", "fleet", "login", "register",
		// reused MessageResponse (message-only or empty result)
		"claim", "leave_faction", "logout", "trade_cancel", "trade_decline",
		"faction_deposit_items", "faction_withdraw_items", "session",
	}
	for _, a := range actions {
		if _, ok := actionResponseTypes[a]; !ok {
			t.Errorf("action %q not registered in actionResponseTypes", a)
		}
	}
}
