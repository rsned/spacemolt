package main

import (
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func wreck(id, typ string, salvage int, towedBy string, cargo ...serverapi.CargoItem) serverapi.Wreck {
	return serverapi.Wreck{ID: id, Type: typ, SalvageValue: salvage, TowedByPlayerID: towedBy, Cargo: cargo}
}

// buildLootPlan turns a get_wrecks reply into the ordered list of loot_wreck
// calls: richest wreck first, every cargo stack, skipping wrecks with no
// cargo and wrecks towed by someone else (their loot is in the tower's
// custody). Modules are NOT in the plan — they need salvage, not loot.
func TestBuildLootPlan(t *testing.T) {
	resp := serverapi.GetWrecksResponse{Wrecks: []serverapi.Wreck{
		wreck("poor", "ship", 100, "", serverapi.CargoItem{ItemID: "scrap", Quantity: 2}),
		wreck("rich", "ship", 900, "",
			serverapi.CargoItem{ItemID: "gold_ore", Quantity: 10},
			serverapi.CargoItem{ItemID: "titanium_ore", Quantity: 4}),
		wreck("empty", "ship", 500, ""),
		wreck("towed", "ship", 700, "someone_else", serverapi.CargoItem{ItemID: "gem", Quantity: 1}),
		wreck("mine", "ship", 300, "me", serverapi.CargoItem{ItemID: "ice", Quantity: 5}),
		wreck("can", "jettison", 0, "", serverapi.CargoItem{ItemID: "fuel_cell", Quantity: 3}),
	}}
	plan, skipped := buildLootPlan(resp, "", "me")
	want := []lootTask{
		{WreckID: "rich", ItemID: "gold_ore", Qty: 10},
		{WreckID: "rich", ItemID: "titanium_ore", Qty: 4},
		{WreckID: "mine", ItemID: "ice", Qty: 5},
		{WreckID: "poor", ItemID: "scrap", Qty: 2},
		{WreckID: "can", ItemID: "fuel_cell", Qty: 3},
	}
	if len(plan) != len(want) {
		t.Fatalf("plan = %+v, want %+v", plan, want)
	}
	for i := range want {
		if plan[i] != want[i] {
			t.Errorf("plan[%d] = %+v, want %+v", i, plan[i], want[i])
		}
	}
	if skipped != 1 { // only the foreign-towed wreck counts as skipped
		t.Errorf("skipped = %d, want 1", skipped)
	}
}

// A wreck-id argument narrows the plan to that wreck alone — even a
// foreign-towed one (explicit id = the operator knows better).
func TestBuildLootPlanSingleWreck(t *testing.T) {
	resp := serverapi.GetWrecksResponse{Wrecks: []serverapi.Wreck{
		wreck("a", "ship", 900, "", serverapi.CargoItem{ItemID: "gold_ore", Quantity: 10}),
		wreck("b", "ship", 100, "someone_else", serverapi.CargoItem{ItemID: "gem", Quantity: 1}),
	}}
	plan, skipped := buildLootPlan(resp, "b", "me")
	if len(plan) != 1 || plan[0].WreckID != "b" || plan[0].ItemID != "gem" {
		t.Errorf("plan = %+v", plan)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
}

// classifyLootErr decides the loop's reaction to a failed loot: a full hold
// ends the whole run, a still-pending action asks for a retry, anything else
// skips just that stack.
func TestClassifyLootErr(t *testing.T) {
	cases := []struct {
		err  error
		want lootErrKind
	}{
		{errors.New("Cargo hold is full"), lootStop},
		{errors.New("Not enough cargo space for that"), lootStop},
		{errors.New("You already have an action pending"), lootRetry},
		{errors.New("Action 'loot_wreck' already queued this tick"), lootRetry},
		{errors.New("Wreck not found"), lootSkip},
		{errors.New("Item not present in wreck"), lootSkip},
	}
	for _, c := range cases {
		if got := classifyLootErr(c.err); got != c.want {
			t.Errorf("classifyLootErr(%q) = %v, want %v", c.err, got, c.want)
		}
	}
}

// lootOutcome reads the request's true result from the action_result frame
// that lands a tick after the pending-ok (live craftsman-1 capture,
// 2026-08-30 19:04): {"command":"loot_wreck","result":{"action":"loot_wreck",
// "item_id":"iron_ore","quantity":11,"wreck_empty":false,...},"tick":...}.
// matched is false when the cached frame is for a different stack (stale) or
// unparseable — the loop then reports the submit-level ok only.
func TestLootOutcome(t *testing.T) {
	raw := []byte(`{"command":"loot_wreck","result":{"action":"loot_wreck","item_id":"iron_ore","quantity":11,"wreck_empty":false,"xp_gained":{"salvaging":11}},"tick":1755886}`)
	qty, empty, xp, matched := lootOutcome(raw, "iron_ore")
	if !matched || qty != 11 || empty || xp["salvaging"] != 11 {
		t.Errorf("outcome = qty %v empty %v xp %v matched %v", qty, empty, xp, matched)
	}

	// The frame also arrives flat (no envelope) on some paths.
	flat := []byte(`{"action":"loot_wreck","item_id":"gold_ore","quantity":3,"wreck_empty":true}`)
	qty, empty, _, matched = lootOutcome(flat, "gold_ore")
	if !matched || qty != 3 || !empty {
		t.Errorf("flat outcome = qty %v empty %v matched %v", qty, empty, matched)
	}

	// A stale frame for a different item must not be reported as ours.
	if _, _, _, matched = lootOutcome(raw, "gold_ore"); matched {
		t.Error("stale frame matched the wrong stack")
	}
	if _, _, _, matched = lootOutcome(nil, "iron_ore"); matched {
		t.Error("empty cache matched")
	}
	if _, _, _, matched = lootOutcome([]byte("junk"), "iron_ore"); matched {
		t.Error("garbage matched")
	}
}
