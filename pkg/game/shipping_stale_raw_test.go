package game

import (
	"context"
	"testing"
)

// A shipping reply is cached under a session-lifetime key that nothing
// invalidates per-command, so a call that returns without its own reply landing
// would leave the PREVIOUS call's body readable under the same key. That is far
// worse than an empty slot: a stale body decodes cleanly into a real contract
// with a real ID, so the `c.ID == ""` guard written for this class cannot fire,
// and the worker acts on a contract it already settled while the one it just
// accepted rides untracked to a breach.
//
// Clearing before the request is issued makes the failure mode empty-not-stale.
// The canceled context here fails the submit deliberately: it proves the clear
// happens BEFORE the request goes out, which is the property that holds even
// when no reply ever arrives.
func TestShippingClearsStaleRawSlotBeforeIssuing(t *testing.T) {
	c := NewClient("wss://test.example.com", "u", "p", nil)
	c.latestRawJSON["shipping_accept"] = []byte(`{"action":"accept","contract":{"id":"ship_OLD"}}`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = c.ShippingAccept(ctx, "ship_NEW", "player")

	if raw := c.GetRawJSON("shipping_accept"); len(raw) != 0 {
		t.Fatalf("a new accept must not leave the previous contract readable, got %s", raw)
	}
}

// The clear must be scoped to the action being issued: a shipping call must not
// wipe another action's cached body (freightCandidate reads shipping_profile and
// shipping_list across separate calls in one pass).
func TestShippingClearsOnlyItsOwnRawSlot(t *testing.T) {
	c := NewClient("wss://test.example.com", "u", "p", nil)
	c.latestRawJSON["shipping_profile"] = []byte(`{"action":"profile"}`)
	c.latestRawJSON["shipping_list"] = []byte(`{"action":"list"}`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = c.ShippingAccept(ctx, "ship_NEW", "player")

	if len(c.GetRawJSON("shipping_profile")) == 0 {
		t.Fatal("an accept must not clear the cached profile body")
	}
	if len(c.GetRawJSON("shipping_list")) == 0 {
		t.Fatal("an accept must not clear the cached board body")
	}
}
