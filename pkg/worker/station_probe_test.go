package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// probeFakeClient records the calls a survey makes and lets a test dictate the
// outcome of each dock and refuel.
type probeFakeClient struct {
	game.GameClient
	fuel      float64
	dockErr   map[string]error // keyed by the POI the ship last flew to
	refuelErr map[string]error
	at        string
	docks     []string
	refuels   []string
	undocks   int
	travelErr map[string]error
	// cargoBurns counts RefuelFromCargo calls. A probe must never make one: the
	// desk's verdict is the measurement, and cells are scarce.
	cargoBurns int
	docked     bool
	refuelTo   float64 // fuel level a successful refuel fills to
	// nilStateAfter makes GetState return nil after this many calls, modelling a
	// connection lost partway through a run.
	nilStateAfter int
	stateCalls    int
}

func (c *probeFakeClient) GetState() *game.State {
	c.stateCalls++
	if c.nilStateAfter > 0 && c.stateCalls > c.nilStateAfter {
		return nil
	}
	s := &game.State{}
	// GetFuel() reads State.Fuel/State.MaxFuel, NOT State.Ship.*
	s.Fuel = c.fuel
	s.MaxFuel = 120
	s.System.ID = c.at
	s.Doc = c.docked
	s.CurrentPOI = c.at

	return s
}

func (c *probeFakeClient) FindRoute(_ context.Context, target string) ([]game.RouteStep, error) {
	if err := c.travelErr[target]; err != nil {
		return nil, err
	}
	c.at = target

	return []game.RouteStep{{}}, nil
}

// Autopilot travels to the station POI after routing; the fake treats a POI
// travel failure the same way as a routing failure.
func (c *probeFakeClient) Travel(_ context.Context, poi string) (*game.TravelResult, error) {
	if err := c.travelErr[poi]; err != nil {
		return nil, err
	}

	return &game.TravelResult{}, nil
}

func (c *probeFakeClient) Dock(_ context.Context) error {
	c.docks = append(c.docks, c.at)

	return c.dockErr[c.at]
}

func (c *probeFakeClient) Refuel(_ context.Context) error {
	c.refuels = append(c.refuels, c.at)
	if err := c.refuelErr[c.at]; err != nil {
		return err
	}
	if c.refuelTo > 0 {
		c.fuel = c.refuelTo
	}

	return nil
}

// RefuelFromCargo records the burn rather than panicking through the embedded
// interface, so a probe that reaches for a cargo cell fails the assertion that
// names the bug instead of dying in an unrelated nil-method panic.
func (c *probeFakeClient) RefuelFromCargo(_ context.Context, _ string, _ int) error {
	c.cargoBurns++
	return nil
}

func (c *probeFakeClient) Undock(_ context.Context) error {
	c.undocks++

	return nil
}

func (c *probeFakeClient) GetStatus(_ context.Context) error { return nil }

func probeDeps(t *testing.T, c *probeFakeClient, jumps map[string]int) (ProbeDeps, *StationAccess) {
	t.Helper()
	acc := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))

	return ProbeDeps{
		Client:      c,
		Access:      acc,
		FuelPerJump: 2,
		JumpsTo: func(_ context.Context, sys string) (int, error) {
			j, ok := jumps[sys]
			if !ok {
				return 0, fmt.Errorf("no route to %s", sys)
			}

			return j, nil
		},
	}, acc
}

func probeTarget(name, station string, jumps int) ProbeTarget {
	return ProbeTarget{StationID: station, SystemID: name, POIID: name, Name: name}
}

// The survey exists to turn "unverified" into a fact, in both directions: a
// refusal is a successful outcome, not a failed stop.
func TestProbeRecordsBothAdmissionAndRefusal(t *testing.T) {
	c := &probeFakeClient{
		fuel:    120,
		dockErr: map[string]error{"closedsys": errors.New("Error: Access denied")},
	}
	deps, acc := probeDeps(t, c, map[string]int{"opensys": 3, "closedsys": 3})
	targets := []ProbeTarget{
		probeTarget("opensys", hexStarBase, 3),
		probeTarget("closedsys", blackthornBase, 3),
	}

	got, err := ProbeStations(context.Background(), deps, targets)
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("verdicts = %d, want 2", len(got))
	}
	if !got[0].Docked || got[1].Docked {
		t.Errorf("docked = %v/%v, want true/false", got[0].Docked, got[1].Docked)
	}
	if !acc.Deliverable(hexStarBase) {
		t.Error("an admitted station must be recorded open")
	}
	if !acc.Denied(blackthornBase) {
		t.Error("a refusal must be recorded denied -- that is the whole point of the run")
	}
}

// The fuel gate is what keeps the ship recoverable. It must refuse a leg it
// cannot afford rather than start it and find out.
func TestProbeStopsBeforeALegItCannotAfford(t *testing.T) {
	c := &probeFakeClient{fuel: 10}
	deps, _ := probeDeps(t, c, map[string]int{"near": 3, "far": 9})
	var sb strings.Builder
	deps.Out = &sb

	// near: 3 jumps * 2 = 6, +2 approach = 8 <= 10. far: 9*2+2 = 20 > 10.
	got, err := ProbeStations(context.Background(), deps, []ProbeTarget{
		probeTarget("near", hexStarBase, 3),
		probeTarget("far", obsidianBase, 9),
	})
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("verdicts = %d, want 1 (the far leg must not be attempted)", len(got))
	}
	if len(c.docks) != 1 {
		t.Errorf("docks = %v, want only the affordable stop", c.docks)
	}
	if !strings.Contains(sb.String(), "parking for recovery") {
		t.Errorf("a short-fuel stop must say why it stopped; got %q", sb.String())
	}
}

// In-system approach fuel is small per stop and decisive in aggregate. A gate
// that counts only jumps would take this leg and arrive unable to reach the POI.
func TestProbeGateIncludesInSystemApproachFuel(t *testing.T) {
	c := &probeFakeClient{fuel: 6} // exactly 3 jumps' worth, nothing for the approach
	deps, _ := probeDeps(t, c, map[string]int{"near": 3})

	got, err := ProbeStations(context.Background(), deps, []ProbeTarget{probeTarget("near", hexStarBase, 3)})
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("verdicts = %d, want 0: 3 jumps costs 6 fuel plus the approach", len(got))
	}

	// One more fuel than the jumps-plus-approach total is enough.
	c2 := &probeFakeClient{fuel: 9}
	deps2, _ := probeDeps(t, c2, map[string]int{"near": 3})
	got2, err := ProbeStations(context.Background(), deps2, []ProbeTarget{probeTarget("near", hexStarBase, 3)})
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if len(got2) != 1 {
		t.Errorf("verdicts = %d, want 1 with fuel to spare", len(got2))
	}
}

// Docking proves access; it says nothing about a fuel desk until a refuel is
// actually attempted. Both facts must be recorded from the one stop.
func TestProbeLearnsFuelDeskSeparatelyFromAccess(t *testing.T) {
	c := &probeFakeClient{
		fuel:      120,
		refuelErr: map[string]error{"drysys": errors.New("Error: no_fuel_source")},
	}
	deps, acc := probeDeps(t, c, map[string]int{"drysys": 2, "wetsys": 2})
	got, err := ProbeStations(context.Background(), deps, []ProbeTarget{
		probeTarget("drysys", hexStarBase, 2),
		probeTarget("wetsys", obsidianBase, 2),
	})
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if !got[0].Docked || !got[0].FuelKnown || got[0].SellsFuel {
		t.Errorf("dry station verdict = %+v, want docked with a known-dry desk", got[0])
	}
	if !got[1].FuelKnown || !got[1].SellsFuel {
		t.Errorf("wet station verdict = %+v, want a known fuel desk", got[1])
	}
	if sells, known := acc.Fuel(hexStarBase); !known || sells {
		t.Error("no_fuel_source must be recorded against the station")
	}
	// A cargo-cell fallback here would answer the question the probe came to ask:
	// the burn succeeds, the refuel reports nil, and a dry station gets written
	// into the learned access file as one that sells fuel.
	if c.cargoBurns != 0 {
		t.Errorf("probe burned %d cargo cells; a dry desk is the finding, not a problem to solve", c.cargoBurns)
	}
}

// The ship must be left adrift, not docked: GSA recovery tows a drifting ship
// home, while one parked at a station nobody routes to waits indefinitely.
func TestProbeUndocksSoTheShipCanBeRecovered(t *testing.T) {
	c := &probeFakeClient{fuel: 120}
	deps, _ := probeDeps(t, c, map[string]int{"a": 1, "b": 1})
	if _, err := ProbeStations(context.Background(), deps, []ProbeTarget{
		probeTarget("a", hexStarBase, 1),
		probeTarget("b", obsidianBase, 1),
	}); err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if c.undocks != 2 {
		t.Errorf("undocks = %d, want 2 (a docked ship is not recoverable by tow)", c.undocks)
	}
}

// An unroutable target is that target's problem. The survey must skip it and
// keep its remaining stops rather than abandon the run.
func TestProbeSkipsUnroutableTargetAndContinues(t *testing.T) {
	c := &probeFakeClient{fuel: 120}
	deps, _ := probeDeps(t, c, map[string]int{"reachable": 2}) // "island" absent -> no route
	got, err := ProbeStations(context.Background(), deps, []ProbeTarget{
		probeTarget("island", blackthornBase, 0),
		probeTarget("reachable", hexStarBase, 2),
	})
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if len(got) != 2 || got[0].Reached || !got[1].Docked {
		t.Errorf("verdicts = %+v, want the unroutable one skipped and the next one flown", got)
	}
	if !strings.Contains(got[0].Note, "unroutable") {
		t.Errorf("note = %q, want it to say unroutable", got[0].Note)
	}
}

// A transit failure never reached the station, so it must not be recorded as a
// refusal -- that would blacklist a station nobody actually asked.
func TestProbeTransitFailureTeachesNothing(t *testing.T) {
	c := &probeFakeClient{
		fuel: 120,
		// Deliberately carries the refusal marker: a transit error can wrap a
		// server message, and only "never record on transit failure" survives it.
		travelErr: map[string]error{"sys": errors.New("giving up: Access denied by route planner")},
	}
	deps, acc := probeDeps(t, c, map[string]int{"sys": 2})
	got, err := ProbeStations(context.Background(), deps, []ProbeTarget{probeTarget("sys", blackthornBase, 2)})
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if got[0].Reached || got[0].Docked {
		t.Errorf("verdict = %+v, want neither reached nor docked", got[0])
	}
	if acc.Denied(blackthornBase) {
		t.Error("a transit failure must not be recorded as a refusal")
	}
	if len(c.docks) != 0 {
		t.Error("a failed transit must not attempt a dock")
	}
}

// Losing state mid-run must halt the survey and keep the verdicts already
// earned. Treating an unreadable tank as empty would be survivable; treating it
// as full would strand the ship. Distinct from an unreadable state at DEPARTURE,
// which is a hard error -- there is nothing to keep.
func TestProbeHaltsWhenFuelCannotBeRead(t *testing.T) {
	c := &probeFakeClient{fuel: 120, docked: true, at: "home", nilStateAfter: 4}
	deps, _ := probeDeps(t, c, map[string]int{"a": 1, "b": 1})
	var sb strings.Builder
	deps.Out = &sb

	got, err := ProbeStations(context.Background(), deps, []ProbeTarget{
		probeTarget("a", hexStarBase, 1),
		probeTarget("b", obsidianBase, 1),
	})
	if err != nil {
		t.Fatalf("a mid-run state loss must not fail the survey: %v", err)
	}
	if len(got) == 2 {
		t.Error("want the run to halt before the second stop once state is unreadable")
	}
	// A lost connection and an exhausted fuel budget both end the run, but they
	// mean different things to whoever reads the log: one is a fault to chase,
	// the other is the survey finishing as designed.
	if !strings.Contains(sb.String(), "cannot read fuel") {
		t.Errorf("want an unreadable-tank halt to say so, got %q", sb.String())
	}
}

// A missing fuel figure is not a survey the operator can trust, so the probe
// refuses to start rather than flying on a guessed margin.
func TestProbeRefusesToRunWithoutAFuelBudget(t *testing.T) {
	c := &probeFakeClient{fuel: 120}
	deps, _ := probeDeps(t, c, map[string]int{"a": 1})
	deps.FuelPerJump = 0
	if _, err := ProbeStations(context.Background(), deps, []ProbeTarget{probeTarget("a", hexStarBase, 1)}); err == nil {
		t.Error("want an error when FuelPerJump is unset")
	}
}

// A survey ship is usually one that has been parked, and a parked ship is often
// dry -- salvager-9's Cobble sat at First Step on an empty tank. Without a
// pre-departure top-up the fuel gate correctly refuses the first leg and the run
// returns a survey of nothing.
func TestProbeRefuelsBeforeDeparture(t *testing.T) {
	c := &probeFakeClient{fuel: 0, docked: true, at: "home", refuelTo: 120}
	deps, _ := probeDeps(t, c, map[string]int{"a": 3})

	got, err := ProbeStations(context.Background(), deps, []ProbeTarget{probeTarget("a", hexStarBase, 3)})
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if len(c.refuels) == 0 || c.refuels[0] != "home" {
		t.Fatalf("refuels = %v, want a top-up at the origin first", c.refuels)
	}
	if len(got) != 1 || !got[0].Docked {
		t.Errorf("verdicts = %+v, want the first stop flown on the topped-up tank", got)
	}
}

// The origin is the one station the survey can ask about for free.
func TestProbePreflightRecordsTheOriginFuelDesk(t *testing.T) {
	c := &probeFakeClient{
		fuel: 50, docked: true, at: "home",
		refuelErr: map[string]error{"home": errors.New("Error: no_fuel_source")},
	}
	deps, acc := probeDeps(t, c, map[string]int{"a": 1})
	if _, err := ProbeStations(context.Background(), deps, []ProbeTarget{probeTarget("a", hexStarBase, 1)}); err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if sells, known := acc.Fuel("home"); !known || sells {
		t.Error("a dry origin must be recorded like any other station")
	}
}

// A failed top-up on a tank that still has fuel is not fatal -- the gate just
// stops the run earlier. A failed top-up on an EMPTY tank is: there is nothing
// to survey with, and saying so beats returning an empty result set.
func TestProbePreflightOnlyFailsWhenTheShipCannotStart(t *testing.T) {
	dry := &probeFakeClient{
		fuel: 0, docked: true, at: "home",
		refuelErr: map[string]error{"home": errors.New("Error: no_fuel_source")},
	}
	deps, _ := probeDeps(t, dry, map[string]int{"a": 1})
	if _, err := ProbeStations(context.Background(), deps, []ProbeTarget{probeTarget("a", hexStarBase, 1)}); err == nil {
		t.Error("want an error when the tank is empty and the origin will not refuel")
	}

	partial := &probeFakeClient{
		fuel: 40, docked: true, at: "home",
		refuelErr: map[string]error{"home": errors.New("Error: no_fuel_source")},
	}
	deps2, _ := probeDeps(t, partial, map[string]int{"a": 1})
	got, err := ProbeStations(context.Background(), deps2, []ProbeTarget{probeTarget("a", hexStarBase, 1)})
	if err != nil {
		t.Fatalf("a partial tank must still fly: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("verdicts = %d, want the run to proceed on the fuel aboard", len(got))
	}
}

// Undocked with fuel is fine; undocked without it cannot be fixed from here.
func TestProbePreflightUndocked(t *testing.T) {
	c := &probeFakeClient{fuel: 0, docked: false, at: "void"}
	deps, _ := probeDeps(t, c, map[string]int{"a": 1})
	if _, err := ProbeStations(context.Background(), deps, []ProbeTarget{probeTarget("a", hexStarBase, 1)}); err == nil {
		t.Error("want an error when undocked with an empty tank")
	}
	if len(c.refuels) != 0 {
		t.Error("must not attempt a refuel while undocked")
	}
}

// A dismantled station answers `Unknown destination` after you have already
// spent the fuel to get there. That is a conclusive fact and must be banked,
// not discarded as an ordinary transit failure -- otherwise every later run
// spends the same fuel to learn the same thing. Both ghosts on the 2026-08-12
// survey (Veilwatch Shoal, ENDL Kitalpha Cache) failed exactly this way.
func TestProbeRecordsAVanishedStation(t *testing.T) {
	c := &probeFakeClient{
		fuel: 120,
		travelErr: map[string]error{
			"gonesys": errors.New(`travel failed: {"code":"invalid_poi","message":"Unknown destination: x"}`),
		},
	}
	deps, acc := probeDeps(t, c, map[string]int{"gonesys": 3})

	got, err := ProbeStations(context.Background(), deps, []ProbeTarget{probeTarget("gonesys", hexStarBase, 3)})
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if !acc.Gone(hexStarBase) {
		t.Fatal("a station answering `Unknown destination` must be recorded as gone")
	}
	if len(got) != 1 || got[0].Reached {
		t.Fatalf("a vanished station was not reached: %+v", got)
	}
	if len(c.docks) != 0 {
		t.Fatalf("must not attempt a dock at a station that does not exist: %v", c.docks)
	}
}

// The payoff: once known gone, the tour must not spend another leg on it. The
// survey's budget is its whole constraint -- 19 stations need 200 fuel against
// a 120 tank -- so a wasted leg costs a real station at the far end.
func TestProbeSkipsStationsAlreadyKnownGone(t *testing.T) {
	c := &probeFakeClient{fuel: 120}
	deps, acc := probeDeps(t, c, map[string]int{"gonesys": 3, "goodsys": 3})
	acc.RecordTransit(hexStarBase, errors.New("Unknown destination: x"))

	got, err := ProbeStations(context.Background(), deps, []ProbeTarget{
		probeTarget("gonesys", hexStarBase, 3),
		probeTarget("goodsys", blackthornBase, 3),
	})
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the known-gone stop must not be attempted, got %d verdict(s): %+v", len(got), got)
	}
	if got[0].Target.StationID != blackthornBase {
		t.Fatalf("wrong station surveyed: %+v", got[0])
	}
}
