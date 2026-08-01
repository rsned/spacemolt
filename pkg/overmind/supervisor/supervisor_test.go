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

// A worker that reports its game connection down is self-healing via the
// reconnect gate; the stall watchdog must NOT restart it within DisconnectGrace,
// because a restart's fresh login cannot succeed during a per-IP block and
// deepens it (the server-deploy → mass-disconnect → login-storm outage).
func TestDisconnectedWorkerNotStallRestartedWithinGrace(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "netdown"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.StallTimeout = time.Nanosecond // would stall instantly if not disconnected
	sup.DisconnectGrace = time.Hour    // generous grace: gate is still working
	sup.KillGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "netdown", Role: "hauler"}, 1, now)
	// Frozen (undocked, fueled) AND disconnected: the disconnect exemption must win.
	fleet.ApplyStatus("netdown", control.Status{Fuel: 300, MaxFuel: 420, Disconnected: true}, now)

	sup.reapAndRestart(ctx)
	if spawned.Load() != 1 {
		t.Fatalf("disconnected worker within grace must NOT be restarted, got %d spawns", spawned.Load())
	}
	if got := fleet.Snapshot()[0].StallRestarts; got != 0 {
		t.Fatalf("no stall restart expected while disconnected, got %d", got)
	}
}

// hasArg reports whether needle appears anywhere in args.
func hasArg(args []string, needle string) bool {
	for _, a := range args {
		if a == needle {
			return true
		}
	}
	return false
}

// hasArgPair reports whether flag is immediately followed by value in args.
func hasArgPair(args []string, flag, value string) bool {
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

// DefaultSpawn's freight flags are the last hop from WorkerSpec to the worker
// process's argv; a table-driven check here is what would have caught a typo
// in the flag name or a missed "> 1" guard.
func TestDefaultSpawnFreightArgs(t *testing.T) {
	tests := []struct {
		name     string
		spec     WorkerSpec
		wantArgs []string // must all be present
		wantNo   []string // must all be absent
	}{
		{
			name:     "enable freight with multi-package cap",
			spec:     WorkerSpec{AgentID: "canary", EnableFreight: true, FreightMaxPackages: 3},
			wantArgs: []string{"--enable-freight"},
		},
		{
			name:   "freight disabled: no freight flags at all",
			spec:   WorkerSpec{AgentID: "plain"},
			wantNo: []string{"--enable-freight", "--freight-max-packages"},
		},
		{
			name:   "cap of 0 leaves the flag absent (worker default 1)",
			spec:   WorkerSpec{AgentID: "zero", EnableFreight: true, FreightMaxPackages: 0},
			wantNo: []string{"--freight-max-packages"},
		},
		{
			name:   "cap of 1 leaves the flag absent (worker default 1)",
			spec:   WorkerSpec{AgentID: "one", EnableFreight: true, FreightMaxPackages: 1},
			wantNo: []string{"--freight-max-packages"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spawn := DefaultSpawn("true", "", "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cmd, err := spawn(ctx, tt.spec, "/tmp/does-not-matter.sock")
			if err != nil {
				t.Fatalf("spawn: %v", err)
			}
			_ = cmd.Wait()
			for _, want := range tt.wantArgs {
				if !hasArg(cmd.Args, want) {
					t.Errorf("args %v missing %q", cmd.Args, want)
				}
			}
			for _, unwanted := range tt.wantNo {
				if hasArg(cmd.Args, unwanted) {
					t.Errorf("args %v must not contain %q", cmd.Args, unwanted)
				}
			}
		})
	}

	// The pair check needs its own case since it inspects flag+value adjacency.
	t.Run("cap of 3 forwards the paired flag and value", func(t *testing.T) {
		spawn := DefaultSpawn("true", "", "")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cmd, err := spawn(ctx, WorkerSpec{AgentID: "canary", EnableFreight: true, FreightMaxPackages: 3}, "/tmp/does-not-matter.sock")
		if err != nil {
			t.Fatalf("spawn: %v", err)
		}
		_ = cmd.Wait()
		if !hasArgPair(cmd.Args, "--freight-max-packages", "3") {
			t.Errorf("args %v missing --freight-max-packages 3", cmd.Args)
		}
	})
}

// The bootstrap kill switch defaults on (CLI default), so DefaultSpawn must
// only ever emit --freight-bootstrap=false, and only when the operator has
// explicitly set DisableFreightBootstrap; the default-on case must forward no
// flag at all, relying on the worker's own CLI default.
func TestDefaultSpawnFreightBootstrapDisable(t *testing.T) {
	spawn := DefaultSpawn("true", "", "")

	t.Run("bootstrap on by default: no disable flag", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cmd, err := spawn(ctx, WorkerSpec{AgentID: "on", Role: "missions", EnableFreight: true}, "/tmp/does-not-matter.sock")
		if err != nil {
			t.Fatalf("spawn: %v", err)
		}
		_ = cmd.Wait()
		if hasArg(cmd.Args, "--freight-bootstrap=false") {
			t.Errorf("bootstrap on by default must not emit the disable flag: %v", cmd.Args)
		}
	})

	t.Run("DisableFreightBootstrap set: disable flag present", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cmd, err := spawn(ctx, WorkerSpec{AgentID: "off", Role: "missions", EnableFreight: true, DisableFreightBootstrap: true}, "/tmp/does-not-matter.sock")
		if err != nil {
			t.Fatalf("spawn: %v", err)
		}
		_ = cmd.Wait()
		if !hasArg(cmd.Args, "--freight-bootstrap=false") {
			t.Errorf("DisableFreightBootstrap must emit --freight-bootstrap=false: %v", cmd.Args)
		}
	})
}

// TestDefaultSpawnAssetsDBPath mirrors handoffQueuePath's forwarding
// behavior exactly: an empty path forwards nothing (leaving the worker's own
// --assets-db-path default of "" in effect, which disables asset capture),
// and a non-empty path is forwarded verbatim as --assets-db-path. Without
// this, no spawn path can ever pass the flag, and the rollout runbook's
// "add --assets-db-path to a single worker" step is impossible.
func TestDefaultSpawnAssetsDBPath(t *testing.T) {
	t.Run("empty path: flag absent", func(t *testing.T) {
		spawn := DefaultSpawn("true", "", "")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cmd, err := spawn(ctx, WorkerSpec{AgentID: "canary"}, "/tmp/does-not-matter.sock")
		if err != nil {
			t.Fatalf("spawn: %v", err)
		}
		_ = cmd.Wait()
		if hasArg(cmd.Args, "--assets-db-path") {
			t.Errorf("args %v must not contain --assets-db-path when unset", cmd.Args)
		}
	})

	t.Run("non-empty path: forwarded verbatim", func(t *testing.T) {
		spawn := DefaultSpawn("true", "", "data/assets.db")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cmd, err := spawn(ctx, WorkerSpec{AgentID: "canary"}, "/tmp/does-not-matter.sock")
		if err != nil {
			t.Fatalf("spawn: %v", err)
		}
		_ = cmd.Wait()
		if !hasArgPair(cmd.Args, "--assets-db-path", "data/assets.db") {
			t.Errorf("args %v missing --assets-db-path data/assets.db", cmd.Args)
		}
	})
}

// Past DisconnectGrace the worker is treated as genuinely wedged (its reconnect
// never recovered) and falls through to the stall watchdog's restart.
func TestDisconnectedWorkerRestartedAfterGrace(t *testing.T) {
	var spawned atomic.Int32
	specs := []WorkerSpec{{AgentID: "wedged"}}
	fleet := NewFleet()
	sup := NewSupervisor(nil, fleet, specs, aliveSpawn(&spawned), log.New(io.Discard, "", 0))
	sup.StallTimeout = time.Nanosecond
	sup.DisconnectGrace = time.Millisecond // tiny grace so the past timestamp exceeds it
	sup.KillGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup.launch(ctx, specs[0])
	past := time.Now().Add(-time.Second) // disconnected a full second ago (>> 1ms grace)
	fleet.ApplyHello(control.Hello{AgentID: "wedged", Role: "hauler"}, 1, past)
	fleet.ApplyStatus("wedged", control.Status{Fuel: 300, MaxFuel: 420, Disconnected: true}, past)

	sup.reapAndRestart(ctx)
	if spawned.Load() != 2 {
		t.Fatalf("worker disconnected past grace must restart, got %d spawns", spawned.Load())
	}
}
