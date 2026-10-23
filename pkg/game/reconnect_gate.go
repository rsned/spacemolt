package game

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DialKind says what a dial will cost the server's per-IP limiters.
type DialKind int

const (
	// DialToken is a dial authenticated with a Clerk token: it spends only the
	// WebSocket-upgrade budget.
	DialToken DialKind = iota
	// DialPassword is a dial authenticated by password: it also spends the
	// (smaller) password-login budget.
	DialPassword
)

// GateConfig sizes the rolling window the gate enforces. The defaults sit
// below the server's per-IP limits (100 upgrades/min, 30 password logins/min).
type GateConfig struct {
	Window         time.Duration
	DialBudget     int // dials of any kind per Window
	PasswordBudget int // password logins per Window; <= 0 means no separate cap
}

// DefaultGateConfig returns the production budgets. A positive integer in
// $SPACEMOLT_CONNECT_BUDGET overrides DialBudget; anything else is ignored.
func DefaultGateConfig() GateConfig {
	cfg := GateConfig{
		Window:         ConnectWindow,
		DialBudget:     ConnectDialBudget,
		PasswordBudget: ConnectPasswordBudget,
	}
	if n, err := strconv.Atoi(os.Getenv("SPACEMOLT_CONNECT_BUDGET")); err == nil && n > 0 {
		cfg.DialBudget = n
	}
	return cfg
}

// ReconnectGate is a host-wide coordinator that keeps every client sharing one
// IP inside the server's per-IP connection budgets. After a server restart all
// clients lose their socket together; without coordination they stampede the
// login endpoint and trip a per-IP rate-limit block that then *escalates* every
// time any client keeps probing it.
//
// The gate is backed by a single small file (shared by every process on the
// host, across fleets) holding:
//   - dials: timestamps of every dial in the rolling window, so the fleet stays
//     under DialBudget per Window and dials are spaced Window/DialBudget apart;
//   - password_logins: the subset of dials that were password logins, capped
//     separately at PasswordBudget per Window; and
//   - blocked_until: a rate-limit block one client discovered, which ALL clients
//     then honor so the block can expire instead of being re-triggered.
//
// Access is serialized with an advisory file lock (flock), so the read-modify-
// write is atomic across processes.
type ReconnectGate struct {
	path string
	cfg  GateConfig

	// Injectable for tests; default to wall clock and real sleeps.
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
	jitter func() time.Duration
}

// gateSleep sleeps for d unless ctx ends first.
func gateSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// localWait sleeps d through the gate's sleep seam without touching the gate
// file: a wait owed by this client alone (the per-player login_token limit).
// A nil gate sleeps on the wall clock.
func (g *ReconnectGate) localWait(ctx context.Context, d time.Duration) error {
	if g == nil {
		return gateSleep(ctx, d)
	}
	return g.sleep(ctx, d)
}

type gateState struct {
	Dials             []int64 `json:"dials"`
	PasswordLogins    []int64 `json:"password_logins"`
	BlockedUntilNanos int64   `json:"blocked_until_nanos"`
	BlockSource       string  `json:"block_source,omitempty"`
	LastAttemptNanos  int64   `json:"last_attempt_nanos,omitempty"` // legacy read: folded into Dials on load
}

// DefaultReconnectGatePath is the host-global gate file. All spacemolt clients
// on one host (and therefore one outbound IP) share it. Overridable via the
// SPACEMOLT_RECONNECT_GATE environment variable.
func DefaultReconnectGatePath() string {
	if p := os.Getenv("SPACEMOLT_RECONNECT_GATE"); p != "" {
		return p
	}
	return os.TempDir() + "/spacemolt-reconnect.gate"
}

// NewReconnectGate returns a gate writing to path and enforcing cfg.
func NewReconnectGate(path string, cfg GateConfig) *ReconnectGate {
	g := &ReconnectGate{
		path:  path,
		cfg:   cfg,
		now:   time.Now,
		sleep: gateSleep,
	}
	g.jitter = defaultJitter(g.minSpacing())
	return g
}

// dialBudget is the effective dial budget; a non-positive value counts as 1.
func (g *ReconnectGate) dialBudget() int { return max(g.cfg.DialBudget, 1) }

// minSpacing is the smallest gap between two dials: Window / DialBudget.
func (g *ReconnectGate) minSpacing() time.Duration {
	return g.cfg.Window / time.Duration(g.dialBudget())
}

// DefaultBlock is how long to hold the fleet off on a bare per-IP block that
// states no duration: ConnectBareBlock, deliberately longer than the dial
// window. It does not read the receiver, so a nil gate is safe.
func (*ReconnectGate) DefaultBlock() time.Duration {
	return ConnectBareBlock
}

// defaultJitter returns up to half the spacing of randomness so that clients
// released from a shared block do not re-synchronize on the spacing boundary.
// math/rand's top-level source is auto-seeded and safe for concurrent use.
func defaultJitter(spacing time.Duration) func() time.Duration {
	span := spacing / 2
	if span <= 0 {
		return func() time.Duration { return 0 }
	}
	return func() time.Duration { return time.Duration(rand.Int63n(int64(span))) } //nolint:gosec // jitter, not security
}

// Acquire blocks until it is this process's turn to dial: it waits out any
// fleet-wide block and the rolling budgets for kind, then claims the slot by
// recording the dial. Returns ctx.Err() if the context ends while waiting.
func (g *ReconnectGate) Acquire(ctx context.Context, kind DialKind) error {
	if g == nil {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		wait, err := g.tryClaim(kind)
		if err != nil {
			// A gate I/O error must not strand the client; proceed uncoordinated.
			return nil //nolint:nilerr // best-effort coordination
		}
		if wait <= 0 {
			return nil
		}
		if err := g.sleep(ctx, wait+g.jitter()); err != nil {
			return err
		}
	}
}

// prune drops entries that have aged out of the window: an entry is kept while
// now - entry < window.
func prune(entries []int64, now time.Time, window time.Duration) []int64 {
	cutoff := now.Add(-window).UnixNano()
	return slices.DeleteFunc(entries, func(e int64) bool { return e <= cutoff })
}

// tryClaim atomically reads the gate; if a slot is free for kind it records the
// dial and returns 0; otherwise it returns how long to wait before retrying.
func (g *ReconnectGate) tryClaim(kind DialKind) (time.Duration, error) {
	var wait time.Duration
	err := g.withLock(func(st *gateState) bool {
		now := g.now()
		window := g.cfg.Window
		if st.LastAttemptNanos != 0 { // legacy file: that attempt counts as one dial
			st.Dials = append(st.Dials, st.LastAttemptNanos)
			st.LastAttemptNanos = 0
		}
		st.Dials = prune(st.Dials, now, window)
		st.PasswordLogins = prune(st.PasswordLogins, now, window)

		readyAt := st.BlockedUntilNanos
		if len(st.Dials) >= g.dialBudget() {
			readyAt = max(readyAt, slices.Min(st.Dials)+window.Nanoseconds())
		}
		if kind == DialPassword && g.cfg.PasswordBudget > 0 && len(st.PasswordLogins) >= g.cfg.PasswordBudget {
			readyAt = max(readyAt, slices.Min(st.PasswordLogins)+window.Nanoseconds())
		}
		if len(st.Dials) > 0 {
			readyAt = max(readyAt, slices.Max(st.Dials)+g.minSpacing().Nanoseconds())
		}

		n := now.UnixNano()
		if n < readyAt {
			wait = time.Duration(readyAt - n)
			return true // persist the pruned / legacy-folded state
		}
		st.Dials = append(st.Dials, n)
		if kind == DialPassword {
			st.PasswordLogins = append(st.PasswordLogins, n)
		}
		return true
	})
	return wait, err
}

// RecordBlock publishes a fleet-wide rate-limit block lasting d from now, so
// every client honors it. A longer existing block is never shortened. source
// names who found the block; it is persisted and logged for incident review.
func (g *ReconnectGate) RecordBlock(d time.Duration, source string) error {
	if g == nil || d <= 0 {
		return nil
	}
	return g.withLock(func(st *gateState) bool {
		until := g.now().Add(d)
		if until.UnixNano() <= st.BlockedUntilNanos {
			return false
		}
		st.BlockedUntilNanos = until.UnixNano()
		st.BlockSource = source
		log.Printf("gate block source=%s for=%s until=%s", source, d, until.Format(time.RFC3339))
		return true
	})
}

// withLock opens the gate file, takes an exclusive advisory lock, reads the
// state, runs fn, and (if fn returns true) writes the mutated state back.
func (g *ReconnectGate) withLock(fn func(*gateState) bool) error {
	f, err := os.OpenFile(g.path, os.O_RDWR|os.O_CREATE, 0o644) //nolint:gosec // operator-controlled path
	if err != nil {
		return fmt.Errorf("reconnect gate: open: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("reconnect gate: flock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	var st gateState
	data, err := os.ReadFile(g.path) //nolint:gosec // operator-controlled path
	if err == nil && len(data) > 0 {
		if json.Unmarshal(data, &st) != nil {
			st = gateState{} // a corrupt gate resets to empty, not fatal
		}
	}
	if !fn(&st) {
		return nil
	}
	out, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("reconnect gate: marshal: %w", err)
	}
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("reconnect gate: truncate: %w", err)
	}
	if _, err := f.WriteAt(out, 0); err != nil {
		return fmt.Errorf("reconnect gate: write: %w", err)
	}
	return nil
}

var blockSecondsRe = regexp.MustCompile(`(?i)try again in (\d+)\s*second`)

// retryAfterRe matches the other two shapes the duration arrives in: the
// standard Retry-After header (rendered as "retry-after: N" by rateLimitDetail)
// and a JSON body field. Consulted only after blockSecondsRe, which is the
// server's own prose and the most specific.
var retryAfterRe = regexp.MustCompile(`(?i)retry[_-]after"?\s*[:=]\s*"?(\d+)`)

// rateLimitBlock extracts how long to back off from a reconnect error. It
// returns the server-stated "try again in N seconds" when present, else a
// default cooldown for a bare per-IP block (HTTP 429 / "temporarily blocked" /
// "rate limit"), else (0,false) for unrelated errors.
func rateLimitBlock(errText string, deflt time.Duration) (time.Duration, bool) {
	if m := blockSecondsRe.FindStringSubmatch(errText); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return time.Duration(n) * time.Second, true
		}
	}
	if m := retryAfterRe.FindStringSubmatch(errText); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return time.Duration(n) * time.Second, true
		}
	}
	lower := strings.ToLower(errText)
	for _, sub := range []string{"429", "temporarily blocked", "rate limit"} {
		if strings.Contains(lower, sub) {
			return deflt, true
		}
	}
	return 0, false
}
