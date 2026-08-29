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

**Loop intervals (defaults, not flag-overridden in cmd/worker/main.go):**
idle pass = `SleepShort` = **3.33s**; scheduler tick = `SleepLong` = **20s**. So a
marketbot cycles ~3x per tick — the fastest loop in the fleet, and a park takes effect on
it within seconds. See [[reference_sleep_constants_actual]].
