package assets

import (
	"context"
	"testing"
	"time"
)

// liveStorageDataBot is the verbatim view_storage frame captured from databot
// on 2026-08-06 while docked at confederacy_central_command. Its ten quantities
// sum to exactly 920, matching the hint's stated total -- which is what makes
// the total usable as a truncation detector.
const liveStorageDataBot = `{"action":"view_storage","base_id":"confederacy_central_command","hint":"920 items in storage at confederacy_central_command","items":[{"item_id":"mining_laser_i","name":"Mining Laser I","quantity":1,"size":10},{"item_id":"iron_ore","name":"Iron Ore","quantity":23,"size":1},{"item_id":"titanium_alloy","name":"Titanium Alloy","quantity":3,"size":1},{"item_id":"steel_plate","name":"Steel Plate","quantity":328,"size":1},{"item_id":"sol_alloy_ore","name":"Sol Alloy Ore","quantity":216,"size":2},{"item_id":"copper_ore","name":"Copper Ore","quantity":193,"size":1},{"item_id":"titanium_ore","name":"Titanium Ore","quantity":99,"size":1},{"item_id":"nickel_ore","name":"Nickel Ore","quantity":5,"size":1},{"item_id":"antimatter_containment_cell","name":"Antimatter Containment Cell","quantity":12,"size":3},{"item_id":"nickel_billet","name":"Nickel Billet","quantity":40,"size":1}],"ships":[{"cargo_used":0,"class_id":"catalogue","class_name":"Catalogue","modules":2,"ship_id":"c63763d53539dd8cdde94211d64916d9"}]}`

// liveStorageEmptyRemote is the same agent querying a base where it holds
// nothing. The hint still names the OTHER base -- proof the hint is
// agent-global rather than per-base.
const liveStorageEmptyRemote = `{"action":"view_storage","base_id":"grand_exchange_station","hint":"920 items in storage at confederacy_central_command","items":[],"ships":[]}`

func TestStorageFromLivePayload(t *testing.T) {
	base, hint, ok, err := StorageFrom([]byte(liveStorageDataBot))
	if err != nil || !ok {
		t.Fatalf("StorageFrom = ok %v err %v, want ok=true", ok, err)
	}
	if base.BaseID != "confederacy_central_command" {
		t.Errorf("BaseID = %q", base.BaseID)
	}
	if len(base.Items) != 10 {
		t.Fatalf("items = %d, want 10", len(base.Items))
	}
	var sum float64
	for _, it := range base.Items {
		sum += it.Quantity
	}
	if sum != 920 {
		t.Errorf("quantity sum = %v, want 920 (must equal the hint total)", sum)
	}
	if hint != "920 items in storage at confederacy_central_command" {
		t.Errorf("hint = %q", hint)
	}
}

// TestStorageFromEmptyRemoteStillReportsGlobalHint pins that a query against a
// base holding nothing still decodes cleanly, and the hint it carries still
// names the OTHER base -- proof the hint is agent-global, not per-base.
func TestStorageFromEmptyRemoteStillReportsGlobalHint(t *testing.T) {
	base, hint, ok, err := StorageFrom([]byte(liveStorageEmptyRemote))
	if err != nil || !ok {
		t.Fatalf("StorageFrom = ok %v err %v, want ok=true", ok, err)
	}
	if base.BaseID != "grand_exchange_station" {
		t.Errorf("BaseID = %q", base.BaseID)
	}
	if len(base.Items) != 0 {
		t.Errorf("items = %d, want 0", len(base.Items))
	}
	if hint != "920 items in storage at confederacy_central_command" {
		t.Errorf("hint = %q, want it to still name the OTHER base", hint)
	}
}

func TestStorageFromEmptyRawIsNotCaptured(t *testing.T) {
	if _, _, ok, err := StorageFrom(nil); ok || err != nil {
		t.Errorf("empty raw = ok %v err %v, want ok=false err=nil", ok, err)
	}
}

// TestReplaceStorageDropsVanishedItemsAndBases pins BOTH deletion grains. An
// item sold at a base must not linger, and a base emptied entirely must not
// linger either -- phantom stock is exactly what would poison the "what can we
// source for free" query this ledger exists to answer.
func TestReplaceStorageDropsVanishedItemsAndBases(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	first := []StorageBase{
		{BaseID: "A", Credits: 100, Items: []StorageItem{
			{ItemID: "x", Quantity: 5}, {ItemID: "y", Quantity: 7},
		}},
		{BaseID: "B", Items: []StorageItem{{ItemID: "z", Quantity: 1}}},
	}
	if err := st.ReplaceStorage(ctx, "p1", first, now); err != nil {
		t.Fatalf("first ReplaceStorage: %v", err)
	}

	second := []StorageBase{
		{BaseID: "A", Credits: 100, Items: []StorageItem{{ItemID: "x", Quantity: 5}}},
	}
	if err := st.ReplaceStorage(ctx, "p1", second, now.Add(time.Hour)); err != nil {
		t.Fatalf("second ReplaceStorage: %v", err)
	}

	var items, bases int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_storage_items WHERE player_id='p1'`).Scan(&items); err != nil {
		t.Fatalf("count items: %v", err)
	}
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_storage WHERE player_id='p1'`).Scan(&bases); err != nil {
		t.Fatalf("count bases: %v", err)
	}
	if items != 1 {
		t.Errorf("agent_storage_items = %d, want 1 (y and z must be deleted)", items)
	}
	if bases != 1 {
		t.Errorf("agent_storage = %d, want 1 (base B must be deleted)", bases)
	}
}

// TestReplaceStorageEmptySetClearsEverything pins that zero storage is
// LEGITIMATE -- the inverse of the hull rule. An agent genuinely can sell
// everything, so an empty (successful) sweep must delete. Protection against
// "empty because the call failed" lives in CaptureStorage, not here.
func TestReplaceStorageEmptySetClearsEverything(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceStorage(ctx, "p1", []StorageBase{
		{BaseID: "A", Items: []StorageItem{{ItemID: "x", Quantity: 5}}},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.ReplaceStorage(ctx, "p1", nil, now.Add(time.Hour)); err != nil {
		t.Fatalf("clear: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_storage WHERE player_id='p1'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("agent_storage = %d, want 0", n)
	}
}
