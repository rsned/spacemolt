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

**What remains is steady-state command volume from 170 concurrently active
workers.** The idle loop is one pass per 10s tick per worker, so the floor scales
linearly with the active count — and the count had grown: the runbook's proven
baseline was ~144, the 09-09 cold start brought up **170**, then 6 quarantined
agents were un-quarantined and 8 unlock graduates added. Nothing misbehaved;
the fleet is simply larger than the per-IP budget tolerates.

**Levers, cheapest first:** run fewer workers (the pools are the obvious cut) ·
raise `IdleInterval` above one tick for low-value roles (`standing.go:96`) ·
coarsen capture cadences ([[reference_capture_cadence_retune]]).

See [[reference_sigstop_preserves_game_sessions]] ·
[[reference_idle_loop_ran_3x_per_tick]] · [[reference_login_rate_limits]] ·
[[reference_cold_start_runbook_drift]]
