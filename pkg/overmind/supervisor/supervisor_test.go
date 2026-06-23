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
	sup.SilenceTimeout = time.Hour  // disable restart churn for this test
	sup.StaggerInterval = 0         // launch back-to-back for this test

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
