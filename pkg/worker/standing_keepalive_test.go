package worker

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// manualClock is an injectable NowFn whose value only moves when the test says
// so, which is what lets these tests assert a 15-minute cadence in milliseconds.
type manualClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *manualClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// countingRunner records commands like recordRunner and additionally reports a
// server-call count, so RunStanding can tell a pass that hit the wire from one
// that did nothing.
type countingRunner struct {
	recordRunner
	calls atomic.Uint64
}

func (r *countingRunner) Run(ctx context.Context, tokens []string) error {
	r.calls.Add(1)
	return r.recordRunner.Run(ctx, tokens)
}

func (r *countingRunner) ServerCalls() uint64 { return r.calls.Load() }

func countLines(lines []string, cmd string) int {
	n := 0
	for _, l := range lines {
		if strings.EqualFold(strings.TrimSpace(l), cmd) {
			n++
		}
	}
	return n
}

// An agent with nothing to do must not poll the server every tick. It sends one
// keepalive so the server knows it is alive, then stays silent until the
// keepalive interval elapses -- regardless of how many idle passes run in
// between.
func TestIdleWorkerKeepalivesOncePerInterval(t *testing.T) {
	r := &countingRunner{}
	var mu sync.Mutex
	clock := &manualClock{t: time.Unix(1_700_000_000, 0).UTC()}

	deps := StandingDeps{
		Runner:            r,
		Client:            stateClient{st: &game.State{}},
		ExecMu:            &mu,
		Out:               io.Discard,
		AgentID:           "idle-1",
		IdleInterval:      time.Millisecond,
		ScheduleInterval:  time.Hour,
		KeepaliveInterval: 15 * time.Minute,
		NowFn:             clock.now,
	}
	// No idle script on disk: this is the "truly idle" worker.
	role := Role{Idle: ""}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = RunStanding(ctx, role, deps) }()

	time.Sleep(40 * time.Millisecond) // dozens of idle passes
	if got := countLines(r.snapshot(), "get_status"); got != 1 {
		t.Fatalf("after dozens of passes inside one keepalive window, get_status ran %d times, want exactly 1; recorded=%v",
			got, r.snapshot())
	}

	clock.advance(16 * time.Minute)
	time.Sleep(40 * time.Millisecond)
	if got := countLines(r.snapshot(), "get_status"); got != 2 {
		t.Fatalf("after the keepalive interval elapsed, get_status ran %d times, want 2; recorded=%v",
			got, r.snapshot())
	}
}

// A worker that did real work in the pass already proved it is alive. It must
// not append a keepalive on top of that work.
func TestKeepaliveSuppressedWhenThePassDidWork(t *testing.T) {
	r := &countingRunner{}
	var mu sync.Mutex
	clock := &manualClock{t: time.Unix(1_700_000_000, 0).UTC()}

	t.Chdir(writeIdleScript(t, "busy_idle", "get_ship\n"))
	deps := StandingDeps{
		Runner:            r,
		Client:            stateClient{st: &game.State{}},
		ExecMu:            &mu,
		Out:               io.Discard,
		AgentID:           "busy-1",
		IdleInterval:      time.Millisecond,
		ScheduleInterval:  time.Hour,
		KeepaliveInterval: time.Millisecond, // due on every single pass
		NowFn:             clock.now,
	}
	role := Role{Idle: "busy_idle"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = RunStanding(ctx, role, deps) }()
	time.Sleep(40 * time.Millisecond)

	lines := r.snapshot()
	if countLines(lines, "get_ship") == 0 {
		t.Fatalf("idle script never ran; recorded=%v", lines)
	}
	if got := countLines(lines, "get_status"); got != 0 {
		t.Fatalf("keepalive ran %d times alongside real work, want 0 -- a pass that hit the wire is already proof of life; recorded=%v",
			got, lines)
	}
}

// The default cadence must be the 15-20 minute liveness window, not a tick.
func TestDefaultKeepaliveInterval_IsAtLeastFifteenMinutes(t *testing.T) {
	deps := StandingDeps{}
	applyStandingDefaults(&deps)
	if deps.KeepaliveInterval < 15*time.Minute {
		t.Errorf("default KeepaliveInterval = %v, want >= 15m; a truly idle agent only has to prove liveness, not poll",
			deps.KeepaliveInterval)
	}
	if deps.KeepaliveInterval != game.SleepKeepalive {
		t.Errorf("default KeepaliveInterval = %v, want game.SleepKeepalive (%v)", deps.KeepaliveInterval, game.SleepKeepalive)
	}
}
