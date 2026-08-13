package worker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// The live rejection, quoted exactly as the server sends it.
var errDeskDry = errors.New("station_fuel_empty: This station's fuel reserves are depleted. Buy fuel cells from the market and use them directly.") //nolint:staticcheck

// TestDryDeskFallsBackToCargoCells is the fix for the strand that cost three
// agents a night: `refuel` prefers the station desk while docked and fails
// outright when it is dry, never reaching the cells in the hold. The operator
// gifted 20 fuel cells to a stranded hauler and nothing in the fleet could burn
// one.
func TestDryDeskFallsBackToCargoCells(t *testing.T) {
	fc := &fakeClient{refuelErr: errDeskDry}
	var out strings.Builder
	if err := RefuelAndSync(context.Background(), fc, &out, "test"); err != nil {
		t.Fatalf("a dry desk with cells aboard must not be an error: %v", err)
	}
	if len(fc.refuelCargoCalls) != 1 {
		t.Fatalf("expected one cargo-cell burn, got %d", len(fc.refuelCargoCalls))
	}
	got := fc.refuelCargoCalls[0]
	if got.itemID != "fuel_cell" {
		t.Errorf("item_id = %q; naming it is what selects the server's cargo mode", got.itemID)
	}
	if got.quantity != 1 {
		t.Errorf("quantity = %d, want 1: cells are consumed whole and may be the only fuel for many jumps", got.quantity)
	}
}

// TestATransientRefuelFailureDoesNotBurnACell guards the expensive direction. A
// full tank or a rate limit is answered by waiting; spending a cell on one throws
// away fuel that cannot be replaced at a dry station.
func TestATransientRefuelFailureDoesNotBurnACell(t *testing.T) {
	for _, err := range []error{
		errors.New("tank_full"),
		errors.New("rate limited: 1 mutation per tick"),
		errors.New("not connected"),
		errors.New("You must be docked at a station to perform this action"),
	} {
		fc := &fakeClient{refuelErr: err}
		if rerr := RefuelAndSync(context.Background(), fc, io.Discard, "test"); rerr == nil {
			t.Errorf("%v should surface as an error, not be swallowed", err)
		}
		if len(fc.refuelCargoCalls) != 0 {
			t.Errorf("%v must not burn a fuel cell", err)
		}
	}
}

// TestAWorkingDeskNeverTouchesTheCells: station fuel is cheap and replenishes;
// cells are scarce. The fallback must stay a fallback.
func TestAWorkingDeskNeverTouchesTheCells(t *testing.T) {
	fc := &fakeClient{}
	if err := RefuelAndSync(context.Background(), fc, io.Discard, "test"); err != nil {
		t.Fatalf("refuel: %v", err)
	}
	if len(fc.refuelCargoCalls) != 0 {
		t.Fatal("a successful station refuel must not also burn a cell")
	}
}

// TestBothFuelSourcesFailingReportsBoth: a worker with no desk AND no cells is
// genuinely stranded, and the log has to say so — that is the difference between
// a diagnosable strand and a silent one.
func TestBothFuelSourcesFailingReportsBoth(t *testing.T) {
	fc := &fakeClient{refuelErr: errDeskDry, refuelCargoErr: errors.New("no_fuel_cells: No fuel cells found in cargo")}
	err := RefuelAndSync(context.Background(), fc, io.Discard, "test")
	if err == nil {
		t.Fatal("no desk and no cells is an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "station_fuel_empty") || !strings.Contains(msg, "no_fuel_cells") {
		t.Fatalf("both causes must be named, got: %s", msg)
	}
}

func TestDeskIsDryRecognisesTheServerWording(t *testing.T) {
	if !deskIsDry(errDeskDry) {
		t.Error("the live station_fuel_empty rejection must classify as a dry desk")
	}
	if !deskIsDry(errors.New("no_fuel_source")) {
		t.Error("a station with no fuel desk at all is also dry")
	}
	if deskIsDry(nil) || deskIsDry(errors.New("tank_full")) {
		t.Error("a success or a full tank is not a dry desk")
	}
}
