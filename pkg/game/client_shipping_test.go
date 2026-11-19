package game

import (
	"io"
	"log"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestStoreRawJSONShippingKeysByAction verifies that /shipping's type:ok
// replies are namespaced under "shipping_<action>" so Task 3's methods (and
// Sub-project B) can retrieve them via GetRawJSON without colliding with
// other commands' generic-looking action verbs (list/get/accept/post, etc.).
func TestStoreRawJSONShippingKeysByAction(t *testing.T) {
	c := &Client{
		latestRawJSON: map[string][]byte{},
		debugLogger:   log.New(io.Discard, "", 0),
	}
	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action": "list",
			"total":  float64(1),
		},
	})
	if raw := c.GetRawJSON("shipping_list"); len(raw) == 0 {
		t.Fatalf("shipping_list not stored")
	}
	c.storeRawJSON(protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"action": "profile", "debt_blocks_acceptance": false},
	})
	if raw := c.GetRawJSON("shipping_profile"); len(raw) == 0 {
		t.Fatalf("shipping_profile not stored")
	}
}

// TestStoreRawJSONShippingListAll: v0.724.0's action=list_all (every charted
// station's board) is keyed apart from the docked board, so a worker reading
// both in one pass cannot mistake one for the other.
func TestStoreRawJSONShippingListAll(t *testing.T) {
	c := &Client{
		latestRawJSON: map[string][]byte{},
		debugLogger:   log.New(io.Discard, "", 0),
	}
	c.storeRawJSON(protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"action": "list_all", "total": float64(2)},
	})
	if raw := c.GetRawJSON("shipping_list_all"); len(raw) == 0 {
		t.Fatalf("shipping_list_all not stored")
	}
	if raw := c.GetRawJSON("shipping_list"); len(raw) != 0 {
		t.Fatalf("list_all reply leaked into shipping_list")
	}
}
