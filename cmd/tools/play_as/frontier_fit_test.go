package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/explore"
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

// fitFake records the apply calls; withdrawErr fails withdrawals of that item.
type fitFake struct {
	calls       []string
	withdrawErr map[string]error
}

func (f *fitFake) UninstallMod(_ context.Context, id string) error {
	f.calls = append(f.calls, "uninstall "+id)
	return nil
}

func (f *fitFake) InstallMod(_ context.Context, id string) error {
	f.calls = append(f.calls, "install "+id)
	return nil
}

func (f *fitFake) FactionDepositItems(_ context.Context, item string, _ int) error {
	f.calls = append(f.calls, "deposit "+item)
	return nil
}

func (f *fitFake) FactionWithdrawItems(_ context.Context, item string, n int) error {
	if err := f.withdrawErr[item]; err != nil {
		return err
	}
	f.calls = append(f.calls, fmt.Sprintf("withdraw %d %s", n, item))
	return nil
}

func (f *fitFake) WithdrawItems(_ context.Context, item string, n float64) error {
	f.calls = append(f.calls, fmt.Sprintf("own-withdraw %v %s", n, item))
	return nil
}

// A ship with no faction-storage permission draws staged gear from its own
// station storage instead.
func TestApplyFrontierFitFallsBackToOwnStorage(t *testing.T) {
	refused := errors.New("no_permission: You need 'manage treasury' permission")
	f := &fitFake{withdrawErr: map[string]error{sweep.FitScanner: refused, explore.KitItem: refused, "fuel_cell": refused}}
	if err := applyFrontierFit(context.Background(), f, func() {}, fitTestPlan(), 0, 20); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"own-withdraw 2 " + sweep.FitScanner, "own-withdraw 10 " + explore.KitItem, "own-withdraw 20 fuel_cell"} {
		if !slices.Contains(f.calls, want) {
			t.Errorf("calls %v, want %q", f.calls, want)
		}
	}
	if f.calls[0] != "own-withdraw 2 "+sweep.FitScanner {
		t.Errorf("calls %v, want the scanners drawn before anything is removed", f.calls)
	}
}

func fitTestPlan() sweep.FitPlan {
	return sweep.FitPlan{OK: true,
		Remove: []sweep.FitModule{{ID: "m1", TypeID: "mining_laser_i", Name: "Mining Laser I"}},
		Add:    []string{sweep.FitScanner, sweep.FitScanner}}
}

// With no scanners to draw, nothing is stripped from the ship.
func TestApplyFrontierFitWithdrawsScannersBeforeStripping(t *testing.T) {
	f := &fitFake{withdrawErr: map[string]error{sweep.FitScanner: errors.New("not enough in storage")}}
	if err := applyFrontierFit(context.Background(), f, func() {}, fitTestPlan(), 10, 0); err == nil {
		t.Fatal("no error when the scanners could not be withdrawn")
	}
	if len(f.calls) != 0 {
		t.Errorf("calls %v, want the ship untouched", f.calls)
	}
	f = &fitFake{}
	if err := applyFrontierFit(context.Background(), f, func() {}, fitTestPlan(), 10, 0); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) == 0 || f.calls[0] != "withdraw 2 "+sweep.FitScanner {
		t.Errorf("calls %v, want the scanner withdrawal first", f.calls)
	}
}

// A kit or fuel-cell shortfall is an error, not a footnote.
func TestApplyFrontierFitReportsSupplyShortfall(t *testing.T) {
	f := &fitFake{withdrawErr: map[string]error{explore.KitItem: errors.New("not enough in storage")}}
	err := applyFrontierFit(context.Background(), f, func() {}, fitTestPlan(), 4, 0)
	if err == nil || !strings.Contains(err.Error(), explore.KitItem) {
		t.Errorf("err = %v, want the repair kit shortfall", err)
	}
	if !slices.Contains(f.calls, "install "+sweep.FitScanner) {
		t.Errorf("calls %v, want the scanners still fitted", f.calls)
	}
}
