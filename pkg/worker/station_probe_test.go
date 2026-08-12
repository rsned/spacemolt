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
}

func (c *probeFakeClient) GetState() *game.State {
	s := &game.State{}
	// GetFuel() reads State.Fuel/State.MaxFuel, NOT State.Ship.*
	s.Fuel = c.fuel
	s.MaxFuel = 120
	s.System.ID = c.at

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

	return c.refuelErr[c.at]
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
		fuel:      120,
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

// Losing state mid-run must halt the survey. Treating an unreadable tank as
// empty would be survivable; treating it as full would strand the ship.
func TestProbeHaltsWhenFuelCannotBeRead(t *testing.T) {
	c := &nilStateClient{}
	deps, _ := probeDeps(t, &probeFakeClient{fuel: 120}, map[string]int{"a": 1})
	deps.Client = c
	var sb strings.Builder
	deps.Out = &sb
	got, err := ProbeStations(context.Background(), deps, []ProbeTarget{probeTarget("a", hexStarBase, 1)})
	if err != nil {
		t.Fatalf("ProbeStations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("verdicts = %d, want 0 when the tank cannot be read", len(got))
	}
	// A lost connection and an exhausted fuel budget both end the run, but they
	// mean different things to whoever reads the log: one is a fault to chase,
	// the other is the survey finishing as designed. Saying "short on fuel" when
	// the tank was simply unreadable sends the operator after the wrong problem.
	if !strings.Contains(sb.String(), "cannot read fuel") {
		t.Errorf("want an unreadable-tank halt to say so, got %q", sb.String())
	}
}

type nilStateClient struct{ game.GameClient }

func (c *nilStateClient) GetState() *game.State { return nil }

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
