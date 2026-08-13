package supervisor

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestAFailedMoveReturnsTheAgentToItsHomeFleet is the fix for a live outage.
//
// 2026-08-13: trader-1 nominated itself out of haul, the reconciler added it to
// haul's removed-set, SIGHUPed, waited for the worker to exit, and gave up after
// 90s. It returned an error and stopped — but the removed-set entry stayed.
// haul would no longer start the agent and unlock had never been told to, so
// trader-1 ran in NO fleet. salvager-2 followed 16 minutes later. Two agents sat
// idle until an operator noticed.
//
// A move that cannot be completed must leave membership exactly as it found it.
func TestAFailedMoveReturnsTheAgentToItsHomeFleet(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	from := &fakeFleet{
		name: "haul", ovPath: filepath.Join(dir, "haul-overrides.json"),
		running: true, stopAfter: -1, // never stops: the live failure mode
		trace: &trace,
	}
	to := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-overrides.json"), trace: &trace}

	err := moveAgent("trader-1", from.side(), to.side(), testOpts(&trace))
	if err == nil {
		t.Fatal("a worker that never stops must fail the move")
	}

	ov, lerr := LoadOverrides(from.ovPath)
	if lerr != nil {
		t.Fatalf("read home overrides: %v", lerr)
	}
	if ov.IsRemoved("trader-1") {
		t.Error("agent left in the home fleet's removed-set: it now runs in NO fleet")
	}
}

// TestAFailedMoveNeverReleasesTheAgentToTheAwayFleet: the away fleet must not be
// told it may start an agent whose old worker is still alive. That is exactly
// the session_replaced double-run the ordering exists to prevent.
func TestAFailedMoveNeverReleasesTheAgentToTheAwayFleet(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	from := &fakeFleet{
		name: "haul", ovPath: filepath.Join(dir, "haul-overrides.json"),
		running: true, stopAfter: -1, trace: &trace,
	}
	to := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-overrides.json"), trace: &trace}
	// The away fleet starts with the agent suppressed, as an on-call roster does.
	toOv := Overrides{}
	toOv.Add("trader-1")
	if err := SaveOverrides(to.ovPath, toOv); err != nil {
		t.Fatalf("seed away overrides: %v", err)
	}

	if err := moveAgent("trader-1", from.side(), to.side(), testOpts(&trace)); err == nil {
		t.Fatal("expected the move to fail")
	}

	ov, err := LoadOverrides(to.ovPath)
	if err != nil {
		t.Fatalf("read away overrides: %v", err)
	}
	if !ov.IsRemoved("trader-1") {
		t.Error("away fleet was released to start an agent still running at home")
	}
}

// TestRollbackFailureIsReportedAlongsideTheCause: if the rollback itself cannot
// be applied, the agent really is orphaned and the operator must be told both
// facts — the original failure alone would send them looking in the wrong place.
func TestRollbackFailureIsReportedAlongsideTheCause(t *testing.T) {
	dir := t.TempDir()
	var trace []string
	from := &fakeFleet{
		name: "haul", ovPath: filepath.Join(dir, "haul-overrides.json"),
		running: true, stopAfter: -1, trace: &trace,
	}
	side := from.side()
	// Reload succeeds on the way out and fails on the rollback.
	calls := 0
	side.Reload = func() error {
		calls++
		if calls > 1 {
			return errors.New("socket closed")
		}
		return nil
	}
	to := &fakeFleet{name: "unlock", ovPath: filepath.Join(dir, "unlock-overrides.json"), trace: &trace}

	err := moveAgent("trader-1", side, to.side(), testOpts(&trace))
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "still running") || !strings.Contains(msg, "socket closed") {
		t.Errorf("both the cause and the failed rollback must be named, got: %s", msg)
	}
}

// TestStopTimeoutOutlastsTheDrainItWaitsOn: the overmind force-stops a draining
// worker only after RemoveDrainTimeout, so a reconciler that gives up sooner
// declares failure while the drain it is watching is still legitimately running.
// Measured live: salvager-2's drain completed in 4m05s against a 90s wait, so
// the default could essentially never succeed on a busy hauler.
func TestStopTimeoutOutlastsTheDrainItWaitsOn(t *testing.T) {
	got := ReconcileOptions{}.stopTimeout()
	if got <= DefaultRemoveDrainTimeout {
		t.Fatalf("default stop timeout %s must exceed the drain window %s it waits on", got, DefaultRemoveDrainTimeout)
	}
}
