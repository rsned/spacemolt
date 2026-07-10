package craftbrain

import (
	"strings"
	"testing"
)

func TestFormat_ShowsFootersAndStatuses(t *testing.T) {
	p := &Plan{
		Target: "sensor_array", Quantity: 2,
		Nodes: []Node{
			{ID: "mine-1", Kind: KindMine, ItemID: "iron_ore", Qty: 40, Status: StatusOK},
			{ID: "craft-2", Kind: KindCraft, ItemID: "plate", Qty: 4, Runs: 2,
				RecipeID: "forge_plate", StationID: "hub", FacilityID: "f1",
				FeeTotal: 70, TicksEst: 8, Status: StatusOK},
			{ID: "blocked-3", Kind: KindBlocked, ItemID: "matrix", Qty: 2,
				Status: StatusBlocked, Reason: "no public facility known"},
		},
		Surplus:        map[string]int{"plate": 1},
		Diagnostics:    []string{"cycle broken: dropped recipe edge a->b"},
		TotalFee:       70,
		TotalTicks:     8,
		TotalHaulJumps: 5,
		Coverage:       Coverage{Stations: 30, FacilityOnlyCovered: 101, FacilityOnlyTotal: 317},
	}
	out := Format(p)
	for _, want := range []string{
		"sensor_array", "x2",
		"mine", "iron_ore", "40",
		"craft", "forge_plate", "hub", "f1",
		"BLOCKED", "matrix",
		"Total fee: 70",
		"Total haul: 5 jumps",
		"surplus", "plate",
		"30 stations", "101/317",
		"cycle broken",
		// Coverage.FacilityOnlyCovered (101) < FacilityOnlyTotal (317), so the
		// caveat footer must render — this is what stops a reviewer from
		// mistaking "not swept yet" for "impossible" on a BLOCKED node.
		"a BLOCKED node may mean 'not swept yet', not 'impossible'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Format output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormat_EmptyPlanIsReadable(t *testing.T) {
	out := Format(&Plan{Target: "widget", Quantity: 1})
	if !strings.Contains(out, "widget") {
		t.Errorf("want target in output, got %q", out)
	}
}

// TestFormat_HaulStaleCraftSlowAndBuyNodes pins the three node-kind render
// branches that no prior test constructed: KindHaul with StatusStale,
// KindCraft with StatusSlow, and KindBuy. Each assertion copies the literal
// text straight out of format.go rather than a loose substring, so a broken
// tag() branch or a reworded buy line actually fails the test.
func TestFormat_HaulStaleCraftSlowAndBuyNodes(t *testing.T) {
	p := &Plan{
		Target: "widget", Quantity: 1,
		Nodes: []Node{
			{ID: "haul-1", Kind: KindHaul, ItemID: "iron_ore", Qty: 20,
				FromBase: "outpost", ToBase: "hub", Jumps: 3, Holder: "agent7",
				Status: StatusStale},
			{ID: "craft-9", Kind: KindCraft, ItemID: "gizmo", Qty: 5, Runs: 3,
				RecipeID: "forge_gizmo", StationID: "hub2",
				FeeTotal: 40, TicksEst: 12.5, Status: StatusSlow},
			{ID: "buy-4", Kind: KindBuy, ItemID: "fuel_cell", Qty: 15,
				StationID: "port9"},
		},
	}
	out := Format(p)

	haulLine := "  [haul-1] haul   iron_ore                 x20  outpost -> hub (3 jumps, holder agent7)  [STALE]"
	if !strings.Contains(out, haulLine) {
		t.Errorf("Format output missing haul+stale line %q\n---\n%s", haulLine, out)
	}

	craftLine := "  [craft-9] craft  gizmo                    x5  3 runs of forge_gizmo @ hub2  fee 40, 12.5 ticks  [SLOW]"
	if !strings.Contains(out, craftLine) {
		t.Errorf("Format output missing craft+slow line %q\n---\n%s", craftLine, out)
	}

	buyLine := "  [buy-4] buy    fuel_cell                x15  @ port9"
	if !strings.Contains(out, buyLine) {
		t.Errorf("Format output missing buy line %q\n---\n%s", buyLine, out)
	}
}

// TestFormat_CoverageCaveatAbsentWhenFullyCovered is the negative twin of the
// caveat assertion in TestFormat_ShowsFootersAndStatuses: when the plan's
// facility_only coverage is complete (Covered == Total), the "not swept yet"
// caveat must NOT render. Without this, a Format() that always prints the
// caveat would still pass every other test.
func TestFormat_CoverageCaveatAbsentWhenFullyCovered(t *testing.T) {
	p := &Plan{
		Target: "widget", Quantity: 1,
		Coverage: Coverage{Stations: 10, FacilityOnlyCovered: 50, FacilityOnlyTotal: 50},
	}
	out := Format(p)
	if strings.Contains(out, "not swept yet") {
		t.Errorf("Format output should not contain the coverage caveat when fully covered\n---\n%s", out)
	}
}
