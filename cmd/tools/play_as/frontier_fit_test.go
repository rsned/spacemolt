package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/sweep"
)

func reserveShip() serverapi.GetShipResponse {
	var resp serverapi.GetShipResponse
	resp.Ship.ClassID = "reserve"
	resp.Ship.CPUCapacity, resp.Ship.PowerCapacity, resp.Ship.UtilitySlots = 28, 72, 3
	resp.Modules = []serverapi.ShipModule{
		{ID: "m1", TypeID: "pathfinder_drive", Name: "Pathfinder Drive", Type: "utility", CPUUsage: 18, PowerUsage: 30},
		{ID: "m2", TypeID: "fuel_optimizer_i", Name: "Fuel Optimizer I", Type: "utility", CPUUsage: 4, PowerUsage: 6},
		{ID: "m3", TypeID: "mining_laser_i", Name: "Mining Laser I", Type: "mining", CPUUsage: 3, PowerUsage: 5},
		{ID: "m4", TypeID: "autocannon_i", Name: "Autocannon I", Slot: "weapon", Type: "weapon", CPUUsage: 6, PowerUsage: 8},
	}
	return resp
}

// The plan is built from the live get_ship reply: capacities from the ship,
// each module's slot from the reply, else the catalog, else its type.
func TestFrontierFitPlanFromShip(t *testing.T) {
	slotOf := func(typeID string) string {
		if typeID == "fuel_optimizer_i" {
			return sweep.SlotUtility
		}
		return ""
	}
	p := frontierFitPlan(reserveShip(), slotOf, 5, 7)
	if !p.OK {
		t.Fatalf("plan not OK: %s", p.Why)
	}
	var removed []string
	for _, m := range p.Remove {
		removed = append(removed, m.ID)
	}
	// Two utility modules must go for the slots (most CPU first), then the
	// weapon for CPU: 18 + 10 = 28 of 28.
	if want := []string{"m2", "m3", "m4"}; !slices.Equal(removed, want) {
		t.Errorf("removes %v, want %v", removed, want)
	}
	if p.CPUAfter != 28 || p.CPUCap != 28 || p.PowerCap != 72 {
		t.Errorf("cpu %d/%d power cap %d", p.CPUAfter, p.CPUCap, p.PowerCap)
	}
}

// A reply without capacities on the ship falls back to its class block.
func TestFrontierFitPlanUsesClassCapacity(t *testing.T) {
	resp := reserveShip()
	resp.Ship.CPUCapacity, resp.Ship.PowerCapacity, resp.Ship.UtilitySlots = 0, 0, 0
	resp.Class = &serverapi.ShipClass{CPUCapacity: 32, PowerCapacity: 80, UtilitySlots: 3}
	p := frontierFitPlan(resp, func(string) string { return "" }, 5, 7)
	if p.CPUCap != 32 || p.PowerCap != 80 || !p.OK {
		t.Errorf("plan %+v, want the class's 32 CPU / 80 power", p)
	}
}

func TestFormatFrontierFit(t *testing.T) {
	p := frontierFitPlan(reserveShip(), func(string) string { return "" }, 5, 7)
	got := formatFrontierFit("explorer-1", p)
	for _, want := range []string{"explorer-1", "remove  Fuel Optimizer I", "remove  Autocannon I",
		"fit     survey_scanner_ii x2", "CPU 28/28", "power 44/72"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	bad := sweep.FitPlan{Why: "no pathfinder_drive fitted"}
	if got := formatFrontierFit("explorer-4", bad); !strings.Contains(got, "CANNOT FIT: no pathfinder_drive fitted") {
		t.Errorf("a failed plan printed as:\n%s", got)
	}
}
