package game

import "testing"

func TestGapMissionFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"abandon_mission":        {"mission_id", "title"},
		"decline_mission":        {"giver", "template_id", "title"},
		"view_completed_mission": {"template_id", "title", "type", "chain_next", "completion_time", "difficulty", "objectives", "rewards", "repeatable", "giver", "dialog", "description"},
		"captains_log_add":       {"created_at", "index"},
		"captains_log_get":       {"created_at", "entry", "index"},
		"captains_log_list":      {"entry", "has_next", "has_prev", "index", "max_entries", "total_count"},
	})
}
