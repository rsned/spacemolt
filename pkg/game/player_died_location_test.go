package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// Death relocates the ship — explorer-8 died at Fuyue on 2026-09-14 and
// respawned at war_citadel in krynn — but the handler only cleared combat
// state and never touched location. Three symptoms followed, all from the one
// cause: the status line kept reporting fuyue_i, auto-explore kept touring
// "Fuyue" forever, and its output interleaved with the operator's next command.
//
// This is the same defect class as a jump arrival carrying the departed
// system's connections: a location change that leaves stale per-system data
// behind. Clearing is the honest fix — after a death we KNOW System is wrong,
// and every reader calls GetSystem before using it.
func TestPlayerDied_ClearsStaleLocation(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = map[string][]byte{}
	c.state.System = SystemData{
		ID:          "fuyue",
		Name:        "Fuyue",
		Connections: []ConnectionInfo{{SystemID: "hr_8832", Distance: 100}},
		POIs:        []POI{{ID: "fuyue_i"}},
		ShipPOI:     "fuyue_i",
	}
	c.state.CurrentSystem = "fuyue"
	c.state.CurrentPOI = "fuyue_i"
	c.state.Traveling = true
	c.state.Doc = true

	c.handleResponse(protocol.Response{
		Type: protocol.TypePlayerDied,
		Payload: map[string]any{
			"message":         "You were destroyed by pirates",
			"cause":           "pirate",
			"respawn_base":    "war_citadel",
			"wreck_system_id": "fuyue",
			"wreck_poi_id":    "fuyue_i",
		},
	})

	if c.state.System.ID != "" || c.state.System.Name != "" {
		t.Errorf("System still reads %q/%q after death", c.state.System.ID, c.state.System.Name)
	}
	if len(c.state.System.Connections) != 0 || len(c.state.System.POIs) != 0 {
		t.Error("stale connections/POIs survived the death")
	}
	if c.state.CurrentPOI != "" || c.state.System.ShipPOI != "" {
		t.Errorf("CurrentPOI still reads %q", c.state.CurrentPOI)
	}
	if c.state.Traveling {
		t.Error("still marked as traveling after death")
	}
	if !c.state.Died {
		t.Error("Died not set — callers cannot tell a death from an ordinary error")
	}
}

// The wreck location is the only record of where the lost cargo went, and the
// server now sends it. Dropping it loses the one chance to go and recover it.
func TestPlayerDied_RecordsWreckLocation(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = map[string][]byte{}

	c.handleResponse(protocol.Response{
		Type: protocol.TypePlayerDied,
		Payload: map[string]any{
			"wreck_system_id":   "fuyue",
			"wreck_system_name": "Fuyue",
			"wreck_poi_id":      "fuyue_i",
			"wreck_poi_name":    "Fuyue I",
			"respawn_base":      "war_citadel",
		},
	})

	if c.state.LastWreck.SystemID != "fuyue" || c.state.LastWreck.POIID != "fuyue_i" {
		t.Errorf("wreck location not recorded: %+v", c.state.LastWreck)
	}
	if c.state.LastWreck.RespawnBase != "war_citadel" {
		t.Errorf("respawn base not recorded: %+v", c.state.LastWreck)
	}
}
