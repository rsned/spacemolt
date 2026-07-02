package main

import (
	"testing"
	"time"
)

func TestMergeHeld(t *testing.T) {
	cargo := []storageItem{
		{ItemID: "oxygen_gas", Name: "Oxygen Gas", Quantity: 475},
		{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 10},
	}
	storage := []storageItem{
		{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 5},
		{ItemID: "empty", Name: "Empty", Quantity: 0},
	}
	got := mergeHeld(cargo, storage)

	byID := map[string]heldItem{}
	for _, h := range got {
		byID[h.ItemID] = h
	}
	if len(byID) != 2 {
		t.Fatalf("expected 2 held items (zero-qty dropped), got %d: %+v", len(byID), got)
	}
	if h := byID["oxygen_gas"]; h.Cargo != 475 || h.Storage != 0 || h.total() != 475 {
		t.Errorf("oxygen_gas: got %+v", h)
	}
	if h := byID["iron_ore"]; h.Cargo != 10 || h.Storage != 5 || h.total() != 15 {
		t.Errorf("iron_ore: want cargo 10 storage 5, got %+v", h)
	}
	if _, ok := byID["empty"]; ok {
		t.Errorf("zero-quantity item should have been dropped")
	}
}

func TestBuildUnloadPlan(t *testing.T) {
	now := time.Date(2026, 7, 1, 22, 0, 0, 0, time.UTC)
	capFresh := now.Add(-170 * time.Second) // 17 ticks at 10s/tick

	held := []heldItem{
		{ItemID: "oxygen_gas", Name: "Oxygen Gas", Cargo: 475},
	}
	rungs := map[string][]unloadRung{
		"oxygen_gas": {
			// blood_forge: two rungs; best price 193. 475 held fills 200@193 + 275@180.
			{StationID: "blood_forge", StationName: "Blood Forge Smelting", SystemName: "Blood Forge", Price: 193, Qty: 200, CapturedAt: capFresh},
			{StationID: "blood_forge", StationName: "Blood Forge Smelting", SystemName: "Blood Forge", Price: 180, Qty: 500, CapturedAt: capFresh},
			// nova_terra: single rung 105, plenty of depth.
			{StationID: "nova_terra", StationName: "Nova Terra Central", SystemName: "Nova Terra", Price: 105, Qty: 1000, CapturedAt: capFresh},
			// tiny: best price but trivial depth -> low proceeds, should rank last.
			{StationID: "tiny", StationName: "Tiny", SystemName: "Tiny", Price: 300, Qty: 2, CapturedAt: capFresh},
		},
	}

	plan := buildUnloadPlan(held, rungs, now, unloadDefaultTopN, 0)
	if len(plan.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(plan.Items))
	}
	it := plan.Items[0]
	if len(it.Dests) != 3 {
		t.Fatalf("expected 3 destinations, got %d: %+v", len(it.Dests), it.Dests)
	}

	// blood_forge: 200*193 + 275*180 = 38600 + 49500 = 88100, ranks first.
	bf := it.Dests[0]
	if bf.StationID != "blood_forge" {
		t.Fatalf("expected blood_forge first, got %s", bf.StationID)
	}
	if bf.FillQty != 475 || bf.Proceeds != 88100 || bf.BestPrice != 193 {
		t.Errorf("blood_forge: got fill=%v proceeds=%v best=%v", bf.FillQty, bf.Proceeds, bf.BestPrice)
	}
	if bf.AgeTicks != 17 {
		t.Errorf("expected age 17 ticks, got %d", bf.AgeTicks)
	}

	// nova_terra: 475*105 = 49875, second.
	if it.Dests[1].StationID != "nova_terra" || it.Dests[1].Proceeds != 49875 {
		t.Errorf("nova_terra: got %+v", it.Dests[1])
	}
	// tiny: only 2 units at 300 = 600, last despite highest unit price.
	if it.Dests[2].StationID != "tiny" || it.Dests[2].Proceeds != 600 {
		t.Errorf("tiny should rank last with proceeds 600, got %+v", it.Dests[2])
	}
}

func TestBuildUnloadPlanTopNAndMinProceeds(t *testing.T) {
	now := time.Date(2026, 7, 1, 22, 0, 0, 0, time.UTC)
	held := []heldItem{{ItemID: "x", Name: "X", Cargo: 100}}
	rungs := map[string][]unloadRung{
		"x": {
			{StationID: "a", Price: 50, Qty: 100, CapturedAt: now},
			{StationID: "b", Price: 30, Qty: 100, CapturedAt: now},
			{StationID: "c", Price: 5, Qty: 100, CapturedAt: now},
		},
	}
	// topN=2 keeps the two best (a=5000, b=3000), drops c.
	plan := buildUnloadPlan(held, rungs, now, 2, 0)
	if got := len(plan.Items[0].Dests); got != 2 {
		t.Fatalf("topN=2: expected 2 dests, got %d", got)
	}
	// minProceeds=4000 keeps only a (5000); c(500) and b(3000) filtered.
	plan = buildUnloadPlan(held, rungs, now, 0, 4000)
	if got := len(plan.Items[0].Dests); got != 1 || plan.Items[0].Dests[0].StationID != "a" {
		t.Fatalf("minProceeds=4000: expected only 'a', got %+v", plan.Items[0].Dests)
	}
}

func TestBuildUnloadPlanNoBuyers(t *testing.T) {
	now := time.Now()
	held := []heldItem{{ItemID: "junk", Name: "Junk", Cargo: 5}}
	plan := buildUnloadPlan(held, map[string][]unloadRung{}, now, unloadDefaultTopN, 0)
	if len(plan.Items) != 1 || len(plan.Items[0].Dests) != 0 {
		t.Fatalf("expected 1 item with 0 dests, got %+v", plan.Items)
	}
	if plan.Items[0].bestProceeds() != 0 {
		t.Errorf("no-buyer item should have bestProceeds 0")
	}
}
