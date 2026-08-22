package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

// blockingRunner never returns until its context is cancelled — the shape of a
// command whose server reply was lost to a disconnect.
type blockingRunner struct{ entered chan struct{} }

func (b *blockingRunner) Run(ctx context.Context, _ []string) error {
	if b.entered != nil {
		close(b.entered)
	}
	<-ctx.Done()
	return ctx.Err()
}

// A lost reply used to park the standing loop forever: RequestHandle.Result
// waits on a sync.Cond that only the reply (or a context cancel) broadcasts, and
// nothing cancelled the request when the connection dropped. Because the idle
// script and the scheduler share ExecMu, that one stuck call took the scheduler
// down with it — six workers across three fleets ran 3.5 days with every
// scheduled command dead while still heartbeating and reporting healthy
// (2026-08-22; goroutine 47 parked 4,964 minutes in Client.Refuel).
//
// Nothing bounded a dispatched command, so "forever" was literal. This asserts
// the bound exists: the runner blocks, and dispatch still returns.
func TestDispatchBoundsAStuckCommand(t *testing.T) {
	deps := StandingDeps{
		Runner:         &blockingRunner{entered: make(chan struct{})},
		CommandTimeout: 50 * time.Millisecond,
	}

	done := make(chan error, 1)
	go func() { done <- deps.dispatch(context.Background(), []string{"refuel"}) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("dispatch returned %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch never returned: a stuck command still hangs the standing loop forever")
	}
}

// The bound must not fire on a command that simply takes a while — autopilot
// flies a whole multi-jump route inside one dispatch.
func TestDispatchLetsASlowCommandFinish(t *testing.T) {
	slow := runnerFunc(func(ctx context.Context, _ []string) error {
		select {
		case <-time.After(20 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	deps := StandingDeps{Runner: slow, CommandTimeout: 2 * time.Second}
	if err := deps.dispatch(context.Background(), []string{"autopilot", "dheneb"}); err != nil {
		t.Fatalf("dispatch aborted a slow-but-healthy command: %v", err)
	}
}

// A caller-cancelled context must still win immediately — the bound adds a
// ceiling, it does not take cancellation away from the worker's own shutdown.
func TestDispatchStillHonoursCallerCancellation(t *testing.T) {
	b := &blockingRunner{entered: make(chan struct{})}
	deps := StandingDeps{Runner: b, CommandTimeout: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- deps.dispatch(ctx, []string{"mine"}) }()
	<-b.entered
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dispatch returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("caller cancellation no longer reaches the runner")
	}
}

type runnerFunc func(ctx context.Context, tokens []string) error

func (f runnerFunc) Run(ctx context.Context, tokens []string) error { return f(ctx, tokens) }
