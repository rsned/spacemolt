# Demand History & Freshness Gate — Design

**Date:** 2026-05-30
**Status:** Approved, ready for implementation plan
**Builds on:** the demand ledger (`market_buy_orders`, `captureDemand`, `demand` command) — see `docs/superpowers/specs/2026-05-29-demand-ledger-design.md` and `2026-05-29-demand-capture-from-compact-design.md`.

## Problem

The live demand ledger (`market_buy_orders`) keeps only the latest snapshot per station — `ReplaceStationBuyOrders` does delete-by-station then insert, so there is no history. We want to:

1. **Accumulate a time series of buy-order demand** so trends become visible "down the road" — is Station-Manager (SM) demand for an item rising or drying up, and at which stations? Earnings depend on SM buy orders, so the SM split must be preserved over time.
2. **Avoid redundant work across many agents.** With 100+ agents potentially operating out of the same stations, re-capturing the same station's demand minute-apart wastes API ticks and causes SQLite single-writer contention. A cheap freshness query lets callers skip work when the data is already fresh.

## Existing context (verified)

- Pattern precedent: `poi_resources` (current state) + `resource_history` (append-only time series). This feature mirrors it: `market_buy_orders` (current) + `market_demand_history` (time series).
- The sell-side `market_snapshots`/`market_listings` machinery is a separate, listing-focused subsystem; `price_trends` is defined but never written. We do not reuse them.
- KB demand methods live on `*SQLiteKB` only (faction pattern); callers type-assert `globalKB.(*knowledge.SQLiteKB)`.
- `captureDemand` (play_as) fires on every full market read (full `view_market`, `sellable`, `dock`); the `--category`/`item_id` guard already ensures only full-station compact reads trigger it.

## Design

### 1. History table — `market_demand_history` (migration 38)

Append-only, one row per **(station, item, hourly bucket)**:

```sql
CREATE TABLE market_demand_history (
  station_id     TEXT NOT NULL,
  system_id      TEXT,
  item_id        TEXT NOT NULL,
  item_name      TEXT,
  bucket_utc     TEXT NOT NULL,   -- captured time truncated to the hour (bucket key)
  captured_utc   TEXT NOT NULL,   -- actual last observation time within the bucket
  best_price     REAL NOT NULL DEFAULT 0,  -- highest buy price across all orders
  total_qty      REAL NOT NULL DEFAULT 0,  -- total demand quantity across all orders
  sm_best_price  REAL NOT NULL DEFAULT 0,  -- best price among source=="station" orders (0 if none)
  sm_qty         REAL NOT NULL DEFAULT 0,  -- total qty among station-source orders
  order_count    INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (station_id, item_id, bucket_utc)
);
CREATE INDEX market_demand_history_item ON market_demand_history(item_id, bucket_utc);
```

The composite PK makes hourly sampling a clean upsert: re-reading a station within the same hour **updates** that bucket's row to the latest aggregate (last-observation-in-the-hour wins); a new hour appends a new row. Bucket size is a tunable constant `demandHistoryBucket = time.Hour` in the play_as capture layer.

### 2. Write path

**KB (`*SQLiteKB`):**
- `RecordDemandHistory(ctx, samples []DemandHistorySample) error` — `INSERT … ON CONFLICT(station_id,item_id,bucket_utc) DO UPDATE` setting `system_id, item_name, captured_utc, best_price, total_qty, sm_best_price, sm_qty, order_count` from `excluded`.
- `LatestDemandCapture(ctx, stationID string) (time.Time, bool, error)` — `SELECT MAX(captured_utc) FROM market_buy_orders WHERE station_id=?`. Returns the most recent capture time for the station and a bool for "any data exists." Cheap, uses the existing `market_buy_orders_station_item` index. This is the shared freshness primitive.

`DemandHistorySample` struct (pkg/knowledge/demand.go) mirrors the table columns, with `BucketAt`/`CapturedAt time.Time`.

**play_as (`cmd/tools/play_as`):**
- Pure `aggregateDemandHistory(orders []knowledge.MarketBuyOrderRow, now time.Time, bucket time.Duration) []knowledge.DemandHistorySample` — groups the already-parsed orders by item; per item computes `best_price` (max price), `total_qty` (sum qty), `sm_best_price` (max price where `Source=="station"`), `sm_qty` (sum qty where `Source=="station"`), `order_count`, and sets `bucket_utc = now.UTC().Truncate(bucket)`, `captured_utc = now`.
- `captureDemand` is extended: after `parseStationBuyOrders` and before writing, it consults the freshness gate; when it does write, it calls **both** `ReplaceStationBuyOrders` (live) and `RecordDemandHistory` (history), best-effort (errors swallowed, as today).

### 3. Freshness gate

A tunable constant `demandFreshness = 5 * time.Minute` (play_as). In `captureDemand`, after confirming there are orders to write:
1. Call `LatestDemandCapture(ctx, stationID)`.
2. If data exists and `now.Sub(last) < demandFreshness`, **skip both writes and return** — the station's demand is already fresh (likely just captured by this or another agent).
3. Otherwise write the live ledger and the history sample.

The 5-min write gate and the 1-hour history bucket are independent: gated writes within an hour upsert the same bucket (last-wins), and live data stays ≤5 min fresh. Because `LatestDemandCapture` is public, an agent can call it to skip issuing `view_market` entirely when demand is fresh — saving the API tick, not just the DB write. (Wiring it into the autonomous agent decision loops is out of scope here; this ships the primitive + the play_as gate.)

### 4. Read path & `demand history` command

**KB:** `LoadDemandHistory(ctx, itemID, stationID string, limit int) ([]DemandHistorySample, error)` — required `itemID`; optional `stationID` ("" = all stations); ordered by `station_id, bucket_utc ASC` (chronological); `limit` caps the most recent N buckets per the query (0 = no limit). Implementation: `WHERE item_id=? [AND station_id=?] ORDER BY station_id, bucket_utc DESC LIMIT ?` then reversed to chronological per station, or an `ORDER BY ... ASC` with a subselect — implementation detail left to the plan; the contract is chronological ascending per station.

**Command:** `demand history <item> [--station <id>] [--limit N]`, dispatched via a `history` sub-branch of `case "demand"` (checked before option parsing, like the removed `scan` branch was).
- Groups output **per station** (one section per station that has history for the item; `--station` narrows to one).
- Per station, a table: `bucket time | best price | total qty | SM price | SM qty`, oldest→newest, capped to `--limit` (default 24 — one day of hourly buckets).
- A one-line **direction** summary per station: compare the first vs last sample in the shown window for `best_price` and `total_qty`, emitting ↑ / ↓ / → (a small pure helper `trendDirection(first, last float64) string`).
- Styled and JSON output via the existing `format` switch.

### 5. Error handling & retention

- All writes inside `captureDemand` remain best-effort (swallowed errors); a history-write failure never affects the read command's result.
- Retention: **deferred.** Hourly buckets keep the table small (≈ items × stations-visited × active-hours). The table keeps everything for now, matching the other `*_history` tables. A `--prune`/TTL is noted as future work, not built.

### 6. Testing

- **KB:** `RecordDemandHistory` upsert-within-bucket (same `(station,item,bucket)` updates in place; a new bucket appends) and `LoadDemandHistory` filter (item, optional station) + chronological order round-trip. `LatestDemandCapture` returns the latest `captured_utc` for a station and `false` when the station has no rows.
- **play_as:** `aggregateDemandHistory` pure test — best price, total qty, SM split from mixed `station`/`""` orders, hour truncation of `bucket_utc`. `trendDirection` pure test (up/down/flat). Freshness-gate behavior: a small testable predicate (e.g. `isFresh(last time.Time, now time.Time) bool`) so the skip logic is unit-tested without the network.
- Gates: `go build ./...`, `go test ./...`, `golangci-lint run ./...` all clean.

## Files (anticipated)

| File | Change |
|------|--------|
| `pkg/knowledge/sqlite_migrations.go` | Migration 38: create `market_demand_history` |
| `scripts/sql/initialize_database.sql` | Regenerate (tooling) |
| `pkg/knowledge/demand.go` | Add `DemandHistorySample` struct |
| `pkg/knowledge/demand_store.go` | Add `RecordDemandHistory` |
| `pkg/knowledge/demand_load.go` | Add `LoadDemandHistory`, `LatestDemandCapture` |
| `pkg/knowledge/demand_test.go` | History + freshness round-trip tests |
| `cmd/tools/play_as/demand_capture.go` | `aggregateDemandHistory`, freshness gate, dual write |
| `cmd/tools/play_as/demand_history.go` (new) | `runDemandHistory`, render, `trendDirection` |
| `cmd/tools/play_as/main.go` | `demand history` dispatch sub-branch |
| `cmd/tools/play_as/demand_capture_test.go` | `aggregateDemandHistory` + freshness predicate tests |
| `cmd/tools/play_as/demand_history_test.go` (new) | `trendDirection` test |

## Out of scope (YAGNI)

- Moving averages / regression / "drying up" alerts surfaced in the demand report (the richer-analytics option).
- Cross-station aggregation in the history view.
- Retention/prune command or TTL.
- Wiring demand capture + the freshness gate into the autonomous `auto-*` agents (their own future feature).
- Faction-storage-across-systems (the standing phase 2 of the demand ledger).
