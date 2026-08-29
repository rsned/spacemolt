package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Each observation must say WHEN in game time and WHO saw it, or the
// sightings cannot be lined up against a battle log's ticks.
func TestNotifyPlayers_StampsTickAndObserver(t *testing.T) {
	c := newHandleResponseTestClient("sys_a")
	c.state.CurrentTick = 1731534
	c.state.Player.ID = "observer_pid"
	got := captureObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"agents":    []serverapi.NearbyPlayer{{PlayerID: "p1", Username: "u1"}},
		"system_id": "sys_a",
		"count":     1,
	})
	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: payload})

	if len(*got) != 1 {
		t.Fatalf("observer got %d, want 1", len(*got))
	}
	if (*got)[0].Tick != 1731534 {
		t.Errorf("Tick=%d, want 1731534", (*got)[0].Tick)
	}
	if (*got)[0].ObserverID != "observer_pid" {
		t.Errorf("ObserverID=%q, want observer_pid", (*got)[0].ObserverID)
	}
}
