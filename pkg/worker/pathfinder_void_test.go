package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// voidWave is V01's square wave: east along y=0, up 100, back west.
var voidWave = []sweep.Point{{X: 1000, Y: 0}, {X: 2000, Y: 0}, {X: 2000, Y: 100}, {X: 1000, Y: 100}}

// voidLedger writes a run: V01 (voidWave) and, with second, a smaller V02.
func voidLedger(t *testing.T, second bool) string {
	t.Helper()
	r := &sweep.VoidRun{Created: time.Now().UTC().Format(time.RFC3339), Zones: []*sweep.VoidZone{
		{Zone: sweep.Zone{ID: "V01", Area: 2e6}, Waypoints: slices.Clone(voidWave)},
	}}
	if second {
		r.Zones = append(r.Zones, &sweep.VoidZone{Zone: sweep.Zone{ID: "V02", Area: 1e6},
			Waypoints: []sweep.Point{{X: 0, Y: 1500}, {X: 900, Y: 1500}}})
	}
	path := filepath.Join(t.TempDir(), "void-run.json")
	if err := sweep.SaveVoidRun(path, r); err != nil {
		t.Fatal(err)
	}
	return path
}

func zoneOf(t *testing.T, ledger, id string) *sweep.VoidZone {
	t.Helper()
	r, err := sweep.LoadVoidRun(ledger)
	if err != nil {
		t.Fatal(err)
	}
	for _, z := range r.Zones {
		if z.ID == id {
			return z
		}
	}
	t.Fatalf("no zone %s", id)
	return nil
}

// voidState is explorer-3 flying V01 at waypoint wp.
func voidState(t *testing.T, ledger string, wp int) *sweep.State {
	t.Helper()
	if _, err := sweep.ClaimZone(ledger, "explorer-3", 90, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if wp > 0 {
		if err := sweep.SaveProgress(ledger, "explorer-3", "V01", wp, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	s := sweep.NewVoidState(45)
	s.LaunchCostMeasured = true
	s.ZoneID, s.Waypoint, s.Waypoints = "V01", wp, slices.Clone(voidWave)
	return s
}

func voidDispatch(t *testing.T, c *arrivalFakeClient, kb knowledge.Base, s *sweep.State, ledger string) (*WorkerDispatch, string, *bytes.Buffer) {
	t.Helper()
	d, path, _ := newArrivalDispatch(t, c, kb, s)
	d.SweepMode, d.SweepVoidRun = "void", ledger
	out := &bytes.Buffer{}
	d.Out = out
	return d, path, out
}

func loadVoidState(t *testing.T, path string) *sweep.State {
	t.Helper()
	s, _, err := sweep.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func lastJump(t *testing.T, c *arrivalFakeClient) float64 {
	t.Helper()
	if len(c.jumps) == 0 {
		t.Fatalf("no launch (calls %v)", c.calls)
	}
	return c.jumps[len(c.jumps)-1]
}

func TestVoidDockedWithNoZoneClaimsAndHeadsForTheFirstWaypoint(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 500, 0)
	ledger := voidLedger(t, false)
	c := inSystem("home", 900)
	c.state.Doc = true
	s := sweep.NewVoidState(45)
	s.LaunchCostMeasured = true
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := loadVoidState(t, path); got.ZoneID != "V01" || len(got.Waypoints) != 4 {
		t.Fatalf("state zone %q waypoints %d, want V01 with 4", got.ZoneID, len(got.Waypoints))
	}
	if b := lastJump(t, c); b != 0 {
		t.Errorf("launched on %.1f, want 0 (east to (1000,0))", b)
	}
	if z := zoneOf(t, ledger, "V01"); z.Claimant != "explorer-3" {
		t.Errorf("ledger claimant %q", z.Claimant)
	}
}

func TestVoidDriftReachingAWaypointAdvancesAndSavesProgress(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 1)
	s.Drifted, s.PlannedBearing = true, 0
	s.TargetX, s.TargetY = 2000, 0
	c := inSystem("x", 900)
	c.locs = [][]byte{driftLoc(2000, 0, 0)}
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := loadVoidState(t, path); got.Waypoint != 2 {
		t.Fatalf("waypoint %d, want 2", got.Waypoint)
	}
	if z := zoneOf(t, ledger, "V01"); z.Next != 2 {
		t.Errorf("ledger next %d, want 2", z.Next)
	}
	if b := lastJump(t, c); b != 90 {
		t.Errorf("redirect on %.1f, want 90 (north to the next lane)", b)
	}
}

func TestVoidStormDoesNotAdvance(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 1)
	s.Drifted, s.PlannedBearing = true, 0
	s.TargetX, s.TargetY = 2000, 0
	c := inSystem("x", 900)
	c.locs = [][]byte{driftLoc(1500, 0, 10)}
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := loadVoidState(t, path)
	if got.Waypoint != 1 || len(got.Gaps) != 1 {
		t.Fatalf("waypoint %d gaps %v, want 1 and one storm gap", got.Waypoint, got.Gaps)
	}
	if b := lastJump(t, c); b != 0 {
		t.Errorf("re-aim on %.1f, want 0 (same waypoint)", b)
	}
}

func TestVoidLandingRejoinsAtTheCurrentWaypoint(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "hidden_one", 1500, 0, 1, 1)
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 1)
	s.Drifted = true
	c := inSystem("hidden_one", 900)
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	for range 2 { // landing + tour, then the rejoin
		if err := d.PathfinderSweep(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := loadVoidState(t, path); !slices.Contains(got.ZoneFound, "hidden_one") {
		t.Errorf("zone found %v, want hidden_one", got.ZoneFound)
	}
	if z := zoneOf(t, ledger, "V01"); !slices.Contains(z.Found, "hidden_one") {
		t.Errorf("ledger found %v, want hidden_one", z.Found)
	}
	if b := lastJump(t, c); b != 0 {
		t.Errorf("rejoin on %.1f, want 0 (on to (2000,0))", b)
	}
}

func TestVoidLastWaypointMarksZoneDoneAndHeadsHome(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 500, 100)
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 3)
	s.Drifted, s.PlannedBearing = true, 180
	s.TargetX, s.TargetY = 1000, 100
	c := inSystem("x", 900)
	c.locs = [][]byte{driftLoc(1000, 100, 180)}
	d, path, out := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if z := zoneOf(t, ledger, "V01"); !z.Done {
		t.Fatalf("zone %+v, want done", z)
	}
	if got := loadVoidState(t, path); got.Phase != sweep.PhaseHome || got.ZoneID != "" {
		t.Errorf("phase %s zone %q, want home with no zone", got.Phase, got.ZoneID)
	}
	if !strings.Contains(out.String(), "V01 done") {
		t.Errorf("output %q should report the zone done", out.String())
	}
}

func TestVoidDockedAfterZoneClaimsTheNext(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 0, 1000)
	ledger := voidLedger(t, true)
	s := voidState(t, ledger, 4) // V01 finished
	s.ZoneID, s.Waypoints, s.Waypoint, s.Phase = "", nil, 0, sweep.PhaseDone
	c := inSystem("home", 1020)
	c.state.Doc = true
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := loadVoidState(t, path); got.ZoneID != "V02" {
		t.Fatalf("zone %q, want V02", got.ZoneID)
	}
	if b := lastJump(t, c); b != 90 {
		t.Errorf("launch on %.1f, want 90 (north to (0,1500))", b)
	}
}

func TestVoidNoZonesLeftStaysDocked(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 0, 1000)
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 4)
	s.ZoneID, s.Waypoints, s.Waypoint, s.Phase = "", nil, 0, sweep.PhaseDone
	c := inSystem("home", 1020)
	c.state.Doc = true
	d, _, out := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.jumps) != 0 || !strings.Contains(out.String(), "no zones left") {
		t.Fatalf("jumps %v output %q, want no launch and 'no zones left'", c.jumps, out.String())
	}
}

func TestVoidRestartResumesSavedWaypoint(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "parked", 2000, 0, 1, 1)
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 2)
	c := inSystem("parked", 900)
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := loadVoidState(t, path); got.ZoneID != "V01" || got.Waypoint != 2 {
		t.Fatalf("zone %q waypoint %d, want V01 at 2", got.ZoneID, got.Waypoint)
	}
	if b := lastJump(t, c); b != 90 {
		t.Errorf("launch on %.1f, want 90 (to (2000,100))", b)
	}
}

func TestVoidLowFuelGoesHomeThenResumes(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 2000, -500)
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 1)
	s.Drifted, s.PlannedBearing = true, 0
	s.TargetX, s.TargetY = 2000, 0
	c := inSystem("x", 100) // under the home threshold
	c.locs = [][]byte{driftLoc(2000, 0, 0)}
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := loadVoidState(t, path)
	if got.Phase != sweep.PhaseHome || got.ZoneID != "V01" || got.Waypoint != 2 {
		t.Fatalf("phase %s zone %q wp %d, want home, keeping V01 at 2", got.Phase, got.ZoneID, got.Waypoint)
	}

	// Docked at home and refuelled: back out to waypoint 2.
	got.Phase, got.Drifted = sweep.PhaseDone, false
	if err := sweep.Save(path, got); err != nil {
		t.Fatal(err)
	}
	c2 := inSystem("home", 100)
	c2.state.Doc = true
	d.Client = c2
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b := lastJump(t, c2); b != 90 {
		t.Errorf("resume launch on %.1f, want 90 (from (2000,-500) to (2000,100))", b)
	}
}

func TestVoidLostLeaseDropsTheZone(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "parked", 2000, 0, 1, 1)
	addSweepStation(t, kb, "home", 0, 1000) // V02 needs a station within reach
	ledger := voidLedger(t, true)
	s := voidState(t, ledger, 2)
	// Another explorer took V01 over (our lease lapsed).
	r, _ := sweep.LoadVoidRun(ledger)
	r.Zones[0].Claimant = "explorer-9"
	if err := sweep.SaveVoidRun(ledger, r); err != nil {
		t.Fatal(err)
	}
	c := inSystem("parked", 900)
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := loadVoidState(t, path); got.ZoneID != "V02" {
		t.Fatalf("zone %q, want V01 dropped and V02 claimed", got.ZoneID)
	}
}

// Review C1: a lease lost while adrift must not leave the ship drifting on
// with nothing to aim at.
func TestVoidLeaseLostAdriftClaimsAnotherAndReaims(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 0, 1000)
	ledger := voidLedger(t, true)
	s := voidState(t, ledger, 1)
	s.Drifted, s.PlannedBearing = true, 0
	r, _ := sweep.LoadVoidRun(ledger)
	r.Zones[0].Claimant = "explorer-9"
	if err := sweep.SaveVoidRun(ledger, r); err != nil {
		t.Fatal(err)
	}
	c := inSystem("x", 900)
	c.locs = [][]byte{driftLoc(1500, 0, 0)}
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := loadVoidState(t, path); got.ZoneID != "V02" {
		t.Fatalf("zone %q, want V02 claimed mid-drift", got.ZoneID)
	}
	lastJump(t, c) // re-aimed
}

// Review I1: a landing caught inside the drift poll is recorded too.
func TestVoidLandingCaughtMidDriftIsRecorded(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "hidden_one", 1500, 0, 1, 1)
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 1)
	s.Drifted, s.PlannedBearing = true, 0
	c := inSystem("hidden_one", 900)
	c.locs = [][]byte{driftLoc(1400, 0, 0), systemLoc("hidden_one", 0)}
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := loadVoidState(t, path); !slices.Contains(got.ZoneFound, "hidden_one") {
		t.Fatalf("zone found %v, want hidden_one", got.ZoneFound)
	}
}

// Review I3: a landing far outside the zone (on the long drift in) is not
// credited to it.
func TestVoidFindOutsideTheZoneIsNotCounted(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "far_one", 5000, 5000, 1, 1)
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 0)
	s.Drifted = true
	c := inSystem("far_one", 900)
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := loadVoidState(t, path); len(got.ZoneFound) != 0 {
		t.Fatalf("zone found %v, want nothing", got.ZoneFound)
	}
}

// Review I2: a system sitting on the waypoint captures every relaunch at
// it; the rejoin steps on to the next waypoint instead of looping.
func TestVoidLandingOnTheWaypointStepsPastIt(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "on_wp", 1980, 0, 1, 1)
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 1)
	s.Phase = sweep.PhaseExplore
	s.ArrivalToured = true
	c := inSystem("on_wp", 900)
	d, path, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := loadVoidState(t, path)
	if got.Waypoint != 2 || len(got.Gaps) != 1 {
		t.Fatalf("waypoint %d gaps %v, want 2 with a gap logged", got.Waypoint, got.Gaps)
	}
	if b := lastJump(t, c); b != round1(sweep.BearingTo(1980, 0, 2000, 100)) {
		t.Errorf("rejoin on %.1f, want toward (2000,100)", b)
	}
}

// Review I3: the next zone after a finished one is staged by lanes too.
func TestVoidNextZoneIsStaged(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 0, 1000)
	placeSystem(t, kb, "near_v02", 4900, 1500, 1, 1, "home")
	r := &sweep.VoidRun{Zones: []*sweep.VoidZone{
		{Zone: sweep.Zone{ID: "V01", Area: 2e6}, Waypoints: slices.Clone(voidWave), Done: true, Next: 4},
		{Zone: sweep.Zone{ID: "V02", Area: 1e6}, Waypoints: []sweep.Point{{X: 5000, Y: 1500}, {X: 5900, Y: 1500}}},
	}}
	ledger := filepath.Join(t.TempDir(), "void-run.json")
	if err := sweep.SaveVoidRun(ledger, r); err != nil {
		t.Fatal(err)
	}
	addSweepStation(t, kb, "v02_station", 5200, 1600)
	s := sweep.NewVoidState(45)
	s.LaunchCostMeasured, s.Phase = true, sweep.PhaseDone
	c := inSystem("home", 1020)
	c.state.Doc = true
	d, _, _ := voidDispatch(t, c, kb, s, ledger)
	seams := d.ensureHomeNav
	var navs []string
	d.ensureHomeNav = func(ctx context.Context, sys, poi string) error {
		navs = append(navs, sys)
		return seams(ctx, sys, poi)
	}
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(navs, "near_v02") || len(c.jumps) != 0 {
		t.Fatalf("navs %v jumps %v, want lanes to near_v02 and no launch yet", navs, c.jumps)
	}
}

// Review I4: done but undocked (play_as undocked it) goes back to a station
// rather than holding the zone forever.
func TestVoidDoneButUndockedDocksAgain(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 0, 1000)
	ledger := voidLedger(t, false)
	s := voidState(t, ledger, 2)
	s.Phase = sweep.PhaseDone
	c := inSystem("home", 900)
	d, _, _ := voidDispatch(t, c, kb, s, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.calls, "dock") {
		t.Fatalf("calls %v, want a dock", c.calls)
	}
}

// Review M1 (re-graded): switching a finished ring ship to void mode
// refuels at its station before the first launch.
func TestVoidFirstSwitchRefuelsBeforeLaunching(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 500, 0)
	ledger := voidLedger(t, false)
	ring := sweep.NewState(7500, 148, 45)
	ring.Phase, ring.LaunchCostMeasured = sweep.PhaseDone, true
	c := inSystem("home", 100)
	c.state.Doc = true
	d, _, _ := voidDispatch(t, c, kb, ring, ledger)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, j := slices.Index(c.calls, "refuel"), slices.IndexFunc(c.calls, func(x string) bool { return strings.HasPrefix(x, "jump ") })
	if r < 0 || (j >= 0 && j < r) {
		t.Fatalf("calls %v, want a refuel before any launch", c.calls)
	}
}

// Rollout gap #4 (11-15, explorer-5): staging that hits "action pending"
// waits it out and keeps lane-staging instead of launching from far away.
func TestVoidStagingWaitsOutAPendingAction(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 0, 1000)
	placeSystem(t, kb, "near_v02", 4900, 1500, 1, 1, "home")
	r := &sweep.VoidRun{Zones: []*sweep.VoidZone{
		{Zone: sweep.Zone{ID: "V02", Area: 1e6}, Waypoints: []sweep.Point{{X: 5000, Y: 1500}, {X: 5900, Y: 1500}}},
	}}
	ledger := filepath.Join(t.TempDir(), "void-run.json")
	if err := sweep.SaveVoidRun(ledger, r); err != nil {
		t.Fatal(err)
	}
	addSweepStation(t, kb, "v02_station", 5200, 1600)
	s := sweep.NewVoidState(45)
	s.LaunchCostMeasured, s.Phase = true, sweep.PhaseDone
	c := inSystem("home", 1020)
	c.state.Doc = true
	d, _, _ := voidDispatch(t, c, kb, s, ledger)
	var navs []string
	d.ensureHomeNav = func(_ context.Context, sys, _ string) error {
		navs = append(navs, sys)
		if len(navs) == 1 {
			return errors.New("jump 5/9 to Atlas failed: action pending: another action () is in progress")
		}
		return nil
	}
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(navs) != 2 || navs[1] != "near_v02" || len(c.jumps) != 0 {
		t.Fatalf("navs %v jumps %v, want a second lane attempt to near_v02 and no launch", navs, c.jumps)
	}
}

// Rollout gap #5 (11-15): explorer-9 picked Taygeta (avoided) and explorer-8
// picked Turnback (frontier, no lanes) as staging points, failed to route and
// launched from far away. Staging tries the next-nearest system instead.
func TestVoidStagingSkipsUnroutableSystems(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 0, 1000)
	placeSystem(t, kb, "turnback", 4950, 1500, 1, 1, "home")
	placeSystem(t, kb, "taygeta", 4900, 1500, 1, 1, "home")
	placeSystem(t, kb, "near_v02", 4800, 1500, 1, 1, "home")
	r := &sweep.VoidRun{Zones: []*sweep.VoidZone{
		{Zone: sweep.Zone{ID: "V02", Area: 1e6}, Waypoints: []sweep.Point{{X: 5000, Y: 1500}, {X: 5900, Y: 1500}}},
	}}
	ledger := filepath.Join(t.TempDir(), "void-run.json")
	if err := sweep.SaveVoidRun(ledger, r); err != nil {
		t.Fatal(err)
	}
	addSweepStation(t, kb, "v02_station", 5200, 1600)
	s := sweep.NewVoidState(45)
	s.LaunchCostMeasured, s.Phase = true, sweep.PhaseDone
	c := inSystem("home", 1020)
	c.state.Doc = true
	d, _, _ := voidDispatch(t, c, kb, s, ledger)
	var navs []string
	d.ensureHomeNav = func(_ context.Context, sys, _ string) error {
		navs = append(navs, sys)
		switch sys {
		case "turnback":
			return errors.New("find_route failed: no route found: No route found to target system")
		case "taygeta":
			return fmt.Errorf("%w: destination Taygeta", ErrRouteThroughAvoided)
		}
		return nil
	}
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"turnback", "taygeta", "near_v02"}; !slices.Equal(navs, want) || len(c.jumps) != 0 {
		t.Fatalf("navs %v jumps %v, want %v and no launch yet", navs, c.jumps, want)
	}
}

// When no staging system can be reached, the ship launches from where it is
// instead of reporting a move it never made.
func TestVoidStagingLaunchesWhenNothingIsRoutable(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "home", 0, 1000)
	placeSystem(t, kb, "turnback", 4950, 1500, 1, 1)
	r := &sweep.VoidRun{Zones: []*sweep.VoidZone{
		{Zone: sweep.Zone{ID: "V02", Area: 1e6}, Waypoints: []sweep.Point{{X: 5000, Y: 1500}, {X: 5900, Y: 1500}}},
	}}
	ledger := filepath.Join(t.TempDir(), "void-run.json")
	if err := sweep.SaveVoidRun(ledger, r); err != nil {
		t.Fatal(err)
	}
	addSweepStation(t, kb, "v02_station", 5200, 1600)
	s := sweep.NewVoidState(45)
	s.LaunchCostMeasured, s.Phase = true, sweep.PhaseDone
	c := inSystem("home", 1020)
	c.state.Doc = true
	d, _, _ := voidDispatch(t, c, kb, s, ledger)
	d.ensureHomeNav = func(_ context.Context, sys, _ string) error {
		if sys == "home" {
			return nil
		}
		return errors.New("no route found")
	}
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.jumps) == 0 {
		t.Fatalf("calls %v, want a launch from home after every staging system failed", c.calls)
	}
}

// Staging picks only systems the lanes can reach from here: slow_clock and
// bristle (a hidden island, lanes only to each other; explorer-5/-8 11-16) and an avoided system (explorer-9 ->
// Taygeta, 11-15) are skipped, as is a stronghold without the unlock.
// Nearest first among the rest.
func TestVoidStageCandidatesSkipLanelessAndAvoided(t *testing.T) {
	sys := func(id string, x float64) knowledge.System {
		s := knowledge.System{ID: id}
		s.Position.X, s.Position.Y = x, 1
		return s
	}
	hold := sys("hold", 2)
	hold.IsStronghold = true
	systems := []knowledge.System{sys("slow_clock", 1), hold, sys("taygeta", 3), sys("near", 4), sys("far", 9)}
	conns := []knowledge.Connection{
		{FromSystem: "here", ToSystem: "near"},
		{FromSystem: "hold", ToSystem: "near"}, {FromSystem: "taygeta", ToSystem: "near"},
		{FromSystem: "near", ToSystem: "far"},
		// A hidden island: lanes, but none to the charted map (explorer-8, 11-16).
		{FromSystem: "slow_clock", ToSystem: "bristle"},
	}
	systems = append(systems, sys("bristle", 1.5))
	avoid := map[string]string{"taygeta": "leviathan"}
	got := voidStageCandidates(systems, conns, avoid, false, "here", 0, 1)
	var ids []string
	for _, c := range got {
		ids = append(ids, c.id)
	}
	if want := []string{"near", "far"}; !slices.Equal(ids, want) {
		t.Fatalf("candidates %v, want %v", ids, want)
	}
}

// A ship whose tank holds many launches may take zones farther from a full
// station: the western zones (V12, V26, V28, V30, V32) sit 2,658-3,466 LY from
// one, past the 2,500 LY reach, while explorer-9/-11 carry ~97 launches.
func TestVoidReachGrowsWithTheTank(t *testing.T) {
	for _, c := range []struct {
		perTank, want float64
	}{
		{0, voidStationReach},
		{350.0 / 23, voidStationReach},  // stock pathfinder tank
		{1020.0 / 45, voidStationReach}, // explorer-7
		{voidBigTankLaunches, voidBigTankReach},
		{4000.0 / 41, voidBigTankReach}, // explorer-9, explorer-11
	} {
		if got := voidReach(c.perTank); got != c.want {
			t.Errorf("voidReach(%.1f) = %v, want %v", c.perTank, got, c.want)
		}
	}
}
