package worker

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/market"
)

// A drain gate that fires between PASSES is not a stand-down: a haul spans many
// passes (claim, buy, jump, jump, ..., sell), so draining a hauler mid-run stops
// it holding cargo against a live claim, and the supervisor's readyToStop reads
// that as a clean exit. Rolling the haul fleet onto a new binary therefore meant
// choosing between aborting in-flight hauls and not rolling at all — which is
// why the fleet has been running an old worker binary since 2026-07-26.
//
// SafeToStandDown lets the ROLE define the boundary. While it reports false the
// worker keeps taking passes so it can finish what it started, and crucially
// does NOT publish Drained, so nothing stops it mid-unit.
func TestDrainingWaitsForTheRoleSafePoint(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var draining, drained, safe atomic.Bool
	draining.Store(true) // drain requested immediately
	safe.Store(false)    // ... but the role is mid-unit

	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false },
		Draining: draining.Load, SetDrained: drained.Store,
		SafeToStandDown: func() bool { return safe.Load() },
		Out:             io.Discard,
		NowFn:           func() time.Time { return time.Unix(0, 0).UTC() },
		IdleInterval:    time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, Role{Idle: "noop_idle"}, deps) }()

	time.Sleep(30 * time.Millisecond)
	if len(r.snapshot()) == 0 {
		t.Fatal("worker stopped working mid-unit; it must keep running to reach its safe point")
	}
	if drained.Load() {
		t.Fatal("published Drained mid-unit — the supervisor would stop it holding cargo")
	}

	// The unit completes: now the worker must hold and report drained.
	safe.Store(true)
	time.Sleep(30 * time.Millisecond)
	if !drained.Load() {
		t.Fatal("reached the safe point but never published Drained; the roll would hang")
	}
	before := len(r.snapshot())
	time.Sleep(30 * time.Millisecond)
	if after := len(r.snapshot()); after != before {
		t.Fatalf("kept working after standing down (%d -> %d commands)", before, after)
	}
}

// A role with no notion of in-flight work (nil hook) keeps today's behavior:
// drain holds immediately.
func TestDrainingWithoutASafePointHoldsImmediately(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var draining, drained atomic.Bool
	draining.Store(true)
	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false },
		Draining: draining.Load, SetDrained: drained.Store,
		Out:          io.Discard,
		NowFn:        func() time.Time { return time.Unix(0, 0).UTC() },
		IdleInterval: time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, Role{Idle: "noop_idle"}, deps) }()
	time.Sleep(30 * time.Millisecond)

	if n := len(r.snapshot()); n != 0 {
		t.Fatalf("nil SafeToStandDown must hold immediately, ran %d commands", n)
	}
	if !drained.Load() {
		t.Fatal("expected Drained published immediately with no safe-point hook")
	}
}

// An operator PARK is the same contract: park at the next safe point, not
// mid-unit. Quiesced must not be published while the role reports unsafe.
func TestParkWaitsForTheRoleSafePoint(t *testing.T) {
	r := &recordRunner{}
	var mu sync.Mutex
	var safe atomic.Bool
	var quiescedPub atomic.Bool
	safe.Store(false)

	sched, _ := LoadScheduler(t.TempDir() + "/sched.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := StandingDeps{
		Runner: r, Scheduler: sched, Client: stateClient{st: &game.State{}},
		ExecMu: &mu, Paused: func() bool { return false },
		Quiesced:        func() (bool, string) { return true, "operator park" },
		SetQuiesced:     func(q bool, _ string) { quiescedPub.Store(q) },
		SafeToStandDown: func() bool { return safe.Load() },
		Out:             io.Discard,
		NowFn:           func() time.Time { return time.Unix(0, 0).UTC() },
		IdleInterval:    time.Millisecond, AgentID: "test",
	}
	go func() { _ = RunStanding(ctx, Role{Idle: "noop_idle"}, deps) }()

	time.Sleep(30 * time.Millisecond)
	if quiescedPub.Load() {
		t.Fatal("published Quiesced mid-unit; a park must not truncate a run in flight")
	}
	safe.Store(true)
	time.Sleep(30 * time.Millisecond)
	if !quiescedPub.Load() {
		t.Fatal("reached the safe point but never published Quiesced")
	}
}

// HaulAtSafePoint defines the haul role's boundary: a hauler is stoppable only
// when it holds no claim. Mid-claim it may be carrying goods it has already
// paid for, and stopping there strands them (salvager-10, 784 steel_plate,
// 2026-09-19).
func TestHaulAtSafePoint(t *testing.T) {
	for name, tc := range map[string]struct {
		store OpportunityStore
		want  bool
	}{
		"no claim held":    {&fakeStore{}, true},
		"claim held":       {&fakeStore{claimedByAgent: []market.ArbitrageOpportunity{opp(1, "b", "c", 100)}}, false},
		"claim unreadable": {&fakeStore{claimedErr: errors.New("db locked")}, false},
		"no store at all":  {nil, true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := HaulAtSafePoint(context.Background(), tc.store, "hauler-0"); got != tc.want {
				t.Fatalf("HaulAtSafePoint = %v, want %v", got, tc.want)
			}
		})
	}
}
