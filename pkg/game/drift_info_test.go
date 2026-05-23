package game

import "testing"

func TestDriftInfoFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"catalog":        {"analysis", "passive_recipe_details", "recipes"},
		"chat":           {"channel"},
		"faction_info":   {"alliance_proposals", "facilities", "roles"},
		"get_action_log": {"faction_id", "page", "page_size", "total", "total_pages"},
		"get_nearby":     {"empire_npc_count", "empire_npcs", "offline_collapsed"},
		"get_version":    {"has_more", "search_term"},
		"survey_system":  {"anomaly_hint"},
		"uninstall_mod":  {"damaged"},
	})
}
