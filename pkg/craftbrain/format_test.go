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
