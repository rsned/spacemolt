---
name: reference_long_foreground_command_starves_the_scheduler
description: "play_as holds execMu for a whole command, so auto-explore freezes the scheduler for hours: boundaries are LOST (dropped ticks), then double-fire. Fixed with a per-hop yield."
metadata:
  type: reference
---

**A long foreground command in play_as freezes ALL scheduled work.** The REPL
takes `execMu` around the whole of `executeLogicalCommand` (`main.go:~762`),
which is right for a command that costs a tick and catastrophic for
`auto-explore --max_hops 500` — that is a session, not a command. The scheduler
goroutine blocks on the same mutex inside `Scheduler.fire`, so nothing
scheduled runs for the duration.

**Confirmed live 2026-10-16, craftsman-1** (times local, UTC+7 to the state
file):

```
19:00:15  hourly get_skills fired normally
20:00:11  ticker -> due -> LastRun STAMPED 20:00:11 -> blocks on execMu
          21:00 and 22:00 boundaries NEVER EVALUATED
22:51:14  operator Ctrl+C freed the lock -> the 20:00 task ran, 2h51m late
23:00:11  boundary 23:00 > LastRun 20:00:11 -> due -> fired, 9 min later
```

Two of four hourly runs were **lost outright**, one ran nearly three hours
late, then one ran nine minutes after that. Operator diagnosed it: *"is it
because it was tied up in an uninterruptable auto-explore loop?"* — yes, and
*"2250 is when i interrupted the auto-explore because of fuel"* is the
independent confirmation: the task executed when the LOCK freed, not at any
natural yield point.

Three stacked defects:
1. **The scheduler is one goroutine and `fire` blocks inside it.** A
   `time.Ticker` has a ONE-SLOT buffer, so ticks arriving while blocked are
   dropped — missed boundaries are not queued, they are gone.
2. **`checkDue` stamps `LastRun = now` BEFORE the run**, so the scheduler
   believes the last run happened at the decision time. That is what makes the
   next boundary look legitimately due minutes after a late execution. Still
   unfixed; latent.
3. **`execMu` held across an entire multi-hour command.** The root cause.

**Fix (3): `yieldExec()` at the top of autoExplore's hop loop** —
`cmd/tools/play_as/exec_yield.go`, installed from `main` as
`execMu.Unlock(); execMu.Lock()`. A bare unlock/lock suffices with no sleep:
Go's mutex enters starvation mode for a waiter blocked >1ms and hands off
directly. `globalExecYield` is nil unless `main` installs it, because
**unlocking a mutex you do not hold PANICS** and autoExplore is reachable from
paths that never took it.

Yield only where nothing is mid-transaction: **between systems**, not between
the POIs of one system. Residual gap: a POI-rich system whose tour exceeds the
60s `SleepLong` tick can still drop one tick. Per-POI yielding would close it
but needs `exploreSystem`'s capture bookkeeping checked first.

**The worker fleet is NOT affected.** `pkg/worker/standing.go:261-288` takes
`ExecMu` per idle pass and releases it at the end of each one, so the scheduler
gets a window every pass. The hop-loop yield brings play_as to that same
granularity.

The hop loop (`auto_explore.go:124`) is the established checkpoint — it already
breaks there on `st.Died`, `unscannedHalt`, `isInBattle` and now fuel-strand.
Related: [[reference_schedule_seeding_is_additive_only]] ·
[[reference_capture_loss_taxonomy]] (every capture task is frozen for the
duration, exactly when an explore generates the most data)
