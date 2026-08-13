package supervisor

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// fakeFleet records the order of every membership operation, because the ORDER is
// the property under test: an agent started in a second fleet before the first
// released it loses its game session to status 4001 (session_replaced).
type fakeFleet struct {
	name      string
	ovPath    string
	running   bool
	runErr    error
	reloadErr error
	// stopAfter is how many Running() polls it takes before the worker reports
	// gone; -1 means it never stops.
	stopAfter int
	polls     int
	trace     *[]string
}

func (f *fakeFleet) side() FleetSide {
	return FleetSide{
		Name:          f.name,
		OverridesPath: f.ovPath,
		Reload: func() error {
			*f.trace = append(*f.trace, "reload:"+f.name)
			return f.reloadErr
		},
		Running: func(string) (bool, error) {
			if f.runErr != nil {
				return false, f.runErr
			}
			f.polls++
			if f.stopAfter >= 0 && f.polls > f.stopAfter {
				f.running = false
			}
			*f.trace = append(*f.trace, fmt.Sprintf("poll:%s=%v", f.name, f.running))
			return f.running, nil
		},
	}
}

func testOpts(trace *[]string) ReconcileOptions {
	t0 := time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC)
	n := 0
	return ReconcileOptions{
		MaxInFlight:  1,
		StopTimeout:  30 * time.Second,
		PollInterval: time.Second,
		Now: func() time.Time {
			n++
			return t0.Add(time.Duration(n) * time.Second)
		},
		Sleep: func(d time.Duration) { *trace = append(*trace, "sleep") },
	}
}

func seedLedger(t *testing.T, dir string, entries ...Secondment) string {
	t.Helper()
	path := filepath.Join(dir, "secondments.json")
	if err := SaveSecondments(path, Secondments{Entries: entries}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	return path
}

func nominated(agent string) Secondment {
	return Secondment{AgentID: agent, HomeFleet: "haul", AwayFleet: "unlock", Phase: PhaseNominated, NominatedAt: "2026-08-12T22:00:00Z"}
}

// TestSecondmentStopsHomeBeforeStartingAway is THE test. Everything else about
// this feature is convenience; this is the part that, done wrong, takes an agent
// off the board entirely.
func TestSecondmentStopsHomeBeforeStartingAway(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	home := &fakeFleet{name: "haul", ovPath: filepath.Join(dir, "haul-ov.json"), running: true, stopAfter: 1, trace: &trace}
	away := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-ov.json"), stopAfter: -1, trace: &trace}
	path := seedLedger(t, dir, nominated("hauler-0"))

	log, err := ReconcileSecondments(path, home.side(), away.side(), testOpts(&trace))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(log) != 1 {
		t.Fatalf("expected one action, got %v", log)
	}

	// The away fleet must not be reloaded until after the home fleet reported
	// its worker gone.
	homeReload, awayReload, lastRunningPoll := -1, -1, -1
	for i, ev := range trace {
		switch {
		case ev == "reload:haul" && homeReload < 0:
			homeReload = i
		case ev == "reload:unlock":
			awayReload = i
		case ev == "poll:haul=true":
			lastRunningPoll = i
		}
	}
	if homeReload < 0 || awayReload < 0 {
		t.Fatalf("both fleets must be reloaded; trace %v", trace)
	}
	if homeReload > awayReload {
		t.Fatalf("home must be reloaded (stopped) before away starts; trace %v", trace)
	}
	if lastRunningPoll > awayReload {
		t.Fatalf("away fleet started while home still reported the worker running; trace %v", trace)
	}

	// And the sidecars must reflect the move.
	hov, _ := LoadOverrides(home.ovPath)
	if !hov.IsRemoved("hauler-0") {
		t.Error("agent must be override-removed from its home fleet")
	}
	aov, _ := LoadOverrides(away.ovPath)
	if aov.IsRemoved("hauler-0") {
		t.Error("agent must NOT be override-removed from the fleet it moved to")
	}
}

// TestSecondmentFailsRatherThanStartATwiceRunningAgent: if the home worker will
// not die, the correct outcome is a failed trip, not a second live session.
func TestSecondmentFailsRatherThanStartATwiceRunningAgent(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	home := &fakeFleet{name: "haul", ovPath: filepath.Join(dir, "haul-ov.json"), running: true, stopAfter: -1, trace: &trace}
	away := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-ov.json"), stopAfter: -1, trace: &trace}
	path := seedLedger(t, dir, nominated("hauler-0"))

	log, err := ReconcileSecondments(path, home.side(), away.side(), testOpts(&trace))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, ev := range trace {
		if ev == "reload:unlock" {
			t.Fatalf("the away fleet must never be reloaded when the home worker is still alive; trace %v", trace)
		}
	}
	led, _ := LoadSecondments(path)
	if led.Entries[0].Phase != PhaseFailed {
		t.Fatalf("phase = %q, want %q", led.Entries[0].Phase, PhaseFailed)
	}
	if len(log) != 1 {
		t.Fatalf("the failure must be reported, got %v", log)
	}
}

// TestReconcileHonoursTheInFlightCap keeps a burst of qualifying haulers from
// emptying the fleet: extra nominations wait, they are not dropped.
func TestReconcileHonoursTheInFlightCap(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	home := &fakeFleet{name: "haul", ovPath: filepath.Join(dir, "haul-ov.json"), running: true, stopAfter: 1, trace: &trace}
	away := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-ov.json"), stopAfter: -1, trace: &trace}
	path := seedLedger(t, dir, nominated("hauler-0"), nominated("hauler-1"), nominated("hauler-2"))

	if _, err := ReconcileSecondments(path, home.side(), away.side(), testOpts(&trace)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	led, _ := LoadSecondments(path)
	seconded, waiting := 0, 0
	for _, e := range led.Entries {
		switch e.Phase {
		case PhaseSeconded:
			seconded++
		case PhaseNominated:
			waiting++
		}
	}
	if seconded != 1 {
		t.Fatalf("seconded = %d, want exactly 1 under a cap of 1", seconded)
	}
	if waiting != 2 {
		t.Fatalf("waiting = %d, want 2 still queued (not dropped)", waiting)
	}
}

// TestReturnTripMovesTheAgentBack closes the loop: the fleet ends the round trip
// at the size it started, with a stronghold-capable hull.
func TestReturnTripMovesTheAgentBack(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	home := &fakeFleet{name: "haul", ovPath: filepath.Join(dir, "haul-ov.json"), stopAfter: -1, trace: &trace}
	away := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-ov.json"), running: true, stopAfter: 1, trace: &trace}
	// Home already removed it on the way out; that is the state a return starts from.
	hov := Overrides{}
	hov.Add("hauler-0")
	if err := SaveOverrides(home.ovPath, hov); err != nil {
		t.Fatalf("seed home overrides: %v", err)
	}
	path := seedLedger(t, dir, Secondment{AgentID: "hauler-0", HomeFleet: "haul", AwayFleet: "unlock", Phase: PhaseReturning})

	if _, err := ReconcileSecondments(path, home.side(), away.side(), testOpts(&trace)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	hov2, _ := LoadOverrides(home.ovPath)
	if hov2.IsRemoved("hauler-0") {
		t.Error("a returned agent must be back in its home fleet")
	}
	aov, _ := LoadOverrides(away.ovPath)
	if !aov.IsRemoved("hauler-0") {
		t.Error("a returned agent must be removed from the away fleet")
	}
	led, _ := LoadSecondments(path)
	if led.Entries[0].Phase != PhaseHome {
		t.Fatalf("phase = %q, want %q", led.Entries[0].Phase, PhaseHome)
	}
}

// TestReconcileLeavesSettledTripsAlone: a sweep runs repeatedly, so it must be a
// no-op once there is nothing to do.
func TestReconcileLeavesSettledTripsAlone(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	home := &fakeFleet{name: "haul", ovPath: filepath.Join(dir, "haul-ov.json"), stopAfter: -1, trace: &trace}
	away := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-ov.json"), stopAfter: -1, trace: &trace}
	path := seedLedger(t, dir,
		Secondment{AgentID: "a", Phase: PhaseHome},
		Secondment{AgentID: "b", Phase: PhaseFailed},
		Secondment{AgentID: "c", Phase: PhaseSeconded},
	)
	log, err := ReconcileSecondments(path, home.side(), away.side(), testOpts(&trace))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(log) != 0 || len(trace) != 0 {
		t.Fatalf("a settled ledger must produce no actions; log %v trace %v", log, trace)
	}
}

// TestReconcileReportsARunningCheckFailure: an unanswerable "is it stopped?" must
// fail the trip, never be read as "stopped".
func TestReconcileReportsARunningCheckFailure(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	home := &fakeFleet{name: "haul", ovPath: filepath.Join(dir, "haul-ov.json"), runErr: errors.New("socket closed"), stopAfter: -1, trace: &trace}
	away := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-ov.json"), stopAfter: -1, trace: &trace}
	path := seedLedger(t, dir, nominated("hauler-0"))

	if _, err := ReconcileSecondments(path, home.side(), away.side(), testOpts(&trace)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, ev := range trace {
		if ev == "reload:unlock" {
			t.Fatalf("an unanswerable stop check must not start the away fleet; trace %v", trace)
		}
	}
	led, _ := LoadSecondments(path)
	if led.Entries[0].Phase != PhaseFailed {
		t.Fatalf("phase = %q, want failed", led.Entries[0].Phase)
	}
}

// TestGraduationQueuesTheReturn closes the design loop: the reconciler owns the
// whole lifecycle, so a worker never has to decide (or execute) its own return.
func TestGraduationQueuesTheReturn(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	home := &fakeFleet{name: "haul", ovPath: filepath.Join(dir, "haul-ov.json"), stopAfter: -1, trace: &trace}
	away := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-ov.json"), stopAfter: -1, trace: &trace}
	path := seedLedger(t, dir, Secondment{AgentID: "hauler-0", HomeFleet: "haul", AwayFleet: "unlock", Phase: PhaseSeconded})

	opts := testOpts(&trace)
	opts.Graduated = func(string) (bool, error) { return true, nil }
	if _, err := ReconcileSecondments(path, home.side(), away.side(), opts); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	led, _ := LoadSecondments(path)
	if led.Entries[0].Phase != PhaseReturning {
		t.Fatalf("phase = %q, want %q", led.Entries[0].Phase, PhaseReturning)
	}
}

// TestAnUngraduatedAgentStaysPut: the away fleet keeps it until the work is done.
func TestAnUngraduatedAgentStaysPut(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	home := &fakeFleet{name: "haul", ovPath: filepath.Join(dir, "haul-ov.json"), stopAfter: -1, trace: &trace}
	away := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-ov.json"), stopAfter: -1, trace: &trace}
	path := seedLedger(t, dir, Secondment{AgentID: "hauler-0", Phase: PhaseSeconded})

	opts := testOpts(&trace)
	opts.Graduated = func(string) (bool, error) { return false, nil }
	if _, err := ReconcileSecondments(path, home.side(), away.side(), opts); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	led, _ := LoadSecondments(path)
	if led.Entries[0].Phase != PhaseSeconded {
		t.Fatalf("phase = %q, want it to stay %q", led.Entries[0].Phase, PhaseSeconded)
	}
}

// TestAGraduationCheckFailureIsNotAGraduation: an unanswerable check must never
// be read as "done" — that would pull an agent home mid-chain.
func TestAGraduationCheckFailureIsNotAGraduation(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	home := &fakeFleet{name: "haul", ovPath: filepath.Join(dir, "haul-ov.json"), stopAfter: -1, trace: &trace}
	away := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-ov.json"), stopAfter: -1, trace: &trace}
	path := seedLedger(t, dir, Secondment{AgentID: "hauler-0", Phase: PhaseSeconded})

	opts := testOpts(&trace)
	opts.Graduated = func(string) (bool, error) { return false, errors.New("assets.db locked") }
	log, err := ReconcileSecondments(path, home.side(), away.side(), opts)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	led, _ := LoadSecondments(path)
	if led.Entries[0].Phase != PhaseSeconded {
		t.Fatalf("phase = %q, want it left at %q", led.Entries[0].Phase, PhaseSeconded)
	}
	if len(log) != 1 {
		t.Fatalf("the failed check must be reported, got %v", log)
	}
}
