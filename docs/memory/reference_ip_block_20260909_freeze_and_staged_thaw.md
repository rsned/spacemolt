---
name: reference_ip_block_20260909_freeze_and_staged_thaw
description: "2026-09-09 IP block at 170 active workers — the SIGSTOP freeze/staged-thaw runbook that recovered it, and the four causes ruled out by evidence"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2d7bbf54-7b88-4de5-ba93-9477eb1cb891
  modified: 2026-09-09T10:10:07.215Z
---

**2026-09-09, 01:59–02:30 local: IP blocked ~31 minutes.** Symptoms the operator
saw: `IP rate limited, waiting 3m32s before sending (type: view_storage)` — our
own client self-throttling — then `Your IP has been temporarily blocked due to
excessive rate limit violations. Try again in 905 seconds.`

## The freeze (this is the runbook)

**Stop the OVERMINDS FIRST, then the workers.** A supervisor that sees a stalled
worker relaunches it, and a relaunch is a LOGIN — the one thing you must not
spend during a block. Order matters more than speed.

```bash
# overminds first
for d in /proc/[0-9]*; do c=$(tr '\0' ' ' < $d/cmdline 2>/dev/null); \
  case "$c" in *bin/overmind\ *) kill -STOP $(basename $d);; esac; done
# then workers
for d in /proc/[0-9]*; do c=$(tr '\0' ' ' < $d/cmdline 2>/dev/null); \
  case "$c" in *bin/worker*--agent*) kill -STOP $(basename $d);; esac; done
```

Verify with `ps -eo stat,cmd | ... | awk '{print $1}' | sort | uniq -c` — `T`
is stopped. 169 workers + 9 overminds froze cleanly. **Leave
`overmind-dashboard` and `overmind-status` running**: they read status files and
never touch the game server.

**SIGCONT cost ZERO logins and the sessions survived** — assist-sol came back
still holding the 51,630 credits gifted before the freeze. This is the whole
reason to freeze rather than stop.

## Staged thaw

Thaw one fleet per 60s, counting fresh block lines between stages, and abort if
they climb. Earners first, the low-value pools last (or not at all):
**assist → mb → haul → craft → hunt → shuttle**, holding **unlock (15),
mission-learn (24), mining (27) = 66 workers (39% of the load)** frozen.
Script kept at `scratchpad/thaw.sh`.

## What it was NOT (each ruled out by evidence, not guessed)

| suspect | verdict |
|---|---|
| heartbeats | **free** — `cmd/worker/main.go` builds them from `client.GetState()`, the cached state; no server call |
| craft's dock/refuel spam (175+175 lines in 3 min from 9 workers) | **free** — `WorkerDispatch.redundant()` (`dispatch.go:222`) suppresses the call and logs "skipped". That guard exists FOR this problem |
| reconnect storm | **no** — 6 reconnect events in two hours |
| backfill bursts | **no** — minimal in the window |
| my hunt-fleet restart | **no** — the block began 01:59, the restart was 02:27, near the END |

**~~What remains is steady-state command volume from 170 workers~~ — WRONG,
corrected by the operator the same day.**

> "other players run 1000 on 1 IP with no blocks"

**So 170 workers is NOT inherently too many, and fleet size is not the cause.**
The elimination was sound but the conclusion did not follow: ruling out four
suspects left "volume" as the residue, and I reported that as the answer instead
of as the absence of one. With a 1000-agent reference point, we are doing
something *structurally* wasteful that other operators are not — a specific
command over-issued, not a headcount problem.

**This makes `idle_ticks` a mitigation, not a fix.** Halving the pool roles'
idle rate buys headroom; it does not find the waste. The instrumentation is what
finds it.

**Levers, cheapest first:** ~~run fewer workers~~ (see the correction above —
headcount is not the cause) ·
raise `IdleInterval` above one tick for low-value roles (`standing.go:96`) ·
coarsen capture cadences ([[reference_capture_cadence_retune]]).

See [[reference_sigstop_preserves_game_sessions]] ·
[[reference_idle_loop_ran_3x_per_tick]] · [[reference_login_rate_limits]] ·
[[reference_cold_start_runbook_drift]]

## The fix for the cause: `idle_ticks` (2026-09-09, commit `094edfe0`, UNDEPLOYED)

`Role.IdleTicks` (yaml `idle_ticks`) multiplies a role's idle-loop period in
GAME TICKS. Set to **2 on the three POOL roles — `unlock`, `missionrunner`,
`miner`** (66 of the 170 workers). Earning roles keep the one-tick loop.

- **Ticks, not a duration**, because the loop's unit is the tick: the game
  advances once per tick and a mutation is capped at one per tick per agent, so a
  sub-tick period only emits redundant calls. A duration would desync if
  `SleepTick` changed.
- **Declarative** — it lives in `data/overmind/roles.yaml`, so future cadence
  changes are a config edit, not a code change. `LoadRoles` REJECTS a negative
  value (it would spin the loop with no delay).
- **Scope, stated honestly:** this halves the IDLE-PASS rate for those three
  roles only. Scheduled captures run on their own cadence and are untouched, and
  commands-per-pass are unchanged. It is a cut to one component of fleet traffic,
  **not a 50% cut overall.**

**Deploying it costs 66 staggered logins** (~11 min at `--stagger 10s`, ~6/min,
under the ~10/min limit) because the three pool fleets are SIGSTOPped and must be
restarted onto a new binary — SIGCONT alone resumes them on the OLD cadence.
Deliberately not done on the night of the block: a login burst is what escalates
the next one ([[reference_rate_limit_buckets_and_escalation]]).
