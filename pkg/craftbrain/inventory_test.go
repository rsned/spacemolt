package craftbrain

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/navigation"
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

// A holding at a base whose system does not resolve (SystemOf returns "",
// mirroring sql.ErrNoRows against the real KB for an uncatalogued base) is
// still drawn — the stock is real — but its haul node must not leak the
// navigation.RouteInf sentinel into Jumps, and must say why the distance is
// unknown instead. Nearer stock ("near") is insufficient on its own, forcing
// the draw to reach the unresolvable holding.
func TestConsumeOnHand_UnresolvableSystemDrawnButNotSentinel(t *testing.T) {
	f := newFakeSource()
	f.onHand["ore"] = []Holding{
		{Holder: "trader-2", BaseID: "near", Qty: 3, CapturedAt: fresh()},
		{Holder: "trader-9", BaseID: "unmapped", Qty: 5, CapturedAt: fresh()},
	}
	// "unmapped" is deliberately absent from f.systems, so SystemOf resolves
	// it to "".
	f.systems["dest"], f.systems["near"] = "sol", "alpha"
	f.jumps["alpha"] = 1
	// The real Source (navigation.BFSJumps) marks an unreachable/unresolved
	// system RouteInf but still returns the map key for it; mirror that.
	f.jumps[""] = navigation.RouteInf
	e := New(f)
	opts := DefaultOptions()
	opts.Now = testNow()

	rem, nodes, err := e.consumeOnHand(context.Background(), "ore", 8, "dest", opts, &idGen{})
	if err != nil {
		t.Fatal(err)
	}
	if rem != 0 {
		t.Errorf("remaining = %d, want 0 (quantity conservation: both holdings drawn)", rem)
	}
	if len(nodes) != 2 {
		t.Fatalf("want 2 haul nodes, got %d", len(nodes))
	}
	// Nearest first: "near" (1 jump) drains before the unresolvable one.
	if nodes[0].FromBase != "near" || nodes[0].Qty != 3 {
		t.Errorf("first haul = %s x%d, want near x3", nodes[0].FromBase, nodes[0].Qty)
	}
	unresolved := nodes[1]
	if unresolved.FromBase != "unmapped" || unresolved.Qty != 5 {
		t.Errorf("second haul = %s x%d, want unmapped x5", unresolved.FromBase, unresolved.Qty)
	}
	if unresolved.Jumps == navigation.RouteInf {
		t.Errorf("Jumps leaked the RouteInf sentinel: %d", unresolved.Jumps)
	}
	if unresolved.Jumps != 0 {
		t.Errorf("Jumps = %d, want 0 for an unknown distance", unresolved.Jumps)
	}
	if unresolved.Status != StatusUnknownRoute {
		t.Errorf("status = %q, want %q", unresolved.Status, StatusUnknownRoute)
	}
	if unresolved.Reason == "" {
		t.Error("want a reason explaining the unknown distance")
	}
}
