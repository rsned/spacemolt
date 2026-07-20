package game

import (
	"encoding/json"
	"io"
	"log"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// shippingAcceptActionResult is the live v0.531.4 wire shape of a tick-deferred
// shipping accept: the contract body sits one level down under "result", and
// there is no top-level "action". Captured during the 2026-07-19 play_as smoke.
func shippingAcceptActionResult() protocol.Response {
	return protocol.Response{
		Type: protocol.TypeActionResult,
		Payload: map[string]any{
			"command": "shipping",
			"tick":    float64(1200),
			"result": map[string]any{
				"action": "accept",
				"contract": map[string]any{
					"id":                  "ship_abc123",
					"package_id":          "ed9edd4346ed071f3c890ca73f9456b2",
					"origin_base_id":      "treasure_cache_trading_post",
					"destination_base_id": "sol_central",
					"status":              "in_transit",
					"service_level":       "standard",
					"accepted_tick":       float64(1200),
					"target_tick":         float64(1290),
					"deadline_tick":       float64(1380),
					"base_reward":         float64(100),
					"route_hops":          float64(3),
				},
			},
		},
	}
}

// A wrapped shipping mutation must be cached under "shipping_<action>" with the
// INNER result body, so callers decode the serverapi struct directly. Before the
// fix this key was absent entirely and callers silently saw a zero contract.
func TestStoreRawJSONCachesWrappedShippingMutation(t *testing.T) {
	c := &Client{
		latestRawJSON: map[string][]byte{},
		debugLogger:   log.New(io.Discard, "", 0),
	}
	c.storeRawJSON(shippingAcceptActionResult())

	raw := c.GetRawJSON("shipping_accept")
	if len(raw) == 0 {
		t.Fatal("shipping_accept must be cached from the action_result frame; got nothing")
	}
	var resp serverapi.ShippingContractResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode shipping_accept: %v", err)
	}
	if resp.Contract.ID != "ship_abc123" {
		t.Fatalf("contract id: want ship_abc123, got %q", resp.Contract.ID)
	}
	if resp.Contract.DeadlineTick != 1380 {
		t.Fatalf("deadline_tick: want 1380, got %d", resp.Contract.DeadlineTick)
	}
	if resp.Contract.RouteHops != 3 {
		t.Fatalf("route_hops: want 3, got %d", resp.Contract.RouteHops)
	}
}

// A synchronous read (list/get/profile/track) carries a top-level action and must
// keep working exactly as before — the fix must not regress the read path.
func TestStoreRawJSONCachesSynchronousShippingRead(t *testing.T) {
	c := &Client{
		latestRawJSON: map[string][]byte{},
		debugLogger:   log.New(io.Discard, "", 0),
	}
	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action":    "list",
			"shipments": []any{},
			"total":     float64(0),
		},
	})
	if len(c.GetRawJSON("shipping_list")) == 0 {
		t.Fatal("shipping_list must still be cached from the synchronous top-level-action reply")
	}
}
