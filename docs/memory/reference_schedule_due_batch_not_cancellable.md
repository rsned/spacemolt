---
name: reference-schedule-due-batch-not-cancellable
description: "Removing a task from schedule.json does NOT cancel it once checkDue has put it in the current due batch; batches run sequentially and can take ~1h, so one-off buys fire twice"
metadata:
  type: reference
---

Measured 2026-11-12 on trader-2. `checkDue` (pkg/worker/schedule.go) stamps every due task's LastRun and hands the whole batch to the standing runner, which works through it one task at a time. A batch with travel (buy_directed, deliver) took 14:22 → past 15:15 local. Deleting a task from schedule.json mid-batch only affects the NEXT batch: id 36 (`buy_directed dark_matter_cell 30 …`) was removed ~14:40 but still ran at 15:15 → 30 extra cells bought.

- schedule-state.json shows the batch: every id stamped with the same timestamp was in it.
- New tasks added to the file ARE due at once on the next checkDue (reloadIfChangedLocked gives unknown ids no LastRun) — the earlier "seeded to now" belief applies only to `Scheduler.Add` (in-game schedule add), not hand edits.

**How to apply:** for a one-off buy, remove the task BEFORE the batch that contains it starts (check schedule-state.json timestamps), or make the command itself idempotent (deliver/craft_stock-style "top up to N") instead of a fixed-quantity buy.
