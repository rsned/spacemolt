package supervisor

import (
	"context"
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// A worker that HAS reported at some point keeps its fleet record across a
// restart, LastSeen and all. The silence check compares against that record, so
// the moment the old incarnation went quiet every subsequent respawn was judged
// on a timestamp that predated it: killed ~5s into a connect that needs 10-20s,
// respawned, killed again, at the reap tick's cadence until MaxRestarts.
//
// The existing guard above this branch covers only a ZERO LastSeen ("never an
// actual Hello"). A STALE non-zero LastSeen walks straight into NeedsRestart.
//
// Live cost 2026-08-22: five agents across four fleets — johnny_cab (100
// restarts), assist-frontier, craftsman-9, craftsman-8, and miner-1 (138
// restarts, 5h dead). Each needed a manual dashboard remove/readd, which works
// precisely because it clears the stale record. Every redeploy re-creates the
// conditions, so this fires again on the next one.
func TestReapGivesARespawnedWorkerItsBootWindow(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "respawned"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Nanosecond // the old record is instantly "silent"
	sup.BootTimeout = time.Hour          // a fresh process is comfortably still booting
	sup.KillGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	// Model a worker that has been up a while: launched 20m ago, last reported
	// 10m ago (after its own launch), then went quiet. Backdating launchedAt is
	// what makes the first kill legitimate — a report can never predate a launch.
	procOf(sup, "respawned").launchedAt = time.Now().Add(-20 * time.Minute)
	old := time.Now().Add(-10 * time.Minute)
	fleet.ApplyHello(control.Hello{AgentID: "respawned", Role: "idle"}, 1, old)
	fleet.ApplyStatus("respawned", control.Status{}, old)

	// First reap: correctly kills the silent worker and respawns it.
	sup.reapAndRestart(ctx)
	if got := spawned.Load(); got != 2 {
		t.Fatalf("first reap: spawned=%d, want 2 (kill the silent worker, respawn it)", got)
	}

	// The replacement launched just now and cannot have reported yet. Further
	// reaps must leave it alone until BootTimeout, exactly as a cold boot is.
	for range 5 {
		sup.reapAndRestart(ctx)
	}
	if got := spawned.Load(); got != 2 {
		t.Fatalf("spawned=%d after 5 more reaps, want 2: the respawn is being killed on the "+
			"PREVIOUS incarnation's LastSeen before it can send Hello — the restart loop", got)
	}
}

// The guard must not blunt the real silence watchdog: once the current process
// has itself reported, a genuine heartbeat gap still restarts it.
func TestReapStillRestartsAWorkerThatWentSilentAfterReporting(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "wentquiet"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Nanosecond
	sup.BootTimeout = time.Hour
	sup.KillGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	// This incarnation reported AFTER it launched, then fell silent. The stamp
	// must be in the PAST or NeedsRestart sees a negative gap and never fires.
	reported := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "wentquiet", Role: "idle"}, 1, reported)
	fleet.ApplyStatus("wentquiet", control.Status{}, reported)

	sup.reapAndRestart(ctx)
	if got := spawned.Load(); got != 2 {
		t.Fatalf("spawned=%d, want 2: a worker silent since ITS OWN report must still restart", got)
	}
}
