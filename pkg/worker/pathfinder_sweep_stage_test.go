package worker

import (
	"context"
	"os"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

const testStage = "node_beta,gsc_0049,ash_pearl"

// stageKB holds the staging route: Node Beta (station), GSC-0049 (launch
// point) and Ash Pearl (frontier drift target).
func stageKB(t *testing.T) knowledge.Base {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "node_beta", -3995, 2585)
	placeSystem(t, kb, "gsc_0049", -5894, 3073, 1, 1)
	placeSystem(t, kb, "ash_pearl", -6259, 3300, 0, 0)
	return kb
}

func stagedState(refueled bool) *sweep.State {
	s := sweep.NewState(7500, 148, 45)
	s.Phase = sweep.PhaseStage
	s.StageRefueled = refueled
	return s
}

// A fresh sweep with a stage configured does not launch from wherever the
// ship is: it flies to the refuel station first and tops up.
func TestSweepStageFreshRefuelsFirst(t *testing.T) {
	c := inSystem("sol", 300)
	d, path, seams := newArrivalDispatch(t, c, stageKB(t), stagedState(false))
	d.SweepStage = testStage
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seams.navs) != 1 || seams.navs[0] != "node_beta/node_beta_station" {
		t.Fatalf("navs = %v, want one trip to node_beta's station", seams.navs)
	}
	if len(c.jumps) != 0 {
		t.Errorf("launched %v before staging", c.jumps)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseStage || !got.StageRefueled || c.state.Ship.Fuel != 1020 {
		t.Errorf("phase %q refueled %v fuel %v, want stage/true/full", got.Phase, got.StageRefueled, c.state.Ship.Fuel)
	}
}

func TestSweepStageTravelsToLaunchPoint(t *testing.T) {
	c := inSystem("node_beta", 1020)
	d, _, seams := newArrivalDispatch(t, c, stageKB(t), stagedState(true))
	d.SweepStage = testStage
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seams.navs) != 1 || seams.navs[0] != "gsc_0049/" || len(c.jumps) != 0 {
		t.Errorf("navs %v jumps %v, want lanes to gsc_0049 and no launch", seams.navs, c.jumps)
	}
}

func TestSweepStageLaunchesTowardTarget(t *testing.T) {
	c := inSystem("gsc_0049", 1000)
	d, path, _ := newArrivalDispatch(t, c, stageKB(t), stagedState(true))
	d.SweepStage = testStage
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := round1(sweep.BearingTo(-5894, 3073, -6259, 3300))
	if len(c.jumps) != 1 || c.jumps[0] != want {
		t.Fatalf("jumps %v, want one launch on %v", c.jumps, want)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseStage || got.TargetX != -6259 || got.TargetY != 3300 || !got.Drifted {
		t.Errorf("state %+v, want stage phase drifting at ash_pearl", got)
	}
}

// Landing in the target ends the stage: the system is measured and explored
// like any frontier landing, and the lap still starts at the start angle.
func TestSweepStageLandingStartsExplore(t *testing.T) {
	kb := stageKB(t)
	s := stagedState(true)
	s.Drifted, s.ReadSinceLaunch, s.LastX, s.LastY = true, true, -6262, 3301
	s.TargetX, s.TargetY = -6259, 3300
	c := inSystem("ash_pearl", 950)
	d, path, seams := newArrivalDispatch(t, c, kb, s)
	d.SweepStage = testStage
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseExplore || got.ProgressDeg != 148 || got.SweptDeg != 0 || got.Landed {
		t.Errorf("state phase %q progress %v swept %v landed %v, want explore/148/0/false", got.Phase, got.ProgressDeg, got.SweptDeg, got.Landed)
	}
	if len(seams.tours) != 1 {
		t.Errorf("tours %v, want ash_pearl toured", seams.tours)
	}
}

// A stage drift that passes the target without landing gives up on it and
// heads for the ring.
func TestSweepStageMissFallsBackToLap(t *testing.T) {
	s := stagedState(true)
	s.Drifted, s.Launches, s.LaunchCostMeasured = true, 1, true
	s.TargetX, s.TargetY = -6259, 3300
	b := round1(sweep.BearingTo(-5894, 3073, -6259, 3300))
	s.PlannedBearing = b
	x, y := back(-6259, 3300, b, -40) // 40 LY past the target
	c := &sweepFakeClient{state: shipWith(900, 1020), launchCost: 45, locs: [][]byte{driftLoc(x, y, b)}}
	d, path := newSweepDispatch(t, c, stageKB(t), s)
	d.SweepStage = testStage
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseLap || len(c.jumps) != 1 {
		t.Fatalf("phase %q jumps %v, want lap and one launch", got.Phase, c.jumps)
	}
	rx, ry := sweep.RingPoint(7500, 148)
	if want := round1(sweep.BearingTo(x, y, rx, ry)); c.jumps[0] != want {
		t.Errorf("launch %v, want %v toward the 148° ring point", c.jumps[0], want)
	}
}

// Still short of the target, a stage drift just keeps going.
func TestSweepStageDriftWaitsShortOfTarget(t *testing.T) {
	s := stagedState(true)
	s.Drifted, s.Launches, s.LaunchCostMeasured = true, 1, true
	s.TargetX, s.TargetY = -6259, 3300
	b := round1(sweep.BearingTo(-5894, 3073, -6259, 3300))
	s.PlannedBearing = b
	x, y := back(-6259, 3300, b, 200)
	c := &sweepFakeClient{state: shipWith(900, 1020), launchCost: 45, locs: [][]byte{driftLoc(x, y, b)}}
	d, path := newSweepDispatch(t, c, stageKB(t), s)
	d.SweepStage = testStage
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if len(c.jumps) != 0 || got.Phase != sweep.PhaseStage {
		t.Errorf("jumps %v phase %q, want no relaunch while short of the target", c.jumps, got.Phase)
	}
}

func TestSweepBadStageRejected(t *testing.T) {
	c := inSystem("sol", 300)
	d, _, _ := newArrivalDispatch(t, c, stageKB(t), stagedState(false))
	d.SweepStage = "node_beta,gsc_0049"
	if err := d.PathfinderSweep(context.Background()); err == nil {
		t.Error("two-part stage accepted")
	}
}
