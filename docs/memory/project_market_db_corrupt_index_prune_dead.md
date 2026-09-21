---
name: project_market_db_corrupt_index_prune_dead
description: "market.db index idx_orders_station_item is corrupt; market-prune has failed every run since 2026-09-15 22:21, so market_orders is unpruned and the file is 40GB"
metadata:
  type: project
---

**`idx_orders_station_item` is corrupt, and it has silently killed pruning
since 2026-09-15 22:21** (found 2026-09-21). 254 consecutive failures:

```
[market-prune] prune: database disk image is malformed (779)
```

**Diagnosis.** The error follows the ACCESS PATH, not the data:

- `SELECT station_id, MAX(captured_at) ... GROUP BY station_id` → malformed.
  Plan: `SCAN market_orders USING INDEX idx_orders_station_item`.
- `SELECT count(DISTINCT station_id) ... WHERE bucket_utc >= ?` → fine, 1.2 s.
  Plan: `SEARCH ... USING INDEX idx_orders_bucket`.

So the table rows are intact and one index is damaged. `PruneOrders` deletes
via `idx_orders_bucket` but a DELETE must maintain EVERY index on the removed
rows, so it hits the bad one and aborts the whole transaction — nothing is
ever deleted.

**Blast radius — this is the root cause of several things blamed elsewhere:**

| symptom | real cause |
| --- | --- |
| market.db at 40 GB | 6 days unpruned at 0.85-2.4M rows/hour |
| fleet credits plateaued ~09-15 | prune died 09-15 23:20 |
| haul claims 651/day → 111 from 09-16 | the day after |
| arbitrage scan 2 min → 22 min | `MAX(captured_at)` scans 6 days, not 4 hours |

The 22-minute scan was first blamed on the new depth-aware pricing. Depth
pricing does cost more, but the table is ~30x its intended size.

**Fix: `REINDEX idx_orders_station_item`** — rebuilds from the rows, no data
loss. NOT done: it takes an exclusive write lock on a 40 GB table, and both
prior fleet-wide outages (07-30, 08-18) were this same shape against live
writers. Do it with writers stopped, or prefer `cmd/tools/market-rebuild` at
cold start, which writes only the keep-set and rebuilds every index with no
VACUUM. See [[reference_live_kb_schema_drift]] for the backup rule (VACUUM
INTO, never shell `.backup`) — but note market.db is 40 GB, so a copy is not
cheap.

**Retention was set to `--retain 2h --interval 30m` on 09-21** (was 4h/30m).
It has no effect until the reindex: the prune still errors. Floor is ~2h
because `bucket_utc` is HOURLY and the cutoff drops whole buckets — at
`retain 1h`, 10:05 deletes the 09:00 bucket and leaves only 5 minutes of data.
`retain` must also stay well above `interval`, or an outage turns a routine
prune into a whole-table delete.

**Who writes market.db:** far more than the marketbots. `--market-db-path`
DEFAULTS to `data/market.db` in `cmd/worker/main.go:82`, so all ~179 workers
open it for write even though only the mb overmind passes the flag; plus all 9
overminds (`fleet_timeseries`, quarter-hourly), the arbitrage scanner, and
market-prune. That is the contention behind the chronic SQLITE_BUSY.
