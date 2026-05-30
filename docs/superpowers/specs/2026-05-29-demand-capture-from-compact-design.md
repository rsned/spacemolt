# Demand Capture From Compact view_market — Design

**Date:** 2026-05-29
**Status:** Approved, ready for implementation plan
**Supersedes part of:** `docs/superpowers/specs/2026-05-29-demand-ledger-design.md` (the two-table + deep-scan model). The demand-ledger feature shipped earlier today (merge `da833c8`); this is a follow-up simplification.

## Problem

The demand-ledger feature shipped with a two-tier capture model:
- `market_buy_demand` — a cheap compact best-buy summary (no per-order `source`), upserted on every market read.
- `market_buy_orders` — full `source`-classified order depth, captured only by an explicit, chatty `demand scan` that issued one `view_market(item_id)` call per item (500+ items, one at a time).

That model assumed the compact `view_market` response was a truncated summary and that per-item deep calls were required to get complete orders and `source`. **Investigation of real captured responses disproved both assumptions.**

## Key findings (from `data/game-api/*/view_market.json`, which are compact no-`item_id` responses)

1. The compact response includes a **complete** `buy_orders` array per item: `sum(buy_orders[].quantity) == buy_quantity` for every item in every snapshot examined (368/368 in 20260521, including an item with 19 orders). The compact call is **not** truncated.
2. The compact response **already carries `source`**: orders are tagged `source: "station"` (Station Manager) or `source: null`. Across all snapshots the only values seen are `"station"` and `null` — there is no `"player"` value. (Latest snapshot 20260527: 111 `station`, 139 `null` buy orders.)
3. **Confirmed by the user:** a per-item `view_market(item_id)` deep call adds nothing the compact call lacks — null `source` stays null, no extra order depth.

Conclusion: the compact call is authoritative and complete. The deep scan is unnecessary, and the `market_buy_demand` summary table is redundant because the per-order data (with source) is available for free on every read.

## Design: capture full per-order demand from compact; one table

`captureDemand` becomes the sole writer. On every full market read (full `view_market` / `sellable` / `dock`) it parses every item's `buy_orders` (with `source`) and replaces that station's order set in a single table. The report reads it directly. `demand scan` is removed.

### Data flow

```
market read (full view_market / sellable / dock)
        │  compact response: all items, complete buy_orders[] with source
        ▼
captureDemand → parseStationBuyOrders(raw, station, system, now) []MarketBuyOrderRow
        │
        ▼
ReplaceStationBuyOrders(ctx, stationID, orders)   // DELETE WHERE station_id=?; bulk INSERT (one tx)
        │
        ▼
   market_buy_orders   ──LoadMarketBuyOrders──►  buildDemandReport(orders, onHand, canCraft, now, opts)
                                                            │
                                                  styled / JSON renderer
```

### `source` semantics

The compact JSON `source` is `"station"` or `null`. JSON `null` (or a missing key) unmarshals into the Go `Source string` field as `""`. So stored values are `"station"` (Station Manager) or `""` (player/unattributed). `classifyDemand` already treats any non-`"station"` source as player, so:
- top order `source == "station"` → `STN`
- a `""`-source order priced above the best station order → `PLR>SM`
- otherwise → `PLR`

The `?`/`classUnknown` state (formerly "compact-only, source unknown") no longer occurs and is removed.

### KB layer (`pkg/knowledge`)

- **Migration 37**: `DROP TABLE IF EXISTS market_buy_demand;` (its index drops with it). Regenerate `scripts/sql/initialize_database.sql` via the existing tooling so `TestInitializeDatabaseSQLInSync` passes.
- **Remove**: `MarketDemandRow` (demand.go), `UpsertMarketDemand` (demand_store.go), `loadMarketDemandSummary` (demand_load.go).
- **Add** `ReplaceStationBuyOrders(ctx context.Context, stationID string, orders []MarketBuyOrderRow) error` (demand_store.go): one `kb.inTx` transaction — `DELETE FROM market_buy_orders WHERE station_id=?`, then insert each order. Replaces the per-`(station,item)` `ReplaceMarketBuyOrders`, which is removed (its only caller, `runDemandScan`, is deleted). Replace-by-station is correct because a full compact read covers every item at the station; it also prunes items whose demand vanished since the last read.
- **Change** `LoadMarketDemand` → `LoadMarketBuyOrders(ctx) ([]MarketBuyOrderRow, error)` (demand_load.go): keep `loadMarketBuyOrders`, drop the summary half and the tuple return.

`market_buy_orders` schema is unchanged (already has `station_id, system_id, item_id, item_name, price_each, quantity, source, captured_utc` + indexes on `(station_id,item_id)` and `item_id`). Methods remain on `*SQLiteKB` only (faction pattern); callers type-assert `globalKB.(*knowledge.SQLiteKB)`.

### play_as (`cmd/tools/play_as`)

- **`demand_capture.go`**: replace `parseDemandRows` with `parseStationBuyOrders(raw []byte, stationID, systemID string, now time.Time) []knowledge.MarketBuyOrderRow` — unmarshal `{"items":[ViewMarketItem...]}`, and for each item append one `MarketBuyOrderRow` per entry in `it.BuyOrders` (skip `PriceEach<=0 || Quantity<=0`), carrying `Source`, `ItemID`, `ItemName`. `captureDemand` builds the full cross-item slice and calls `ReplaceStationBuyOrders`. Guards unchanged (nil KB, type-assert, empty raw/station → no-op). Capture hooks (view_market full-summary-only, dock, sellable) unchanged.
- **Delete `demand_scan.go`** entirely (`parseDeepOrders`, `runDemandScan`).
- **`main.go` dispatch**: `case "demand":` drops the `scan` sub-branch — always `parseDemandOptions(parts[1:])` → `runDemand`.
- **`demand_report.go`**: `buildDemandReport(deep []knowledge.MarketBuyOrderRow, onHand map[string]float64, canCraft map[string]int, now time.Time, opts demandOptions) demandReport` — drop the `summary` parameter; build `agg` from orders only. Remove `classUnknown` and the summary-overlay branch. `classifyDemand`, filters, fulfill/craft scoring, sort, staleness unchanged.
- **`demand_cmd.go`**: `runDemand` loads via `LoadMarketBuyOrders` and calls the new `buildDemandReport` signature.

### Error handling

`captureDemand` remains best-effort: nil-KB guard, type assertion, and the `ReplaceStationBuyOrders` error is swallowed (`_ =`). It is a passive side-effect of read commands and must never change their user-visible result. `ReplaceStationBuyOrders` is transactional, so each station snapshot is all-or-nothing.

### Testing

- **KB** (`demand_test.go`): round-trip for `ReplaceStationBuyOrders` + `LoadMarketBuyOrders`; a **cross-station isolation** test (replace at station A leaves station B's rows intact) — now load-bearing since deletes are by station; a replace-prunes test (a second replace with fewer orders removes the dropped ones). Remove the `market_buy_demand` summary tests.
- **play_as**: `parseStationBuyOrders` against canned compact JSON exercising station-source, null-source (→ `""`), and zero-price/zero-qty skipping. `buildDemandReport` table tests reworked to orders-only fixtures covering STN / PLR>SM / PLR, fulfill-now, craftable flag, staleness, total-after-limit. Remove `parseDeepOrders` tests.
- Gates: `go build ./...`, `go test ./...`, `golangci-lint run ./...` all clean.

## Out of scope (YAGNI)

- Faction-storage-across-systems — still the separate phase 2 of the original design.
- Any order-count heuristic (e.g. "≤2 orders") — unnecessary: the compact array is complete and deep calls add nothing, so all items are captured directly.
- Backfilling/migrating existing `market_buy_demand` rows — the table is one day old; dropping it is fine. Existing `market_buy_orders` rows (if any) coexist; they age out via staleness and get replaced on the next read.
