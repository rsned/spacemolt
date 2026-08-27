package market

import (
	"testing"
	"time"
)

// The fleet runs ~153 workers holding market.db open read-write. Five attempts
// against a 5s busy_timeout gave up while the lock was still merely contended,
// which surfaced as a steady 4-6 "write failed after 5 attempts" per minute in
// mb-overmind.log and stopped market-prune from ever draining its backlog.
func TestRetryPolicy_SurvivesFleetContention(t *testing.T) {
	if maxRetryAttempts < 20 {
		t.Errorf("maxRetryAttempts = %d, want >= 20 to ride out fleet-wide contention", maxRetryAttempts)
	}
	if got := DefaultConfig().BusyTimeout; got < 15*time.Second {
		t.Errorf("DefaultConfig().BusyTimeout = %v, want >= 15s", got)
	}
}

// Exponential backoff must be capped: without a cap, raising the attempt count
// makes the final waits grow to minutes (50ms << 18 is over an hour).
func TestRetryDelay_IsCapped(t *testing.T) {
	for attempt := 1; attempt < maxRetryAttempts; attempt++ {
		if got := retryDelay(attempt); got > maxRetryDelay {
			t.Fatalf("retryDelay(%d) = %v, want <= %v", attempt, got, maxRetryDelay)
		}
	}
	if got := retryDelay(1); got != baseRetryDelay {
		t.Errorf("retryDelay(1) = %v, want %v", got, baseRetryDelay)
	}
	if got, want := retryDelay(3), 4*baseRetryDelay; got != want {
		t.Errorf("retryDelay(3) = %v, want %v", got, want)
	}
}
