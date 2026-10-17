package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A long foreground command holds the REPL's execMu for its whole duration, so
// the scheduler goroutine -- which blocks on that same mutex inside fire() --
// is frozen until the command returns. Confirmed live 2026-10-16: craftsman-1
// ran auto-explore from before 20:00 until a manual Ctrl+C at ~22:50, and its
// hourly get_skills fired at 19:00:15, then not again until 22:51:14 (the
// 20:00 task, executing 2h51m late the instant the lock freed), then again at
// 23:00:11. The 21:00 and 22:00 boundaries were never even evaluated: the
// scheduler's ticker has a one-slot buffer, so ticks arriving while fire()
// blocks are dropped outright.
//
// yieldExec is the fix: a release-and-retake at a safe point in the hop loop.
func TestYieldExec_HandsOffToABlockedWaiter(t *testing.T) {
	var mu sync.Mutex
	saved := globalExecYield
	t.Cleanup(func() { globalExecYield = saved })

	mu.Lock() // stand in for the REPL holding it across a long command
	globalExecYield = func() {
		mu.Unlock()
		mu.Lock()
	}

	var waiterRan atomic.Bool
	started := make(chan struct{})
	go func() {
		close(started)
		mu.Lock() // the scheduler, blocked inside fire()
		waiterRan.Store(true)
		mu.Unlock()
	}()
	<-started

	// Go's mutex enters starvation mode for a waiter blocked over 1ms and hands
	// the lock off directly, so no sleep is needed between unlock and lock --
	// but the waiter does need to have reached Lock() first.
	deadline := time.Now().Add(2 * time.Second)
	for !waiterRan.Load() && time.Now().Before(deadline) {
		yieldExec()
	}
	mu.Unlock()

	if !waiterRan.Load() {
		t.Error("blocked waiter never acquired the mutex across repeated yields")
	}
}

// Unlocking a mutex this goroutine does not hold panics, which would kill the
// process. autoExplore is reachable from paths that never took execMu (tests,
// and any future direct caller), so an unset yield must be a silent no-op
// rather than a crash.
func TestYieldExec_NoopWhenUnset(t *testing.T) {
	saved := globalExecYield
	t.Cleanup(func() { globalExecYield = saved })
	globalExecYield = nil

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("yieldExec panicked with no yield installed: %v", r)
		}
	}()
	yieldExec()
	yieldExec()
}
