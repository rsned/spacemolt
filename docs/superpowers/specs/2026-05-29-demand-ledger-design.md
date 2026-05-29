# Demand Ledger & Report — Design

**Date:** 2026-05-29
**Status:** Approved, ready for implementation plan
**Topic:** Track market buy-order demand (especially Station Manager orders) across systems; surface what the player can fulfill now and what they can craft to fulfill.

## Problem

The player market has collapsed — most player-listed goods sit at the price floor. The only reliable earnings come from selling into **buy orders**, especially the standing orders placed by NPC **Station Managers** (and any player orders willing to pay *more* than the Station Manager price).

The player needs to:

1. Track buy-order demand across stations/systems over time.
2. See which orders they can **fulfill right away** from current inventory.
3. See which orders they have the **parts to craft** to fulfill.
4. *(Secondary / phase 2)* Know which ores and supplies sit in **faction storage across systems** that could meet demand.

## Key constraint discovered

Buy-order demand can only be read at the station you are **currently docked at**:

- `view_market` is station-local. There is no galaxy-wide "search all buy orders" endpoint.
- `view_orders` accepts a remote `station_id` but only returns *your own* orders, not open demand.
- **Exception:** faction storage *can* be polled remotely via `ViewFactionStorageAt(stationID)` without docking — relevant to phase 2.

Therefore "tracking demand across systems" fundamentally means: **capture each station's order book as you visit it, persist it, and report across those captured snapshots.** There is no way to see remote demand without having been there.

### Capture-depth constraint

`view_market` **without** `item_id` returns only a *compact summary* (best buy price + quantity per item) — confirmed in `server_docs/openapi.json`. The per-order `BuyOrders[]` array carrying `Source` (`"station"` vs player) only comes back **with** an `item_id`, i.e. one call per item. So:

- Compact summary = cheap, no extra calls, gives "best-paying demand and where" — but no per-order source classification.
- Full order-book depth (with `Source`) = one `view_market(item_id)` call per item — expensive, opt-in.

(Note: the existing `sellable` feature's unit tests construct `BuyOrders` by hand; its live path uses the compact call, so it only ever sees top-of-book. Out of scope here, noted for accuracy.)

## What already exists (reused, not rebuilt)

- **`sellable`** (`cmd/tools/play_as/sellable.go`) — matches cargo + current-station storage against the current station's buy orders, greedy best-price-first fill (`fillItem`). Its fill logic is reused for "fulfill now."
- **`craftable`** / **`craftplan`** (`cmd/tools/play_as/craftable.go`, `pkg/craftplan`) — given inventory + skills, computes craftable recipes and `CanMake` batch counts. Reused for "craftable to fulfill."
- **`globalKB`** (`knowledge.Base`, `cmd/tools/play_as/main.go:42`) — the knowledge SQLite DB, already wired into `play_as` (nil if no `--db` path). Home for the ledger.
- **`faction_orders`** KB pattern (`pkg/knowledge/faction*.go`, migration in `sqlite_migrations.go`) — template for the new ledger tables and store/load code.

## Relevant verbatim API/data shapes

- `serverapi.ViewMarketItem` (`pkg/game/serverapi/types.go:304`): `ItemID, ItemName, Category, BestBuy, BestSell, BuyPrice, BuyQuantity, SellPrice, SellQuantity, Spread, BuyOrders []MarketOrder, SellOrders []MarketOrder`.
- `serverapi.MarketOrder` (`types.go:320`): `PriceEach float64, Quantity float64, MyQuantity int, Source string` (`"station"` = NPC/Station Manager).
- `serverapi.CargoItem` (`types.go:14`): `ItemID, Name, Quantity float64, Size`.
- `serverapi.Recipe` (`types.go:438`): `ID, Name, Category, RequiredSkills map[string]int, Inputs []RecipeItem, Outputs []RecipeItem, ...`.
- `craftplan.CraftableRow`: `Recipe, CanMake int, OutputItemID, OutputQuantity, Depth`.

## Design

### Architecture overview

```
market read (view_market / sellable / dock)
        │  compact summary (cheap)
        ▼
captureDemand(client, ctx) ──► globalKB.UpsertMarketDemand ──► market_buy_demand
                                                                    │
demand scan (explicit) ─ per-item view_market(item_id) ─► ReplaceMarketBuyOrders ─► market_buy_orders
                                                                    │
                                                                    ▼
demand (report, offline) ── LoadMarketDemand ──► buildDemandReport(demand, cargo+storage, craftable set)
                                                                    │
                                                          styled / JSON renderer
```

### Data model (migration #15, `pkg/knowledge/sqlite_migrations.go`)

**`market_buy_demand`** — always-on compact layer, one row per `(station_id, item_id)`, upserted on every market read:

```sql
CREATE TABLE market_buy_demand (
  station_id      TEXT NOT NULL,
  system_id       TEXT,
  item_id         TEXT NOT NULL,
  item_name       TEXT,
  best_buy_price  REAL NOT NULL DEFAULT 0,
  buy_quantity    REAL NOT NULL DEFAULT 0,
  captured_utc    TEXT NOT NULL,
  PRIMARY KEY (station_id, item_id)
);
CREATE INDEX market_buy_demand_item ON market_buy_demand(item_id);
```

**`market_buy_orders`** — per-order depth from deep scans, replaced per `(station_id, item_id)` on each scan:

```sql
CREATE TABLE market_buy_orders (
  station_id    TEXT NOT NULL,
  system_id     TEXT,
  item_id       TEXT NOT NULL,
  item_name     TEXT,
  price_each    REAL NOT NULL DEFAULT 0,
  quantity      REAL NOT NULL DEFAULT 0,
  source        TEXT,
  captured_utc  TEXT NOT NULL
);
CREATE INDEX market_buy_orders_station_item ON market_buy_orders(station_id, item_id);
CREATE INDEX market_buy_orders_item ON market_buy_orders(item_id);
```

### KB layer (`pkg/knowledge/demand.go`, `demand_store.go`, `demand_load.go`)

Following the `faction_orders` pattern. New methods on the `knowledge.Base` interface:

- `UpsertMarketDemand(ctx, rows []MarketDemandRow) error` — upsert compact best-buy per `(station, item)`.
- `ReplaceMarketBuyOrders(ctx, stationID, itemID string, orders []MarketBuyOrderRow) error` — replace deep order rows for one `(station, item)`.
- `LoadMarketDemand(ctx) ([]MarketDemandRow, []MarketBuyOrderRow, error)` — load full ledger for the report (or a filtered variant by item/station).

Row structs (`MarketDemandRow`, `MarketBuyOrderRow`) carry the columns above plus a parsed `CapturedAt time.Time`.

> Implementation note: because `globalKB` is typed as the `knowledge.Base` interface, these methods must be added to the interface and to **every** implementor — `SQLiteKB` (real) and `MemoryKB` (no-op/empty stubs) — plus any test mocks of `knowledge.Base`. `go build` will catch missing implementors, but run `go test ./...` too in case mocks live in `_test.go` files.

### Capture

- **`captureDemand(client game.GameClient, ctx context.Context)`** (new, e.g. `cmd/tools/play_as/demand_capture.go`): reads cached compact market JSON via `client.GetRawJSON("market")`, derives `station_id`/`system_id` from `client.GetState()`, builds `MarketDemandRow`s from `ViewMarketItem` (`item_id`, `item_name`, `best_buy`/`buy_price`, `buy_quantity`), and calls `globalKB.UpsertMarketDemand`. No-op when `globalKB == nil` or no market data. Zero extra server calls.
  - Wired into: the `view_market` command handler, `runSellable`, and the dock event path — all places a fresh compact market response lands.
- **`demand scan`** (explicit command): at the current station, iterate items from the compact summary, issue `client.ViewMarket(ctx, {item_id})` per item with a `SleepQuick` pause between calls, parse `BuyOrders` (with `Source`), and call `ReplaceMarketBuyOrders` per item. The only chatty path; opt-in. Prints progress and a summary count.

### The `demand` report (`cmd/tools/play_as/demand.go`)

Pure builder `buildDemandReport(...)` (no network, table-test friendly, mirrors `sellable`) + styled/JSON renderers. Steps:

1. **Load** ledger via `LoadMarketDemand`. For each `(station, item)`, prefer fresh `market_buy_orders` depth; fall back to the compact `market_buy_demand` row.
2. **Classify** each demand:
   - `STN` — `Source == "station"` (Station Manager).
   - `PLR>SM` — player order priced **above** the best station order for that item at that station (the "pays more than SM" case).
   - `PLR` — other player order.
   - `?` — compact-only, source unknown.
3. **Fulfill-now**: ship cargo (`state.Ship.Cargo`) + current-station personal storage, matched against each demand using `sellable`'s greedy fill (qty fulfillable × price → proceeds).
4. **Craftable-to-fulfill**: run `craftplan.Craftable`; flag demand items that are a craftable recipe output with `CanMake > 0`.
5. **Sort & annotate**: sort by best price / total proceeds; annotate each row with capture **age**, flagging entries older than 1 day (station freshness threshold) as `STALE`.

**Flags:** `--item`, `--station`, `--max-age`, `--min-price`, `--only=fulfillable|craftable|all` (default `all`), `--station-only` (confirmed SM demand only), `--sort=price|proceeds|age`, `--limit`, `--detail`. JSON via the existing global `format` flag.

**Defaults / boundaries:**
- Report shows **all demand** by default (with `Source` column `STN`/`PLR>SM`/`PLR`/`?`); `--station-only` narrows to confirmed Station Manager demand.
- Fulfill-now inventory = **ship cargo + current-station personal storage** only. Galaxy-wide personal storage is the same unbuilt problem as faction storage (phase 2).
- Report works **offline** from the ledger; cargo/storage come from live state when available.

### Command dispatch

Add cases to the `executeCommand` switch in `cmd/tools/play_as/main.go`:
- `case "demand":` — if next token is `scan`, run the deep-scan handler; otherwise parse report flags and run `runDemand(...)`.

## Phase 2 (specced, not built): faction storage across systems

A parallel `faction_storage_ledger` table (`faction_id, station_id, item_id, item_name, quantity, captured_utc`), populated by polling `ViewFactionStorageAt(stationID)` across known stations (works remotely, no dock needed — iterate stations from the `pois` KB table). The `demand` report gains a third supply source: "faction has N of item X at station Y meeting this demand," and a `--include-faction` flag. Not built in v1.

## Testing

- `buildDemandReport` and the classification logic: table-driven unit tests mirroring `sellable_test.go` (STN vs PLR>SM vs PLR vs ?, fulfill-now fills, craftable flagging, staleness).
- KB methods: SQLite round-trip test (upsert/replace/load).
- `captureDemand`: parse + upsert against canned raw market JSON.
- Gates: `go build ./...`, `go test ./...`, `golangci-lint` all clean. New binaries (none expected; `play_as` already builds to `bin/`) go to `bin/`.

## Out of scope (YAGNI)

- Auto-traveling agent that visits stations to refresh demand (could be a later strategy).
- Galaxy-wide personal storage tracking.
- Buy-order price-trend / forecasting analytics (the existing `price_trends` machinery is separate).
- Fixing `sellable`'s compact-vs-deep live behavior.
