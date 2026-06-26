package game

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// fakeClock drives the gate's now/sleep deterministically: sleeping advances
// the clock instead of blocking.
type fakeClock struct {
	t     time.Time
	slept []time.Duration
}

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) sleep(_ context.Context, d time.Duration) error {
	c.slept = append(c.slept, d)
	c.t = c.t.Add(d)
	return nil
}

func newTestGate(t *testing.T, path string, cooldown time.Duration, clk *fakeClock) *ReconnectGate {
	t.Helper()
	g := NewReconnectGate(path, cooldown)
	g.now = clk.now
	g.sleep = clk.sleep
	g.jitter = func() time.Duration { return 0 }
	return g
}

func TestGateFirstAcquireIsImmediate(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), 5*time.Second, clk)
	if err := g.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if len(clk.slept) != 0 {
		t.Fatalf("first acquire should not wait, slept=%v", clk.slept)
	}
}

func TestGateSpacesConsecutiveAcquires(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), 5*time.Second, clk)
	if err := g.Acquire(context.Background()); err != nil { // stamps last_attempt
		t.Fatal(err)
	}
	if err := g.Acquire(context.Background()); err != nil { // must wait the cooldown
		t.Fatal(err)
	}
	if len(clk.slept) != 1 || clk.slept[0] != 5*time.Second {
		t.Fatalf("second acquire should wait one 5s cooldown, slept=%v", clk.slept)
	}
}

func TestGateHonorsBlockFleetWide(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	// One client records a 100s block; another client must wait it out.
	blocker := newTestGate(t, path, 5*time.Second, clk)
	if err := blocker.RecordBlock(100 * time.Second); err != nil {
		t.Fatalf("RecordBlock: %v", err)
	}
	other := newTestGate(t, path, 5*time.Second, clk)
	if err := other.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	total := time.Duration(0)
	for _, d := range clk.slept {
		total += d
	}
	if total < 100*time.Second {
		t.Fatalf("other client should wait out the 100s block, waited %v", total)
	}
}

func TestGateRecordBlockNeverShortens(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, 5*time.Second, clk)
	if err := g.RecordBlock(100 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := g.RecordBlock(10 * time.Second); err != nil { // shorter — ignored
		t.Fatal(err)
	}
	g2 := newTestGate(t, path, 5*time.Second, clk)
	_ = g2.Acquire(context.Background())
	total := time.Duration(0)
	for _, d := range clk.slept {
		total += d
	}
	if total < 100*time.Second {
		t.Fatalf("a shorter RecordBlock must not shorten the block, waited %v", total)
	}
}

func TestGateAcquireRespectsCanceledContext(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), 5*time.Second, clk)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.Acquire(ctx); err == nil {
		t.Fatal("Acquire on a cancelled context should return its error")
	}
}

func TestGateNilIsNoop(t *testing.T) {
	var g *ReconnectGate
	if err := g.Acquire(context.Background()); err != nil {
		t.Fatalf("nil gate Acquire should no-op, got %v", err)
	}
	if err := g.RecordBlock(time.Minute); err != nil {
		t.Fatalf("nil gate RecordBlock should no-op, got %v", err)
	}
}

func TestRateLimitBlockParsing(t *testing.T) {
	deflt := 60 * time.Second
	cases := []struct {
		in     string
		wantD  time.Duration
		wantOK bool
	}{
		{"Your IP has been temporarily blocked due to excessive rate limit violations. Try again in 952 seconds.", 952 * time.Second, true},
		{"try again in 80 second(s)", 80 * time.Second, true},
		{"failed to WebSocket dial: expected handshake response status code 101 but got 429", deflt, true},
		{"temporarily blocked", deflt, true},
		{"find_route failed: not connected", 0, false},
		{"some unrelated error", 0, false},
	}
	for _, c := range cases {
		gotD, gotOK := rateLimitBlock(c.in, deflt)
		if gotOK != c.wantOK || (gotOK && gotD != c.wantD) {
			t.Errorf("rateLimitBlock(%q) = (%v,%v), want (%v,%v)", c.in, gotD, gotOK, c.wantD, c.wantOK)
		}
	}
}
