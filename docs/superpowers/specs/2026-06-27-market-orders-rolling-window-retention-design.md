# market_orders Rolling-Window Retention — Design

**Date:** 2026-06-27
**Status:** Approved design (decisions locked); implementation plan to follow.

## Goal

Stop the unbounded append-only growth of `market_orders` (35.6M rows / 13 GB
under 30 agents capturing every 15 min) by retaining only a **rolling window of
the last N captures per station**. Each capture prunes its own station, retiring
the external prune tool + its trigger. `market_ohlcv` — the real price history —
is untouched.

## Problem / Context

`Collector.WriteSnapshot` (`pkg/market/collector.go:330`) upserts the station,
items, and hourly OHLCV, but **plain-INSERTs every order** (`insertOrders`,
`collector.go:192` — the doc comment itself says "raw order rows always
accumulate"). With ~30 agents capturing full station order books every 15 min,
that append path produces ~3M rows/hour and a 13 GB file. The 12h
`PruneOrders` (`pkg/market/prune.go:17`, `DELETE FROM market_orders WHERE
bucket_utc < ?`) keeps the span bounded to ~12h but still leaves 35.6M live rows
+ 3 indexes — and freed pages are only reused, never shrunk (no VACUUM).

A read-path audit shows **no consumer uses that multi-bucket history**:

| Reader (file) | Anchor |
|---|---|
| `GetLatestOrders` (`query.go:68`) | `ORDER BY captured_at DESC LIMIT n` |
| `GetLatestSnapshot` (`query.go:101`) | `MAX(captured_at)` per station |
| `HasSnapshotToday` (`query.go:150`) | any row `captured_at >= today` |
| `FindBestPrices` (`query.go:165`) | `MAX(captured_at) GROUP BY station` |
| `GetMatrix` (`query.go:357`) | latest bucket → `MAX(captured_at)` per station×item |
| `GetStationOrders` (`query.go:422`) | latest bucket → `MAX(captured_at)` |
| `GetItemStationPrices` (`arbitrage.go:16`) | `MAX(captured_at) GROUP BY station` |
| `GetCaptureHealth` (`query.go:520`) | latest bucket only |

Every reader wants the **latest capture per station**. The append-only history
serves no read path; `market_ohlcv` (dedup'd hourly via `ON CONFLICT`,
`collector.go:302`, never pruned) already carries all price history, which
`GetItemPriceHistory` (`query.go:489`) reads. So a short per-station window
satisfies all readers with a safety margin.

> Note on the assumed "`ops.db` split": it does not exist — no file, no Go/config
> reference, no branch. All market data is one `data/market.db`. The root cause of
> the space/contention pain is the append-only `market_orders` table, which this
> design fixes directly; no second database is warranted.

## Design Decisions (locked)

1. **Retention = rolling window of the last N captures per station**, N = 3 by
   default (~45 min at 15-min cadence; ~2.3M-row steady state vs 35.6M today).
   Chosen over pure-latest for robustness: a partial/bad capture no longer
   destroys the prior good one, and the window gives "what just changed"
   visibility.
2. **Self-pruning on capture.** Each `WriteSnapshot` trims its station to its N
   most recent captures, inside the same transaction as the insert. No external
   scheduler is required.
3. **No core schema change.** A "capture" is already identified by
   `(station_id, captured_at)`; the window reuses it. One index is added (below).
4. **`market_ohlcv` untouched** — remains the unbounded price-history series.
5. **No VACUUM at deploy.** Ship self-pruning; as captures cycle, each station
   drops from ≤12h-history to ≤N captures. Freed pages are reused, so the 13 GB
   file stops growing (stays ~13 GB, mostly free space) until a future
   maintenance VACUUM. Zero downtime.
6. **Retire the scheduled `market-prune` trigger.** The `cmd/tools/market-prune`
   CLI stays in-tree as a manual backstop only (e.g. a one-off 48h sweep); it is
   no longer invoked on a schedule.
7. **No reader changes** — all readers already anchor on latest-per-station and
   work unchanged against rolling-window data.

## Architecture

Capture remains a single call to `WriteSnapshot` per station (a `MarketSnapshot`
carries one `StationID`; all four callers — `cmd/auto-explorer/main.go:416`,
`pkg/worker/capture.go:560`, `pkg/agent/market_capture.go:70`,
`pkg/market/capture.go:105` — funnel through it). The only change is what
happens after the order insert, inside the existing `writeRetry` transaction.

```
agent capture ──► WriteSnapshot(station S)  [collector.go:330]
                     │  tx (writeRetry, WAL-serialized):
                     │    upsert station / items
                     │    INSERT orders for S               (existing)
                     │    upsert hourly OHLCV               (existing, untouched)
                     │    prune S to last N captures        (NEW)
                     ▼
              market_orders: ≤ N captures per station (self-managed)
```

Because WAL allows only one writer transaction at a time and `writeRetry`
(`collector.go:110`) already backs off on `SQLITE_BUSY`, two agents capturing the
same station serialize cleanly. Distinct `captured_at` values mean both
snapshots are retained as separate captures; same-stamp captures merge
harmlessly. No corruption, no last-writer hazard.

## Components

1. **Config knob** (`collector.go` `Config`): add `RetainCaptures int`.
   `DefaultConfig()` sets it to `3`. `Open()` defaults `RetainCaptures` to `3`
   when `≤ 0` (matching the existing `MaxOpenConns`/`BusyTimeout` defaulting), so
   every production caller that omits it — `worker`, `overmind`, `auto-explorer`,
   `play_as` — gets rolling-window retention with **no call-site changes**. There
   is no "disabled" mode (YAGNI).
2. **Per-station count-prune** (`collector.go`, new helper called from
   `WriteSnapshot`'s tx):
   ```sql
   DELETE FROM market_orders
   WHERE station_id = ?            -- snapshot.StationID
     AND captured_at NOT IN (
         SELECT DISTINCT captured_at FROM market_orders
         WHERE station_id = ?
         ORDER BY captured_at DESC
         LIMIT ?                   -- RetainCaptures
     )
   ```
   Runs after `insertOrders` in the same tx, so each capture is atomic and
   self-cleaning. Prune targets the snapshot's station only (cross-station
   isolation is structural: one station per `MarketSnapshot`).
3. **Index** (`schema.sql` + migrations): add
   `CREATE INDEX IF NOT EXISTS idx_orders_station_time ON market_orders(station_id, captured_at)`.
   The current indexes (`idx_orders_station_item`, `idx_orders_item_time`,
   `idx_orders_bucket`) don't cover "latest `captured_at` per station," which the
   prune and several readers (`FindBestPrices`, `GetItemStationPrices`,
   `GetLatestSnapshot`) need.
4. **Prune tool:** leave `cmd/tools/market-prune` in place; update its doc
   comment to "manual backstop only — retention is now self-managed per capture."
   No scheduled caller.

## Read path

Unchanged. All eight readers anchor on the latest capture per station and return
identical results against rolling-window data. `GetCaptureHealth` now naturally
surfaces up to N captures per station (richer than today's single-latest-bucket
view); no code change required.

## Operational steps (deploy)

1. Ship the code (Config + prune-in-tx + index migration).
2. On first run, `runMigrations` adds `idx_orders_station_time`.
3. As agents cycle, each station converges from its ≤12h history to ≤N captures
   within ~1 hour. The file stabilizes at ~13 GB (free pages reused) and stops
   growing.
4. **Locate and stop whatever currently invokes `market-prune` on a schedule.**
   Recon could not find the trigger in code, crontab, systemd, Makefile, or
   scripts — it runs from somewhere external (likely a tmux/session loop). This
   is an operational TODO, not a code change; leaving it running is harmless
   (it would only delete rows already aged past 12h, which the new per-station
   prune also subsumes) but it should be retired to avoid confusion.
5. (Deferred, not this spec) A future maintenance-window VACUUM, or a
   truncate-and-recreate, can reclaim the ~12 GB of free pages when desired.

## Error handling & freshness

- A prune failure inside the tx fails the whole `WriteSnapshot` (rolled back by
  `writeRetry`); the capture is retried or dropped, never half-written. The
  station keeps its prior ≤N captures — no data loss from a failed prune.
- Cold stations (captured at deploy then never again) keep their last-known
  data; their footprint is already bounded by the prior 12h global prune and is
  small. They do not auto-shrink, which is acceptable (their last market remains
  queryable).
- `RetainCaptures` is a runtime `Config` field, adjustable per binary without a
  reschema.

## Testing

- `pkg/market` collector test: after N+1 captures of one station, exactly N
  distinct `captured_at` remain; the oldest is gone and the newest intact.
- Cross-station isolation: capturing station S never prunes station T's rows.
- OHLCV still accumulates one point per distinct UTC hour across repeated
  captures (regression guard for the untouched OHLCV path), even when the older
  `market_orders` capture has been pruned by the window.
- Configurable N (e.g. `RetainCaptures: 1` in a test) prunes aggressively.
- Existing query/round-trip tests pass unchanged against rolling-window data;
  update any capture test that asserts monotonically growing row counts.
- Concurrency: two simulated same-station captures (distinct stamps) both
  survive; the window holds at most N.

## Out of scope (this spec)

- The `ops.db` split (nonexistent; root cause was append-only orders — fixed).
- Phase 4b arbitrage logistics and Phase 5 agent integration (separate specs).
- Reclaiming the ~12 GB of free pages (VACUUM / recreate) — deferred to a
  maintenance window.
- React skin / demand-ledger views.
