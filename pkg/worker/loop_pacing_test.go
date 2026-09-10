package worker

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// The 2026-09-10 IP block. `loop -f 100 mine` spun at up to 99 iterations in a
// SINGLE second because -f swallowed every rate_limited reply and a rejected
// command costs no tick: the harder the server throttled us, the faster we
// hammered it. Force tolerates FAILURES, but a rate limit is the server telling
// us to stop — continuing past it is what escalates a 2-minute block to 30.
func TestExecuteLoop_RateLimitedAbortsEvenUnderForce(t *testing.T) {
	body := mustParseStmts(t, "mine")
	limited := &game.ServerError{
		Code:    "rate_limited",
		Message: "Rate limit reached: game actions are capped at 30/min for this session",
	}
	dispatch, calls := recordingDispatcher([]error{nil, limited})

	err := ExecuteLoop(context.Background(), io.Discard, 100, true, body, 0, dispatch)

	if err == nil {
		t.Fatal("force swallowed a rate limit; the loop must abort and report it")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want it to wrap ErrRateLimited", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("dispatched %d times, want 2 (stop ON the rate limit, not after)", len(*calls))
	}
}

// The block itself, not just its precursor. Every command returns the same
// countdown while it lasts, so there is nothing to gain from another attempt.
func TestExecuteLoop_IPTimedOutAbortsEvenUnderForce(t *testing.T) {
	body := mustParseStmts(t, "mine")
	blocked := &game.ServerError{
		Code:    "ip_timed_out",
		Message: "Your IP has been temporarily blocked. Try again in 918 seconds.",
	}
	dispatch, calls := recordingDispatcher([]error{blocked})

	err := ExecuteLoop(context.Background(), io.Discard, 100, true, body, 0, dispatch)

	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want it to wrap ErrRateLimited", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("dispatched %d times, want 1", len(*calls))
	}
}

// Some transports (MCP tool errors) deliver only message text -- no code, no
// details. Classifying on the code alone would miss those entirely, which is
// exactly the transport where we have the least other signal.
func TestExecuteLoop_RateLimitedByMessageTextAbortsUnderForce(t *testing.T) {
	body := mustParseStmts(t, "mine")
	textOnly := errors.New("Rate limit reached: game actions are capped at 30/min for this session")
	dispatch, calls := recordingDispatcher([]error{textOnly})

	err := ExecuteLoop(context.Background(), io.Discard, 100, true, body, 0, dispatch)

	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want it to wrap ErrRateLimited", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("dispatched %d times, want 1", len(*calls))
	}
}

// Regression guard for what -f is actually FOR: an ordinary failure (a dry
// belt, a missed jump) must still be tolerated, or raising the abort above
// would quietly turn every -f loop into a non-force loop.
func TestExecuteLoop_OrdinaryErrorStillToleratedUnderForce(t *testing.T) {
	body := mustParseStmts(t, "mine")
	dispatch, calls := recordingDispatcher([]error{
		errors.New("Nothing to mine here"),
		errors.New("Nothing to mine here"),
	})

	if err := ExecuteLoop(context.Background(), io.Discard, 3, true, body, 0, dispatch); err != nil {
		t.Fatalf("force must tolerate ordinary errors, got %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("dispatched %d times, want all 3", len(*calls))
	}
}

// recordSleeps swaps the package sleep seam for one that records durations
// instead of waiting, and restores it when the test ends.
func recordSleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	var got []time.Duration
	prev := sleepFunc
	sleepFunc = func(ctx context.Context, d time.Duration) error {
		got = append(got, d)
		return ctx.Err()
	}
	t.Cleanup(func() { sleepFunc = prev })
	return &got
}

// Defence in depth for every OTHER error that costs no tick. This is the
// 2026-08-26 failure: a missed jump left the agent docked and the loop then ran
// 100 `mine` attempts against "Nothing to mine here" in a burst, every pass. An
// iteration retried under -f must not be allowed to outrun the game tick.
func TestExecuteLoop_ErroredIterationIsPacedToTheTick(t *testing.T) {
	slept := recordSleeps(t)
	body := mustParseStmts(t, "mine")
	dispatch, _ := recordingDispatcher([]error{
		errors.New("Nothing to mine here"),
		errors.New("Nothing to mine here"),
	})

	if err := ExecuteLoop(context.Background(), io.Discard, 3, true, body, 0, dispatch); err != nil {
		t.Fatalf("err: %v", err)
	}

	if len(*slept) != 2 {
		t.Fatalf("slept %d times (%v), want one pace per tolerated error", len(*slept), *slept)
	}
	for i, d := range *slept {
		if d != game.SleepTick {
			t.Errorf("pace %d = %v, want %v (the tick the server resolves actions on)", i, d, game.SleepTick)
		}
	}
}

// A successful iteration already cost a server tick, so pacing it would halve
// the throughput of every healthy loop in the fleet for no benefit.
func TestExecuteLoop_SuccessfulIterationIsNotPaced(t *testing.T) {
	slept := recordSleeps(t)
	body := mustParseStmts(t, "mine")
	dispatch, _ := recordingDispatcher(nil)

	if err := ExecuteLoop(context.Background(), io.Discard, 5, true, body, 0, dispatch); err != nil {
		t.Fatalf("err: %v", err)
	}

	if len(*slept) != 0 {
		t.Fatalf("paced a healthy loop %d times (%v), want 0", len(*slept), *slept)
	}
}

// The exact 2026-09-09 initiator, worth its own name because the whole block
// traces back to it. `mine` is acknowledged with "Mine action pending. Will
// execute on next tick", which our terminator does not accept as terminal, so
// the client waits for the yield and can time out (SleepActionStartTimeout,
// 30s). The loop then retries immediately -- but the server still holds that
// mine pending, so every retry is REFUSED INSTANTLY with "Another action is
// already pending". Each refusal is a real round-trip costing no tick, so the
// loop ran 51 attempts in one minute (fighter-7, 21:44) and crossed the 30/min
// game_mutation cap for its session. Only waiting a tick can clear it.
func TestExecuteLoop_ActionAlreadyPendingIsPacedNotSpun(t *testing.T) {
	slept := recordSleeps(t)
	body := mustParseStmts(t, "mine")
	pending := &game.ServerError{
		Code:    "action_pending",
		Message: "Another action is already pending (mine). Wait for it to complete.",
	}
	dispatch, calls := recordingDispatcher([]error{pending, pending, pending})

	if err := ExecuteLoop(context.Background(), io.Discard, 4, true, body, 0, dispatch); err != nil {
		t.Fatalf("err: %v", err)
	}

	// -f still tolerates it: mining resumes once the pending action lands.
	if len(*calls) != 4 {
		t.Fatalf("dispatched %d times, want 4", len(*calls))
	}
	// But every retry waits a tick, so 4 attempts span ~3 ticks, not one second.
	if len(*slept) != 3 {
		t.Fatalf("paced %d times (%v), want 3", len(*slept), *slept)
	}
}

// A loop whose body cannot reach the server at all must not spin either. On
// 2026-09-09 a reconnect storm left workers disconnected and they burned 878
// iterations of `loop -f 100 mine` in a single second against "not connected".
// Those sent no bytes, so they did not earn the block, but they are the same
// defect and they starve the scheduler that would reconnect them.
func TestExecuteLoop_DisconnectedBodyIsPacedNotSpun(t *testing.T) {
	slept := recordSleeps(t)
	body := mustParseStmts(t, "mine")
	dispatch, _ := recordingDispatcher([]error{
		errors.New("not connected"),
		errors.New("not connected"),
	})

	if err := ExecuteLoop(context.Background(), io.Discard, 3, true, body, 0, dispatch); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(*slept) != 2 {
		t.Fatalf("paced %d times (%v), want 2", len(*slept), *slept)
	}
}
