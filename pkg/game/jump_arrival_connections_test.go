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
