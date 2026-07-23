package supervisor

import (
	"context"
	"io"
	"log"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

type fakeSender struct {
	mu   sync.Mutex
	sent []control.Envelope
}

func (f *fakeSender) Send(agentID string, env control.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	env.AgentID = agentID
	f.sent = append(f.sent, env)
	return nil
}

func (f *fakeSender) drains() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for _, e := range f.sent {
		if e.Type == control.TypeDrain {
			ids = append(ids, e.AgentID)
		}
	}
	return ids
}

// newTestSup builds a supervisor with a spawn that starts real do-nothing
// processes (so kill/exited plumbing works) and a fake sender.
func newTestSup(t *testing.T, specs []WorkerSpec) (*Supervisor, *fakeSender, *Fleet) {
	t.Helper()
	fleet := NewFleet()
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, "sleep", "60")
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return cmd, nil
	}
	sup := NewSupervisor(nil, fleet, specs, spawn, log.New(io.Discard, "", 0))
	sender := &fakeSender{}
	sup.Sender = sender
	sup.StaggerInterval = 0
	return sup, sender, fleet
}

func TestRemoveDrainsThenStops(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "a1", Role: "missionrunner"}}
	sup, sender, fleet := newTestSup(t, specs)
	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "a1", Role: "missionrunner"}, 1, time.Now())
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t", System: "sol"}, time.Now())

	sup.EnqueueMembership(MembershipRequest{Op: MembershipRemove, Spec: WorkerSpec{AgentID: "a1"}})
	sup.Tick(ctx)

	// Phase 1: drain sent, worker marked leaving, process still alive.
	if got := sender.drains(); len(got) != 1 || got[0] != "a1" {
		t.Fatalf("drains = %v, want [a1]", got)
	}
	snap := fleet.Snapshot()
	if len(snap) != 1 || !snap[0].Leaving {
		t.Fatalf("after remove tick: %+v, want a1 Leaving", snap)
	}
	if p := procSnapshot(sup, "a1"); p == nil || !p.alive() {
		t.Fatal("worker was stopped before it drained")
	}

	// Worker reports drained -> next tick stops and removes it.
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t", System: "sol", Drained: true}, time.Now())
	sup.Tick(ctx)
	if p := procSnapshot(sup, "a1"); p != nil && p.alive() {
		t.Fatal("worker still alive after drained tick")
	}
	if got := fleet.Snapshot(); len(got) != 0 {
		t.Fatalf("fleet still has %+v after removal", got)
	}
	if got := sup.Roster(); len(got) != 0 {
		t.Fatalf("roster still has %+v after removal", got)
	}

	// The removed agent must NOT be respawned by later ticks.
	sup.Tick(ctx)
	if p := procSnapshot(sup, "a1"); p != nil && p.alive() {
		t.Fatal("removed agent was respawned")
	}
}

func TestRemoveForceStopsAfterTimeout(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "a1", Role: "missionrunner"}}
	sup, _, fleet := newTestSup(t, specs)
	sup.RemoveDrainTimeout = -time.Second // already expired: force path
	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "a1", Role: "missionrunner"}, 1, time.Now())
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t"}, time.Now()) // never Drained

	sup.EnqueueMembership(MembershipRequest{Op: MembershipRemove, Spec: WorkerSpec{AgentID: "a1"}})
	sup.Tick(ctx) // sends drain, deadline already past
	sup.Tick(ctx) // force-stop fires
	if p := procSnapshot(sup, "a1"); p != nil && p.alive() {
		t.Fatal("worker not force-stopped after timeout")
	}
	if got := len(sup.Roster()); got != 0 {
		t.Fatalf("roster size = %d, want 0", got)
	}
}

func TestAddLaunchesThroughBudgetAndResetsRestarts(t *testing.T) {
	ctx := t.Context()
	sup, _, _ := newTestSup(t, nil)
	sup.restarts["b1"] = 7 // stale counter from a previous life
	sup.EnqueueMembership(MembershipRequest{Op: MembershipAdd, Spec: WorkerSpec{AgentID: "b1", Role: "missionrunner"}})
	sup.EnqueueMembership(MembershipRequest{Op: MembershipAdd, Spec: WorkerSpec{AgentID: "b2", Role: "missionrunner"}})
	sup.Tick(ctx)
	alive := 0
	for _, id := range []string{"b1", "b2"} {
		if p := procSnapshot(sup, id); p != nil && p.alive() {
			alive++
		}
	}
	if alive != 1 {
		t.Fatalf("launched %d workers in one tick, want 1 (RestartBatch budget)", alive)
	}
	if got := len(sup.Roster()); got != 2 {
		t.Fatalf("roster size = %d, want 2 (both specs recorded)", got)
	}
	// memberAdd cleared the stale counter of 7; the budgeted launch itself
	// then counts as restart #1 via tryRestart. Stale-clear is what matters.
	if got := sup.restarts["b1"]; got > 1 {
		t.Fatalf("restarts[b1] = %d, want <=1 (stale counter must be cleared on add)", got)
	}
	sup.Tick(ctx)
	for _, id := range []string{"b1", "b2"} {
		if p := procSnapshot(sup, id); p == nil || !p.alive() {
			t.Fatalf("%s not launched by second tick", id)
		}
	}
}

func TestUpdateRollsOneWorker(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "a1", Role: "missionrunner", FreightMaxPackages: 3}}
	sup, sender, fleet := newTestSup(t, specs)
	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "a1", Role: "missionrunner"}, 1, time.Now())
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t"}, time.Now())

	updated := WorkerSpec{AgentID: "a1", Role: "missionrunner", FreightMaxPackages: 7}
	sup.EnqueueMembership(MembershipRequest{Op: MembershipUpdate, Spec: updated})
	sup.Tick(ctx)
	if got := sender.drains(); len(got) != 1 {
		t.Fatalf("drains = %v, want one for a1", got)
	}
	old := procSnapshot(sup, "a1")
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t", Drained: true}, time.Now())
	sup.Tick(ctx) // stop
	sup.Tick(ctx) // relaunch through budget
	fresh := procSnapshot(sup, "a1")
	if fresh == nil || !fresh.alive() || fresh == old {
		t.Fatal("update did not relaunch a fresh process")
	}
	roster := sup.Roster()
	if len(roster) != 1 || roster[0].FreightMaxPackages != 7 {
		t.Fatalf("roster = %+v, want a1 with FreightMaxPackages 7", roster)
	}
}

func TestDuplicateRequestsCollapseLastOpWins(t *testing.T) {
	ctx := t.Context()
	sup, sender, _ := newTestSup(t, nil)
	sup.EnqueueMembership(MembershipRequest{Op: MembershipAdd, Spec: WorkerSpec{AgentID: "c1"}})
	sup.EnqueueMembership(MembershipRequest{Op: MembershipRemove, Spec: WorkerSpec{AgentID: "c1"}})
	sup.Tick(ctx)
	// Last op wins: remove of a never-launched agent completes immediately, no drain needed.
	if got := len(sup.Roster()); got != 0 {
		t.Fatalf("roster = %d entries, want 0", got)
	}
	if got := sender.drains(); len(got) != 0 {
		t.Fatalf("drains = %v, want none (no live worker)", got)
	}
}

// TestReaddDuringDrainCancelsRemoval is the F2 regression: an add for an agent
// whose removal is still draining (cross-tick) must cancel the removal — the
// worker ends ALIVE, in the roster, and not marked Leaving — rather than
// letting the in-flight drain race to completion and vanish it from both the
// live roster and the Removed section.
func TestReaddDuringDrainCancelsRemoval(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "a1", Role: "missionrunner"}}
	sup, sender, fleet := newTestSup(t, specs)
	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "a1", Role: "missionrunner"}, 1, time.Now())
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t", System: "sol"}, time.Now())

	// Tick 1: remove -> marks leaving, drain sent, still alive.
	sup.EnqueueMembership(MembershipRequest{Op: MembershipRemove, Spec: WorkerSpec{AgentID: "a1"}})
	sup.Tick(ctx)
	if snap := fleet.Snapshot(); len(snap) != 1 || !snap[0].Leaving {
		t.Fatalf("after remove tick: %+v, want a1 Leaving", snap)
	}

	// The worker reports drained AND the deadline is already past, so without the
	// cancel the very next tick's progressLeaving would stop+remove it. A re-add
	// enqueued for the same tick must win.
	sup.RemoveDrainTimeout = -time.Second
	fleet.ApplyStatus("a1", control.Status{Timestamp: "t", System: "sol", Drained: true}, time.Now())
	sup.EnqueueMembership(MembershipRequest{Op: MembershipAdd, Spec: WorkerSpec{AgentID: "a1", Role: "missionrunner"}})
	sup.Tick(ctx)

	if p := procSnapshot(sup, "a1"); p == nil || !p.alive() {
		t.Fatal("re-added worker was stopped by the racing removal (F2)")
	}
	roster := sup.Roster()
	if len(roster) != 1 || roster[0].AgentID != "a1" {
		t.Fatalf("roster = %+v, want just a1", roster)
	}
	snap := fleet.Snapshot()
	if len(snap) != 1 || snap[0].AgentID != "a1" || snap[0].Leaving {
		t.Fatalf("fleet = %+v, want a1 present and not Leaving", snap)
	}
	if sup.leaving["a1"] != nil {
		t.Fatal("leaving state for a1 not cleared after re-add")
	}
	// The re-added worker must be resumed (un-drained), not left parked.
	resumed := false
	for _, e := range sender.sent {
		if e.Type == control.TypeResume && e.AgentID == "a1" {
			resumed = true
		}
	}
	if !resumed {
		t.Fatal("re-add did not send Resume to un-drain the kept worker")
	}
}

// TestReleaseAndRemoveSameTickNoGhost is the F3 regression: a quarantined agent
// that receives both a ReleaseQuarantine and a membership-remove in the same
// tick must leave NO ghost fleet entry (completeRemoval deletes it; a later
// ClearQuarantine must not re-create a blank one via fleet.get).
func TestReleaseAndRemoveSameTickNoGhost(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "q1", Role: "missionrunner"}}
	sup, _, fleet := newTestSup(t, specs)
	fleet.Quarantine("q1", "stranded")

	sup.ReleaseQuarantine("q1")
	sup.EnqueueMembership(MembershipRequest{Op: MembershipRemove, Spec: WorkerSpec{AgentID: "q1"}})
	sup.Tick(ctx)

	if got := fleet.Snapshot(); len(got) != 0 {
		t.Fatalf("ghost fleet entry left behind: %+v", got)
	}
	if got := sup.Roster(); len(got) != 0 {
		t.Fatalf("roster = %+v, want empty", got)
	}
}

func TestRemoveQuarantinedIsBookkeepingOnly(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "q1", Role: "missionrunner"}}
	sup, sender, fleet := newTestSup(t, specs)
	fleet.Quarantine("q1", "stranded")
	sup.EnqueueMembership(MembershipRequest{Op: MembershipRemove, Spec: WorkerSpec{AgentID: "q1"}})
	sup.Tick(ctx)
	if got := sender.drains(); len(got) != 0 {
		t.Fatalf("drain sent to quarantined worker: %v", got)
	}
	if got := len(sup.Roster()); got != 0 {
		t.Fatalf("roster = %d, want 0", got)
	}
	if got := fleet.Snapshot(); len(got) != 0 {
		t.Fatalf("fleet still has %+v", got)
	}
}
