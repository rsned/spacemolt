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

// restartState hands out a State whose announced restart clears after
// pendingFor reads, the way a reconnect clears it live.
type restartState struct {
	st         *game.State
	pendingFor int
	reads      int
}

func (r *restartState) GetState() *game.State {
	r.reads++
	if r.reads > r.pendingFor {
		r.st.ServerRestart = nil
	}
	return r.st
}

func announcedRestart() *game.ServerRestart {
	return &game.ServerRestart{At: time.Now().Add(time.Minute), Target: "0.733.0"}
}

// instantSleep makes every settle wait return at once.
func instantSleep(t *testing.T) {
	t.Helper()
	prev := sleepFunc
	sleepFunc = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { sleepFunc = prev })
}

func TestWaitOutRestartWaitsUntilCleared(t *testing.T) {
	instantSleep(t)
	r := &restartState{pendingFor: 3, st: &game.State{ServerRestart: announcedRestart()}}
	var out bytes.Buffer
	waitOutRestart(context.Background(), &out, r)
	if r.reads <= 3 {
		t.Fatalf("returned after %d reads, want it to wait until the restart cleared", r.reads)
	}
	for _, want := range []string{"server restart", "0.733.0", "resum"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q missing %q", out.String(), want)
		}
	}
}

func TestWaitOutRestartNoopWhenNone(t *testing.T) {
	var out bytes.Buffer
	waitOutRestart(context.Background(), &out, &restartState{st: &game.State{}})
	if out.Len() != 0 {
		t.Errorf("printed %q with no restart announced", out.String())
	}
}

// A restart that is announced and never happens must not hold the worker
// forever, even when the test sleep takes no time.
func TestWaitOutRestartIsBounded(t *testing.T) {
	instantSleep(t)
	r := &restartState{pendingFor: 1 << 30, st: &game.State{ServerRestart: announcedRestart()}}
	waitOutRestart(context.Background(), io.Discard, r)
	if limit := restartWaitTicks + 2; r.reads > limit {
		t.Fatalf("%d reads, want at most %d", r.reads, limit)
	}
}

// restartFake breaks the jump into breakAt the way the 11-22 restart did: the
// jump hangs and times out, and the restart warning is on State.
type restartFake struct {
	*burnFake
	breakAt string
	breaks  int // how many jumps into breakAt fail; -1 = every one
}

func (r *restartFake) Jump(ctx context.Context, sys string) (*game.JumpResult, error) {
	if sys == r.breakAt && r.breaks != 0 {
		r.breaks--
		r.calls = append(r.calls, "jump-broken:"+sys)
		at := time.Now()
		r.state.LastRestartAt = at
		return nil, errors.New("jump to " + sys + ": timeout waiting for state change after 5m0s")
	}
	return r.burnFake.Jump(ctx, sys)
}

// Live 2026-11-22: explorer-3 and craftsman-2 started jumps into the v0.733.0
// restart, timed out and dropped their routes. A route broken by a restart
// must say so and resume from where the ship is.
func TestAutopilotResumesRouteAfterRestartInterruption(t *testing.T) {
	instantSleep(t)
	b, kb := routeFake(t, 100)
	f := &restartFake{burnFake: b, breakAt: "s2", breaks: 1}
	var out bytes.Buffer
	err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: &out, KB: kb, AvoidSystemsPath: t.TempDir() + "/none.json"}, "s5", "")
	if err != nil {
		t.Fatalf("Autopilot: %v (calls %v)", err, b.calls)
	}
	if !slices.Contains(b.calls, "jump:s5") {
		t.Errorf("calls %v, want the route completed after the resume", b.calls)
	}
	if !strings.Contains(out.String(), "interrupted by server restart") {
		t.Errorf("output %q does not say the route was interrupted by the restart", out.String())
	}
}

// A jump failure with no restart anywhere near it is returned exactly as
// before: no resume, no second attempt.
func TestAutopilotJumpFailureWithoutRestartIsReturned(t *testing.T) {
	instantSleep(t)
	b, kb := routeFake(t, 100)
	f := &noRestartBreak{burnFake: b}
	err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: io.Discard, KB: kb, AvoidSystemsPath: t.TempDir() + "/none.json"}, "s5", "")
	if err == nil {
		t.Fatal("Autopilot succeeded, want the jump failure returned")
	}
	if n := countPrefix(b.calls, "jump-broken:"); n != 1 {
		t.Errorf("%d broken jumps, want exactly 1 (no resume without a restart); calls %v", n, b.calls)
	}
}

// A route that keeps failing during a restart resumes once, then gives up.
func TestAutopilotResumesAtMostOnce(t *testing.T) {
	instantSleep(t)
	b, kb := routeFake(t, 100)
	f := &restartFake{burnFake: b, breakAt: "s2", breaks: -1}
	err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: io.Discard, KB: kb, AvoidSystemsPath: t.TempDir() + "/none.json"}, "s5", "")
	if err == nil {
		t.Fatal("Autopilot succeeded, want the second failure returned")
	}
	if n := countPrefix(b.calls, "jump-broken:"); n != 2 {
		t.Errorf("%d broken jumps, want 2 (the first and one resume); calls %v", n, b.calls)
	}
}

type noRestartBreak struct{ *burnFake }

func (n *noRestartBreak) Jump(ctx context.Context, sys string) (*game.JumpResult, error) {
	if sys == "s2" {
		n.calls = append(n.calls, "jump-broken:"+sys)
		return nil, errors.New("jump to s2: timeout waiting for state change after 5m0s")
	}
	return n.burnFake.Jump(ctx, sys)
}

func countPrefix(calls []string, prefix string) int {
	n := 0
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}
