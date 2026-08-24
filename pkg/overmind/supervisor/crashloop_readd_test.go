package supervisor

import (
	"testing"
)

// TestReaddClearsCrashLoopCounter pins the operator escape hatch.
//
// s.restarts is cleared in exactly one place -- the "seen && Healthy" branch of
// the reap loop -- so an agent that reaches MaxRestarts without ever coming up
// healthy is stuck forever: tryRestart returns early (and silently), so it
// never launches, never goes healthy, and never clears its own counter. Only
// restarting the whole overmind frees it.
//
// Live 2026-08-23: after a ~30-minute network outage, marketbot_000,
// marketbot_003 and miner-1 all sat at exactly restarts=100 with seen=false and
// no process. The documented remedy (dashboard remove then readd) returned
// "accepted" for each and changed nothing, because the sidecar and the fleet
// record are not where the crash-loop counter lives.
func TestReaddClearsCrashLoopCounter(t *testing.T) {
	ctx := t.Context()
	specs := []WorkerSpec{{AgentID: "a1", Role: "miner"}}
	sup, _, _ := newTestSup(t, specs)
	sup.MaxRestarts = 2

	// An agent that crash-looped to the cap without ever reporting healthy.
	sup.restarts["a1"] = sup.MaxRestarts

	budget := 5
	sup.tryRestart(ctx, specs[0], false, &budget)
	if procSnapshot(sup, "a1") != nil {
		t.Fatal("precondition failed: an agent at MaxRestarts must not launch")
	}

	// The documented operator remedy: readd it.
	sup.EnqueueMembership(MembershipRequest{Op: MembershipAdd, Spec: specs[0]})
	sup.Tick(ctx)

	if got := sup.restarts["a1"]; got >= sup.MaxRestarts {
		t.Errorf("readd must clear the crash-loop counter so the agent can launch; got restarts=%d cap=%d", got, sup.MaxRestarts)
	}
	if procSnapshot(sup, "a1") == nil {
		t.Error("readd must actually relaunch the agent; no process was started")
	}
}
