package game

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

func newTestGate(t *testing.T, path string, cfg GateConfig, clk *fakeClock) *ReconnectGate {
	t.Helper()
	g := NewReconnectGate(path, cfg)
	g.now = clk.now
	g.sleep = clk.sleep
	g.jitter = func() time.Duration { return 0 }
	return g
}

// testGateCfg keeps the numbers small: spacing is 60s/6 = 10s.
var testGateCfg = GateConfig{Window: 60 * time.Second, DialBudget: 6, PasswordBudget: 2}

func totalSlept(clk *fakeClock) time.Duration {
	var total time.Duration
	for _, d := range clk.slept {
		total += d
	}
	return total
}

func readGateFile(t *testing.T, path string) gateState {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read gate file: %v", err)
	}
	var st gateState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("gate file is not valid JSON: %v (%q)", err, data)
	}
	return st
}

func TestGate_FirstAcquireIsImmediate(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), testGateCfg, clk)
	if err := g.Acquire(context.Background(), DialToken); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if len(clk.slept) != 0 {
		t.Fatalf("first acquire should not wait, slept=%v", clk.slept)
	}
}

func TestGate_MinSpacingIsWindowOverBudget(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), testGateCfg, clk)
	for range 2 {
		if err := g.Acquire(context.Background(), DialToken); err != nil {
			t.Fatal(err)
		}
	}
	if len(clk.slept) != 1 || clk.slept[0] != 10*time.Second {
		t.Fatalf("second acquire should wait one 10s spacing, slept=%v", clk.slept)
	}
}

func TestGate_DialBudgetExhaustedWaitsForOldest(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), testGateCfg, clk)
	first := clk.t
	for i := range 6 {
		if err := g.Acquire(context.Background(), DialToken); err != nil {
			t.Fatal(err)
		}
		if i < 5 {
			clk.t = clk.t.Add(10 * time.Second) // satisfy spacing without sleeping
		}
	}
	// Dials at +0..+50s; the clock sits at +50s. A 7th must wait for first+60s.
	if len(clk.slept) != 0 {
		t.Fatalf("spaced acquires should not sleep, slept=%v", clk.slept)
	}
	if err := g.Acquire(context.Background(), DialToken); err != nil {
		t.Fatal(err)
	}
	if want := first.Add(60 * time.Second); !clk.t.Equal(want) {
		t.Fatalf("7th acquire should resume at first+60s (%v), clock=%v slept=%v", want, clk.t, clk.slept)
	}
}

func TestGate_PasswordBudgetIsSeparate(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), testGateCfg, clk)
	first := clk.t
	ctx := context.Background()
	if err := g.Acquire(ctx, DialPassword); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(10 * time.Second)
	if err := g.Acquire(ctx, DialPassword); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(10 * time.Second)
	// A token dial at +20s does not consult the password budget.
	if err := g.Acquire(ctx, DialToken); err != nil {
		t.Fatal(err)
	}
	if len(clk.slept) != 0 {
		t.Fatalf("token dial must not wait on the password budget, slept=%v", clk.slept)
	}
	clk.t = clk.t.Add(10 * time.Second) // +30s, spacing satisfied
	// A third password login must wait until first+60s.
	if err := g.Acquire(ctx, DialPassword); err != nil {
		t.Fatal(err)
	}
	if want := first.Add(60 * time.Second); !clk.t.Equal(want) {
		t.Fatalf("3rd password login should resume at first+60s (%v), clock=%v slept=%v", want, clk.t, clk.slept)
	}
}

func TestGate_PasswordAcquireCountsAsDialToo(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)
	for range 2 {
		if err := g.Acquire(context.Background(), DialPassword); err != nil {
			t.Fatal(err)
		}
	}
	st := readGateFile(t, path)
	if len(st.Dials) != 2 || len(st.PasswordLogins) != 2 {
		t.Fatalf("want 2 dials and 2 password logins, got %d and %d", len(st.Dials), len(st.PasswordLogins))
	}
}

func TestGate_HonorsBlockFleetWide(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	// One client records a 100s block; another client must wait it out.
	blocker := newTestGate(t, path, testGateCfg, clk)
	if err := blocker.RecordBlock(100*time.Second, "test"); err != nil {
		t.Fatalf("RecordBlock: %v", err)
	}
	other := newTestGate(t, path, testGateCfg, clk)
	if err := other.Acquire(context.Background(), DialToken); err != nil {
		t.Fatal(err)
	}
	if total := totalSlept(clk); total < 100*time.Second {
		t.Fatalf("other client should wait out the 100s block, waited %v", total)
	}
}

func TestGate_RecordBlockNeverShortens(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)
	if err := g.RecordBlock(100*time.Second, "first"); err != nil {
		t.Fatal(err)
	}
	if err := g.RecordBlock(10*time.Second, "second"); err != nil { // shorter: ignored
		t.Fatal(err)
	}
	if src := readGateFile(t, path).BlockSource; src != "first" {
		t.Fatalf("block_source = %q, want %q (a shorter block must not replace it)", src, "first")
	}
	g2 := newTestGate(t, path, testGateCfg, clk)
	_ = g2.Acquire(context.Background(), DialToken)
	if total := totalSlept(clk); total < 100*time.Second {
		t.Fatalf("a shorter RecordBlock must not shorten the block, waited %v", total)
	}
}

func TestGate_ReadsLegacyFormat(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	legacy := fmt.Sprintf(`{"last_attempt_nanos":%d,"blocked_until_nanos":%d}`,
		clk.t.UnixNano(), clk.t.Add(30*time.Second).UnixNano())
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	g := newTestGate(t, path, testGateCfg, clk)
	if err := g.Acquire(context.Background(), DialToken); err != nil {
		t.Fatal(err)
	}
	if total := totalSlept(clk); total != 30*time.Second {
		t.Fatalf("should wait out the 30s legacy block, waited %v (slept=%v)", total, clk.slept)
	}
	st := readGateFile(t, path)
	// Legacy attempt folded into Dials (one dial) plus this claim.
	if len(st.Dials) != 2 || st.LastAttemptNanos != 0 {
		t.Fatalf("want 2 dials and zeroed legacy field, got %+v", st)
	}
}

func TestGate_LegacyAttemptAppliesSpacing(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	legacy := fmt.Sprintf(`{"last_attempt_nanos":%d,"blocked_until_nanos":0}`, clk.t.UnixNano())
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	g := newTestGate(t, path, testGateCfg, clk)
	if err := g.Acquire(context.Background(), DialToken); err != nil {
		t.Fatal(err)
	}
	if len(clk.slept) != 1 || clk.slept[0] != 10*time.Second {
		t.Fatalf("legacy last_attempt should count as one dial, slept=%v", clk.slept)
	}
}

func TestGate_CorruptFileResetsToEmpty(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	if err := os.WriteFile(path, []byte(`{"dials":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	g := newTestGate(t, path, testGateCfg, clk)
	if err := g.Acquire(context.Background(), DialToken); err != nil {
		t.Fatal(err)
	}
	if len(clk.slept) != 0 {
		t.Fatalf("a corrupt gate must behave as empty, slept=%v", clk.slept)
	}
	if st := readGateFile(t, path); len(st.Dials) != 1 {
		t.Fatalf("want 1 dial after reset, got %+v", st)
	}
}

func TestGate_PrunesOldEntries(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)
	for range 3 {
		if err := g.Acquire(context.Background(), DialToken); err != nil {
			t.Fatal(err)
		}
	}
	clk.t = clk.t.Add(2 * time.Minute)
	if err := g.Acquire(context.Background(), DialToken); err != nil {
		t.Fatal(err)
	}
	if st := readGateFile(t, path); len(st.Dials) > 1 {
		t.Fatalf("old entries should be pruned, got %+v", st)
	}
}

func TestDefaultGateConfig_EnvOverridesDialBudget(t *testing.T) {
	t.Setenv("SPACEMOLT_CONNECT_BUDGET", "30")
	if got := DefaultGateConfig().DialBudget; got != 30 {
		t.Fatalf("DialBudget = %d, want 30", got)
	}
	for _, bad := range []string{"abc", "0", "-5", ""} {
		t.Setenv("SPACEMOLT_CONNECT_BUDGET", bad)
		cfg := DefaultGateConfig()
		if cfg.DialBudget != ConnectDialBudget || cfg.PasswordBudget != ConnectPasswordBudget || cfg.Window != ConnectWindow {
			t.Fatalf("%q should leave defaults, got %+v", bad, cfg)
		}
	}
}

func TestGate_DefaultBlock(t *testing.T) {
	var nilGate *ReconnectGate
	if got := nilGate.DefaultBlock(); got != ConnectBareBlock {
		t.Fatalf("nil DefaultBlock = %v, want %v", got, ConnectBareBlock)
	}
	g := NewReconnectGate(filepath.Join(t.TempDir(), "g"), testGateCfg)
	if got := g.DefaultBlock(); got != ConnectBareBlock {
		t.Fatalf("DefaultBlock = %v, want %v", got, ConnectBareBlock)
	}
}

func TestGate_AcquireRespectsCanceledContext(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), testGateCfg, clk)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.Acquire(ctx, DialToken); err == nil {
		t.Fatal("Acquire on a cancelled context should return its error")
	}
}

func TestGate_NilIsNoop(t *testing.T) {
	var g *ReconnectGate
	if err := g.Acquire(context.Background(), DialToken); err != nil {
		t.Fatalf("nil gate Acquire should no-op, got %v", err)
	}
	if err := g.RecordBlock(time.Minute, "test"); err != nil {
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
		{"HTTP 429 Too Many Requests", deflt, true},
		{"clerk: POST /api/player/x/ws-token -> 429", deflt, true},
		{"temporarily blocked", deflt, true},
		{"find_route failed: not connected", 0, false},
		{"some unrelated error", 0, false},
		// 2026-10-22 22:07: a local DNS outage was recorded as a 5-minute
		// fleet-wide block because the resolver error carried an ephemeral
		// source port containing "429". Digits glued to a ':' or another digit
		// are never an HTTP status.
		{`dial tcp: lookup game.spacemolt.com on 127.0.0.53:53: read udp 127.0.0.1:42901->127.0.0.53:53: i/o timeout`, 0, false},
		{"connection reset by peer (conn 584291)", 0, false},
		{"request_id 1429.7 timed out", 0, false},
	}
	for _, c := range cases {
		gotD, gotOK := rateLimitBlock(c.in, deflt)
		if gotOK != c.wantOK || (gotOK && gotD != c.wantD) {
			t.Errorf("rateLimitBlock(%q) = (%v,%v), want (%v,%v)", c.in, gotD, gotOK, c.wantD, c.wantOK)
		}
	}
}
