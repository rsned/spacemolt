---
name: reference_worker_quiesce_park
description: "Park an agent at its next safe point: write data/agents/<id>/quiesce.json. Survives restarts, unlike drain. Also: admin remove force-kills after 4 min, shorter than a haul run."
metadata:
  node_type: memory
---

**To park an agent** (stops taking new role work at its next safe boundary):

```bash
echo '{"quiesce":true,"reason":"wildlife testing"}' > data/agents/<id>/quiesce.json
```

Delete the file to release it. Shows as `⏸ PARKED` on the dashboard.

**Why a file and not the control channel:** the existing drain is in-memory, so a fleet
cycle — routine here — puts a drained worker straight back to work. The park had to
survive a restart. **Fails open** by design: missing/malformed/unreadable reads as *not*
parked, so a typo in a hand-edited file can never silently park the fleet.

**Where the boundary is.** `RunStanding` checks its gates ONLY between idle passes, never
inside one — and **one idle pass is one run** for every role: `Haul()` claims one
opportunity and returns; the miner script is one jump→mine→deposit round trip. So a park
never truncates a run in flight. Same gate as `Paused`/`Draining` (pkg/worker/standing.go).

**Parking does NOT stop the scheduler** — a parked marketbot still runs its hourly
captures. `Scheduler.StartLoop` never consulted `Paused`/`Draining` either; this matches.

**⚠ THE GOTCHA THIS ALSO FIXED:** `DefaultRemoveDrainTimeout = 4 minutes`
(supervisor/reconcile.go). `admin remove` sends drain then **force-kills at the deadline**
— and 4 minutes is *shorter than a haul run*, so a removal can kill a worker holding
cargo. `readyToStop()` now completes a removal immediately when the worker reports
Drained **or** Quiesced. `Stalled()` also exempts Quiesced — a parked hauler can sit
UNDOCKED, and without the exemption the stall detector restarts exactly the workers you
just took out of service.

**⚠🔴 A PARK DOES NOT FREE THE GAME SESSION — it is the wrong tool for hand-flying.**
A quiesced worker stops *acting* but stays LOGGED IN and auto-reconnects, so every
`play_as` login kicks it and it takes the session straight back: `StatusCode(4001)
reason = "session_replaced"`, one round every ~15s, indefinitely. Killing the process
alone does not help either — the supervisor relaunches it in **~5 seconds** and it logs
back in. Measured 2026-09-02 on assist-haven, whose park held it idle at trade_winds
while it fought the operator for the session for 16 minutes.

**To actually take an agent away to fly by hand: remove it from the fleet roster.**

```bash
# comment out its line in data/overmind/<fleet>-fleet.yaml, then:
kill -HUP <that fleet's overmind pid>     # reloads roster, enqueues the membership change
```

Log confirms it: `SIGHUP: N membership change(s) enqueued` → `membership: removing "<id>"
— drain sent, force-stop after 4m0s` → `shutdown complete` → `membership: "<id>" removed
from fleet`. The 4-minute drain does NOT apply to a parked or idle worker — assist-haven
went from SIGHUP to gone in **3 seconds**. Reverse it by uncommenting + a second SIGHUP;
**also `rm` the quiesce.json**, or it rejoins and immediately sits idle.
[[reference_sigstop_preserves_game_sessions]] is the alternative for a SHORT window only
(≤60s, bounded by the 90s `SilenceTimeout`).

**Loop intervals (defaults, not flag-overridden in cmd/worker/main.go):**
idle pass = `SleepShort` = **3.33s**; scheduler tick = `SleepLong` = **20s**. So a
marketbot cycles ~3x per tick — the fastest loop in the fleet, and a park takes effect on
it within seconds. See [[reference_sleep_constants_actual]].
