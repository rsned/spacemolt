package game

import "testing"

func TestGapShipFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"attack":              {"command", "pending"},
		"name_ship":           {"ship_id", "ship_name"},
		"release_tow":         {"wreck_id"},
		"scrap_wreck":         {"materials", "ship_class", "stored_at", "total_value", "wreck_id"},
		"cancel_ship_listing": {"class_id", "ship_id"},
	})
}
