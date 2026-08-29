---
name: reference_capture_cadence_retune_pending
description: capture_action_log retune to twice_daily is written but UNAPPLIED — needs a fleet stop; live workers revert schedule.json edits
metadata:
  type: project
---

`scripts/retune_action_log_capture.py --apply` converts all **161** agents'
`capture_action_log` from `hourly` to `twice_daily`. Committed `f856ed0a`,
**NOT YET APPLIED**. Run it during the next fleet stop, before relaunch.

**Why it cannot run live:** `Scheduler.checkDue` → `saveLocked()`
(`pkg/worker/schedule.go:356`) rewrites `schedule.json` from memory on every
fire, so an edit under a running worker reverts within minutes.

**Why the roles.yaml seed cannot do it:** seeding is covered-aware and
`Covers("hourly","twice_daily")` is TRUE (12h is a multiple of 1h), so the
coarser entry reads as already covered and `RetireCovered` would drop the NEW
one. **Cadence can only be moved finer, never coarser** — a real seeder
limitation, not just an inconvenience here. Making role-declared commands
reconcile their frequency would make this declarative.

Most of the benefit already shipped in `f856ed0a` without a restart:
`BoundaryPhaseFor` spreads the eight `capture_*` commands over
`min(period/2, 1h)` instead of the shared 5-minute window — measured on 161
agents × 4 hourly captures, worst second **16 → 4**, distinct seconds 127 → 550.
`update_market` is deliberately excluded; its burst is wanted.

See [[reference_login_rate_limits]] · [[project_pending_rollout_queue]]
