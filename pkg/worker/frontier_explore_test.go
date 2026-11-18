package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/explore"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

var frontierNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// frontierFake is a ship that launches, drifts and lands where the test says.
type frontierFake struct {
	arrivalFakeClient
	lands     []string // where each launch ends; "" is a drift a storm turns away
	pending   string
	adrift    bool
	bearing   float64
	polls     int
	from      [2]float64 // drift reading on the way to a landing
	miss      [2]float64 // drift reading of a launch that does not land
	stances   []string
	distress  []string
	jumpErr   error // the next Jump fails with this, once
	repairErr error
}

func (c *frontierFake) RawCommand(_ context.Context, command string, _ map[string]any) error {
	if command != "get_location" {
		return nil
	}
	switch {
	case !c.adrift:
		c.raw["location"] = systemLoc(c.state.System.ID, 0)
	case c.pending == "":
		c.raw["location"] = driftLoc(c.miss[0], c.miss[1], c.bearing+10)
	case c.polls == 0:
		c.polls++
		c.raw["location"] = driftLoc(c.from[0], c.from[1], c.bearing)
	default:
		c.adrift = false
		c.state.System = game.SystemData{ID: c.pending}
		c.raw["location"] = systemLoc(c.pending, 0)
	}
	return nil
}

func (c *frontierFake) Jump(_ context.Context, target string) (*game.JumpResult, error) {
	b, err := strconv.ParseFloat(target, 64)
	if err != nil {
		return nil, fmt.Errorf("fake: non-numeric jump %q", target)
	}
	if err := c.jumpErr; err != nil {
		c.jumpErr = nil
		return nil, err
	}
	c.calls = append(c.calls, "jump "+target)
	c.jumps = append(c.jumps, b)
	c.state.Ship.Fuel -= c.launchCost
	c.adrift, c.bearing, c.polls, c.pending = true, b, 0, ""
	if len(c.lands) > 0 {
		c.pending, c.lands = c.lands[0], c.lands[1:]
	}
	return &game.JumpResult{Drifting: true}, nil
}

func (c *frontierFake) Battle(_ context.Context, action string, payload map[string]any) error {
	c.stances = append(c.stances, fmt.Sprint(action, " ", payload["stance"]))
	return nil
}

func (c *frontierFake) Repair(context.Context) error {
	c.calls = append(c.calls, "repair")
	if c.repairErr != nil {
		return c.repairErr
	}
	c.state.Ship.Hull = c.state.Ship.MaxHull
	return nil
}

func (c *frontierFake) DistressSignal(_ context.Context, kind string) error {
	c.distress = append(c.distress, kind)
	return nil
}

func frontierShip(scanners int) []byte {
	mods := []map[string]any{{"id": "m0", "type_id": "pathfinder_drive"}}
	for i := range scanners {
		mods = append(mods, map[string]any{"id": fmt.Sprint("s", i), "type_id": "survey_scanner_ii"})
	}
	b, _ := json.Marshal(map[string]any{"modules": mods})
	return b
}

// frontierRig is a frontier ship, its ledger and the seams that stand in for
// lane travel and the per-system tour.
type frontierRig struct {
	t     *testing.T
	c     *frontierFake
	d     *WorkerDispatch
	kb    knowledge.Base
	run   string
	out   *bytes.Buffer
	tours []string
	navs  []string
	// results and lanes script the tour of a system; a system with no result
	// surveys at power 90 and visits its one POI.
	results map[string]explore.Result
	lanes   map[string][]string
	charted map[string]bool
	navErr  map[string]error
	tourErr map[string]error
	avoid   map[string]string // the avoid list of the latest lane trip
	power   int               // what the pre-flight survey reads
	surveys int
}

// newFrontierRig puts explorer-3 in sysID with 1000 fuel, two scanners and
// ten repair kits, and a ledger over systems joined by lanes. Cloverfield
// (home, with a station) is charted ground at (100, 0).
func newFrontierRig(t *testing.T, sysID string, systems []sweep.FrontierInput, lanes [][2]string) *frontierRig {
	t.Helper()
	st := shipWith(1000, 1020, game.CargoItem{ItemID: "repair_kit", Quantity: 10})
	st.System.ID = sysID
	st.CurrentTick = 2_400_000
	c := &frontierFake{from: [2]float64{150, 0}, miss: [2]float64{900, 0}}
	c.state, c.launchCost = st, 45
	c.raw = map[string][]byte{"ship": frontierShip(2)}
	kb := knowledge.NewMemoryKB()
	laneStation(t, kb, huntHomeSystem, 100, 0)

	r := &frontierRig{t: t, c: c, kb: kb, results: map[string]explore.Result{}, lanes: map[string][]string{},
		charted: map[string]bool{huntHomeSystem: true}, navErr: map[string]error{}, tourErr: map[string]error{}, power: 90}
	r.run = filepath.Join(t.TempDir(), "frontier-run.json")
	if err := sweep.SaveFrontierRun(r.run, sweep.NewFrontierRun(systems, lanes, frontierNow)); err != nil {
		t.Fatal(err)
	}
	r.d = r.dispatch(t.TempDir())
	return r
}

// dispatch is a fresh worker process over the same ship, ledger and agents dir.
func (r *frontierRig) dispatch(agentsDir string) *WorkerDispatch {
	noSweepSleep(r.t)
	r.out = &bytes.Buffer{}
	d := NewWorkerDispatch(r.c, r.kb, nil, r.out)
	d.AgentID, d.AgentsDir = "explorer-3", agentsDir
	d.FrontierWedge, d.FrontierRun, d.SweepLaunchCost = "E", r.run, 45
	d.sweepCapture = func(context.Context, string) {}
	d.frontierCharted = func(_ context.Context, id string) bool { return r.charted[id] }
	d.frontierTour = func(context.Context) (explore.Result, error) {
		id := r.c.state.System.ID
		r.tours = append(r.tours, id)
		if err := r.tourErr[id]; err != nil {
			return explore.Result{}, err
		}
		r.c.state.System.Connections = nil
		for _, l := range r.lanes[id] {
			r.c.state.System.Connections = append(r.c.state.System.Connections, game.ConnectionInfo{SystemID: l, Name: l})
		}
		if res, ok := r.results[id]; ok {
			return res, nil
		}
		return explore.Result{SystemID: id, SurveyPower: 90, POIs: []explore.POIResult{{ID: id + "_star", Visited: true}}}, nil
	}
	d.frontierSurvey = func(context.Context) (int, error) {
		r.surveys++
		return r.power, nil
	}
	d.frontierNav = func(_ context.Context, system, poi string, avoid map[string]string) error {
		r.navs = append(r.navs, system+"/"+poi)
		r.avoid = avoid
		if err := r.navErr[system]; err != nil {
			return err
		}
		r.c.state.System = game.SystemData{ID: system}
		return nil
	}
	return d
}

func (r *frontierRig) pass() {
	r.t.Helper()
	if err := r.d.FrontierExplore(context.Background()); err != nil {
		r.t.Fatalf("pass: %v\n%s", err, r.out)
	}
}

// passUntil runs passes until the saved phase is want, at most n of them.
func (r *frontierRig) passUntil(want string, n int) {
	r.t.Helper()
	for range n {
		r.pass()
		if r.state().Phase == want {
			return
		}
	}
	r.t.Fatalf("phase %q after %d passes, want %q\n%s", r.state().Phase, n, want, r.out)
}

func (r *frontierRig) state() *frontierState {
	r.t.Helper()
	s, err := loadFrontierState(frontierStatePath(r.d.AgentsDir, r.d.AgentID))
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

func (r *frontierRig) setState(s *frontierState) {
	r.t.Helper()
	if err := saveFrontierState(frontierStatePath(r.d.AgentsDir, r.d.AgentID), s); err != nil {
		r.t.Fatal(err)
	}
}

func (r *frontierRig) ledger(id string) *sweep.FrontierSystem {
	r.t.Helper()
	run, err := sweep.LoadFrontierRun(r.run)
	if err != nil {
		r.t.Fatal(err)
	}
	return run.Systems[id]
}

func eastPair() ([]sweep.FrontierInput, [][2]string) {
	return []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}, {ID: "b", X: 420, Y: 0}}, [][2]string{{"a", "b"}}
}

func TestFrontierVerifyRefusesWithoutTwoScanners(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, huntHomeSystem, sys, lanes)
	r.c.raw["ship"] = frontierShip(1)
	r.pass()
	if got := r.state().Phase; got != frontierVerify {
		t.Errorf("phase %q, want it held at verify", got)
	}
	if len(r.c.jumps) != 0 || len(r.navs) != 0 {
		t.Errorf("an unfit ship moved: jumps %v navs %v", r.c.jumps, r.navs)
	}
	if !strings.Contains(r.out.String(), "survey_scanner_ii") {
		t.Errorf("log does not say what is missing:\n%s", r.out)
	}
}

func TestFrontierVerifyNeedsARepairKit(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, huntHomeSystem, sys, lanes)
	r.c.state.Ship.Cargo = nil
	r.pass()
	if got := r.state().Phase; got != frontierVerify || !strings.Contains(r.out.String(), "repair_kit") {
		t.Errorf("phase %q, log:\n%s", got, r.out)
	}
}

// From home: launch at the wedge's first system, tour it, follow the lanes
// through the cluster (one of them found on the way), then come home.
func TestFrontierWorksWholeClusterAndMarksDone(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, huntHomeSystem, sys, lanes)
	r.c.lands = []string{"a"}
	r.lanes["b"] = []string{"a", "c"}
	r.passUntil(frontierDone, 25)

	if want := []string{"a", "b", "c"}; !slices.Equal(r.tours, want) {
		t.Errorf("toured %v, want %v", r.tours, want)
	}
	for _, id := range []string{"a", "b", "c"} {
		s := r.ledger(id)
		if s == nil || s.Status != sweep.FrontierDone || s.Claimant != "explorer-3" || s.SurveyPower != 90 || s.POIsVisited != 1 {
			t.Errorf("ledger %s = %+v, want done by explorer-3 at power 90", id, s)
		}
	}
	if c := r.ledger("c"); c != nil && c.Cluster != r.ledger("a").Cluster {
		t.Errorf("c found by lane from b is in cluster %q, want a's %q", c.Cluster, r.ledger("a").Cluster)
	}
	if len(r.c.jumps) != 1 || r.c.jumps[0] != 0 {
		t.Errorf("launches %v, want one due east at a", r.c.jumps)
	}
	if want := []string{"b/", "c/", huntHomeSystem + "/", huntHomeSystem + "/" + huntHomePOI}; !slices.Equal(r.navs, want) {
		t.Errorf("lane travel %v, want %v", r.navs, want)
	}
	if !r.c.state.Doc {
		t.Error("the run ended undocked")
	}
}

// Review Focus 1: a restart mid-cluster resumes from the state file.
func TestFrontierResumesClusterAfterRestart(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, "a", sys, lanes)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.pass() // claims the cluster and tours a
	if !slices.Equal(r.tours, []string{"a"}) {
		t.Fatalf("toured %v", r.tours)
	}

	r.d = r.dispatch(r.d.AgentsDir) // the worker restarts
	r.pass()
	r.pass()
	if want := []string{"a", "b"}; !slices.Equal(r.tours, want) {
		t.Errorf("toured %v after the restart, want %v (a not toured again)", r.tours, want)
	}
	if got := r.ledger("b"); got.Status != sweep.FrontierDone {
		t.Errorf("b = %+v", got)
	}
}

// A crash after the ledger write but before the state write must not tour
// the system again: the ledger is the record.
func TestFrontierTrustsTheLedgerOverItsState(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, "a", sys, lanes)
	if _, ok, err := sweep.ClaimCluster(r.run, "explorer-3", "a", frontierNow); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := sweep.FinishSystem(r.run, "explorer-3", sweep.FrontierSystem{ID: "a", Status: sweep.FrontierDone, SurveyPower: 90}, frontierNow); err != nil {
		t.Fatal(err)
	}
	r.setState(&frontierState{Phase: frontierWork, Target: "a", Cluster: []string{"a", "b"}, LaunchCost: 45})
	r.pass()
	if len(r.tours) != 0 || !slices.Equal(r.navs, []string{"b/"}) {
		t.Errorf("tours %v navs %v, want no tour of a and a lane jump to b", r.tours, r.navs)
	}
}

// Review Focus 2: survey power under 90 stops the ship; nothing is marked done.
func TestFrontierLowSurveyPowerStops(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, "a", sys, lanes)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.results["a"] = explore.Result{SystemID: "a", SurveyPower: 60, POIs: []explore.POIResult{{ID: "a_star", Visited: true}}}
	r.pass()
	r.pass()
	if got := r.ledger("a"); got.Status != sweep.FrontierClaimed {
		t.Errorf("a = %q, want it left claimed and not done", got.Status)
	}
	if got := r.state().Phase; got != frontierWork {
		t.Errorf("phase %q, want work", got)
	}
	if !strings.Contains(r.out.String(), "survey power 60") {
		t.Errorf("log does not name the power:\n%s", r.out)
	}
	if len(r.tours) != 1 || len(r.navs) != 0 {
		t.Errorf("tours %v navs %v, want one tour and the ship stopped", r.tours, r.navs)
	}
}

func TestFrontierSecondShipLeavesClaimedCluster(t *testing.T) {
	sys, lanes := eastPair()
	sys = append(sys, sweep.FrontierInput{ID: "z", X: 900, Y: 0})
	r := newFrontierRig(t, "a", sys, lanes)
	if _, ok, err := sweep.ClaimCluster(r.run, "explorer-9", "b", frontierNow); err != nil || !ok {
		t.Fatal(ok, err)
	}
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.pass()
	if len(r.tours) != 0 {
		t.Errorf("toured %v in another ship's cluster", r.tours)
	}
	if got := r.ledger("a"); got.Claimant != "explorer-9" {
		t.Errorf("a claimant %q", got.Claimant)
	}
	r.pass()
	if s := r.state(); s.Phase != frontierStage || s.Target != "z" {
		t.Errorf("state %+v, want the next target z", s)
	}
}

// Review Focus 5: hull at the floor with no kits and no station in the
// cluster sends the ship to a station, with the system to come back to.
func TestFrontierHullStopDetoursToStation(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, "a", sys, lanes)
	r.c.state.Ship.Cargo = nil
	r.c.state.Ship.Hull, r.c.state.Ship.MaxHull = 50, 100
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.results["a"] = explore.Result{SystemID: "a", SurveyPower: 90, Stopped: explore.StoppedHull,
		POIs: []explore.POIResult{{ID: "a_star", Visited: true}, {ID: "a_belt", Skipped: "hull floor"}}}
	r.pass()
	if s := r.state(); s.Phase != frontierRefuel || s.ResumeAt != "a" {
		t.Fatalf("state %+v, want refuel with ResumeAt a", s)
	}
	if got := r.ledger("a"); got.Status != sweep.FrontierClaimed {
		t.Errorf("a = %q, want still claimed (to be finished after the repair)", got.Status)
	}
	// No lanes out of a: the ship launches at the nearest station.
	r.c.lands = []string{huntHomeSystem}
	r.pass()
	if len(r.c.jumps) != 1 || r.c.jumps[0] != 180 {
		t.Fatalf("launches %v, want one due west at the station\n%s", r.c.jumps, r.out)
	}
	r.passUntil(frontierStage, 4)
	if s := r.state(); s.Target != "a" || s.ResumeAt != "" || !r.c.state.Doc {
		t.Errorf("after the stop: state %+v docked %v, want target a again", s, r.c.state.Doc)
	}
	if len(r.tours) != 1 {
		t.Errorf("toured %v", r.tours)
	}
}

func TestFrontierFuelGateBeforeLaunch(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, huntHomeSystem, sys, lanes)
	r.c.state.Ship.Fuel = 100 // under three launches of 45
	r.setState(&frontierState{Phase: frontierLaunch, Target: "a", TargetX: 400, LaunchCost: 45})
	r.pass()
	if len(r.c.jumps) != 0 {
		t.Errorf("launched on %v fuel", r.c.state.Ship.Fuel)
	}
	if s := r.state(); s.Phase != frontierRefuel || s.ResumeAt != "a" {
		t.Errorf("state %+v, want refuel with ResumeAt a", s)
	}
}

func TestFrontierFuelGateBeforeLaneJump(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, "a", sys, lanes)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.pass()
	r.c.state.Ship.Fuel = 100
	r.pass()
	if len(r.navs) != 0 {
		t.Errorf("lane travel %v on 100 fuel", r.navs)
	}
	if s := r.state(); s.Phase != frontierRefuel || s.ResumeAt != "b" {
		t.Errorf("state %+v, want refuel with ResumeAt b", s)
	}
}

func TestFrontierUnreachableAfterTwoLaunches(t *testing.T) {
	r := newFrontierRig(t, huntHomeSystem, []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	r.setState(&frontierState{Phase: frontierLaunch, Target: "a", TargetX: 400, LaunchCost: 45})
	r.c.lands = []string{"", "", huntHomeSystem}
	r.pass() // launch 1
	r.pass() // turned away: launch 2
	if got := r.ledger("a"); got.Status != sweep.FrontierTodo {
		t.Fatalf("a = %q after one failed launch", got.Status)
	}
	r.pass() // turned away again: give up, head for a station
	got := r.ledger("a")
	if got.Status != sweep.FrontierIncomplete || got.Reason != "unreachable" {
		t.Errorf("a = %q / %q, want incomplete: unreachable", got.Status, got.Reason)
	}
	if len(r.c.jumps) != 3 || r.c.jumps[2] != 180 {
		t.Errorf("launches %v, want two at a and one back at the station", r.c.jumps)
	}
	if s := r.state(); s.Phase != frontierHome {
		t.Errorf("phase %q, want home (nothing left todo)", s.Phase)
	}
}

func TestFrontierApiLaneNotFollowed(t *testing.T) {
	r := newFrontierRig(t, "a", []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.lanes["a"] = []string{"old_map", "d"}
	r.charted["old_map"] = true
	r.pass()
	if r.ledger("old_map") != nil {
		t.Error("a charted system joined the ledger")
	}
	if d := r.ledger("d"); d == nil || d.Status != sweep.FrontierClaimed || d.Claimant != "explorer-3" {
		t.Errorf("d = %+v, want it added and claimed", d)
	}
	if s := r.state(); !slices.Equal(s.Cluster, []string{"a", "d"}) {
		t.Errorf("cluster %v, want [a d]", s.Cluster)
	}
	r.pass()
	if !slices.Equal(r.navs, []string{"d/"}) {
		t.Errorf("lane travel %v, want only d", r.navs)
	}
}

func TestFrontierBattleFleesAndMarksHostile(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, "a", sys, lanes)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.results["a"] = explore.Result{SystemID: "a", SurveyPower: 90, Stopped: explore.StoppedBattle}
	r.pass()
	if !slices.Equal(r.c.stances, []string{"stance flee"}) {
		t.Errorf("battle calls %v, want the flee stance", r.c.stances)
	}
	if got := r.ledger("a"); got.Status != sweep.FrontierIncomplete || !strings.HasPrefix(got.Reason, "hostile") {
		t.Errorf("a = %q / %q", got.Status, got.Reason)
	}
	r.pass()
	if !slices.Equal(r.navs, []string{"b/"}) {
		t.Errorf("lane travel %v, want the ship to move on to b", r.navs)
	}
}

func TestFrontierLeviathanIsNotToured(t *testing.T) {
	r := newFrontierRig(t, "a", []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.c.nearby = []string{nearbyLeviathan}
	r.pass()
	if len(r.tours) != 0 {
		t.Error("toured a system with a leviathan in it")
	}
	if got := r.ledger("a"); got.Status != sweep.FrontierIncomplete || !strings.Contains(got.Reason, "Molt Leviathan") {
		t.Errorf("a = %q / %q", got.Status, got.Reason)
	}
}

// Skipped POIs make a system incomplete, with their ids on the ledger.
func TestFrontierSkippedPOIsAreIncomplete(t *testing.T) {
	r := newFrontierRig(t, "a", []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.results["a"] = explore.Result{SystemID: "a", SurveyPower: 95, Relics: 1,
		POIs: []explore.POIResult{{ID: "a_star", Visited: true}, {ID: "a_rift", Skipped: "travel refused"}}}
	r.pass()
	got := r.ledger("a")
	if got.Status != sweep.FrontierIncomplete || !slices.Equal(got.Skipped, []string{"a_rift"}) ||
		!strings.Contains(got.Reason, "travel refused") || got.POIsSeen != 2 || got.POIsVisited != 1 || got.Relics != 1 {
		t.Errorf("a = %+v", got)
	}
}

// Captured on the way by a frontier system the ledger did not know: it joins
// the ledger at the measured position and is worked.
func TestFrontierCapturedByUnknownSystem(t *testing.T) {
	r := newFrontierRig(t, huntHomeSystem, []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	r.setState(&frontierState{Phase: frontierLaunch, Target: "a", TargetX: 400, LaunchCost: 45})
	r.c.lands = []string{"stray"}
	r.pass() // launch
	r.pass() // drift, land
	r.pass() // work
	got := r.ledger("stray")
	if got == nil || got.Status != sweep.FrontierDone || got.X != 150 {
		t.Fatalf("stray = %+v, want it added at the last drift reading and done", got)
	}
	if a := r.ledger("a"); a.Status != sweep.FrontierTodo {
		t.Errorf("a = %q, want still todo", a.Status)
	}
}

// Captured by charted ground: relaunch at the same target.
func TestFrontierCapturedByChartedSystemRelaunches(t *testing.T) {
	r := newFrontierRig(t, huntHomeSystem, []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	placeSystem(t, r.kb, "old_map", 200, 0, 1, 1)
	r.charted["old_map"] = true
	r.setState(&frontierState{Phase: frontierLaunch, Target: "a", TargetX: 400, LaunchCost: 45})
	r.c.lands = []string{"old_map", "a"}
	r.passUntil(frontierWork, 5)
	if len(r.c.jumps) != 2 || slices.Contains(r.tours, "old_map") {
		t.Errorf("launches %v tours %v, want two launches and no tour of charted ground", r.c.jumps, r.tours)
	}
	if r.ledger("old_map") != nil {
		t.Error("charted ground joined the ledger")
	}
}

// Out of fuel with no lane to a station: one tow request, then wait.
func TestFrontierStrandedCallsForATowOnce(t *testing.T) {
	r := newFrontierRig(t, "a", []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	r.c.state.Ship.Fuel = 20
	r.setState(&frontierState{Phase: frontierRefuel, Target: "a", ResumeAt: "a", LaunchCost: 45})
	r.pass()
	r.pass()
	if !slices.Equal(r.c.distress, []string{"tow"}) {
		t.Errorf("distress calls %v, want one tow", r.c.distress)
	}
	if len(r.c.jumps) != 0 {
		t.Errorf("launched on 20 fuel: %v", r.c.jumps)
	}
}

func TestFrontierNeedsAWedge(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, huntHomeSystem, sys, lanes)
	r.d.FrontierWedge = "ENE"
	if err := r.d.FrontierExplore(context.Background()); err == nil || !strings.Contains(err.Error(), "ENE") {
		t.Errorf("err %v, want the bad wedge named", err)
	}
}

// C1: a lane into another ship's cluster must not lose the systems this ship
// found on the same pass.
func TestFrontierLaneIntoAnotherShipsClusterKeepsOwnFinds(t *testing.T) {
	r := newFrontierRig(t, "a", []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}, {ID: "p", X: 430, Y: 0}}, nil)
	if _, ok, err := sweep.ClaimCluster(r.run, "explorer-9", "p", frontierNow); err != nil || !ok {
		t.Fatal(ok, err)
	}
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.lanes["a"] = []string{"p", "n"}
	r.pass()
	if s := r.state(); !slices.Equal(s.Cluster, []string{"a", "n"}) {
		t.Errorf("cluster %v, want [a n]: n is ours, p is explorer-9's", s.Cluster)
	}
	if p := r.ledger("p"); p.Claimant != "explorer-9" {
		t.Errorf("p claimant %q", p.Claimant)
	}
	r.pass()
	if !slices.Equal(r.navs, []string{"n/"}) {
		t.Errorf("lane travel %v, want n", r.navs)
	}
}

// C2: the second hull stop in one system ends it as incomplete rather than
// sending the ship round the station loop again.
func TestFrontierSecondHullStopMarksIncomplete(t *testing.T) {
	r := newFrontierRig(t, "a", []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45, StopSystem: "a", Stops: 1})
	r.results["a"] = explore.Result{SystemID: "a", SurveyPower: 90, Stopped: explore.StoppedHull,
		POIs: []explore.POIResult{{ID: "a_star", Visited: true}, {ID: "a_rift", Skipped: "hull 48%"}}}
	r.pass()
	got := r.ledger("a")
	if got.Status != sweep.FrontierIncomplete || !slices.Equal(got.Skipped, []string{"a_rift"}) || !strings.Contains(got.Reason, "hull") {
		t.Errorf("a = %+v, want incomplete with a_rift skipped for the hull", got)
	}
	if s := r.state(); s.Phase == frontierRefuel {
		t.Errorf("phase %q: a second detour for the same system", s.Phase)
	}
}

// C2: a station that cannot repair the hull does not send the ship back out.
func TestFrontierRefuelHoldsWhileHullUnderFloor(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, huntHomeSystem, sys, lanes)
	r.c.state.Ship.Hull, r.c.state.Ship.MaxHull = 40, 100
	r.c.repairErr = errors.New("repair service unavailable")
	r.setState(&frontierState{Phase: frontierRefuel, Target: "a", ResumeAt: "a", LaunchCost: 45})
	r.pass()
	if s := r.state(); s.Phase != frontierRefuel || s.ResumeAt != "a" {
		t.Errorf("state %+v, want the ship held at the station", s)
	}
	if !r.c.state.Doc || len(r.c.jumps) != 0 || !strings.Contains(r.out.String(), "hull") {
		t.Errorf("docked %v jumps %v log:\n%s", r.c.state.Doc, r.c.jumps, r.out)
	}
}

// I1: a jump the server refused is not a launch at the target.
func TestFrontierFailedJumpIsNotALaunch(t *testing.T) {
	r := newFrontierRig(t, huntHomeSystem, []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	r.setState(&frontierState{Phase: frontierLaunch, Target: "a", TargetX: 400, LaunchCost: 45})
	for range 3 {
		r.c.jumpErr = errors.New("action pending")
		if err := r.d.FrontierExplore(context.Background()); err == nil {
			t.Fatal("a refused jump returned no error")
		}
	}
	if s := r.state(); s.Launches != 0 {
		t.Errorf("launches %d after three refused jumps, want 0", s.Launches)
	}
	if got := r.ledger("a"); got.Status != sweep.FrontierTodo {
		t.Errorf("a = %q, want still todo", got.Status)
	}
}

// I2: a lane route refused for fuel while staging falls back to launching
// from where the ship is.
func TestFrontierStageRefusalLaunchesFromHere(t *testing.T) {
	r := newFrontierRig(t, huntHomeSystem, []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	laneStation(t, r.kb, huntHomeSystem, 100, 0, "near")
	placeSystem(t, r.kb, "near", 300, 0, 1, 1, huntHomeSystem)
	r.navErr["near"] = fmt.Errorf("%w: 3 jumps", ErrInsufficientRouteFuel)
	r.setState(&frontierState{Phase: frontierStage, Target: "a", TargetX: 400, LaunchCost: 45})
	r.pass()
	if len(r.c.jumps) != 1 {
		t.Errorf("launches %v, want one from here after the refused lane trip\n%s", r.c.jumps, r.out)
	}
}

// I2: a cluster lane jump refused for fuel is a refuel detour, not a stall.
func TestFrontierLaneFuelRefusalDetours(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, "a", sys, lanes)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.pass()
	r.navErr["b"] = fmt.Errorf("%w: 1 jump", ErrInsufficientRouteFuel)
	r.pass()
	if s := r.state(); s.Phase != frontierRefuel || s.ResumeAt != "b" {
		t.Errorf("state %+v, want refuel with ResumeAt b", s)
	}
}

// I3: a system whose tour keeps failing is given up as incomplete.
func TestFrontierTourErrorGivesUpAfterThreeTries(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, "a", sys, lanes)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.tourErr["a"] = errors.New("explore interrupted at POI 2/5")
	for i := range 3 {
		err := r.d.FrontierExplore(context.Background())
		if i < 2 && err == nil {
			t.Fatalf("try %d: the tour error was swallowed", i+1)
		}
	}
	got := r.ledger("a")
	if got.Status != sweep.FrontierIncomplete || !strings.Contains(got.Reason, "explore interrupted") {
		t.Errorf("a = %q / %q, want incomplete with the error", got.Status, got.Reason)
	}
	r.pass()
	if !slices.Equal(r.navs, []string{"b/"}) {
		t.Errorf("lane travel %v, want the ship to move on to b", r.navs)
	}
}

// I4: a ship goes back to its own unfinished claims before new ground.
func TestFrontierNextReturnsToOwnClaim(t *testing.T) {
	sys, lanes := eastPair()
	sys = append(sys, sweep.FrontierInput{ID: "z", X: 150, Y: 0})
	r := newFrontierRig(t, huntHomeSystem, sys, lanes)
	if _, ok, err := sweep.ClaimCluster(r.run, "explorer-3", "a", frontierNow); err != nil || !ok {
		t.Fatal(ok, err)
	}
	r.setState(&frontierState{Phase: frontierNext, LaunchCost: 45})
	r.pass()
	if s := r.state(); s.Target != "a" {
		t.Errorf("target %q, want its own claim a (z is nearer but new)", s.Target)
	}
}

// I4: outside its cluster (an interrupted lane trip), the ship goes back to
// it instead of claiming where it stands.
func TestFrontierOutsideClusterGoesBackToIt(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, "midway", sys, lanes)
	if _, ok, err := sweep.ClaimCluster(r.run, "explorer-3", "a", frontierNow); err != nil || !ok {
		t.Fatal(ok, err)
	}
	r.setState(&frontierState{Phase: frontierWork, Target: "a", Cluster: []string{"a", "b"}, LaunchCost: 45})
	r.pass()
	if len(r.tours) != 0 || !slices.Equal(r.navs, []string{"a/"}) || r.ledger("midway") != nil {
		t.Errorf("tours %v navs %v ledger midway %+v; want a lane trip back to a", r.tours, r.navs, r.ledger("midway"))
	}
}

func TestFrontierChartedSystemIsNotWorked(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, huntHomeSystem, sys, lanes)
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.pass()
	if len(r.tours) != 0 || r.ledger(huntHomeSystem) != nil {
		t.Errorf("tours %v, ledger %+v: charted ground was worked", r.tours, r.ledger(huntHomeSystem))
	}
}

// I5: a target another ship took while this one was on its way is dropped.
func TestFrontierRetargetsWhenTargetTaken(t *testing.T) {
	sys, lanes := eastPair()
	sys = append(sys, sweep.FrontierInput{ID: "z", X: 900, Y: 0})
	r := newFrontierRig(t, huntHomeSystem, sys, lanes)
	if _, ok, err := sweep.ClaimCluster(r.run, "explorer-9", "a", frontierNow); err != nil || !ok {
		t.Fatal(ok, err)
	}
	r.setState(&frontierState{Phase: frontierLaunch, Target: "a", TargetX: 400, LaunchCost: 45})
	r.pass()
	if s := r.state(); s.Target != "z" || len(r.c.jumps) != 0 {
		t.Errorf("target %q jumps %v, want z and no launch at explorer-9's a", s.Target, r.c.jumps)
	}
}

// I7: lane trips route around systems the ledger marks hostile.
func TestFrontierRoutesAvoidHostileSystems(t *testing.T) {
	sys, lanes := eastPair()
	sys = append(sys, sweep.FrontierInput{ID: "h", X: 410, Y: 30, Lethal: "leviathan at h_core"},
		sweep.FrontierInput{ID: "u", X: 500, Y: 0})
	r := newFrontierRig(t, "a", sys, lanes)
	if err := sweep.FinishSystem(r.run, "explorer-9", sweep.FrontierSystem{ID: "u", Status: sweep.FrontierIncomplete, Reason: "unreachable"}, frontierNow); err != nil {
		t.Fatal(err)
	}
	r.setState(&frontierState{Phase: frontierWork, Target: "a", LaunchCost: 45})
	r.pass()
	r.pass()
	if _, ok := r.avoid["h"]; !ok || len(r.avoid) != 1 {
		t.Errorf("avoid list %v, want only the hostile system h", r.avoid)
	}
}

// I8: the survey power is read before the ship leaves the station.
func TestFrontierVerifySurveysBeforeLeaving(t *testing.T) {
	sys, lanes := eastPair()
	r := newFrontierRig(t, huntHomeSystem, sys, lanes)
	r.power = 60
	r.pass()
	if s := r.state(); s.Phase != frontierVerify {
		t.Errorf("phase %q, want it held at verify", s.Phase)
	}
	if r.surveys != 1 || !r.c.state.Doc || len(r.c.jumps) != 0 || !strings.Contains(r.out.String(), "survey power 60") {
		t.Errorf("surveys %d docked %v jumps %v log:\n%s", r.surveys, r.c.state.Doc, r.c.jumps, r.out)
	}
}

// A ship that finds itself in a system the ledger wrote off as unreachable
// explores it: being there is the proof it can be reached.
func TestFrontierExploresUnreachableSystemItLandsIn(t *testing.T) {
	r := newFrontierRig(t, "a", []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	if err := sweep.FinishSystem(r.run, "explorer-9", sweep.FrontierSystem{ID: "a", Status: sweep.FrontierIncomplete, Reason: "unreachable"}, frontierNow); err != nil {
		t.Fatal(err)
	}
	r.setState(&frontierState{Phase: frontierWork, Target: "a", TargetX: 400, LaunchCost: 45})
	r.pass()
	if !slices.Equal(r.tours, []string{"a"}) {
		t.Fatalf("tours %v, want a toured\n%s", r.tours, r.out)
	}
	if got := r.ledger("a"); got.Status != sweep.FrontierDone || got.Claimant != "explorer-3" {
		t.Errorf("a = %q by %q, want done by explorer-3", got.Status, got.Claimant)
	}
}

// Captures by charted ground on the way do not use up the launch cap while
// each one leaves the ship nearer the target.
func TestFrontierCapturesNearerTheTargetDoNotCountAgainstIt(t *testing.T) {
	r := newFrontierRig(t, huntHomeSystem, []sweep.FrontierInput{{ID: "a", X: 400, Y: 0}}, nil)
	placeSystem(t, r.kb, "old_one", 200, 0, 1, 1)
	placeSystem(t, r.kb, "old_two", 300, 0, 1, 1)
	r.charted["old_one"], r.charted["old_two"] = true, true
	r.setState(&frontierState{Phase: frontierLaunch, Target: "a", TargetX: 400, LaunchCost: 45})
	r.c.lands = []string{"old_one", "old_two", "a"}
	r.passUntil(frontierWork, 8)
	if len(r.c.jumps) != 3 {
		t.Errorf("launches %v, want three", r.c.jumps)
	}
	if got := r.ledger("a"); got.Status == sweep.FrontierIncomplete {
		t.Errorf("a = %q / %q, want it not written off", got.Status, got.Reason)
	}
}

// Staging prefers a system whose line to the target no charted system
// interrupts over a nearer one whose drift would be captured on the way.
func TestFrontierStagesWhereTheLineIsClear(t *testing.T) {
	r := newFrontierRig(t, huntHomeSystem, []sweep.FrontierInput{{ID: "a", X: 700, Y: 0}}, nil)
	laneStation(t, r.kb, huntHomeSystem, 100, 0, "near", "side")
	placeSystem(t, r.kb, "near", 300, 0, 1, 1, huntHomeSystem)
	placeSystem(t, r.kb, "side", 300, 300, 1, 1, huntHomeSystem)
	placeSystem(t, r.kb, "wall", 500, -20, 1, 1) // charted, on the line from near
	r.setState(&frontierState{Phase: frontierStage, Target: "a", TargetX: 700, LaunchCost: 45})
	r.pass()
	if !slices.Equal(r.navs, []string{"side/"}) {
		t.Errorf("lane travel %v, want staging at side\n%s", r.navs, r.out)
	}
}

// With no clear line anywhere near, staging is still at the nearest system.
func TestFrontierStagesNearestWhenNoLineIsClear(t *testing.T) {
	r := newFrontierRig(t, huntHomeSystem, []sweep.FrontierInput{{ID: "a", X: 700, Y: 0}}, nil)
	laneStation(t, r.kb, huntHomeSystem, 100, 0, "near")
	placeSystem(t, r.kb, "near", 300, 0, 1, 1, huntHomeSystem)
	placeSystem(t, r.kb, "wall", 500, -20, 1, 1)
	r.setState(&frontierState{Phase: frontierStage, Target: "a", TargetX: 700, LaunchCost: 45})
	r.pass()
	if !slices.Equal(r.navs, []string{"near/"}) {
		t.Errorf("lane travel %v, want staging at near\n%s", r.navs, r.out)
	}
}
