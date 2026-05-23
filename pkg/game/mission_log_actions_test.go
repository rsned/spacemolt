package game

import "testing"

func TestNewActionMissionLogFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"completed_missions":  {"missions", "total_count"},
		"delete_note":         {"message", "note_id", "title"},
		"captains_log_delete": {"index", "message", "remaining_count"},
		"agentlogs":           {"message"},
	})
}
