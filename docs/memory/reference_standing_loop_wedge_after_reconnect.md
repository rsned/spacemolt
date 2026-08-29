---
name: reference_standing_loop_wedge_after_reconnect
description: "FIXED 2026-08-22: 6 workers ran 3.5 days with a dead scheduler while reading healthy; SIGQUIT dump found the exact block, fix = bound every dispatched command"
metadata:
  type: reference
---

**Found 2026-08-22 via the asset-ledger freshness panel, not via any health check.**
Six workers across THREE fleets (mb: nova_terra / bellatrix / barnard_44, haul:
salvager-1 / salvager-3, craft: craftsman-8) had a **completely dead standing
loop** — scheduler AND idle script — for 3.5 days while the overmind reported
`healthy=True, restarts=0`.

## Signature

- Every entry in the agent's `data/agents/<id>/schedule.json` frozen at the SAME
  timestamp (nova_terra: all nine at `2026-08-18T22:00`). Not just captures —
  `update_market`, `kb_update`, `view_market`, `facilities` too, so **three
  marketbots contributed no market data for 3.5 days.**
- The worker log contains ONLY `heartbeat` lines after the freeze.
- The last non-heartbeat line for all six is `✓ Reconnected successfully`.
- Process alive the whole time, 0 restarts, `ps etime` 4 days, ~14 threads, S state.

**So the heartbeat and reconnect goroutines survive while the standing loop does
not.** Scheduler and idle both run under `ExecMu` (standing.go), so one blocked
call holding that mutex stops both and nothing else notices.

## Timeline (does NOT simply blame the reconnect)

Scheduler stopped 08-18 21:00–23:00 with **no logged cause**. Disconnects and
SUCCESSFUL reconnects continued after that, through a server-side event on
**08-19 14:55–15:48** (simultaneous `StatusGoingAway` close frames across three
fleets = a server restart). So the reconnects post-date the freeze and prove the
process still worked; they are not proven to be the trigger. **Do not write this
up as "the reconnect killed it" — that is not established.**

## Detection (nothing we run catches it)

fleet-watch alerts on log SILENCE — these were loud. The overmind stall watchdog
uses `SilenceTimeout` — heartbeats satisfied it. Same blind spot as
[[reference_livelock_invisible_to_health_checks]]. **The signal that DID work was
the asset-ledger freshness panel.** Cheap direct check:

```
sqlite3 data/assets.db "select a.agent_id, p.captured_at from agents a
  join agent_profile p using(player_id) order by p.captured_at limit 15;"
```
Anything hours-stale on an hourly capture while its worker is alive is this bug.

## Root-causing it costs one worker

`cmd/worker` exposes **no pprof**, so there is no way to dump goroutines from a
live worker. `SIGQUIT` prints a full stack dump to stderr (→ the fleet's overmind
log) AND kills the process — which the supervisor then restarts. That is the only
way to see where the standing loop is parked, and it costs exactly one restart of
an agent that was already doing nothing. **Do this before plain-restarting the
rest, or the evidence is gone.** Suspected but UNPROVEN: a game command awaiting
a reply that never arrives, with no timeout, holding `ExecMu` forever.

Related: [[project_overmind_stall_kill_connect_loop]] · [[reference_login_vs_reconnect_gating]]


## ⭐ ROOT CAUSE FOUND (SIGQUIT dump, 2026-08-22) AND FIXED

`kill -QUIT` on marketbot_nova_terra printed the stacks and killed it; the
supervisor restarted it, so the whole diagnosis cost one already-idle worker.

**goroutine 47 — the standing loop — blocked 4,964 minutes (82.7h):**
```
sync.Cond.Wait
  game.(*RequestHandle).Result   pkg/game/submit.go:135
  game.(*Client).await           pkg/game/submit.go:209
  game.(*Client).Refuel          pkg/game/client.go:1994
  worker.(*WorkerDispatch).Run   pkg/worker/dispatch.go:222
  worker.RunStanding             pkg/worker/standing.go:165
```
**goroutine 48 — the scheduler — blocked 4,955 minutes** on `sync.Mutex.Lock` at
`schedule.go:318`, queued behind it on ExecMu. One `refuel` whose reply never
arrived took every scheduled command down with it.

**The cancellation path was never the problem** — `submit.go:108,110` already
register `context.AfterFunc` hooks that Broadcast the cond on cancel. The defect
was that **nothing cancelled anything**: no Broadcast exists anywhere in
client.go, and **no dispatched worker command had any deadline**, so a reply lost
to a disconnect parked the caller literally forever.

**FIX (2026-08-22, deployed):** bound the command instead of rewiring the
client's reconnect/replay machinery (which deliberately re-sends staged mutations
under fresh request ids — failing handles there would break it).
- `game.SleepCommandMaxWait = 180 * SleepTick` (30min), operator-chosen. Must
  clear the longest legitimate single command: **autopilot flies an entire
  multi-jump route inside one dispatch** (~12min measured for 16 jumps), so 5min
  (`SleepJumpMaxWait`) would have aborted real work.
- `StandingDeps.dispatch` wraps the runner in `context.WithTimeout`. This one
  seam covers BOTH the idle loop and the scheduler, because the scheduler's `run`
  goes through `runLine` -> `dispatch`.
- `StandingDeps.CommandTimeout` is injectable so tests need not wait 30 minutes.
- Tests: `pkg/worker/command_timeout_test.go` — bounds a stuck command, does NOT
  abort a slow-but-healthy one, and still honours caller cancellation.

**Still true and unfixed:** `cmd/worker` has no pprof, so a live goroutine dump
still costs the worker. Worth adding before the next mystery.
