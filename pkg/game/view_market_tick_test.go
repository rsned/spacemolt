package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestViewMarketAdvancesCurrentTick guards that a view_market OK frame carrying
// the server's current_tick advances the client's internal tick tracking via
// the centralized updateTickFromPayload path in handleResponse. The server
// added current_tick to view_market responses; this confirms it's consumed.
func TestViewMarketAdvancesCurrentTick(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = make(map[string][]byte) // handleResponse → storeRawJSON writes here
	c.state.CurrentTick = 100

	c.handleResponse(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action":       "view_market",
			"base_id":      "test_station",
			"current_tick": float64(142),
			"items":        []any{},
		},
	})

	if got := c.GetState().CurrentTick; got != 142 {
		t.Errorf("CurrentTick = %d, want 142 (view_market current_tick should advance the clock)", got)
	}
}

// TestViewMarketTickSnapsToServer guards the OK-frame convention: the server's
// current_tick is authoritative, so a view_market reply snaps the local clock to
// it even when the optimistic local tick has drifted ahead. This is the same
// drift-correction the clock-sync path performs, not a regression.
func TestViewMarketTickSnapsToServer(t *testing.T) {
	c := newTestClient()
	c.latestRawJSON = make(map[string][]byte) // handleResponse → storeRawJSON writes here
	c.state.CurrentTick = 200                 // local clock optimistically ahead of the server

	c.handleResponse(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action":       "view_market",
			"current_tick": float64(150),
			"items":        []any{},
		},
	})

	if got := c.GetState().CurrentTick; got != 150 {
		t.Errorf("CurrentTick = %d, want 150 (authoritative view_market current_tick should snap the clock)", got)
	}
}
