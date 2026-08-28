package game

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// The server loots the WHOLE wreck -- every cargo item and every module -- only
// when item_id and module_id are both absent. Sending item_id:"" names an empty
// item instead, so the loot-everything path never fired. runner.go calls
// LootWreck(ctx, target, "", 0) for exactly that intent.
//
// It matters beyond correctness: loot_wreck is a mutation at 1 per tick, so
// looting a wreck item-by-item costs a tick each where one loot-all costs one.
func TestLootWreck_OmitsItemKeysWhenLootingEverything(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.LootWreck(ctx, "wreck-1", "", 0) }()

	sent := <-sendCh
	if sent.Type != "loot_wreck" {
		t.Fatalf("sent type = %q", sent.Type)
	}
	if got := sent.Payload["wreck_id"]; got != "wreck-1" {
		t.Errorf("wreck_id = %v", got)
	}
	if _, ok := sent.Payload["item_id"]; ok {
		t.Error(`item_id sent when empty; the server loots everything only when the key is ABSENT`)
	}
	if _, ok := sent.Payload["quantity"]; ok {
		t.Error("quantity sent without an item_id; it is only meaningful alongside one")
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "loot_wreck"},
	})
	if err := <-done; err != nil {
		t.Fatalf("LootWreck: %v", err)
	}
}

// wreck_id is optional while towing -- omitting it defaults to the towed wreck.
// Sending an empty string instead names a wreck that does not exist.
func TestLootWreck_OmitsWreckIDWhenUnset(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.LootWreck(ctx, "", "", 0) }()

	sent := <-sendCh
	if _, ok := sent.Payload["wreck_id"]; ok {
		t.Error("wreck_id sent when empty; omitting it defaults to the towed wreck")
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "loot_wreck"},
	})
	if err := <-done; err != nil {
		t.Fatalf("LootWreck: %v", err)
	}
}

// A named item still sends item_id and its quantity.
func TestLootWreck_SendsNamedItem(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.LootWreck(ctx, "wreck-1", "iron_ore", 12) }()

	sent := <-sendCh
	if got := sent.Payload["item_id"]; got != "iron_ore" {
		t.Errorf("item_id = %v, want iron_ore", got)
	}
	if got := sent.Payload["quantity"]; got != 12.0 {
		t.Errorf("quantity = %v, want 12", got)
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "loot_wreck"},
	})
	if err := <-done; err != nil {
		t.Fatalf("LootWreck: %v", err)
	}
}
