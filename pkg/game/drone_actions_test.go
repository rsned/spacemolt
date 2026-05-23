package game

import "testing"

// assertActionFields verifies that each action is registered in
// actionResponseTypes and its struct exposes the expected JSON field names.
// Shared by all api-currentness phase-1 action tests.
func assertActionFields(t *testing.T, want map[string][]string) {
	t.Helper()
	for action, fields := range want {
		typ, ok := actionResponseTypes[action]
		if !ok {
			t.Errorf("action %q not registered in actionResponseTypes", action)
			continue
		}
		got := jsonFieldNames(typ)
		for _, f := range fields {
			if !got[f] {
				t.Errorf("action %q struct missing json field %q", action, f)
			}
		}
	}
}

func TestNewActionDroneFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"get_drone": {
			"id", "type", "status", "hull", "max_hull", "cargo",
			"cargo_capacity", "cargo_used", "item_id", "poi_id", "system_id",
			"deployed_at", "loaded_at", "script", "memory", "travel_to", "travel_ticks",
		},
		"get_drones": {
			"bandwidth_total", "bandwidth_used", "bay_capacity", "bay_count",
			"deployed_count", "drones",
		},
		"load_drone":          {"bay_capacity", "bay_count", "drone_id", "drone_type", "hull", "message", "status"},
		"unload_drone":        {"drone_id", "item_id", "message"},
		"recall_drone":        {"message", "recalled", "skipped"},
		"upload_drone_script": {"drone_id", "message", "script_len"},
	})
}
