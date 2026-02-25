package main

import (
	"context"
	"sync"
	"time"
)

// AdaptiveRateLimiter tracks rate limit responses and adjusts delay accordingly.
type AdaptiveRateLimiter struct {
	mu          sync.Mutex
	baseDelay   time.Duration
	currentWait time.Duration
	maxWait     time.Duration
	hits        int64
}

// NewAdaptiveRateLimiter creates a rate limiter with the given base delay.
func NewAdaptiveRateLimiter(baseDelay time.Duration) *AdaptiveRateLimiter {
	return &AdaptiveRateLimiter{
		baseDelay:   baseDelay,
		currentWait: 0,
		maxWait:     30 * time.Second,
	}
}

// RecordHit records a rate limit hit and increases the backoff.
func (r *AdaptiveRateLimiter) RecordHit() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits++
	if r.currentWait == 0 {
		r.currentWait = r.baseDelay
	} else {
		r.currentWait *= 2
		if r.currentWait > r.maxWait {
			r.currentWait = r.maxWait
		}
	}
}

// RecordSuccess records a successful command and reduces backoff.
func (r *AdaptiveRateLimiter) RecordSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentWait = r.currentWait / 2
	if r.currentWait < r.baseDelay {
		r.currentWait = 0
	}
}

// Wait blocks until the rate limit delay has elapsed or ctx is cancelled.
func (r *AdaptiveRateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()
	wait := r.currentWait
	r.mu.Unlock()

	if wait == 0 {
		return nil
	}

	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Hits returns the total number of rate limit hits recorded.
func (r *AdaptiveRateLimiter) Hits() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits
}
