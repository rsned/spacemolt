package craftbrain

import (
	"context"
	"testing"
	"time"
)

func TestConsumeOnHand_LocalStockEmitsNoHaul(t *testing.T) {
	f := newFakeSource()
	f.onHand["ore"] = []Holding{{Holder: "hauler-3", BaseID: "hub", Qty: 10, CapturedAt: fresh()}}
	f.systems["hub"] = "sol"
	e := New(f)
	opts := DefaultOptions()
	opts.Now = testNow()

	rem, nodes, err := e.consumeOnHand(context.Background(), "ore", 4, "hub", opts, &idGen{})
	if err != nil {
		t.Fatal(err)
	}
	if rem != 0 {
		t.Errorf("remaining = %d, want 0", rem)
	}
	if len(nodes) != 0 {
		t.Errorf("local stock must emit no haul node, got %d", len(nodes))
	}
}

func TestConsumeOnHand_SplitAcrossBasesEmitsTwoHauls(t *testing.T) {
	f := newFakeSource()
	f.onHand["ore"] = []Holding{
		{Holder: "trader-1", BaseID: "far", Qty: 5, CapturedAt: fresh()},
		{Holder: "trader-2", BaseID: "near", Qty: 3, CapturedAt: fresh()},
	}
	f.systems["dest"], f.systems["near"], f.systems["far"] = "sol", "alpha", "beta"
	f.jumps["alpha"], f.jumps["beta"] = 1, 7
	e := New(f)
	opts := DefaultOptions()
	opts.Now = testNow()

	rem, nodes, err := e.consumeOnHand(context.Background(), "ore", 8, "dest", opts, &idGen{})
	if err != nil {
		t.Fatal(err)
	}
	if rem != 0 {
		t.Errorf("remaining = %d, want 0", rem)
	}
	if len(nodes) != 2 {
		t.Fatalf("want 2 haul nodes, got %d", len(nodes))
	}
	// Nearest first: "near" (1 jump) drains before "far" (7 jumps).
	if nodes[0].FromBase != "near" || nodes[0].Qty != 3 {
		t.Errorf("first haul = %s x%d, want near x3", nodes[0].FromBase, nodes[0].Qty)
	}
	if nodes[1].FromBase != "far" || nodes[1].Qty != 5 {
		t.Errorf("second haul = %s x%d, want far x5", nodes[1].FromBase, nodes[1].Qty)
	}
	if nodes[0].Holder != "trader-2" {
		t.Errorf("holder attribution lost: %q", nodes[0].Holder)
	}
	if nodes[1].Jumps != 7 {
		t.Errorf("jumps = %d, want 7", nodes[1].Jumps)
	}
}

func TestConsumeOnHand_PartialLeavesRemainder(t *testing.T) {
	f := newFakeSource()
	f.onHand["ore"] = []Holding{{Holder: "a", BaseID: "hub", Qty: 2, CapturedAt: fresh()}}
	f.systems["hub"] = "sol"
	e := New(f)
	opts := DefaultOptions()
	opts.Now = testNow()

	rem, _, err := e.consumeOnHand(context.Background(), "ore", 9, "hub", opts, &idGen{})
	if err != nil {
		t.Fatal(err)
	}
	if rem != 7 {
		t.Errorf("remaining = %d, want 7", rem)
	}
}

// Two holdings can legitimately share a BaseID (an agent and faction storage
// ("") both sitting at the same remote station), which ties both the jumps
// and BaseID sort keys. The comparator must fall through to Holder so the
// draw order is deterministic rather than left to sort.Slice's instability.
func TestConsumeOnHand_SameBaseTiesBrokenByHolder(t *testing.T) {
	f := newFakeSource()
	f.onHand["ore"] = []Holding{
		{Holder: "trader-1", BaseID: "hub", Qty: 5, CapturedAt: fresh()},
		{Holder: "", BaseID: "hub", Qty: 3, CapturedAt: fresh()},
	}
	f.systems["dest"], f.systems["hub"] = "sol", "alpha"
	f.jumps["alpha"] = 2
	e := New(f)
	opts := DefaultOptions()
	opts.Now = testNow()

	rem, nodes, err := e.consumeOnHand(context.Background(), "ore", 6, "dest", opts, &idGen{})
	if err != nil {
		t.Fatal(err)
	}
	if rem != 0 {
		t.Errorf("remaining = %d, want 0", rem)
	}
	if len(nodes) != 2 {
		t.Fatalf("want 2 haul nodes, got %d", len(nodes))
	}
	// Holder "" sorts before "trader-1", so faction storage drains first.
	if nodes[0].Holder != "" || nodes[0].FromBase != "hub" || nodes[0].Qty != 3 {
		t.Errorf("first haul = holder %q, base %s, qty %d; want holder \"\", base hub, qty 3",
			nodes[0].Holder, nodes[0].FromBase, nodes[0].Qty)
	}
	if nodes[1].Holder != "trader-1" || nodes[1].FromBase != "hub" || nodes[1].Qty != 3 {
		t.Errorf("second haul = holder %q, base %s, qty %d; want holder trader-1, base hub, qty 3",
			nodes[1].Holder, nodes[1].FromBase, nodes[1].Qty)
	}
	if nodes[0].ID != "haul-1" || nodes[1].ID != "haul-2" {
		t.Errorf("haul ids = %s, %s; want haul-1, haul-2", nodes[0].ID, nodes[1].ID)
	}
}

// Stale stock is still subtracted, but the haul node says so.
func TestConsumeOnHand_StaleHoldingFlagged(t *testing.T) {
	f := newFakeSource()
	old := testNow().Add(-48 * time.Hour)
	f.onHand["ore"] = []Holding{{Holder: "a", BaseID: "far", Qty: 5, CapturedAt: old}}
	f.systems["dest"], f.systems["far"] = "sol", "beta"
	f.jumps["beta"] = 2
	e := New(f)
	opts := DefaultOptions()
	opts.Now = testNow()

	_, nodes, err := e.consumeOnHand(context.Background(), "ore", 5, "dest", opts, &idGen{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	if nodes[0].Status != StatusStale {
		t.Errorf("status = %q, want %q", nodes[0].Status, StatusStale)
	}
}
