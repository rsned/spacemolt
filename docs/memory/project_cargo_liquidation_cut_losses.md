---
name: project_cargo_liquidation_cut_losses
description: "FUTURE FEATURE — 'Find Place To Dump Cargo Nearby to Minimize Losses': shared worker helper to liquidate stuck cargo into the best reachable bid (haulers beaten to destination; mission-runners with underwater/stranded procurement cargo)."
metadata: 
  node_type: memory
  type: project
  originSessionId: 853398d3-bb3c-4431-bf12-2caa11653a5d
  modified: 2026-08-15T15:43:57.973Z
---

**Requested 2026-07-19 (user). NOT STARTED.** A shared cut-losses liquidation helper so a worker holding cargo it can't profitably move recovers the most capital possible instead of stranding on it.

**Two trigger cases (same need):**
1. **Hauler beaten to destination** — claimed an arbitrage book, but the destination buy-order was eaten by a faster player before arrival (book collapsed / claim invalidated). Holding resale goods with no profitable sink. See [[reference_haul_fleet_capacity_ceiling]] (deep orders eaten within a scan window is the norm), [[project_haul_book_coordination_followups]].
2. **Mission-runner underwater/stranded procurement cargo** — took a TRADING mission the server never market-validated ([[reference_trading_missions_not_market_validated]]), overpaid for inputs, can't profitably deliver. Live casualty: fighter-4 (295 units, 0cr, fuel-dead at Procyon, 2026-07-19).

**Existing narrow precursor:** home-base cargo-shed (`fa825da`) — sells ONLY ore, ONLY at the agent's own `home_base`. This feature generalizes it: any cargo, nearest profitable bid at ANY reachable station.

**Design sketch (helper shared by `pkg/worker/haul.go` + `mission.go`, e.g. `pkg/worker/liquidate.go`):**
- BFS nearby stations within a fuel/jump budget from current system (navigation.JumpGraph).
- For each held item, look up real best **BID** (buy orders) in market.db across those stations — bids, not asks (we're selling). Watch: market_ohlcv is order-book, best-bid side.
- Rank options by net proceeds = `bid × qty − fuel/time cost to reach`; sell into the least-bad one to recover capital (accept a loss vs cost basis; goal is minimize loss, not profit).
- **No reachable profitable bid → DEPOSIT into station cargo hold, NEVER jettison** (user directive 2026-07-19). `DepositItems`/`DepositAllItems` at the current (or nearest) station parks the goods with value intact for a later SWEEP, instead of destroying them. Jettison only if storage is unavailable AND cargo must be cleared.
- **Station storage is per-station** (`ViewStorageAt(stationID)`, `WithdrawItems` at that station) — a sweep must return to the SAME station to reclaim. Record (station_id, item, qty, deposited_at) to a swept-cargo ledger; dispatch retrieval when a bid materializes at/near that station (or fold into a hauler/mission pass routed nearby). Watch storage rent/fees + capacity limits.
- Hauler hook: invoke on book-claim invalidation / destination collapse instead of stranding. Mission hook: invoke when mission is abandoned/undeliverable or when fuel-stranded with held cargo.
- Broke-agent interaction: a fuel-stranded broke worker (0 cr) can't buy fuel to reach a bid — pairs with fuel-rescue ([[project_rescue_pipeline_bugs]]); liquidation runs after fuel is restored.

**When building:** use superpowers:brainstorming first (feature design), then SDD. Price source = market.db best-bid (see [[reference_market_ohlcv_orderbook]], [[project_demand_ledger]]).

**Station Manager standing bids = guaranteed liquidation floor (user, 2026-08-15):**
player stations carry always-on Station Manager buy orders — e.g. grand_exchange
(haven) perpetually bids 975 × 1000 for trade_authenticator. These are REAL
external buyers (station-side, not our own vacuum bids — distinguish from
[[reference_craftsman1_vacuum_bid_economics]]), so a liquidation helper always
has a nonzero-floor sink at home before falling back to DEPOSIT. Rank them like
any other bid; they typically sit far under real player bids (975 vs 7,000+ for
trade_authenticator), so they're the "done chasing" option, not the default.
