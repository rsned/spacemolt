package game

import (
	"io"
	"log"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func newTagTestClient() *Client {
	return &Client{
		state:         &State{},
		latestRawJSON: make(map[string][]byte),
		debugLogger:   log.New(io.Discard, "", 0),
	}
}

// TestStoreRawJSON_CapturesOwnFactionTag verifies that a faction_info payload
// describing the worker's own membership lands the readable tag (and, when the
// player has none yet, the faction id) into player state. The tag is not present
// in any player payload, so this handler is the sole source for it.
func TestStoreRawJSON_CapturesOwnFactionTag(t *testing.T) {
	c := newTagTestClient()

	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"is_member": true,
			"tag":       "YSMT",
			"id":        "fac-hash-123",
			"leader_id": "player-7",
		},
	})

	if got := c.state.Player.FactionTag; got != "YSMT" {
		t.Fatalf("FactionTag = %q, want %q", got, "YSMT")
	}
	if got := c.state.Player.FactionID; got != "fac-hash-123" {
		t.Fatalf("FactionID = %q, want %q (should fill when empty)", got, "fac-hash-123")
	}
}

// TestStoreRawJSON_FactionTagDoesNotClobberID verifies that an existing faction
// id is preserved (the faction_info payload only fills it when empty) while the
// tag is still refreshed.
func TestStoreRawJSON_FactionTagDoesNotClobberID(t *testing.T) {
	c := newTagTestClient()
	c.state.Player.FactionID = "original-id"

	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"is_member": true,
			"tag":       "YSMT",
			"id":        "different-id",
		},
	})

	if got := c.state.Player.FactionID; got != "original-id" {
		t.Fatalf("FactionID = %q, want it preserved as %q", got, "original-id")
	}
	if got := c.state.Player.FactionTag; got != "YSMT" {
		t.Fatalf("FactionTag = %q, want %q", got, "YSMT")
	}
}
