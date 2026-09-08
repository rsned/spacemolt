package worker

import (
	"context"
	"time"
)

// sleepFunc is the one real, ctx-aware sleep every settle wait and every
// nil-fallback poll sleep in this package goes through. It is a variable so
// the test binary can swap in an instant stand-in (see TestMain): the suite
// otherwise spends real seconds on every tick-deferred reply, which is what
// pushed `go test -race ./pkg/worker` past its 300s pre-commit budget.
var sleepFunc = func(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// settle waits for a tick-deferred reply to land before state is read back.
// A cancelled ctx cuts the wait short; the next client call reports the
// cancellation, so the error is deliberately dropped here.
func settle(ctx context.Context, d time.Duration) {
	_ = sleepFunc(ctx, d)
}
