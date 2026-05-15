package game

import "context"

// inflight is a counting semaphore implemented over a buffered channel.
// Used by Submit to cap the number of outstanding mutations. Acquire
// honors context cancellation and never consumes a slot on failure.
type inflight struct {
	slots chan struct{}
}

func newInflight(capacity int) *inflight {
	return &inflight{slots: make(chan struct{}, capacity)}
}

// Acquire takes a slot. Returns ctx.Err() if ctx fires first, in which
// case no slot is consumed.
func (s *inflight) Acquire(ctx context.Context) error {
	// Fast path: if context is already cancelled, fail immediately.
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case s.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a slot. Caller MUST balance every successful Acquire
// with exactly one Release.
func (s *inflight) Release() {
	<-s.slots
}

// InFlight returns the current number of held slots (advisory; may be
// stale by the time the caller reads it).
func (s *inflight) InFlight() int { return len(s.slots) }

// Cap returns the configured capacity.
func (s *inflight) Cap() int { return cap(s.slots) }
