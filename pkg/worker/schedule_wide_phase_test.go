package worker

import (
	"fmt"
	"testing"
	"time"
)

// Bookkeeping captures do not care when in the period they run, only that they
// run. Confining them to the same 5-minute window as time-sensitive reads
// concentrates the fleet's steady-state traffic for no benefit: 161 agents
// capturing hourly inside 300s is ~0.54 firings/sec of pure baseline, on a host
// whose shared per-IP limiter we spent 2026-08-27/28 fighting. Spread them over
// the period instead.
func TestWidePhase_SpreadsCapturesBeyondTheTightWindow(t *testing.T) {
	var maxPhase time.Duration
	for i := range 161 {
		p := BoundaryPhaseFor(fmt.Sprintf("agent_%03d", i), "hourly", "capture_action_log")
		if p > maxPhase {
			maxPhase = p
		}
	}
	if maxPhase <= maxBoundaryPhase {
		t.Errorf("widest capture phase = %v, want > %v -- captures are still packed into the tight window",
			maxPhase, maxBoundaryPhase)
	}
}

// The tight window is deliberate for time-sensitive work: an agent's market
// reads should stay together on its own phase. Widening must not touch them.
func TestWidePhase_LeavesTimeSensitiveCommandsOnTheAgentPhase(t *testing.T) {
	for _, freq := range []string{"ten_minutely", "hourly", "daily"} {
		want := BoundaryPhase("marketbot_sol", freq)
		if got := BoundaryPhaseFor("marketbot_sol", freq, "update_market"); got != want {
			t.Errorf("%s update_market phase = %v, want the agent's per-frequency phase %v", freq, got, want)
		}
	}
}

// A phase at or past the period would reorder boundaries -- the invariant the
// original 5-minute cap was chosen to guarantee. It must survive widening.
func TestWidePhase_NeverReachesThePeriod(t *testing.T) {
	for _, freq := range []string{"ten_minutely", "quarter_hourly", "half_hourly", "hourly", "daily", "weekly"} {
		period := FrequencyPeriod(freq)
		if period == 0 {
			t.Fatalf("FrequencyPeriod(%q) = 0", freq)
		}
		for i := range 200 {
			p := BoundaryPhaseFor(fmt.Sprintf("agent_%03d", i), freq, "capture_action_log")
			if p >= period {
				t.Fatalf("%s phase %v >= period %v -- boundaries would reorder", freq, p, period)
			}
		}
	}
}

// Two captures on the SAME agent and frequency should not fire on the same
// instant either; that is the per-agent burst this is meant to break up.
func TestWidePhase_SeparatesDifferentCapturesOnOneAgent(t *testing.T) {
	a := BoundaryPhaseFor("miner-1", "hourly", "capture_action_log")
	b := BoundaryPhaseFor("miner-1", "hourly", "capture_profile")
	if a == b {
		t.Errorf("capture_action_log and capture_profile share phase %v on one agent", a)
	}
}

// Stable across restarts, like the original: a re-herding phase would undo the
// whole point.
func TestWidePhase_IsDeterministic(t *testing.T) {
	first := BoundaryPhaseFor("miner-1", "hourly", "capture_action_log")
	for range 5 {
		if got := BoundaryPhaseFor("miner-1", "hourly", "capture_action_log"); got != first {
			t.Fatalf("phase changed between calls: %v then %v", first, got)
		}
	}
}

func TestFrequencyPeriod_MatchesTheBoundaryGrid(t *testing.T) {
	base := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	for _, freq := range []string{"ten_minutely", "quarter_hourly", "half_hourly", "hourly", "daily", "weekly"} {
		want := NextBoundary(freq, base).Sub(CurrentBoundary(freq, base))
		if got := FrequencyPeriod(freq); got != want {
			t.Errorf("FrequencyPeriod(%q) = %v, want %v", freq, got, want)
		}
	}
	if got := FrequencyPeriod("nonsense"); got != 0 {
		t.Errorf("FrequencyPeriod(unknown) = %v, want 0", got)
	}
}
