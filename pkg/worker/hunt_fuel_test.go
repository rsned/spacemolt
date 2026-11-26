package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// huntFuelFixture makes haven_station (in test_system) an empire pump and
// puts a hunter docked there with the given tank. The belt at pollux is one
// jump out and one jump from that pump, so with the 2-jump reserve and the
// default 8 a jump a leg to it wants 32 fuel.
func huntFuelFixture(t *testing.T, fuel, maxFuel float64) (*huntNeedFake, HuntDeps, *strings.Builder) {
	t.Helper()
	kb := huntNeedKBFixture(t)
	if err := kb.RememberBase(context.Background(), knowledge.SpaceBase{
		ID: "haven_station", POIID: "haven_station", PublicAccess: true, Empire: "solarian",
		Services: map[string]bool{"refuel": true},
	}); err != nil {
		t.Fatal(err)
	}
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{})
	f.state.Fuel, f.state.MaxFuel, f.state.Doc = fuel, maxFuel, true
	log := &strings.Builder{}
	return f, huntNeedDeps(f, kb, log), log
}

func huntFuelGateTo(deps HuntDeps, system, poi string) error {
	return huntFuelGate(context.Background(), deps, io.Discard, AutopilotDeps{Client: deps.Client, Out: io.Discard, KB: deps.KB}, system, poi)
}

func TestHuntFuelGateRefusesAGroundBeyondTheTank(t *testing.T) {
	f, deps, _ := huntFuelFixture(t, 20, 20)
	err := huntFuelGateTo(deps, "pollux", "pollux_outer_belt")
	if !errors.Is(err, errHuntOutOfFuelRange) {
		t.Fatalf("a 20-fuel tank cannot make a 32-fuel round trip; got %v", err)
	}
	if called(f.huntFake, "refuel") {
		t.Errorf("refuelling cannot help a tank that is too small; calls %v", f.calls)
	}
}

func TestHuntFuelGateFillsUpBeforeAShortLeg(t *testing.T) {
	f, deps, _ := huntFuelFixture(t, 10, 100)
	_ = huntFuelGateTo(deps, "pollux", "pollux_outer_belt")
	if !called(f.huntFake, "refuel") {
		t.Fatalf("docked and short of the round trip: want a refuel first; calls %v", f.calls)
	}
}

func TestHuntFuelGatePassesAFullTankAndALegToAPump(t *testing.T) {
	_, deps0, _ := huntFuelFixture(t, 100, 100)
	if err := huntFuelGateTo(deps0, "pollux", "pollux_outer_belt"); err != nil {
		t.Fatalf("a full tank covers the trip: %v", err)
	}
	f, deps, _ := huntFuelFixture(t, 1, 100)
	if err := huntFuelGateTo(deps, "test_system", "haven_station"); err != nil {
		t.Fatalf("a leg to a pump is the autopilot's business: %v", err)
	}
	if called(f.huntFake, "refuel") {
		t.Errorf("no refuel expected; calls %v", f.calls)
	}
}

// A hive or a mobile base lists refuel in the KB but belongs to no empire.
func TestHuntFuelStationWantsAnEmpire(t *testing.T) {
	_, deps, _ := huntFuelFixture(t, 100, 100)
	ctx := context.Background()
	if !huntFuelStation(ctx, deps.KB, "haven_station") {
		t.Fatal("an empire station with refuel is a pump")
	}
	if err := deps.KB.RememberBase(ctx, knowledge.SpaceBase{
		ID: "irlc_civitas", POIID: "irlc_civitas", PublicAccess: true,
		Services: map[string]bool{"refuel": true},
	}); err != nil {
		t.Fatal(err)
	}
	if huntFuelStation(ctx, deps.KB, "irlc_civitas") {
		t.Fatal("a base with no empire is not a pump, whatever the KB lists")
	}
}

func TestHuntStrandedAtDockStaysPut(t *testing.T) {
	f, deps, log := huntFuelFixture(t, 3, 100)
	f.refuelErr = errors.New("no_fuel_source")
	huntStrandedTried = time.Time{}
	if !huntStrandedAtDock(context.Background(), deps, log, "pirate-x") {
		t.Fatal("docked on 3 fuel with no refuel to be had is stranded")
	}
	if !strings.Contains(log.String(), "STRANDED") {
		t.Errorf("want the stranded line, got %q", log.String())
	}
	n := len(f.calls)
	if !huntStrandedAtDock(context.Background(), deps, log, "pirate-x") || len(f.calls) != n {
		t.Errorf("a second pass stays stranded without another refuel attempt; calls %v", f.calls[n:])
	}
	huntStrandedTried = time.Time{}
	_, deps, log = huntFuelFixture(t, 60, 100)
	if huntStrandedAtDock(context.Background(), deps, log, "pirate-x") {
		t.Fatal("60 fuel is not stranded")
	}
}

func TestHuntLegRefusedNamesThePOI(t *testing.T) {
	err := fmt.Errorf("transit to %s: %w", "far_belt", fmt.Errorf("%w: far_belt wants ~90 fuel", errHuntOutOfFuelRange))
	if got := huntLegRefused(err, errHuntOutOfFuelRange); got != "far_belt" {
		t.Fatalf("got %q, want far_belt", got)
	}
	if got := huntLegRefused(err, ErrRouteThroughAvoided); got != "" {
		t.Fatalf("a different refusal must not match, got %q", got)
	}
}

func TestHuntNeedPaysWantsRewardOverFuelByTheMargin(t *testing.T) {
	t.Parallel()
	// pirate-9's Nitrogen Bladder of 2026-11-25: 1,288 cr, grounds far out.
	if huntNeedPays(1288, 20*huntFuelPerJumpDefault*huntFuelCreditsPerUnit) {
		t.Error("1,288 cr must not clear 20 jumps of fuel")
	}
	if !huntNeedPays(8395, 20*huntFuelPerJumpDefault*huntFuelCreditsPerUnit) {
		t.Error("8,395 cr clears 20 jumps of fuel")
	}
	if !huntNeedPays(150, 100) || huntNeedPays(149, 100) {
		t.Error("the floor is exactly huntNeedRewardMargin times the fuel")
	}
	if !huntNeedPays(1, 0) {
		t.Error("a need at the ground and the base costs no fuel and always pays")
	}
}
