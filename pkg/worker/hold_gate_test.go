package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// heldState hands out a State that stays held for the first heldFor reads.
type heldState struct {
	st      *game.State
	heldFor int
	reads   int
}

func (h *heldState) GetState() *game.State {
	h.reads++
	if h.reads > h.heldFor {
		h.st.ActiveHold = nil
	}
	return h.st
}

func newHeld(heldFor int) *heldState {
	return &heldState{heldFor: heldFor, st: &game.State{ActiveHold: &game.ActiveHold{
		HolderName: "[CUSTOMS] The Levy", Source: "customs", At: time.Now(),
	}}}
}

func TestWaitOutHoldWaitsUntilCleared(t *testing.T) {
	h := newHeld(3)
	var out bytes.Buffer
	waitOutHold(context.Background(), &out, h)
	if h.reads <= 3 {
		t.Fatalf("returned after %d reads, want it to keep waiting until the hold cleared", h.reads)
	}
	for _, want := range []string{"[CUSTOMS] The Levy", "waiting", "cleared"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q missing %q", out.String(), want)
		}
	}
}

func TestWaitOutHoldNotHeldReturnsAtOnce(t *testing.T) {
	var out bytes.Buffer
	waitOutHold(context.Background(), &out, &heldState{st: &game.State{}})
	if out.Len() != 0 {
		t.Errorf("printed %q with no hold", out.String())
	}
}

// A hold whose end never arrives (and whose stale window the instant test
// sleep never reaches) must still release the worker.
func TestWaitOutHoldIsBounded(t *testing.T) {
	h := newHeld(1 << 30)
	waitOutHold(context.Background(), io.Discard, h)
	if h.reads > holdWaitTicks+2 {
		t.Fatalf("%d reads, want at most %d", h.reads, holdWaitTicks+2)
	}
}

// holdFake refuses movement while held, like the server ("You cannot jump,
// travel or dock until the inspection ends"). The hold lifts only after the
// worker has waited some game ticks on it (see liftHoldAfterTicks), so a
// caller that does not wait jumps into it.
type holdFake struct {
	*burnFake
	holdAt string // a hold lands right after the jump into this system
}

// liftHoldAfterTicks swaps the settle sleep so the ship's hold ends once the
// worker has spent n tick-long waits on it.
func liftHoldAfterTicks(t *testing.T, st *game.State, n int) {
	t.Helper()
	prev := sleepFunc
	sleepFunc = func(ctx context.Context, d time.Duration) error {
		if d == game.SleepTick && st.ActiveHold != nil {
			if n--; n <= 0 {
				st.ActiveHold = nil
			}
		}
		return nil
	}
	t.Cleanup(func() { sleepFunc = prev })
}

func (h *holdFake) Jump(ctx context.Context, sys string) (*game.JumpResult, error) {
	if h.state.ActiveHold != nil {
		h.calls = append(h.calls, "jump-held:"+sys)
		return nil, errors.New("You cannot jump, travel or dock until the inspection ends")
	}
	res, err := h.burnFake.Jump(ctx, sys)
	if err == nil && sys == h.holdAt {
		h.state.ActiveHold = &game.ActiveHold{HolderName: "[CUSTOMS] The Levy", Source: "customs", At: time.Now()}
	}
	return res, err
}

// A customs hold that lands mid-route must be waited out, not turned into a
// failed jump.
func TestAutopilotWaitsOutHoldBeforeJump(t *testing.T) {
	b, kb := routeFake(t, 100)
	liftHoldAfterTicks(t, b.state, 3)
	h := &holdFake{burnFake: b, holdAt: "s1"}
	if err := Autopilot(context.Background(), AutopilotDeps{Client: h, Out: io.Discard, KB: kb, AvoidSystemsPath: t.TempDir() + "/none.json"}, "s5", "s5_market"); err != nil {
		t.Fatalf("Autopilot: %v (calls %v)", err, b.calls)
	}
	if slices.ContainsFunc(b.calls, func(c string) bool { return strings.HasPrefix(c, "jump-held:") }) {
		t.Fatalf("calls %v: jumped into a hold instead of waiting it out", b.calls)
	}
	if !slices.Contains(b.calls, "jump:s5") {
		t.Errorf("calls %v, want the route completed", b.calls)
	}
}
