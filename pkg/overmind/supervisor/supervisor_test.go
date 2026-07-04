package supervisor

import (
	"context"
	"io"
	"log"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// aliveSpawn returns a SpawnFunc that launches a long-lived `sleep`, counting
// invocations into n.
func aliveSpawn(n *atomic.Int32) SpawnFunc {
	return func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		n.Add(1)
		cmd := exec.CommandContext(ctx, "sleep", "60")
		return cmd, cmd.Start()
	}
}

// procOf fetches the tracked proc for an agent (white-box helper).
func procOf(sup *Supervisor, id string) *workerProc {
	sup.procMu.Lock()
	defer sup.procMu.Unlock()
	return sup.procs[id]
}

func TestReapBootingWorkerNotRespawned(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "boot"}}
	sup := NewSupervisor(nil, NewFleet(), specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0]) // alive, no Hello yet, within BootTimeout
	// Several reap passes must NOT spawn a duplicate (the double-spawn bug).
	for range 5 {
		sup.reapAndRestart(ctx)
	}
	if spawned.Load() != 1 {
		t.Fatalf("booting worker should not be respawned, got %d spawns", spawned.Load())
	}
}

func TestReapWedgedBootKilledAndRespawned(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "wedged"}}
	sup := NewSupervisor(nil, NewFleet(), specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.BootTimeout = time.Nanosecond // any alive-but-unseen worker is "wedged"
	sup.KillGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	sup.reapAndRestart(ctx)
	if spawned.Load() != 2 {
		t.Fatalf("wedged boot should be killed and respawned, got %d spawns", spawned.Load())
	}
}

func TestReapHungEstablishedWorker(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "hung"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Nanosecond // seen worker is immediately "silent"
	sup.KillGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "hung", Role: "idle"}, 1, time.Now())
	fleet.ApplyStatus("hung", control.Status{}, time.Now())

	sup.reapAndRestart(ctx)
	if spawned.Load() != 2 {
		t.Fatalf("hung established worker should be killed and respawned, got %d spawns", spawned.Load())
	}
}

func TestReapHealthyWorkerUntouched(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "ok"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Hour
	sup.restarts["ok"] = 7 // pretend it had a rough start
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	fleet.ApplyHello(control.Hello{AgentID: "ok", Role: "idle"}, 1, time.Now())
	fleet.ApplyStatus("ok", control.Status{}, time.Now())

	sup.reapAndRestart(ctx)
	if spawned.Load() != 1 {
		t.Fatalf("healthy worker must not be respawned, got %d spawns", spawned.Load())
	}
	if sup.restarts["ok"] != 0 {
		t.Fatalf("healthy worker should clear its restart counter, got %d", sup.restarts["ok"])
	}
}

func TestReapDeadWorkerRespawnedUpToCap(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "crash"}}
	// Each spawn exits immediately, modelling a crash-loop.
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		spawned.Add(1)
		cmd := exec.CommandContext(ctx, "true")
		return cmd, cmd.Start()
	}
	sup := NewSupervisor(nil, NewFleet(), specs, spawn, log.New(io.Discard, "", 0))
	sup.MaxRestarts = 3
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for range 10 {
		// Make the reap deterministic: ensure the current proc is fully reaped
		// (exited closed) before the next decision pass.
		if p := procOf(sup, "crash"); p != nil {
			<-p.exited
		}
		sup.reapAndRestart(ctx)
	}
	if spawned.Load() != 3 {
		t.Fatalf("crash-loop should respawn up to MaxRestarts (3), got %d", spawned.Load())
	}
}

func TestReapRestartBatchPacesRelaunches(t *testing.T) {
	specs := []WorkerSpec{{AgentID: "a"}, {AgentID: "b"}, {AgentID: "c"}}
	var spawned atomic.Int32
	sup := NewSupervisor(nil, NewFleet(), specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.RestartBatch = 1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// All three procs are nil (never launched). Each reap tick may relaunch at
	// most RestartBatch=1, so it takes three ticks to bring the fleet up — this
	// is what keeps a mass restart under the per-IP /login limit. The long-lived
	// workers launched on earlier ticks are "booting" and left alone after.
	sup.reapAndRestart(ctx)
	if got := spawned.Load(); got != 1 {
		t.Fatalf("tick 1: expected 1 paced relaunch, got %d", got)
	}
	sup.reapAndRestart(ctx)
	if got := spawned.Load(); got != 2 {
		t.Fatalf("tick 2: expected 2 total relaunches, got %d", got)
	}
	sup.reapAndRestart(ctx)
	if got := spawned.Load(); got != 3 {
		t.Fatalf("tick 3: expected 3 total relaunches, got %d", got)
	}
}

func TestReapRestartBatchUnlimitedWhenZero(t *testing.T) {
	specs := []WorkerSpec{{AgentID: "a"}, {AgentID: "b"}, {AgentID: "c"}}
	var spawned atomic.Int32
	sup := NewSupervisor(nil, NewFleet(), specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.RestartBatch = 0 // opt out of pacing: relaunch all in one pass
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.reapAndRestart(ctx)
	if got := spawned.Load(); got != 3 {
		t.Fatalf("RestartBatch=0 should relaunch all three in one tick, got %d", got)
	}
}

func TestLaunchTracksLiveProcess(t *testing.T) {
	specs := []WorkerSpec{{AgentID: "w1"}}
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, "sleep", "60")
		return cmd, cmd.Start()
	}
	sup := NewSupervisor(nil, NewFleet(), specs, spawn, log.New(io.Discard, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])

	sup.procMu.Lock()
	p := sup.procs["w1"]
	sup.procMu.Unlock()
	if p == nil {
		t.Fatal("launch did not register a workerProc")
	}
	if !p.alive() {
		t.Fatal("freshly launched process should be alive")
	}

	// Killing the process must close exited and flip alive().
	_ = p.cmd.Process.Kill()
	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("exited channel not closed after process death")
	}
	if p.alive() {
		t.Fatal("alive() should be false after process exit")
	}
}

func TestKillGracefulOnSigterm(t *testing.T) {
	// A plain `sleep` dies on SIGTERM, so kill should return well before grace.
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, "sleep", "60")
		return cmd, cmd.Start()
	}
	sup := NewSupervisor(nil, NewFleet(), []WorkerSpec{{AgentID: "g"}}, spawn, log.New(io.Discard, "", 0))
	sup.KillGrace = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup.launch(ctx, WorkerSpec{AgentID: "g"})

	sup.procMu.Lock()
	p := sup.procs["g"]
	sup.procMu.Unlock()

	start := time.Now()
	sup.kill(p)
	if p.alive() {
		t.Fatal("process should be dead after kill")
	}
	if elapsed := time.Since(start); elapsed >= sup.KillGrace {
		t.Fatalf("SIGTERM-respecting process should die before grace, took %v", elapsed)
	}
}

func TestKillEscalatesToSigkill(t *testing.T) {
	// This child ignores SIGTERM, so kill must escalate to SIGKILL after grace.
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, "sh", "-c", `trap "" TERM; sleep 60`)
		return cmd, cmd.Start()
	}
	sup := NewSupervisor(nil, NewFleet(), []WorkerSpec{{AgentID: "k"}}, spawn, log.New(io.Discard, "", 0))
	sup.KillGrace = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup.launch(ctx, WorkerSpec{AgentID: "k"})
	// Allow the shell process a moment to install the SIGTERM trap before we
	// send SIGTERM; without this the signal may arrive before 'trap "" TERM'
	// runs and the shell would exit immediately, making the test racy.
	time.Sleep(20 * time.Millisecond)

	sup.procMu.Lock()
	p := sup.procs["k"]
	sup.procMu.Unlock()

	start := time.Now()
	sup.kill(p)
	if p.alive() {
		t.Fatal("process should be dead after SIGKILL escalation")
	}
	if elapsed := time.Since(start); elapsed < sup.KillGrace {
		t.Fatalf("escalation should wait at least KillGrace, took %v", elapsed)
	}
}

func TestSupervisorSpawnsEachSpecOnce(t *testing.T) {
	specs := []WorkerSpec{{AgentID: "a"}, {AgentID: "b"}}
	var spawned atomic.Int32
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		spawned.Add(1)
		// A real, harmless short-lived command stands in for a worker.
		cmd := exec.CommandContext(ctx, "true")
		return cmd, cmd.Start()
	}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, spawn, log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Hour // disable restart churn for this test
	sup.StaggerInterval = 0        // launch back-to-back for this test

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = sup.Run(ctx)

	if spawned.Load() < 2 {
		t.Fatalf("expected >=2 spawns, got %d", spawned.Load())
	}
}

func TestRunStaggersInitialLaunches(t *testing.T) {
	specs := []WorkerSpec{{AgentID: "a"}, {AgentID: "b"}, {AgentID: "c"}}
	var spawned atomic.Int32
	sup := NewSupervisor(nil, NewFleet(), specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Hour
	sup.StaggerInterval = 100 * time.Millisecond

	// Cancel after only enough time for the first launch (plus margin), well
	// before the second stagger interval elapses.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = sup.Run(ctx)

	if got := spawned.Load(); got != 1 {
		t.Fatalf("with a 100ms stagger and 50ms budget, expected 1 launch, got %d", got)
	}
}

func TestStrandedWorkerQuarantinedAndReleased(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "dead"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.StallTimeout = time.Nanosecond
	sup.KillGrace = time.Second
	var quarantines atomic.Int32
	sup.OnQuarantine = func(w WorkerInfo, reason string) {
		if w.AgentID != "dead" || reason == "" {
			t.Errorf("bad quarantine callback: %q %q", w.AgentID, reason)
		}
		// Ordering pin: the callback (which lands the rescue record) must run
		// before the Quarantined flag is visible, so a concurrent rejoin poll
		// can never observe "quarantined with no record" while the enqueue is
		// still in flight.
		if fleet.IsQuarantined("dead") {
			t.Error("quarantine flag must not be visible yet inside OnQuarantine")
		}
		quarantines.Add(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "dead", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("dead", control.Status{Fuel: 0, MaxFuel: 420}, now)

	sup.reapAndRestart(ctx) // fuel-dead + stalled(1ns) → quarantine, not restart
	if quarantines.Load() != 1 {
		t.Fatalf("want 1 quarantine callback, got %d", quarantines.Load())
	}
	if !fleet.IsQuarantined("dead") {
		t.Fatal("worker not marked quarantined")
	}
	if spawned.Load() != 1 {
		t.Fatalf("quarantined worker must not respawn, got %d spawns", spawned.Load())
	}

	sup.reapAndRestart(ctx) // still held
	if spawned.Load() != 1 || quarantines.Load() != 1 {
		t.Fatalf("quarantine must hold: spawns=%d quarantines=%d", spawned.Load(), quarantines.Load())
	}

	sup.ReleaseQuarantine("dead")
	sup.reapAndRestart(ctx) // released → relaunch
	if spawned.Load() != 2 {
		t.Fatalf("released worker should relaunch, got %d spawns", spawned.Load())
	}
	if fleet.IsQuarantined("dead") {
		t.Fatal("release must clear the quarantine flag")
	}
}

func TestStallRestartStillFiresWhenFueled(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "stuck"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.StallTimeout = time.Nanosecond
	sup.KillGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "stuck", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("stuck", control.Status{Fuel: 300, MaxFuel: 420}, now)

	sup.reapAndRestart(ctx) // fueled → plain stall restart, counter increments
	if spawned.Load() != 2 {
		t.Fatalf("fueled stalled worker should restart, got %d spawns", spawned.Load())
	}
	if got := fleet.Snapshot()[0].StallRestarts; got != 1 {
		t.Fatalf("stall restart must increment counter, got %d", got)
	}
}
