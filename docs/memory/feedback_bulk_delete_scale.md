---
name: feedback_bulk_delete_scale
description: NEVER bulk-DELETE most of a huge SQLite table — 185M-row unbatched DELETE ran 6.5h without committing; copy-out-and-swap or DROP TABLE is the move; check whether the retention window is even non-empty first
metadata: 
  node_type: memory
  type: feedback
  originSessionId: 8c9098dc-7c7c-4768-bb10-b65a2ce84115
  modified: 2026-07-21T17:16:59.730Z
---

2026-07-21 market.db cleanup cost 8+ hours; the avoidable parts:

- **PruneOrders is a single unbatched DELETE** (pkg/market/prune.go) — fine at 30-min cadence, catastrophic on a 5.5-day backlog: 185M rows / 62GB ran 6.5h without committing (WAL kept absorbing index-maintenance churn; 151GB written) before I killed it.
- **Rule: deleting >~30% of a big table ⇒ copy-KEEP-rows to a new table, DROP old, rename, recreate indexes, VACUUM.** Deleting ~everything ⇒ just DROP + recreate.
- **Check first whether anything in the retention window even exists**: fleets had been down >4h, so the 4h window held ZERO rows — the whole table was garbage and could have been dropped in minutes at hour zero.
- Sequencing: don't start a CREATE INDEX over a table you're about to prune (~2h wasted); and pkg/market's collector open path runs migrations, so market-prune/scanner both trigger the index build on open — use raw sqlite3 CLI for surgery to avoid the migration hook.
- Killing a sqlite writer leaves a giant WAL; the NEXT opener pays recovery (~64GB IO for a 54GB WAL). Count that into any "kill and retry" decision.

**Why:** the user watched an 8-hour maintenance window that should have been ~30 min. **How to apply:** before any bulk data operation, estimate rows touched × index count, prefer swap/drop over delete at scale, and verify what's actually worth preserving before preserving it. [[reference_getreferenceask_perf]] [[reference_market_db_prune]]
