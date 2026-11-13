package worker

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// arrivalFakeClient adds the in-system calls a sweep pass makes.
type arrivalFakeClient struct {
	sweepFakeClient
}

func (c *arrivalFakeClient) Dock(context.Context) error {
	c.calls = append(c.calls, "dock")
	c.state.Doc = true
	return nil
}

func (c *arrivalFakeClient) Refuel(context.Context) error {
	c.calls = append(c.calls, "refuel")
	c.state.Ship.Fuel = c.state.Ship.MaxFuel
	return nil
}

func (c *arrivalFakeClient) GetStatus(context.Context) error { return nil }

func systemLoc(systemID string, repeaters int) []byte {
	reps := make([]any, repeaters)
	for i := range reps {
		reps[i] = map[string]any{"repeater_id": "repeater_x", "name": "Repeater Monolith", "ship_class": "monolith"}
	}
	b, _ := json.Marshal(map[string]any{"location": map[string]any{
		"system_id": systemID, "poi_id": systemID + "_poi", "nearby_repeaters": reps,
	}})
	return b
}

// sweepSeams records tour / explore-step / navigation calls instead of
// flying them.
type sweepSeams struct{ tours, steps, navs []string }

func newArrivalDispatch(t *testing.T, c *arrivalFakeClient, kb knowledge.Base, s *sweep.State) (*WorkerDispatch, string, *sweepSeams) {
	t.Helper()
	d, path := newSweepDispatch(t, &c.sweepFakeClient, kb, s)
	d.Client = c
	seams := &sweepSeams{}
	d.sweepTour = func(context.Context, ExploreDeps) error {
		seams.tours = append(seams.tours, c.state.System.ID)
		return nil
	}
	d.sweepExploreStep = func(context.Context, ExploreDeps) error {
		seams.steps = append(seams.steps, c.state.System.ID)
		return nil
	}
	d.ensureHomeNav = func(_ context.Context, system, poi string) error {
		seams.navs = append(seams.navs, system+"/"+poi)
		return nil
	}
	return d, path, seams
}

func inSystem(id string, fuel float64) *arrivalFakeClient {
	st := shipWith(fuel, 1020)
	st.System.ID = id
	st.CurrentTick = 2_400_000
	c := &arrivalFakeClient{sweepFakeClient{state: st, launchCost: 45, locs: [][]byte{systemLoc(id, 0)}}}
	return c
}

func placeSystem(t *testing.T, kb knowledge.Base, id string, x, y float64, visited, updated int64, conns ...string) {
	t.Helper()
	sc := make([]knowledge.SystemConnection, 0, len(conns))
	for _, c := range conns {
		sc = append(sc, knowledge.SystemConnection{SystemID: c, Distance: 1})
	}
	if err := kb.RememberSystem(context.Background(), knowledge.System{ID: id, Name: id,
		Position: game.Position{X: x, Y: y}, LastVisitedTick: visited, LastUpdatedTick: updated, Connections: sc}); err != nil {
		t.Fatal(err)
	}
}

func exploringState(ring, progress float64) *sweep.State {
	s := sweep.NewState(ring, 148, 45)
	s.Phase = sweep.PhaseExplore
	s.ProgressDeg = progress
	s.LaunchCostMeasured = true
	s.ArrivalToured = true
	return s
}

func TestSweepArrivalRecordsMeasuredPosition(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "new_sys", 0, 0, 1, 1)
	s := sweep.NewState(7500, 148, 45)
	s.Drifted, s.ReadSinceLaunch, s.LastX, s.LastY = true, true, -6010, 4120
	c := inSystem("new_sys", 900)
	d, path, seams := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	sys, _ := kb.GetSystem(context.Background(), "new_sys")
	if sys.Position.X != -6010 || sys.Position.Y != 4120 {
		t.Errorf("position = %+v, want the last drift reading", sys.Position)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseExplore || got.Drifted || !got.ArrivalToured {
		t.Errorf("state %+v, want explore, not drifted, toured", got)
	}
	if len(seams.tours) != 1 || len(c.jumps) != 0 {
		t.Errorf("tours %v jumps %v, want one tour and no launch", seams.tours, c.jumps)
	}
}

func TestSweepArrivalLeavesOnRepeaters(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "gloaming", -4170, -7769, 1, 1, "sallow")
	s := sweep.NewState(7900, -120, 45)
	s.ProgressDeg = -120
	s.Drifted, s.LastX, s.LastY = true, -4170, -7769
	c := inSystem("gloaming", 900)
	c.locs = [][]byte{systemLoc("gloaming", 1)}
	d, path, seams := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seams.tours)+len(seams.steps) != 0 {
		t.Errorf("toured/explored next to repeaters: %v %v", seams.tours, seams.steps)
	}
	got, _, _ := sweep.Load(path)
	if len(c.jumps) != 1 || !slices.Contains(got.Unfinished, "gloaming") || got.Phase != sweep.PhaseLap {
		t.Errorf("jumps %v unfinished %v phase %q, want one rejoin launch", c.jumps, got.Unfinished, got.Phase)
	}
}

func TestExploreRunStepsToUnvisited(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "here", -6000, 4000, 1, 2_400_000, "unseen")
	placeSystem(t, kb, "unseen", -6100, 4100, 0, 0, "here")
	c := inSystem("here", 900)
	d, path, seams := newArrivalDispatch(t, c, kb, exploringState(7600, 148))
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if len(seams.steps) != 1 || got.ExploreJumps != 1 || len(c.jumps) != 0 {
		t.Errorf("steps %v jumps %d launches %v", seams.steps, got.ExploreJumps, c.jumps)
	}
}

// Landing in well-known old-galaxy space must not send the ship off to
// re-capture every stale system in reach.
func TestExploreRunSkipsStale(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "alcor", 276, -7504, 1, 1, "mirzam")
	placeSystem(t, kb, "mirzam", 921, -7448, 5, 1, "alcor")
	c := inSystem("alcor", 900)
	d, _, seams := newArrivalDispatch(t, c, kb, exploringState(7500, -88))
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seams.steps) != 0 || len(c.jumps) != 1 {
		t.Errorf("steps %v jumps %v, want no explore step and a rejoin launch", seams.steps, c.jumps)
	}
}

func TestExploreRunCapsJumps(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "deep", -6000, 4000, 1, 2_400_000, "beyond")
	placeSystem(t, kb, "beyond", -6100, 4100, 0, 0, "deep")
	s := exploringState(7600, 148)
	s.ExploreJumps = 30
	c := inSystem("deep", 900)
	d, path, seams := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if len(seams.steps) != 0 || !slices.Contains(got.Unfinished, "deep") || len(c.jumps) != 1 {
		t.Errorf("steps %v unfinished %v jumps %v", seams.steps, got.Unfinished, c.jumps)
	}
}

func TestSweepRejoinTargetsSavedAngle(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	sx, sy := sweep.RingPoint(7600, 121)
	placeSystem(t, kb, "midway", sx, sy, 1, 2_400_000)
	c := inSystem("midway", 900)
	d, _, _ := newArrivalDispatch(t, c, kb, exploringState(7900, 120))
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	tx, ty := sweep.RingPoint(7900, 120)
	if len(c.jumps) != 1 || c.jumps[0] != round1(sweep.BearingTo(sx, sy, tx, ty)) {
		t.Errorf("jumps %v, want one toward the ring at 120°", c.jumps)
	}
}

func TestSweepRejoinFromUnplacedSystem(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "laneonly", 0, 0, 1, 2_400_000)
	s := exploringState(7900, 120)
	s.LastX, s.LastY = sweep.RingPoint(7600, 121)
	c := inSystem("laneonly", 900)
	d, _, _ := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	tx, ty := sweep.RingPoint(7900, 120)
	if len(c.jumps) != 1 || c.jumps[0] != round1(sweep.BearingTo(s.LastX, s.LastY, tx, ty)) {
		t.Errorf("jumps %v, want a bearing from the last drift reading", c.jumps)
	}
}

func TestSweepStartLaunchesFromSystem(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "nexus_prime", -4000, 2000, 1, 2_400_000)
	c := inSystem("nexus_prime", 900)
	d, path, _ := newArrivalDispatch(t, c, kb, sweep.NewState(7550, 148, 23))
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	tx, ty := sweep.RingPoint(7550, 148)
	if len(c.jumps) != 1 || c.jumps[0] != round1(sweep.BearingTo(-4000, 2000, tx, ty)) || got.TargetDeg != 148 {
		t.Errorf("jumps %v target %v, want first launch to the ring at 148°", c.jumps, got.TargetDeg)
	}
}

func TestSweepHomeDocksAndFinishes(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "frontier", -3000, -3000)
	s := sweep.NewState(7900, 148, 45)
	s.Phase, s.HomeSystem = sweep.PhaseHome, "frontier"
	c := inSystem("frontier", 30)
	d, path, seams := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseDone || !slices.Contains(c.calls, "dock") || !slices.Contains(c.calls, "refuel") {
		t.Fatalf("phase %q calls %v, want done after dock+refuel", got.Phase, c.calls)
	}
	if len(seams.navs) != 1 || seams.navs[0] != "frontier/frontier_station" {
		t.Errorf("navs %v, want frontier/frontier_station", seams.navs)
	}
	n := len(c.calls)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.calls) != n {
		t.Errorf("a done sweep still acted: %v", c.calls[n:])
	}
}

func TestSweepHomeWaitsForTowWhenDry(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "dry_hole", -5000, 5000, 1, 1)
	addSweepStation(t, kb, "far_station", 0, 3000) // not lane-connected
	s := sweep.NewState(7900, 148, 45)
	s.Phase, s.HomeSystem = sweep.PhaseHome, "far_station"
	c := inSystem("dry_hole", 10)
	d, _, seams := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.jumps) != 0 || len(seams.navs) != 0 {
		t.Errorf("jumps %v navs %v, want to sit and wait for the tow", c.jumps, seams.navs)
	}
}
