package game

import "testing"

func TestGapFactionIntelFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"faction_accept_ally":        {"target_faction_id", "target_name"},
		"faction_propose_ally":       {"target_faction_id", "target_name"},
		"faction_remove_ally":        {"removed", "target_faction_id", "target_name"},
		"faction_set_enemy":          {"target_faction_id", "target_name"},
		"faction_intel_status":       {"intel_level", "reports_24h", "top_contributors", "total_reports", "unique_players", "unique_systems"},
		"faction_query_intel":        {"count", "entries", "intel_level", "limit", "offset", "total"},
		"faction_query_trade_intel":  {"entries", "intel_level", "limit", "offset", "showing", "total"},
		"faction_submit_trade_intel": {"stations_updated", "status"},
		"faction_trade_intel_status": {"intel_level", "reports_24h", "top_contributors", "total_reports", "unique_items", "unique_stations"},
	})
}
