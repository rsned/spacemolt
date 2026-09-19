package supervisor

import (
	"testing"
	"time"
)

// A fixed force-stop deadline defeats a stand-down-at-the-safe-point roll: the
// worker is deliberately still working (finishing its haul) when the timer
// expires, so it is killed mid-unit — exactly the abort the safe point exists
// to prevent. The code's own comment at the deadline concedes that 4 minutes is
// "shorter than a haul run".
//
// RemoveDrainTimeout == 0 therefore means "wait indefinitely": stop the worker
// only when it reports drained. A roll using it is bounded by the fleet's
// longest in-flight unit rather than by a clock.
func TestRemoveDrainTimeoutZeroMeansNoForceStop(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	if got := drainDeadline(now, 0); !got.IsZero() {
		t.Fatalf("drainDeadline(now, 0) = %v, want the zero time (no force-stop)", got)
	}
	if got, want := drainDeadline(now, 4*time.Minute), now.Add(4*time.Minute); !got.Equal(want) {
		t.Fatalf("drainDeadline(now, 4m) = %v, want %v", got, want)
	}
	// Only an explicit zero disables the clock; a negative stays "already due",
	// which existing callers use to exercise the force-stop path.
	if got := drainDeadline(now, -time.Second); !forceStopDue(now, got) {
		t.Fatal("a negative timeout must remain an expired deadline, not a disabled one")
	}
}

// The deadline check must treat the zero time as "never expires" rather than
// "already expired" — the latter would force-stop instantly, the exact
// opposite of what a disabled timeout means.
func TestForceStopDueRespectsADisabledDeadline(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	if forceStopDue(now, time.Time{}) {
		t.Fatal("a zero deadline must never come due; it would kill every drain instantly")
	}
	if forceStopDue(now, now.Add(time.Minute)) {
		t.Fatal("deadline in the future must not be due")
	}
	if !forceStopDue(now, now.Add(-time.Second)) {
		t.Fatal("deadline in the past must be due")
	}
}
