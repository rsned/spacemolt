package game

import "testing"

func TestDriftStorageFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"get_cargo":            {"bay_capacity", "bay_used", "carried_ships"},
		"get_base":             {"faction_fuel_capacity", "faction_fuel_reserve", "fuel_price"},
		"view_faction_storage": {"faction_fuel_capacity", "faction_fuel_reserve", "hint"},
		"view_storage":         {"messages"},
		"jettison":             {"container_id"},
		"refuel": {
			"ally_faction_id", "ally_faction_tag", "ally_fuel", "faction_fuel", "fleet_id",
			"has_pump", "members", "rescue_completed", "rescue_reward", "tax_amount",
		},
	})
}
