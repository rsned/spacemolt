package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// sourcedKB is a memory KB that also reports each system's position source,
// as the SQLite KB does after migration 75.
type sourcedKB struct {
	*knowledge.MemoryKB
	source map[string]string
}

func (k *sourcedKB) SystemPositionSource(_ context.Context, id string) (string, error) {
	return k.source[id], nil
}

// Review C1: a home drift that misses its station must be corrected while
// the reserve still covers a launch, not left drifting forever.
func TestSweepHomeDriftCorrectsMiss(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "haven_home", -6000, 4000)
	s := sweep.NewState(7900, 148, 45)
	s.Phase, s.HomeSystem, s.Drifted, s.Launches, s.LaunchCostMeasured = sweep.PhaseHome, "haven_home", true, 5, true
	s.TargetX, s.TargetY = -6000, 4000
	s.PlannedBearing = round1(sweep.BearingTo(-6500, 4500, -6000, 4000))
	x, y := back(-6000, 4000, s.PlannedBearing, -30) // 30 LY past it
	c := &sweepFakeClient{state: shipWith(100, 1020), launchCost: 45, locs: [][]byte{driftLoc(x, y, s.PlannedBearing)}}
	d, _ := newSweepDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.jumps) != 1 || c.jumps[0] != round1(sweep.BearingTo(x, y, -6000, 4000)) {
		t.Fatalf("jumps %v, want one correction back toward the station", c.jumps)
	}
}

// Review C1: a ship safe inside a system never launches into open space
// without one spare launch; it waits for the GSA tow instead.
func TestSweepNoLaunchFromSystemWithoutSpare(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "cluster", -6000, 4000, 1, 2_400_000)
	addSweepStation(t, kb, "far_station", 0, 3000)
	c := inSystem("cluster", 60)
	d, path, _ := newArrivalDispatch(t, c, kb, exploringState(7600, 148))
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if len(c.jumps) != 0 || got.Phase != sweep.PhaseHome {
		t.Fatalf("jumps %v phase %q, want no launch and phase home", c.jumps, got.Phase)
	}
}

func TestSweepHomeInSystemNeedsSpare(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "cluster", -6000, 4000, 1, 1)
	addSweepStation(t, kb, "far_station", 0, 3000)
	s := sweep.NewState(7900, 148, 45)
	s.Phase, s.HomeSystem = sweep.PhaseHome, "far_station"
	c := inSystem("cluster", 60)
	d, _, _ := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.jumps) != 0 {
		t.Fatalf("jumps %v, want none with only 60 fuel against a 45 launch", c.jumps)
	}
}

// Review I2: the landing advances progress, and a rejoin after an explore
// run aims past the landing so the ship does not land in the same system.
func TestSweepRejoinAfterLandingStepsPastLanding(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	lx, ly := sweep.RingPoint(7500, 146)
	placeSystem(t, kb, "landed", lx, ly, 1, 2_400_000)
	ox, oy := sweep.RingPoint(7600, 146.5)
	placeSystem(t, kb, "other", ox, oy, 1, 2_400_000)
	s := sweep.NewState(7500, 148, 45)
	s.LaunchCostMeasured, s.Launches, s.Drifted, s.ReadSinceLaunch = true, 1, true, true
	s.TargetX, s.TargetY = sweep.RingPoint(7500, 143)
	s.TargetDeg, s.LastX, s.LastY = 143, lx, ly
	c := inSystem("landed", 900)
	d, path, _ := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil { // arrival + tour
		t.Fatal(err)
	}
	c.state.System.ID = "other"
	c.locs, c.locIdx = [][]byte{systemLoc("other", 0)}, 0
	if err := d.PathfinderSweep(context.Background()); err != nil { // no targets: rejoin
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	nx, ny := sweep.RingPoint(7500, 141)
	if len(c.jumps) != 1 || c.jumps[0] != round1(sweep.BearingTo(ox, oy, nx, ny)) {
		t.Fatalf("jumps %v, want one toward the ring at 141°", c.jumps)
	}
	if got.ProgressDeg != 146 || got.TargetDeg != 141 {
		t.Errorf("progress %v target %v, want 146 / 141", got.ProgressDeg, got.TargetDeg)
	}
}

// Review I3: landing in a system with map (API) coordinates is old-galaxy
// ground; the ship relaunches without touring or exploring.
func TestSweepSkipsExploreInAPISystem(t *testing.T) {
	kb := &sourcedKB{MemoryKB: knowledge.NewMemoryKB(), source: map[string]string{"alcor": "api"}}
	placeSystem(t, kb, "alcor", 276, -7504, 0, 0, "unvisited_neighbour")
	placeSystem(t, kb, "unvisited_neighbour", 300, -7400, 0, 0, "alcor")
	s := sweep.NewState(7500, -88, 45)
	s.LaunchCostMeasured, s.Launches, s.Drifted, s.ReadSinceLaunch = true, 1, true, true
	s.LastX, s.LastY = 276, -7504
	c := inSystem("alcor", 900)
	d, _, seams := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seams.tours)+len(seams.steps) != 0 || len(c.jumps) != 1 {
		t.Fatalf("tours %v steps %v jumps %v, want an immediate relaunch", seams.tours, seams.steps, c.jumps)
	}
}

// Review I4: a brand-new system has no KB row on arrival; its measured
// position is kept and written once the tour has created the row.
func TestSweepMeasuresNewSystemAfterTour(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	s := sweep.NewState(7600, 148, 45)
	s.LaunchCostMeasured, s.Launches, s.Drifted, s.ReadSinceLaunch = true, 1, true, true
	s.LastX, s.LastY = -6100, 4300
	c := inSystem("brand_new", 900)
	d, _, _ := newArrivalDispatch(t, c, kb, s)
	d.sweepTour = func(context.Context, ExploreDeps) error {
		placeSystem(t, kb, "brand_new", 0, 0, 1, 2_400_000)
		return nil
	}
	for range 2 {
		if err := d.PathfinderSweep(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	sys, _ := kb.GetSystem(context.Background(), "brand_new")
	if sys == nil || sys.Position.X != -6100 || sys.Position.Y != 4300 {
		t.Fatalf("brand_new = %+v, want the measured position", sys)
	}
}

// Review I5: a drift that lands before any reading was taken has no
// measurement; the launch point must not be recorded as the new system's.
func TestSweepArrivalWithoutDriftReadingSkipsMeasure(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "quick_hit", 0, 0, 1, 1)
	s := sweep.NewState(7600, 148, 45)
	s.LaunchCostMeasured, s.Launches, s.Drifted = true, 1, true
	s.LastX, s.LastY = -5000, 4000 // where it launched from
	c := inSystem("quick_hit", 900)
	d, _, _ := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sys, _ := kb.GetSystem(context.Background(), "quick_hit"); sys.Position.X != 0 || sys.Position.Y != 0 {
		t.Fatalf("position = %+v, want unrecorded", sys.Position)
	}
}

// Review I6: without a launch-cost estimate the fuel reserve is
// meaningless; refuse to start.
func TestSweepRequiresLaunchCost(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "anywhere", -5000, 4000, 1, 1)
	noSweepSleep(t)
	c := inSystem("anywhere", 900)
	d := NewWorkerDispatch(c, kb, nil, nil)
	d.AgentID, d.AgentsDir, d.SweepRing, d.SweepStartDeg = "explorer-9", t.TempDir(), 7700, 148
	if err := d.PathfinderSweep(context.Background()); err == nil {
		t.Fatal("want an error for a missing sweep_launch_cost")
	}
	if len(c.jumps) != 0 {
		t.Fatalf("launched %v with no launch-cost estimate", c.jumps)
	}
}

// Review I7: the launch-cost measurement reads fuel after the refill lands.
func TestSweepMeasuresCostAfterRefill(t *testing.T) {
	s, b := lapLeg()
	s.LaunchCostMeasured = false
	x, y := back(s.TargetX, s.TargetY, b, 5)
	c := &sweepFakeClient{state: shipWith(40, 1020, cellCargo(30)...), launchCost: 45, lag: true, locs: [][]byte{driftLoc(x, y, b)}}
	d, path := newSweepDispatch(t, c, knowledge.NewMemoryKB(), s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if !got.LaunchCostMeasured || got.LaunchCost != 45 {
		t.Fatalf("launch cost %v measured %v, want 45 true", got.LaunchCost, got.LaunchCostMeasured)
	}
}

func cellCargo(n float64) []game.CargoItem {
	return []game.CargoItem{{ItemID: "fuel_cell", Quantity: n}}
}
