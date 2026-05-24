package game

import "testing"

func TestGapShipFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"attack":              {"command", "pending"},
		"name_ship":           {"ship_id", "ship_name"},
		"release_tow":         {"wreck_id"},
		"repair_module":       {"module_id", "repair_amount", "wear_after", "wear_before", "wear_status", "xp_gained"},
		"scrap_wreck":         {"materials", "ship_class", "stored_at", "total_value", "wreck_id"},
		"cancel_ship_listing": {"class_id", "ship_id"},
	})
}
