package main

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/fitting"
)

func TestFormatCheck(t *testing.T) {
	res := fitting.FitResult{
		Fits: true, MaxCount: 1, SlotType: "utility",
		SlotsUsed: 1, SlotsAvail: 2, CPUUsed: 12, CPUAvail: 12,
		PowerUsed: 15, PowerAvail: 24, BindingConstraint: "power",
		SkillWarnings: []string{"requires mining 8"},
	}
	out := formatCheck("Cobble", "Advanced Drone Bay", res)
	for _, want := range []string{"Cobble", "Advanced Drone Bay", "max 1", "utility 1/2", "CPU 12/12", "power 15/24", "requires mining 8"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatShips(t *testing.T) {
	fits := []fitting.ShipFit{
		{Ship: fitting.Ship{Name: "Hauler", Tier: 2}, Result: fitting.FitResult{MaxCount: 6}},
	}
	out := formatShips("Advanced Drone Bay", 5, fits)
	for _, want := range []string{"Advanced Drone Bay", "Hauler", "6"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	empty := formatShips("X", 5, nil)
	if !strings.Contains(empty, "No ships") {
		t.Errorf("empty output should say No ships: %q", empty)
	}
}
