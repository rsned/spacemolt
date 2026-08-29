---
name: project_market_intelligence
description: "Market Intelligence (pkg/market) — single source of volatile market data; MVP merged 2026-06-21, snapshot/analysis CONSOLIDATION merged 2026-06-22 (knowledge market tables dropped). Remaining: roles.yaml wiring + arbitrage detector"
metadata: 
  node_type: memory
  type: project
  originSessionId: ab1c49b4-60dc-40cf-b763-b03d8d7b4831
---

`pkg/market` — isolates volatile market data into its own SQLite DB (`data/market.db`, WAL + busy_timeout via DSN) so it doesn't churn the main knowledge DB. **MERGED to main 2026-06-21** (merge `944b899`, branch `feature/market-intelligence`; was 14 ahead / 22 behind, forked mid-Plan-B at `cccf208`; merged clean, no conflicts, build+tests green). Contents: item/station catalogs, `market_orders` fact table, hourly **OHLCV** aggregation, `arbitrage_opportunities` table, query helpers, `cmd/tools/market-stats` CLI, and a manual `update_market` play_as command (captures current station order book via ViewMarket→CaptureFromClient).

**CONSOLIDATION (the user's #1) — DONE 2026-06-22** (merge `23c4571` on main, branch `feat/market-data-consolidation`, 15 commits; built via subagent-driven-development, 9 tasks + per-task reviews + Opus whole-branch review). pkg/market is now the SINGLE source of volatile market data. Spec `docs/superpowers/specs/2026-06-21-market-data-consolidation-design.md`, plan `docs/superpowers/plans/2026-06-21-market-data-consolidation.md`. What changed:
- Snapshots + LLM analysis + cross-station best-prices moved to pkg/market (`GetLatestSnapshot`, `HasSnapshotToday`, `FindBestPrices`/`BestPrice`, `StoreAnalysis`/`GetLatestAnalysis`, `analyses` table, shared `OrdersFromListings` converter).
- ALL writers (worker `KBUpdateStation`+`WorkerDispatch.Market`, agent `CaptureMarketData`, auto-explorer) and readers (agentstate, `RefreshMarketData`, `RefreshMarketAnalysis`, auto-craftsman profit_selector, unified server `EnrichedStateFactory` closure) rewired to `*market.Collector`.
- `cmd/tools/view-market` repointed at market.db (was reading knowledge tables; user chose repoint over retire). prices→OHLCV, arbitrage computed from market_orders.
- Knowledge market surface REMOVED from knowledge.Base + impls + mocks; tables `market_snapshots`/`market_listings`/`market_analyses`/`price_trends` DROPPED via **migration 46** (fresh cutover, no data migration). Dead code retired: GetPriceHistory/PricePoint, AnalyzePriceTrends, 4 unused Should*/Get*Age helpers.
- `#4` hardcoded DB path FIXED: `DefaultConfig()` now relative `data/market.db`; `--market-db-path` flags; all binaries open with `WAL: true` (whole-branch review caught play_as+auto-craftsman missing WAL → fixed).
- KEPT in knowledge DB (out of scope): demand ledger (`demand_*.go`, market_buy/sell_orders + history), `base_market`/bases, import-base-data.
- **Pushed to origin/main** 2026-06-22 (`944b899..23c4571`).

**Remaining follow-ups:**
1. ~~Fleet populates market.db (STEP 2)~~ — DONE 2026-06-22 (merge `751495e`, pushed). Dedicated `update_market` command in pkg/worker WorkerDispatch (ViewMarket prime + market.CaptureFromClient) + hourly entry in `data/overmind/roles.yaml`. Resident fleet now fills market.db when docked.
2. **Arbitrage detector (other team, #3)** — `arbitrage_opportunities` table exists, no producer yet.
3. **Demand ledger** still in knowledge DB (deferred slice) — could later consolidate into market.db.
4. **Non-blocking nits from final review** — DONE 2026-06-22 (merge `0d8894c`, pushed): cfg.Database.MarketPath override + cached DefaultConfig; stale MARKET_REFRESH.md rewritten to *market.Collector API; station-metadata Scan errors surfaced + errors.Is; auto-explorer skips fetch when mc==nil; deepened analysis + view-market tests.
