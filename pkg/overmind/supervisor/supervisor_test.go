package supervisor

import (
	"context"
	"io"
	"log"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
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
