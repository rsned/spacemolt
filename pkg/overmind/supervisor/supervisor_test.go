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

func TestReapAndRestartCapsUnseenWorkers(t *testing.T) {
	specs := []WorkerSpec{{AgentID: "ghost"}}
	var spawned atomic.Int32
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		spawned.Add(1)
		cmd := exec.CommandContext(ctx, "true")
		return cmd, cmd.Start()
	}
	sup := NewSupervisor(nil, NewFleet(), specs, spawn, log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Nanosecond // unseen worker always "needs restart"
	sup.MaxRestarts = 3
	ctx := context.Background()
	for range 10 {
		sup.reapAndRestart(ctx)
	}
	if spawned.Load() != 3 {
		t.Fatalf("expected exactly 3 spawns (MaxRestarts cap), got %d", spawned.Load())
	}
}

func TestReapAndRestartCounterResetsOnHealthy(t *testing.T) {
	specs := []WorkerSpec{{AgentID: "flaky"}}
	var spawned atomic.Int32
	spawn := func(ctx context.Context, spec WorkerSpec, socket string) (*exec.Cmd, error) {
		spawned.Add(1)
		cmd := exec.CommandContext(ctx, "true")
		return cmd, cmd.Start()
	}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, spawn, log.New(io.Discard, "", 0))
	sup.SilenceTimeout = time.Nanosecond // unseen worker always "needs restart"
	sup.MaxRestarts = 2
	ctx := context.Background()

	// Drive to the cap.
	for range 5 {
		sup.reapAndRestart(ctx)
	}
	if spawned.Load() != 2 {
		t.Fatalf("expected 2 spawns at cap, got %d", spawned.Load())
	}

	// Simulate the worker becoming healthy: register it in the fleet.
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "flaky", Role: "idle"}, 999, now)
	fleet.ApplyStatus("flaky", control.Status{}, now)

	// With a long SilenceTimeout the worker is no longer "needs restart".
	sup.SilenceTimeout = time.Hour
	sup.reapAndRestart(ctx) // should reset the counter via the else-branch

	// Now make the worker unseen again (short timeout).
	sup.SilenceTimeout = time.Nanosecond
	for range 5 {
		sup.reapAndRestart(ctx)
	}
	// Counter was reset to 0, so we should get MaxRestarts (2) more spawns.
	if spawned.Load() != 4 {
		t.Fatalf("expected 4 total spawns after reset (2+2), got %d", spawned.Load())
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

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = sup.Run(ctx)

	if spawned.Load() < 2 {
		t.Fatalf("expected >=2 spawns, got %d", spawned.Load())
	}
}
