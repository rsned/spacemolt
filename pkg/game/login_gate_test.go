package game

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// A fresh worker start dialed and authenticated outside the host-wide gate:
// InitializeAgent attached the gate to the ReconnectingHandler (reconnects
// only) and then called Connect/Login ungated. Live 2026-08-26: restarting 16
// miners at ~12 logins/min tripped the per-IP auth budget and produced 429s
// and login timeouts across the mining, haul and unlock fleets.
func TestDialWithGate_AcquiresBeforeConnecting(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), 5*time.Second, clk)

	var order []string
	connect := func(context.Context) error { order = append(order, "connect"); return nil }
	login := func(context.Context) error { order = append(order, "login"); return nil }

	// First dial claims the free slot without waiting.
	if err := dialWithGate(context.Background(), g, connect, login); err != nil {
		t.Fatalf("first dial: %v", err)
	}
	if len(clk.slept) != 0 {
		t.Fatalf("first dial should not wait, slept=%v", clk.slept)
	}

	// The second dial must wait out the cooldown -- that is the whole point:
	// two fresh logins on one host are spaced, not simultaneous.
	if err := dialWithGate(context.Background(), g, connect, login); err != nil {
		t.Fatalf("second dial: %v", err)
	}
	if len(clk.slept) == 0 {
		t.Error("second dial did not wait for the gate; fresh logins are ungated")
	}
	want := []string{"connect", "login", "connect", "login"}
	if len(order) != len(want) {
		t.Fatalf("call order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("call order = %v, want %v", order, want)
		}
	}
}

// A 429 discovered by one starting worker must reach the whole host, or the
// next N workers each rediscover it and deepen the block.
func TestDialWithGate_PublishesBlockFoundAtLogin(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, 5*time.Second, clk)

	connect := func(context.Context) error { return nil }
	login := func(context.Context) error {
		return errors.New("login failed: 429 too many requests, try again in 90 seconds")
	}
	if err := dialWithGate(context.Background(), g, connect, login); err == nil {
		t.Fatal("dialWithGate should surface the login error")
	}

	// A second client on the same host must now honor the 90s block, which is
	// far longer than the 5s cooldown it would otherwise wait.
	clk2 := &fakeClock{t: clk.t}
	g2 := newTestGate(t, path, 5*time.Second, clk2)
	if err := g2.Acquire(context.Background()); err != nil {
		t.Fatalf("second client Acquire: %v", err)
	}
	var total time.Duration
	for _, d := range clk2.slept {
		total += d
	}
	if total < 60*time.Second {
		t.Errorf("second client waited %v; want >= 60s from the published block", total)
	}
}

// A connect failure must not be followed by a login attempt.
func TestDialWithGate_StopsWhenConnectFails(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), 5*time.Second, clk)

	connect := func(context.Context) error { return errors.New("dial tcp: connection refused") }
	loginCalls := 0
	login := func(context.Context) error { loginCalls++; return nil }

	if err := dialWithGate(context.Background(), g, connect, login); err == nil {
		t.Fatal("expected the connect error to surface")
	}
	if loginCalls != 0 {
		t.Errorf("login called %d times after a failed connect; want 0", loginCalls)
	}
}

// Direct/test construction leaves the gate nil; that must stay a no-op rather
// than panicking or blocking.
func TestDialWithGate_NilGateStillDials(t *testing.T) {
	connected, loggedIn := false, false
	err := dialWithGate(context.Background(),
		nil,
		func(context.Context) error { connected = true; return nil },
		func(context.Context) error { loggedIn = true; return nil },
	)
	if err != nil {
		t.Fatalf("nil gate: %v", err)
	}
	if !connected || !loggedIn {
		t.Errorf("nil gate skipped work: connected=%v loggedIn=%v", connected, loggedIn)
	}
}

// A cancelled context must abort before dialing, not after.
func TestDialWithGate_HonorsCancelledContext(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), 5*time.Second, clk)
	// Claim the slot so the next Acquire has to wait.
	if err := g.Acquire(context.Background()); err != nil {
		t.Fatalf("priming Acquire: %v", err)
	}
	g.sleep = func(ctx context.Context, _ time.Duration) error { return context.Canceled }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	connectCalls := 0
	err := dialWithGate(ctx, g, func(context.Context) error { connectCalls++; return nil },
		func(context.Context) error { return nil })
	if err == nil {
		t.Fatal("expected a context error")
	}
	if connectCalls != 0 {
		t.Errorf("connect called %d times despite a cancelled context; want 0", connectCalls)
	}
}

// recordRateLimitBlock must ignore ordinary errors: a refused dial or a bad
// password is not a per-IP block and must not stall the whole host.
func TestRecordRateLimitBlock_IgnoresNonRateLimitErrors(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, 5*time.Second, clk)

	recordRateLimitBlock(g, errors.New("login failed: invalid credentials"))

	clk2 := &fakeClock{t: clk.t}
	g2 := newTestGate(t, path, 5*time.Second, clk2)
	if err := g2.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	var total time.Duration
	for _, d := range clk2.slept {
		total += d
	}
	if total >= 60*time.Second {
		t.Errorf("a bad-credentials error published a %v block; want none", total)
	}
}
