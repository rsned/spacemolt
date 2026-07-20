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
