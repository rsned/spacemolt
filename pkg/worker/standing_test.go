package worker

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// recordRunner records every command line it runs.
type recordRunner struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordRunner) Run(_ context.Context, tokens []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, joinTokens(tokens))
	return nil
}
func (r *recordRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}
func joinTokens(t []string) string {
	s := ""
	for i, x := range t {
		if i > 0 {
			s += " "
		}
		s += x
	}
	return s
}

type stateClient struct{ st *game.State }

func (s stateClient) GetState() *game.State { return s.st }

func TestRunStandingRunsIdleLoopThenStopsOnCancel(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")

	ctx, cancel := context.WithCancel(context.Background())
	// idle script with no tokens so resolution is a no-op.
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner:       r,
		Scheduler:    sched,
		Client:       stateClient{st: &game.State{}},
		ExecMu:       &mu,
		Paused:       func() bool { return false },
		Out:          io.Discard,
		NowFn:        func() time.Time { return time.Unix(0, 0).UTC() },
		IdleInterval: time.Millisecond,
		AgentID:      "test",
	}
	// Override script resolution by writing the script under a temp scripts dir
	// the resolver can find; simplest: inject commands directly (see Step 4 note).
	done := make(chan struct{})
	go func() { _ = RunStanding(ctx, role, deps); close(done) }()
	// Let a few idle passes run.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunStanding did not return after cancel")
	}
	if len(r.snapshot()) == 0 {
		t.Fatal("idle loop never ran a command")
	}
}

func TestRunStandingPausedDoesNotRunIdle(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var paused atomic.Bool
	paused.Store(true)
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	role := Role{Idle: "noop_idle"}
	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: paused.Load, Out: io.Discard,
		NowFn: func() time.Time { return time.Unix(0, 0).UTC() }, IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(20 * time.Millisecond)
	if n := len(r.snapshot()); n != 0 {
		t.Fatalf("paused worker ran %d commands", n)
	}
}
