# Market Data Consolidation — Design

**Date:** 2026-06-21
**Status:** Approved (design), pending implementation plan
**Related:** `pkg/market` (Market Intelligence MVP, merged 2026-06-21 `944b899`); `project_overmind_fleet_manager` (roles.yaml wiring is the follow-on)

## Goal

Make `pkg/market` (single SQLite DB at `data/market.db`) the sole source of **volatile market data**. Remove the snapshot/listing and LLM-analysis market surface from `knowledge.Base` so `update_market`, `view_market`, and agent decision paths all read and write one place. Eliminate the current dual-write split where the resident worker writes market snapshots into the knowledge DB while `update_market` writes into `market.db`.

This is **Step 1** of the larger market plan. Roles.yaml wiring of `update_market` (Step 2) and the arbitrage detector (Step 3, another team) build on top and are out of scope here.

## Scope

**Move to `pkg/market`:**
- `market_snapshots` + `market_listings` — raw captured market state from `view_market` / `update_market`.
- `market_analyses` — LLM `analyze_market` insights.
- `FindBestPrices` + the `BestPrice` type — cross-station best buy/sell price query (reads `market_listings`; feeds `agentstate` `NearbyBestBuys`/`NearbyBestSells`). Discovered during planning; it consumes the moved tables so it must move too or `agentstate` breaks after cutover.

**Retire as dead code (no live readers — only a test mock implements it):**
- `AnalyzePriceTrends`, `knowledge.PriceTrend`, and the `price_trends` table.

**Explicitly out of scope (leave where they are):**
- `base_market` — base inventory coupled to the `bases` entity, written by `RememberBase()` and read by base/POI queries. It is **not** volatile market data; it stays in the knowledge DB. `cmd/data/import-base-data` is untouched.
- **Demand ledger** (`market_buy_orders`, `market_sell_orders`, `market_demand_history`, `market_supply_history`, `market_buy_demand` and the `SQLiteKB` demand methods in `demand_load.go` / `demand_store.go`) — deferred to a later slice. These are concrete `SQLiteKB` methods, not on the `knowledge.Base` interface, so they are unaffected by this work.
- The arbitrage detector (`arbitrage_opportunities` producer) — another team.

**Data migration:** none. Fresh cutover — market data is volatile and continuously re-captured. Existing knowledge-DB market rows are dropped.

## Architecture

**Approach: direct injection** (chosen over an adapter-behind-`knowledge.Base`). The ~6 read call sites are few enough that injecting `*market.Collector` directly is cleaner than keeping a delegating shim, and it actually removes market code from `knowledge.Base` rather than hiding it.

### `pkg/market` API additions

`pkg/market` already has the write side (`WriteSnapshot`, `CaptureFromClient`) and a read helper (`GetLatestOrders`). Add the read/write methods needed to replace the knowledge surface:

Snapshots/listings (on `*Collector`):
- `GetLatestSnapshot(ctx, stationID) (*MarketSnapshot, error)` — reconstruct from latest orders; replaces `GetLatestMarketSnapshot` (5 callers).
- `HasSnapshotToday(ctx, stationID) (bool, error)` — replaces `HasMarketSnapshotToday` (1 caller).

Caller audit (during planning) found `GetMarketSnapshots`, `GetMarketItems`, and `GetMarketAnalysisHistory` have **zero live callers** — they are deleted, not ported. Likewise the unused agent-layer helpers `ShouldRefreshMarket`, `GetMarketAge`, `ShouldRefreshMarketAnalysis`, `GetMarketAnalysisAge` (0 callers) are deleted.

`pkg/market.MarketSnapshot` already carries `StationID/StationName/SystemID/SystemName/Orders/CapturedAt`, so the `(systemID, stationID)` consumers are satisfied by `stationID` lookups (the snapshot returns the system fields).

Analysis (new `analyses` table + `MarketAnalysis` type on `*Collector`):
- `StoreAnalysis(ctx, analysis MarketAnalysis) error` (1 caller)
- `GetLatestAnalysis(ctx, stationID) (*MarketAnalysis, error)` (5 callers)

The `analyses` schema mirrors the moved `market_analyses` columns. `pkg/market.MarketAnalysis` is a straight copy of the fields from `knowledge.MarketAnalysis`.

Best prices (new `BestPrice` type on `*Collector`):
- `FindBestPrices(ctx, itemID, side, limit) ([]BestPrice, error)` — best buy/sell prices for an item across stations, computed over the latest `market_orders`.

### Consumer rewiring (inject `*market.Collector`)

| Consumer | Current | Change |
|---|---|---|
| `pkg/worker/capture.go` | `kb.StoreMarketSnapshot` (writer) | convert `game.MarketListing` → `[]market.Order`, write via `collector.WriteSnapshot` |
| `pkg/agentstate` (`New`/`NewWithAgent`, `refresh.go`) | `kb.GetLatestMarketSnapshot`, `MarketAnalysis` | add `collector` field; read from it. Only caller: `pkg/unified/server.go:84` |
| `pkg/agent/market_refresh.go` | 3× `GetLatestMarketSnapshot` | take `*market.Collector` |
| `pkg/agent/market_capture.go` | snapshot writer | take `*market.Collector` |
| `pkg/agent/market_analysis.go` | `StoreMarketAnalysis` / `GetLatestMarketAnalysis` | take `*market.Collector` |
| `cmd/auto-craftsman/profit_selector.go` | `GetLatestMarketSnapshot` | open + pass `*market.Collector` |
| `cmd/auto-explorer/main.go` | `HasMarketSnapshotToday` | open + pass `*market.Collector` |
| `cmd/tools/play_as` | lazy global `Open(DefaultConfig())` | construct once from `--market-db-path`; use for `update_market` + `view_market` capture |

### Knowledge teardown

- Remove from `knowledge.Base` interface: `StoreMarketSnapshot`, `GetMarketSnapshots`, `GetLatestMarketSnapshot`, `GetMarketItems`, `HasMarketSnapshotToday`, `StoreMarketAnalysis`, `GetLatestMarketAnalysis`, `GetMarketAnalysisHistory`, `AnalyzePriceTrends`, `FindBestPrices`.
- Delete unused agent-layer helpers `ShouldRefreshMarket`, `GetMarketAge`, `ShouldRefreshMarketAnalysis`, `GetMarketAnalysisAge` (0 callers; they reference the removed methods so must go for compilation).
- Remove `knowledge.MarketSnapshot`, `knowledge.MarketListing`, `knowledge.MarketAnalysis`, `knowledge.PriceTrend` types (after consumers move to `pkg/market` types).
- Drop implementations from `SQLiteKB` (`sqlite.go`, `analytics.go`) and `MemoryKB` (`memory.go`).
- Update test mocks: `pkg/galaxy/graph_test.go` `mockKB`, `pkg/knowledge/memory_catalog_test.go`, and any other `knowledge.Base` mocks.
- Add a knowledge migration that `DROP`s `market_snapshots`, `market_listings`, `market_analyses`, `price_trends`.
- **Keep** `base_market`, `base_services`, demand-ledger tables, and their methods.

### `#4` DB-path cleanup

- `pkg/market.DefaultConfig()` default path → relative `data/market.db` (mirrors `knowledge` default `data/spacemolt-knowledge.db`), not the hardcoded `$HOME/spacemolt/spacemolt/data/market.db`.
- Add a `--market-db-path` flag (default `data/market.db`) to `cmd/tools/play_as`, `cmd/auto-craftsman`, `cmd/auto-explorer`, and the worker/overmind binaries that capture market data.
- Replace play_as's lazy global `Open(DefaultConfig())` with a single Collector constructed at startup from the flag and injected.

## Data Flow (after)

```
view_market / update_market / resident capture
        │  game.MarketListing
        ▼
  market.CaptureFromClient / WriteSnapshot ──► data/market.db (market_orders, market_ohlcv, analyses)
        ▲                                            │
        │                                            ▼
agent decision paths (market_refresh,        GetLatestSnapshot / GetLatestAnalysis / HasSnapshotToday
 profit_selector, agentstate, auto-explorer) ◄──────┘
```

Knowledge DB retains: systems, POIs, bases (+ base_market/base_services), demand ledger, experiences, catalogs, player state.

## Error Handling

- Collector open failure at startup is fatal for binaries that require market capture (fail fast, like the knowledge DB today); play_as logs a warning and disables market commands if the path is unwritable (mirrors current knowledge-DB-optional behavior).
- Read methods return `(nil, nil)` when no snapshot exists (matches current `GetLatestMarketSnapshot` semantics) so callers' freshness checks keep working.
- `WriteSnapshot` no-ops on empty order lists (already the case for `CaptureFromClient`).

## Testing

- TDD per slice. `pkg/market`: unit tests for the new read methods (`GetLatestSnapshot`, `HasSnapshotToday`, `GetMarketItems`) and the `analyses` table, following existing `pkg/market` test patterns (temp DB per test).
- After the interface change, run `go test ./...` (not just `go build`) — removing `knowledge.Base` methods breaks mocks that `go build` alone won't surface (per the GameClient-interface-mocks lesson).
- Verify the resident worker capture path writes to `market.db` and the decision paths read it back end-to-end (a small integration test reusing `pkg/market`'s existing integration test harness).

## Migration / Sequencing

1. Add `pkg/market` read methods + `analyses` table (additive, no consumer change yet).
2. `#4` path cleanup (relative default + flags + injected construction).
3. Rewire writers (`pkg/worker`, `pkg/agent/market_capture`) to the Collector.
4. Rewire readers (`agentstate`, `market_refresh`, `market_analysis`, `profit_selector`, `auto-explorer`).
5. Remove market methods/types from `knowledge.Base` / `SQLiteKB` / `MemoryKB` + mocks; add drop migration; delete dead `price_trends`.
6. `go test ./...`, integration check.

Each step keeps the tree green. Steps 1–2 are safe to land independently.

## Open Items (follow-on, not this work)

- **Step 2:** add `update_market` to `data/overmind/roles.yaml` resident schedule so the fleet populates `market.db`.
- **Step 3:** arbitrage detector (other team).
- Demand-ledger consolidation into `market.db` (deferred slice).
