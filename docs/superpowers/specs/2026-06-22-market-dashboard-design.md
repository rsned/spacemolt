# Market Dashboard — Design

**Date:** 2026-06-22
**Status:** Approved (design), pending implementation plan
**Related:** `pkg/market` (Market Intelligence MVP, merged 2026-06-21 `944b899`); `docs/superpowers/specs/2026-06-19-market-intelligence-system-design.md` (Phase 2 — "Webapp: Market Matrix"); `docs/superpowers/specs/2026-06-21-market-data-consolidation-design.md` (parallel work, other branch); `cmd/tools/view-market/` (legacy CLI this dashboard succeeds)

## Goal

A standalone, read-only web dashboard over `data/market.db` whose **primary job is validating that market collection is correct** — eyeballing captured data to catch mapping/silent-data-loss bugs, confirm capture cadence, and inspect price behavior. This is **Phase 2** of the Market Intelligence System. It is deliberately a validation tool first, not a live ops console or an arbitrage scanner (those are later phases).

## Scope

**Build:**
- `cmd/market-dashboard/` — a single Go binary: stdlib HTTP server + a vanilla-JS single-page UI embedded via `//go:embed` (no build step, no framework).
- Four read-only views: **matrix**, **cell-detail order book**, **per-item price-over-time**, **capture-health**.
- Additive `pkg/market` read methods to back them, plus a small **category-capture fix** (see below).

**Out of scope (explicitly):**
- Arbitrage / spread detection and opportunity claiming — Phase 4.
- Auto-refresh, WebSockets, auth, multi-user — validation tool, manual refresh only.
- Reusing/embedding the React `frontend/` SPA — this is a standalone tool. (The JSON API is shaped so a React skin could be added later without rework.)
- Live ops monitoring / fleet integration.

**Coordination:** This work branches from `main` (`944b899`, Phase 1 merge) — **not** from the in-flight `feat/market-data-consolidation` branch. All `pkg/market` changes are additive (new methods/files; the category fix is a small in-place edit) so they auto-merge cleanly when synced to head after consolidation lands. **Sync to head before implementing** beyond the spec/plan.

## Category-capture fix (prerequisite, on-theme)

Validation surfaced that `items.category` is 100% empty (`559` rows, all `""`) even though the game sends `category` on every `view_market` item row. Root cause:
- `serverapi.ViewMarketItem` **does** parse `category` (`pkg/game/serverapi/types.go:307`) — the data reaches Go.
- `parseViewMarket` (`pkg/market/capture.go`) copies `item.ItemName` onto each order but **never reads `item.Category`**.
- `WriteSnapshot` (`pkg/market/collector.go:355`) hardcodes `Category: ""` when constructing item rows. The `upsertItem` plumbing already writes category to the DB — it just always receives `""`.

**Fix:** thread `item.Category` through `parseViewMarket` → onto each `Order` (mirroring how `ItemName` is already denormalized there) → into `WriteSnapshot`'s item map (first-non-empty-wins, like `item_name`). Small, localized, additive. **Self-heals:** once fixed, hourly re-captures upsert real categories into `items` within a day; no manual backfill required (a one-shot backfill from `catalog.json` is optional if instant population is wanted). This improves data completeness for all consumers, not just the dashboard.

## Architecture

**Approach: standalone `cmd/market-dashboard` binary** (chosen over a new page in the React SPA). Rationale: the validation use case needs no fleet/auth coupling and benefits from zero build step; the reusable artifacts are the `pkg/market` query methods and the JSON shapes, so the disposable vanilla-JS UI costs nothing to replace later.

**Runtime:** `market-dashboard [--addr :8090] [--market-db-path data/market.db]`. Default DB path `data/market.db` (consistent with the consolidation branch's relative-default direction). Read-only; opens one `*market.Collector` at startup; fails fast if the DB is unreadable (mirrors `market-stats`). Opens a browser tab on start (best-effort).

### `pkg/market` API additions (all additive, TDD)

**Types:**
```go
// MatrixQuery parameterizes a matrix request.
type MatrixQuery struct {
    Category string // "" = all categories
    Search   string // case-insensitive substring on item_id / item_name
    Page     int    // 1-based
    Limit    int    // default 50
}

// MatrixCell is one item×station cell in the matrix.
type MatrixCell struct {
    StationID, StationName, SystemID, SystemName string
    BestSell, BestBuy, VWAP, Volume float64
    OrderCount int
    CapturedAt time.Time
    HasSell, HasBuy bool
}

// MatrixItem is one matrix row (an item across all stations).
type MatrixItem struct {
    ItemID, ItemName, Category string
    Cells []MatrixCell // len == len(Matrix.Stations), aligned
}

// Matrix is a paginated items×stations snapshot.
type Matrix struct {
    Stations   []Station
    Items      []MatrixItem
    TotalItems int // total matching items before pagination
    Page, Limit int
    GeneratedAt time.Time
}
```

**Methods (on `*Collector`):**
- `GetMatrix(ctx, q MatrixQuery) (*Matrix, error)` — for each item matching the filter, the latest capture's best sell (min ask), best buy (max bid), VWAP, volume, order count, and freshness per station. Adapts `view-market`'s latest-per-station CTE onto `market_orders` (latest `captured_at` per `(station_id, item_id, side)`, then aggregate). Joins `stations` for names; joins `items` for category/name.
- `GetItemPriceHistory(ctx, itemID string, limit int) ([]ItemPricePoint, error)` — one item's price across hour-buckets per station, read from `market_ohlcv` (VWAP + high/low + volume + trade_count per `(station, item, side, bucket)`), newest buckets first. This is the view that directly exposes the thin-item `close`-price caveat (VWAP is the robust series).
- `GetCaptureHealth(ctx) ([]StationCaptures, error)` — per station: distinct capture timestamps (desc), count, earliest, latest. Surfaces cadence gaps (current data shows irregular 2–6 captures/station over ~14h).
- `GetStationOrders(ctx, stationID, itemID string) ([]Order, error)` — the latest capture's orders for a station, filtered to `itemID` when non-empty (backs cell-detail). Owns its own latest-capture grouping rather than reusing `GetLatestOrders`, which is unfiltered and limit-bounded.

`ItemPricePoint` / `StationCaptures` are small struct types defined alongside their methods.

### HTTP layer (`cmd/market-dashboard`)

stdlib `http.ServeMux`, JSON responses:
| Endpoint | Backed by | Purpose |
|---|---|---|
| `GET /api/stats` | `GetStats` | header: station/item/order counts + latest capture |
| `GET /api/matrix?category=&q=&page=&limit=` | `GetMatrix` | the matrix |
| `GET /api/station/{id}/orders?item=&limit=` | `GetStationOrders` | cell-detail order book |
| `GET /api/item/{id}/history?limit=` | `GetItemPriceHistory` | price-over-time |
| `GET /api/captures` | `GetCaptureHealth` | capture cadence |
| `GET /` , `GET /static/*` | `//go:embed` | the UI |

### UI (embedded, vanilla JS)

Single `index.html` + `app.js` + minimal CSS, no framework. A top bar with stats + a manual **Refresh** button. A category `<select>` (populated from distinct `items.category`) + a search box + pagination. Four tabs/views:
1. **Matrix** — `<table>`: item columns = item_id/name/category; one column per station; each cell shows best sell / best buy / VWAP / volume / freshness (relative "5 min ago"). Sparse cells (item not traded at a station) render `—`. Click a cell → view 2.
2. **Cell-detail** — full latest order book for one item×station (buy/sell rows).
3. **Price-over-time** — pick an item; per-station VWAP series across buckets (with high/low band), exposing thin-item behavior.
4. **Capture-health** — per-station capture timeline + gaps.

Relative-time formatting mirrors `view-market`'s `formatTimestamp` ("just now", "5 min ago", "2 hr ago", "3 days ago"), reimplemented in JS.

## Data Flow

```
data/market.db (market_orders, market_ohlcv, items, stations)
        │
        ▼
  pkg/market: GetMatrix / GetItemPriceHistory / GetCaptureHealth / GetStats / GetLatestOrders
        │  (JSON)
        ▼
  cmd/market-dashboard HTTP (/api/*)
        │  (fetch)
        ▼
  embedded vanilla-JS UI (4 views)
```

Read-only end to end. No writes, no game connection.

## Data-Quality Considerations

- **Basement orders:** raw best-price will include outlier orders (e.g. 1cr "basement" orders the `2026-06-19` spec calls out). For a *validation* tool we **show them** with `OrderCount`/volume visible rather than silently filter, so they're obvious as outliers. VWAP remains the robust figure. (Filtering basement orders is a later analysis concern.)
- **Thin-item `close`:** `market_ohlcv.close` is the last order in a snapshot's array, so per-bucket deltas for thin items can reflect a single listing appearing/disappearing rather than a market-wide repricing. The price-over-time view surfaces this by plotting VWAP (robust) alongside high/low — a stated validation goal.
- **Freshness:** cells show per-station capture time, so stale stations are immediately visible.
- **Station names:** fall back to POI ID (game `State` carries no friendly names) — consistent with the known Phase 1 caveat.

## Error Handling

- `pkg/market` read methods return empty slices / `(*Matrix=nil, nil)` when no data exists (matches existing `GetLatestSnapshot` semantics).
- Handlers return `200` with an empty `items: []` for no-match filters; `404` for an unknown `station_id` / `item_id` on the detail endpoints.
- Collector open failure at startup is fatal (fail fast, like `market-stats`).
- UI renders explicit empty states ("no captures yet", "no items match").

## Testing

- TDD per slice. `pkg/market`: unit tests for `GetMatrix`, `GetItemPriceHistory`, `GetCaptureHealth`, `GetStationOrders` (temp DB per test, existing `pkg/market` test patterns), including category-filter, sparse-cell, and item-filter cases.
- A test for the **category-capture fix**: write a snapshot whose orders carry `Category`, assert `items.category` is populated (and that the pre-fix path left it empty — the regression guard).
- `cmd/market-dashboard`: `httptest` handler tests asserting JSON shapes + the empty/404 cases. The UI is thin glue, not unit-tested.
- After each series of changes: `go build ./...`, `go test ./...`, `golangci-lint run ./...` (per project rules — interface/struct changes break things the build alone misses).

## Sequencing / Coordination

1. Spec + plan (this work) on the `feat/market-dashboard` branch off `main` (`944b899`).
2. **Sync to head** (rebase onto `main`) once the consolidation branch merges — before implementing. Additive `pkg/market` changes should auto-merge.
3. Implement: (a) category-capture fix, (b) the read methods (`GetMatrix`, `GetItemPriceHistory`, `GetCaptureHealth`, `GetStationOrders`), (c) `cmd/market-dashboard` server + handlers, (d) embedded UI, (e) tests at each step.

## Open Items (follow-on, not this work)

- Arbitrage / spread view and opportunity claiming — Phase 4 (the consolidation branch's `FindBestPrices` is a building block).
- React skin in `frontend/` consuming the same JSON API — if the market view is ever wanted inside the main app.
- Demand-ledger and `base_market` views — separate slices.
- Retire `cmd/tools/view-market` once this dashboard + a thin CLI cover its commands (it targets the legacy `market_snapshots`/`market_listings` tables the consolidation branch drops).
