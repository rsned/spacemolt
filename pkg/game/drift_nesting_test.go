package game

import "testing"

func TestDriftNestingFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"read_note": {"note_id", "title", "content", "created_by", "created_at", "updated_at", "value"},
		"complete_mission": {
			"mission_id", "title", "chain_next", "credits_earned", "items_received",
			"skill_xp_gained", "community_contributed", "community_progress", "community_percent",
		},
	})
}
