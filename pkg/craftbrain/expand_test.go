package craftbrain

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/navigation"
)

func planFor(t *testing.T, f *fakeSource, target string, qty int, opts Options) *Plan {
	t.Helper()
	opts.Now = testNow()
	p, err := New(f).Plan(context.Background(), target, qty, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return p
}

func leafNeed(p *Plan, item string) int {
	n := 0
	for _, nd := range p.Nodes {
		if nd.ItemID == item && (nd.Kind == KindMine || nd.Kind == KindBuy) {
			n += nd.Qty
		}
	}
	return n
}

// THE test this package exists for. alloy_electrum_ingot consumes 2 gold + 3
// silver and yields 2 ingots. bill_of_materials stores per-unit gold 1,
// silver 2 (1.5 rounded up), so 2 units reads as 4 silver. The truth is 3.
func TestPlan_CeilPerRunNotPerUnit(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("alloy_electrum_ingot", "electrum_ingot", 2, 1, false,
		map[string]int{"gold_ore": 2, "silver_ore": 3})
	p := planFor(t, f, "electrum_ingot", 2, DefaultOptions())

	if got := leafNeed(p, "gold_ore"); got != 2 {
		t.Errorf("gold_ore = %d, want 2", got)
	}
	if got := leafNeed(p, "silver_ore"); got != 3 {
		t.Errorf("silver_ore = %d, want 3 (NOT 4 — per-unit rounding is wrong)", got)
	}
}

// One intermediate consumed by three parents: aggregate demand (12) then
// round once -> 4 runs. A tree walk would compute ceil(4/3)=2 runs three
// times = 6 runs = 18 units.
func TestPlan_SharedIntermediateAggregatesBeforeRounding(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_top", "top", 1, 1, false, map[string]int{"a": 1, "b": 1, "c": 1})
	f.addRecipe("make_a", "a", 1, 1, false, map[string]int{"wire": 4})
	f.addRecipe("make_b", "b", 1, 1, false, map[string]int{"wire": 4})
	f.addRecipe("make_c", "c", 1, 1, false, map[string]int{"wire": 4})
	f.addRecipe("make_wire", "wire", 3, 1, false, map[string]int{"copper": 1})

	p := planFor(t, f, "top", 1, DefaultOptions())

	var wireRuns int
	for _, n := range p.Nodes {
		if n.Kind == KindCraft && n.ItemID == "wire" {
			wireRuns = n.Runs
		}
	}
	if wireRuns != 4 {
		t.Errorf("wire runs = %d, want 4 (ceil(12/3)); a tree walk gives 6", wireRuns)
	}
	if got := leafNeed(p, "copper"); got != 4 {
		t.Errorf("copper = %d, want 4", got)
	}
}

// 2-per-run recipe with odd demand leaves a spare that a later sibling uses.
func TestPlan_SurplusIsBankedAndReported(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_pair", "widget", 2, 1, false, map[string]int{"ore": 1})
	p := planFor(t, f, "widget", 3, DefaultOptions())

	if p.Surplus["widget"] != 1 {
		t.Errorf("surplus[widget] = %d, want 1 (2 runs x 2 = 4, need 3)", p.Surplus["widget"])
	}
}

// Stock already held is never re-crafted.
func TestPlan_SubtractEarlySkipsCraft(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_plate", "plate", 1, 1, false, map[string]int{"ore": 1})
	f.onHand["plate"] = []Holding{{Holder: "a", BaseID: "hub", Qty: 5, CapturedAt: fresh()}}
	f.systems["hub"] = "sol"

	p := planFor(t, f, "plate", 5, DefaultOptions())
	for _, n := range p.Nodes {
		if n.Kind == KindCraft {
			t.Errorf("held stock must not be crafted, got %+v", n)
		}
	}
}

// facility_only, no facility, but buyable -> buy node, not BLOCKED.
func TestPlan_DeadEndFallsBackToBuy(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_top", "top", 1, 1, false, map[string]int{"matrix": 2})
	f.addRecipe("grow_matrix", "matrix", 1, 21, true, map[string]int{"gas": 1})
	f.buyable["matrix"] = []finditem.Result{{}}

	p := planFor(t, f, "top", 1, DefaultOptions())
	var kinds []Kind
	for _, n := range p.Nodes {
		if n.ItemID == "matrix" {
			kinds = append(kinds, n.Kind)
		}
	}
	if len(kinds) != 1 || kinds[0] != KindBuy {
		t.Errorf("matrix node kinds = %v, want [buy]", kinds)
	}
}

// facility_only, no facility, not buyable -> BLOCKED, rest of DAG intact.
func TestPlan_DeadEndUnbuyableIsBlockedButPlanSurvives(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_top", "top", 1, 1, false, map[string]int{"matrix": 2, "ore": 1})
	f.addRecipe("grow_matrix", "matrix", 1, 21, true, map[string]int{"gas": 1})

	p := planFor(t, f, "top", 1, DefaultOptions())
	var blocked bool
	var sawOre bool
	for _, n := range p.Nodes {
		if n.ItemID == "matrix" && n.Kind == KindBlocked {
			blocked = true
			if n.Status != StatusBlocked {
				t.Errorf("status = %q, want blocked", n.Status)
			}
		}
		if n.ItemID == "ore" {
			sawOre = true
		}
	}
	if !blocked {
		t.Error("want a blocked node for matrix")
	}
	if !sawOre {
		t.Error("rest of the DAG must survive a blocked subtree")
	}
}

// A cycle terminates and is reported, not fatal.
func TestPlan_CycleTerminatesAndReports(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("refine", "plate", 1, 1, false, map[string]int{"scrap": 2})
	f.addRecipe("recycle", "scrap", 1, 1, false, map[string]int{"plate": 1})

	p := planFor(t, f, "plate", 1, DefaultOptions())
	if len(p.Diagnostics) == 0 {
		t.Error("cycle break must be reported in diagnostics")
	}
}

// A shared intermediate consumed at two different depths: top needs x
// directly AND needs mid, which also needs x. A tree walk would round each
// consumer's push separately (ceil(4/3)=2, ceil(5/3)=2 -> 4 runs); the
// correct answer aggregates demand first (4+5=9) and rounds once
// (ceil(9/3)=3). This pins the "aggregate before rounding" invariant against
// a cross-depth topology, where TestPlan_SharedIntermediateAggregatesBeforeRounding
// only covers same-depth parents.
func TestPlan_CrossDepthSharedIntermediateAggregatesBeforeRounding(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_top", "top", 1, 1, false, map[string]int{"x": 4, "mid": 1})
	f.addRecipe("make_mid", "mid", 1, 1, false, map[string]int{"x": 5})
	f.addRecipe("make_x", "x", 3, 1, false, map[string]int{"ore": 1})

	p := planFor(t, f, "top", 1, DefaultOptions())

	var xRuns int
	for _, n := range p.Nodes {
		if n.Kind == KindCraft && n.ItemID == "x" {
			xRuns = n.Runs
		}
	}
	if xRuns != 3 {
		t.Errorf("x runs = %d, want 3 (ceil(9/3)); rounding separately per depth gives 4", xRuns)
	}
	if got := leafNeed(p, "ore"); got != 3 {
		t.Errorf("ore = %d, want 3", got)
	}
}

// The facility's own output_per_run (4) differs from the recipe's catalog
// output (1) and demand (9) doesn't divide evenly. site.go sizes runs off
// the facility's output_per_run; surplus banking in expand.go must agree,
// or it silently disagrees with what was actually produced.
//
// This test fails against the pre-fix line `perRun := outputPerRun(s.recipe,
// item)` (no facility override): runs=3 stays correct (site.go already used
// facility output), but made = runs*1 = 3, which is not > rem (9), so no
// surplus is banked at all — 0 instead of the true 3.
func TestPlan_FacilitySurplusUsesFacilityOutputPerRun(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("press", "plate", 1, 100, true, map[string]int{"ore": 1})
	f.facilities["press"] = []Facility{
		{StationID: "hub", FacilityID: "f1", RentalFeePerRun: 2, TicksPerRun: 1, OutputPerRun: 4},
	}
	p := planFor(t, f, "plate", 9, DefaultOptions())

	var runs int
	for _, n := range p.Nodes {
		if n.Kind == KindCraft && n.ItemID == "plate" {
			runs = n.Runs
		}
	}
	if runs != 3 {
		t.Fatalf("runs = %d, want 3 (ceil(9/4))", runs)
	}
	want := runs*4 - 9 // 3
	if p.Surplus["plate"] != want {
		t.Errorf("surplus[plate] = %d, want %d (runs*facility.OutputPerRun - demand)", p.Surplus["plate"], want)
	}
}

// A facility-sited craft's node must carry the facility's StationID and
// FacilityID (not the hand-craft default), so downstream siting/dispatch
// knows where to run it.
func TestPlan_FacilityCraftNodePopulatesStationAndFacilityID(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("press", "plate", 1, 100, true, map[string]int{"ore": 1})
	f.facilities["press"] = []Facility{
		{StationID: "hub_7", FacilityID: "fac_99", RentalFeePerRun: 2, TicksPerRun: 1, OutputPerRun: 4},
	}
	p := planFor(t, f, "plate", 9, DefaultOptions())

	var found bool
	for _, n := range p.Nodes {
		if n.Kind == KindCraft && n.ItemID == "plate" {
			found = true
			if n.StationID != "hub_7" {
				t.Errorf("StationID = %q, want hub_7", n.StationID)
			}
			if n.FacilityID != "fac_99" {
				t.Errorf("FacilityID = %q, want fac_99", n.FacilityID)
			}
		}
	}
	if !found {
		t.Fatal("no craft node found for plate")
	}
}

// The RouteInf sentinel must never accumulate into the plan's honesty
// footer: a holding at an unresolvable base is still drawn, but its unknown
// distance must not inflate TotalHaulJumps by a billion.
func TestPlan_UnresolvableSystemHaulExcludedFromTotalHaulJumps(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_widget", "widget", 1, 1, false, map[string]int{"ore": 1})
	f.onHand["widget"] = []Holding{
		{Holder: "trader-2", BaseID: "near", Qty: 3, CapturedAt: fresh()},
		{Holder: "trader-9", BaseID: "unmapped", Qty: 5, CapturedAt: fresh()},
	}
	f.systems[defaultCraftBase], f.systems["near"] = "sol", "alpha"
	f.jumps["alpha"] = 1
	f.jumps[""] = navigation.RouteInf

	p := planFor(t, f, "widget", 8, DefaultOptions())

	if p.TotalHaulJumps != 1 {
		t.Errorf("TotalHaulJumps = %d, want 1 (only the resolvable near leg; the unresolvable one must not count)",
			p.TotalHaulJumps)
	}
	var sawUnknown bool
	for _, n := range p.Nodes {
		if n.Kind == KindHaul && n.FromBase == "unmapped" {
			sawUnknown = true
			if n.Status != StatusUnknownRoute {
				t.Errorf("status = %q, want %q", n.Status, StatusUnknownRoute)
			}
		}
	}
	if !sawUnknown {
		t.Fatal("want a haul node for the unresolvable base")
	}
}

// Guard against over-correcting: a normal, resolvable haul leg must still
// contribute its real jump count to TotalHaulJumps.
func TestPlan_ResolvableHaulStillCountsTowardTotalHaulJumps(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_widget", "widget", 1, 1, false, map[string]int{"ore": 1})
	f.onHand["widget"] = []Holding{{Holder: "trader-1", BaseID: "far", Qty: 4, CapturedAt: fresh()}}
	f.systems[defaultCraftBase], f.systems["far"] = "sol", "beta"
	f.jumps["beta"] = 5

	p := planFor(t, f, "widget", 4, DefaultOptions())

	if p.TotalHaulJumps != 5 {
		t.Errorf("TotalHaulJumps = %d, want 5", p.TotalHaulJumps)
	}
}

func TestPlan_RejectsBadInput(t *testing.T) {
	f := newFakeSource()
	e := New(f)
	if _, err := e.Plan(context.Background(), "nothing", 1, DefaultOptions()); err == nil {
		t.Error("unknown target must error")
	}
	f.addRecipe("make_x", "x", 1, 1, false, map[string]int{"ore": 1})
	if _, err := e.Plan(context.Background(), "x", 0, DefaultOptions()); err == nil {
		t.Error("qty < 1 must error")
	}
}
