---
name: reference_capture_cadence_retune
description: capture_action_log hourly -> twice_daily retune — APPLIED 2026-09-08 at the cold start; why it can only be applied with the fleet stopped, and why it did not stick from 08-30
metadata:
  type: reference
---

`scripts/retune_action_log_capture.py --apply` rewrites every agent's
`capture_action_log` from `hourly` to `twice_daily`. Committed `f856ed0a`.

**APPLIED 2026-09-08** during the post-crash cold start: **128 agents changed,
33 already `twice_daily`**. Verified live in `mb-overmind.log`
(`⏰ [scheduled twice_daily] capture_action_log`).

**It did NOT stick from the 08-30 fleet roll.** The status board recorded the
retune as "applied at the stop" on 08-30, but the 09-08 pre-launch dry run still
found **128 of 161 on `hourly`**. So treat this as a **re-check at every fleet
stop**, not a one-time task: dry-run the script (no `--apply`) during preflight
and apply if the count is non-zero. A memory note saying a schedule change was
applied is not evidence that it is still applied — the dry run is.

**Why it cannot run live:** `Scheduler.checkDue` → `saveLocked()`
(`pkg/worker/schedule.go:356`) rewrites `schedule.json` from memory on every
fire, so an edit under a running worker reverts within minutes.

**Why the roles.yaml seed cannot do it:** seeding is covered-aware and
`Covers("hourly","twice_daily")` is TRUE (12h is a multiple of 1h), so the
coarser entry reads as already covered and `RetireCovered` would drop the NEW
one. **Cadence can only be moved finer, never coarser** — a real seeder
limitation. Making role-declared commands reconcile their frequency would make
this declarative.

Most of the benefit already shipped in `f856ed0a` without a restart:
`BoundaryPhaseFor` spreads the eight `capture_*` commands over
`min(period/2, 1h)` instead of the shared 5-minute window — measured on 161
agents × 4 hourly captures, worst second **16 → 4**, distinct seconds 127 → 550.
`update_market` is deliberately excluded; its burst is wanted.

See [[reference_login_rate_limits]] · [[project_pending_rollout_queue]] ·
[[reference_cold_start_runbook_drift]]
