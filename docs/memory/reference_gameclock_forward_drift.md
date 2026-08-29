---
name: reference_gameclock_forward_drift
description: "GameClock free-runs and only syncs FORWARD, so it drifts ahead of the server tick — don't derive tight timeouts from arrival_tick minus gc.Tick()"
metadata: 
  node_type: memory
  type: reference
  originSessionId: b9d940f1-1595-4045-bcf5-ef30a3dc8faa
---

`pkg/game/clock.go` `GameClock` increments locally every 10s (wall-clock) and re-syncs via get_notifications only every 5 min — and the sync **only adopts the server tick when it is *ahead* of local** (`serverTick > localTick`), never rolls backward. Over WS, get_notifications is effectively a no-op (tick arrives via push frames), so the clock free-runs. Net effect: `gc.Tick()` can drift **ahead** of the true server tick and never self-correct.

**Bug this caused (fixed 2026-06-23, merge `37fff27`):** `client.Travel`/`Jump` sized their arrival-wait as `arrival_tick(server) − gc.Tick()`. With the clock drifted ~3 ticks ahead, a 4-tick journey's `ticksRemaining` hit the `<1→1` clamp → `1*SleepTick+30s = 40s` wait, shorter than the real travel → false "timeout waiting for state change" mid-journey (broke play_as loops / worker travel). The travel ACK carries `arrival_tick` (genuine absolute tick) but **no current-tick field**, so the clock was the only start reference.

**How to apply:**
- Never derive a *tight* timeout from `arrival_tick - gc.Tick()`. The wait is only a SAFETY bound — `waitForStateChange` returns the instant `!Traveling`. Use `arrivalWaitTimeout(arrivalTick, startTick, floor, maxWait)` in `pkg/game/client.go`, which floors at `9*SleepTick` (90s) and caps at `SleepTravelMaxWait`/`SleepJumpMaxWait`.
- Any new tick-delta consumer (ETA displays, the est-vs-actual drift report in play_as) inherits this drift — treat `gc.Tick()` as approximate, biased high.
- Relevant to [[project_overmind_fleet_manager]] #2 (autopilot extraction): mobile workers do heavy travel/jump, so the floored wait matters there too.
