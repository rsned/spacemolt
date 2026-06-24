// Command overmind is the supervisor process that manages worker agents.
//
// It loads a fleet roster, starts a Unix-socket control server, and spawns
// worker subprocesses via DefaultSpawn. Workers connect back over the socket
// to send heartbeats; overmind logs a compact status table every
// game.SleepMedium and sends an Abort to each connected worker on shutdown.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
	"github.com/rsned/spacemolt/pkg/overmind/tasks"
)

func main() {
	socketPath := flag.String("socket", "data/overmind/overmind.sock", "Unix socket path for worker control channel")
	workerBin := flag.String("worker-bin", "bin/worker", "Path to the worker binary")
	fleetPath := flag.String("fleet", "data/overmind/fleet.yaml", "Path to fleet roster YAML")
	tasksPath := flag.String("tasks", "data/overmind/tasks.yaml", "Path to the assigned-task seed file")
	stagger := flag.Duration("stagger", game.SleepMedium, "Delay between initial worker launches (per-IP /login pacing)")
	restartBatch := flag.Int("restart-batch", 1, "Max worker relaunches per reap tick (per-IP /login pacing for mass restarts; <=0 disables)")
	flag.Parse()

	logger := log.New(os.Stdout, "[overmind] ", log.LstdFlags)

	// ── Step 1: Load fleet roster ────────────────────────────────────────────
	specs, err := supervisor.LoadFleet(*fleetPath)
	if err != nil {
		logger.Fatalf("load fleet: %v", err)
	}
	logger.Printf("loaded %d worker spec(s) from %s", len(specs), *fleetPath)

	// ── Step 2: Build fleet registry, control server, and supervisor ─────────
	fleet := supervisor.NewFleet()

	srv, err := supervisor.NewServer(*socketPath, fleet, logger)
	if err != nil {
		logger.Fatalf("new server: %v", err)
	}

	sup := supervisor.NewSupervisor(srv, fleet, specs, supervisor.DefaultSpawn(*workerBin), logger)
	sup.StaggerInterval = *stagger
	sup.RestartBatch = *restartBatch

	// ── Step 2b: Load task store and wire event hook ──────────────────────────
	var taskStore *tasks.Store
	if loaded, terr := tasks.LoadTasks(*tasksPath); terr != nil {
		logger.Printf("tasks: %v (continuing with no tasks)", terr)
		taskStore = tasks.NewStore(nil, logger)
	} else {
		logger.Printf("loaded %d task(s) from %s", len(loaded), *tasksPath)
		taskStore = tasks.NewStore(loaded, logger)
	}
	srv.SetEventHook(func(agentID string, ev control.Event) {
		taskStore.HandleEvent(agentID, ev)
	})

	// ── Step 3: Signal-cancellable root context ──────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("received signal %v, shutting down", sig)
		cancel()
	}()

	// ── Step 4: Start server (Canceled is expected on clean shutdown) ─────────
	go func() {
		if err := srv.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Printf("server error: %v", err)
		}
	}()

	// ── Step 5: Start supervisor ──────────────────────────────────────────────
	go func() {
		if err := sup.Run(ctx); err != nil {
			logger.Printf("supervisor error: %v", err)
		}
	}()

	logger.Printf("overmind running; socket=%s worker-bin=%s fleet=%s", *socketPath, *workerBin, *fleetPath)

	// ── Step 6: Status ticker ─────────────────────────────────────────────────
	ticker := time.NewTicker(game.SleepMedium)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// ── Step 7: Best-effort abort all connected workers ──────────────
			logger.Printf("sending abort to connected workers")
			for _, w := range fleet.Snapshot() {
				env, envErr := control.NewEnvelope(control.TypeAbort, w.AgentID, control.Abort{
					Reason: "overmind shutdown",
				})
				if envErr != nil {
					logger.Printf("build abort envelope for %q: %v", w.AgentID, envErr)
					continue
				}
				if sendErr := srv.Send(w.AgentID, env); sendErr != nil {
					// Not an error — worker may not be connected yet or may have exited.
					logger.Printf("send abort to %q: %v", w.AgentID, sendErr)
				}
			}
			logger.Printf("overmind shutdown complete")
			return

		case <-ticker.C:
			snap := fleet.Snapshot()
			taskStore.AssignPending(snap, srv)
			logFleetSnapshot(logger, snap)
		}
	}
}

// logFleetSnapshot prints a compact table of current worker status.
func logFleetSnapshot(logger *log.Logger, workers []supervisor.WorkerInfo) {
	if len(workers) == 0 {
		logger.Printf("fleet status: no workers registered")
		return
	}
	logger.Printf("fleet status (%d workers):", len(workers))
	logger.Printf("  %-20s %-12s %-20s %6s  %-7s  %s",
		"AGENT", "ROLE", "SYSTEM", "HULL%", "HEALTHY", "RESTARTS")
	for _, w := range workers {
		hullPct := 0.0
		if w.LastStatus.MaxHull > 0 {
			hullPct = 100.0 * w.LastStatus.Hull / w.LastStatus.MaxHull
		}
		healthy := "yes"
		if !w.Healthy {
			healthy = "no"
		}
		logger.Printf("  %-20s %-12s %-20s %5.1f%%  %-7s  %d",
			w.AgentID, w.Role, w.LastStatus.System, hullPct, healthy, w.Restarts)
	}
}
