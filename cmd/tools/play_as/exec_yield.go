package main

// globalExecYield releases and immediately retakes the REPL's command mutex.
//
// It is nil unless main() installs it, and yieldExec is then a no-op. That is
// deliberate: unlocking a mutex this goroutine does not hold PANICS, and
// autoExplore is reachable from paths that never took the mutex, so the
// uninstalled case has to be silent rather than fatal.
//
// WHY THIS EXISTS -- the REPL holds execMu around the whole of
// executeLogicalCommand (main.go), which is correct for a command that takes a
// tick but catastrophic for `auto-explore --max_hops 500`, which is a session
// rather than a command. The scheduler goroutine blocks on that same mutex
// inside Scheduler.fire, so while a long explore runs, NOTHING scheduled runs.
//
// Confirmed live 2026-10-16 on craftsman-1: hourly get_skills fired at
// 19:00:15, then not again until 22:51:14 -- the 20:00 task, executing 2h51m
// late the instant a manual Ctrl+C freed the lock -- then again at 23:00:11,
// nine minutes later. The 21:00 and 22:00 boundaries were never evaluated at
// all, because the ticker has a one-slot buffer and ticks arriving while
// fire() blocks are dropped. So a long explore both LOSES scheduled runs and
// then double-fires, and every capture task is frozen for the duration --
// exactly when an explore is generating the most data worth capturing.
//
// The worker fleet does not have this problem: pkg/worker/standing.go takes
// ExecMu per idle pass and releases it at the end of each one. This brings
// play_as to the same granularity.
var globalExecYield func()

// yieldExec gives any goroutine blocked on the command mutex a chance to run.
//
// A bare unlock/lock is enough and no sleep is needed: Go's sync.Mutex enters
// starvation mode for a waiter blocked longer than 1ms and hands the lock
// directly to it, and a scheduler goroutine that has been waiting minutes or
// hours is comfortably past that. Sleeping here would tax every hop for
// nothing.
//
// Call it only where nothing is mid-transaction. In autoExplore that is the
// top of the hop loop -- between systems, not between POIs of one system.
func yieldExec() {
	if globalExecYield == nil {
		return
	}
	globalExecYield()
}
