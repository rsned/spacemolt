package game

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ReconnectGate is a host-wide coordinator that prevents every client sharing
// one IP from reconnecting at the same instant. After a server restart all
// clients lose their socket together; without coordination they stampede the
// login endpoint and trip a per-IP rate-limit block that then *escalates* every
// time any client keeps probing it.
//
// The gate is backed by a single small file (shared by every process on the
// host, across fleets) holding two timestamps:
//   - last_attempt: when any client last dialed, so dials are spaced >= cooldown
//     apart fleet-wide; and
//   - blocked_until: a rate-limit block one client discovered, which ALL clients
//     then honor so the block can expire instead of being re-triggered.
//
// Access is serialized with an advisory file lock (flock), so the read-modify-
// write of the timestamps is atomic across processes.
type ReconnectGate struct {
	path     string
	cooldown time.Duration

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

type gateState struct {
	LastAttemptNanos  int64 `json:"last_attempt_nanos"`
	BlockedUntilNanos int64 `json:"blocked_until_nanos"`
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

// NewReconnectGate returns a gate writing to path with the given minimum spacing
// between fleet-wide reconnect dials. A non-positive cooldown disables spacing
// (blocked_until is still honored).
func NewReconnectGate(path string, cooldown time.Duration) *ReconnectGate {
	return &ReconnectGate{
		path:     path,
		cooldown: cooldown,
		now:      time.Now,
		sleep:    gateSleep,
		jitter:   defaultJitter(cooldown),
	}
}

// defaultJitter returns up to half the cooldown of randomness so that clients
// released from a shared block do not re-synchronize on the cooldown boundary.
// math/rand's top-level source is auto-seeded and safe for concurrent use.
func defaultJitter(cooldown time.Duration) func() time.Duration {
	span := cooldown / 2
	if span <= 0 {
		return func() time.Duration { return 0 }
	}
	return func() time.Duration { return time.Duration(rand.Int63n(int64(span))) } //nolint:gosec // jitter, not security
}

// Acquire blocks until it is this process's turn to attempt a reconnect:
// it waits out any fleet-wide block, then waits until at least cooldown has
// passed since the last dial by any client, and finally claims the slot by
// stamping last_attempt. Returns ctx.Err() if the context ends while waiting.
func (g *ReconnectGate) Acquire(ctx context.Context) error {
	if g == nil {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		wait, err := g.tryClaim()
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

// tryClaim atomically reads the gate; if the slot is free (past any block and
// past last_attempt+cooldown) it stamps last_attempt and returns 0; otherwise it
// returns how long to wait before retrying.
func (g *ReconnectGate) tryClaim() (time.Duration, error) {
	var wait time.Duration
	err := g.withLock(func(st *gateState) bool {
		now := g.now()
		readyAt := time.Unix(0, st.LastAttemptNanos).Add(g.cooldown)
		if bu := time.Unix(0, st.BlockedUntilNanos); st.BlockedUntilNanos > 0 && bu.After(readyAt) {
			readyAt = bu
		}
		if !now.Before(readyAt) {
			st.LastAttemptNanos = now.UnixNano()
			return true // mutated -> persist
		}
		wait = readyAt.Sub(now)
		return false
	})
	return wait, err
}

// RecordBlock publishes a fleet-wide rate-limit block lasting d from now, so
// every client honors it. A longer existing block is never shortened.
func (g *ReconnectGate) RecordBlock(d time.Duration) error {
	if g == nil || d <= 0 {
		return nil
	}
	return g.withLock(func(st *gateState) bool {
		until := g.now().Add(d).UnixNano()
		if until > st.BlockedUntilNanos {
			st.BlockedUntilNanos = until
			return true
		}
		return false
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
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("reconnect gate: flock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	var st gateState
	data, err := os.ReadFile(g.path) //nolint:gosec // operator-controlled path
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &st) // a corrupt gate resets to zero, not fatal
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
