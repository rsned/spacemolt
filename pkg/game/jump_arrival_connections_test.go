package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// A jump arrival renames the current system but the connection list it carries
// still belongs to the system just departed. Leaving that list in place is how
// the knowledge base grew 38 phantom connection rows: a capture running before
// the next get_system reply writes the OLD system's neighbours under the NEW
// system's id, and the connections upsert never deletes, so every one is
// permanent.
//
// Observed 2026-09-14: first_step carries saiph's neighbour list, iron_reach
// carries first_step's, and the_crucible carries gold_run's. Each is a verbatim
// copy at identical distances — donor-attributable, so none is a wormhole.
func TestJumpArrival_ClearsStaleConnections(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp protocol.Response
	}{
		{"jumped action", protocol.Response{
			Type: protocol.TypeOK,
			Payload: map[string]any{
				"action": "jumped", "system_id": "first_step", "system": "First Step",
			},
		}},
		{"jump action_result", protocol.Response{
			Type: protocol.TypeActionResult,
			Payload: map[string]any{
				"command": "jump",
				"result": map[string]any{
					"action":    "arrived",
					"system_id": "first_step", "system": "First Step",
				},
			},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient()
			c.latestRawJSON = map[string][]byte{}
			c.state.System = SystemData{
				ID:   "saiph",
				Name: "Saiph",
				Connections: []ConnectionInfo{
					{SystemID: "alpheratz", Distance: 604},
					{SystemID: "gsc_0010", Distance: 519},
				},
				POIs: []POI{{ID: "saiph_station"}},
			}

			c.handleResponse(tc.resp)

			if c.state.System.ID != "first_step" {
				t.Fatalf("System.ID = %q, want first_step", c.state.System.ID)
			}
			if len(c.state.System.Connections) != 0 {
				t.Errorf("arrived at first_step still holding saiph's connections: %+v",
					c.state.System.Connections)
			}
			if len(c.state.System.POIs) != 0 {
				t.Errorf("arrived at first_step still holding saiph's POIs: %+v",
					c.state.System.POIs)
			}
		})
	}
}

// Re-reporting the SAME system must not discard data we already hold: a
// duplicate arrival frame is not a reason to blank the system.
func TestJumpArrival_SameSystemKeepsConnections(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = map[string][]byte{}
	c.state.System = SystemData{
		ID:          "saiph",
		Name:        "Saiph",
		Connections: []ConnectionInfo{{SystemID: "alpheratz", Distance: 604}},
	}

	c.handleResponse(protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"action": "jumped", "system_id": "saiph", "system": "Saiph"},
	})

	if len(c.state.System.Connections) != 1 {
		t.Errorf("a same-system arrival discarded the connection list: %+v",
			c.state.System.Connections)
	}
}

// The stronghold flag is the departed system's too, and it is the one field the
// knowledge base cannot recover from: is_stronghold is upserted with MAX, so a
// single capture of a chimera state marks the new system a stronghold for
// good. Observed 2026-10-29: Glenhaven, the only lane out of Alhena, kept being
// re-flagged after a manual reset, and the autopilot stronghold gate then
// refused every route through it.
func TestJumpArrival_ClearsStaleStrongholdFlag(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = map[string][]byte{}
	c.state.System = SystemData{ID: "alhena", Name: "Alhena", IsStronghold: true}

	c.handleResponse(protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"action": "jumped", "system_id": "glenhaven", "system": "Glenhaven"},
	})

	if c.state.System.IsStronghold {
		t.Error("arrived at glenhaven still flagged a stronghold from alhena")
	}
}

// A duplicate arrival frame for a stronghold must not clear its flag.
func TestJumpArrival_SameSystemKeepsStrongholdFlag(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = map[string][]byte{}
	c.state.System = SystemData{ID: "alhena", Name: "Alhena", IsStronghold: true}

	c.handleResponse(protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"action": "jumped", "system_id": "alhena", "system": "Alhena"},
	})

	if !c.state.System.IsStronghold {
		t.Error("a same-system arrival cleared alhena's stronghold flag")
	}
}

// A Pathfinder drift ends with pathfinder_arrival naming the system and the
// POI the ship came out at; the status line kept the departure POI
// (grand_exchange at Haven) when the client ignored it (2026-11-11).
func TestPathfinderArrivalUpdatesLocation(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = map[string][]byte{}
	c.state.System = SystemData{ID: "haven", Name: "Haven"}
	c.state.CurrentPOI = "grand_exchange"

	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{"action": "pathfinder_jump", "bearing": 51.0}})
	if !c.state.Traveling {
		t.Fatal("pathfinder_jump did not mark the ship traveling")
	}
	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{
		"action": "pathfinder_arrival", "from_system": "Haven", "poi": "warehouse_world",
		"system": "Market Prime", "system_id": "market_prime", "piloting_xp": 12.0, "navigation_xp": 3.0,
	}})
	if c.state.Traveling {
		t.Error("still traveling after pathfinder_arrival")
	}
	if c.state.CurrentPOI != "warehouse_world" {
		t.Errorf("CurrentPOI = %q, want warehouse_world", c.state.CurrentPOI)
	}
	if c.state.System.ID != "market_prime" {
		t.Errorf("System.ID = %q, want market_prime", c.state.System.ID)
	}
}

// get_system for a different, lane-less system must replace the lane list,
// not keep the previous system's: The Sanctum (Pathfinder-only) was saved with
// Nexus Prime's two lanes, Node Alpha 353 and Node Beta 585 (2026-11-11).
func TestGetSystemLanelessSystemClearsPreviousLanes(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = map[string][]byte{}
	c.state.System = SystemData{
		ID: "nexus_prime", Name: "Nexus Prime",
		Connections: []ConnectionInfo{{SystemID: "node_alpha", Distance: 353}, {SystemID: "node_beta", Distance: 585}},
		POIs:        []POI{{ID: "nexus_station"}},
	}

	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{
		"action": "get_system",
		"system": map[string]any{
			"id": "the_sanctum", "name": "The Sanctum", "connections": []any{},
			"pois": []any{map[string]any{"id": "receiver_housing", "name": "Receiver Housing", "type": "relic"}},
		},
	}})

	if c.state.System.ID != "the_sanctum" {
		t.Fatalf("System.ID = %q, want the_sanctum", c.state.System.ID)
	}
	if len(c.state.System.Connections) != 0 {
		t.Errorf("The Sanctum kept Nexus Prime's lanes: %+v", c.state.System.Connections)
	}
	if len(c.state.System.POIs) != 1 || c.state.System.POIs[0].ID != "receiver_housing" {
		t.Errorf("POIs = %+v, want only receiver_housing", c.state.System.POIs)
	}
}

// A same-system partial reply that omits the lanes keeps the known lanes.
func TestGetSystemSameSystemKeepsLanesWhenOmitted(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = map[string][]byte{}
	c.state.System = SystemData{ID: "nexus_prime", Name: "Nexus Prime",
		Connections: []ConnectionInfo{{SystemID: "node_alpha", Distance: 353}}}

	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{
		"action": "get_system",
		"system": map[string]any{"id": "nexus_prime", "name": "Nexus Prime"},
	}})

	if len(c.state.System.Connections) != 1 {
		t.Errorf("same-system partial reply dropped the lanes: %+v", c.state.System.Connections)
	}
}

// v0.692.0 parent_id on a get_system POI (Deep Range) survives conversion.
func TestGetSystemKeepsPOIParent(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = map[string][]byte{}
	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{
		"action": "get_system",
		"system": map[string]any{"id": "deep_range", "name": "Deep Range", "connections": []any{},
			"pois": []any{map[string]any{"id": "deep_range_outpost", "type": "station", "parent_id": "deep_range_mineral_fields"}}},
	}})
	if len(c.state.System.POIs) != 1 || c.state.System.POIs[0].ParentID != "deep_range_mineral_fields" {
		t.Fatalf("POIs = %+v", c.state.System.POIs)
	}
}
