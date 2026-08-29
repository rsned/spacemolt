package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// The server announces a kill we scored with a player_kill push carrying the
// wreck id and whether it holds loot. Decoding it is what lets a worker (or
// the operator) go and loot the wreck instead of learning about it from a
// "SERVER API CHANGE" log line.
func TestHandleResponse_PlayerKillRecordsWreck(t *testing.T) {
	c := newHandleResponseTestClient("tau_bootis")
	c.handleResponse(protocol.Response{Type: protocol.TypePlayerKill, Payload: payloadMarshal(t, map[string]any{
		"victim":            "MoltenOne",
		"wreck_id":          "cc2128e841cba61c296423dd372e721a",
		"wreck_has_cargo":   true,
		"wreck_has_modules": true,
	})})

	got := c.GetState().LastKill
	if got.Victim != "MoltenOne" || got.WreckID != "cc2128e841cba61c296423dd372e721a" ||
		!got.WreckHasCargo || !got.WreckHasModules {
		t.Errorf("LastKill = %+v", got)
	}
}
