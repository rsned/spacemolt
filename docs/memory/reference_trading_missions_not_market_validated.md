---
name: reference_trading_missions_not_market_validated
description: Server generates TRADING/procurement missions WITHOUT consulting the market — reward and required qty do not reflect real acquisition cost. Client must price-check before accepting.
metadata: 
  node_type: memory
  type: reference
  originSessionId: 853398d3-bb3c-4431-bf12-2caa11653a5d
---

**Server behavior (player-reported, 2026-07-19): TRADING missions (procurement / "buy N of X, deliver to Z") are created WITHOUT market consultation.** The reward and required quantity are NOT validated against real market prices/depth. So a mission can demand buying an item whose cheap supply is thin/drained, at a reward that does not cover surge-priced acquisition. This is server-side and won't change — **the client is the only place to defend against it.**

**Live casualty:** mission-runner `fighter-4` — took a procurement mission, drained cheap local supply of the required item, broadened the buy to other stations at surge prices, sank ~132k credits (132,238 → 0) into 295 units of overpriced cargo, and stranded fuel-dead (3/240) at Procyon before delivering. Full hull → not a death; capital tied up in undeliverable/underwater cargo. See [[project_rescue_pipeline_bugs]] (rescued via assist-sol 2026-07-19).

**Required client-side guard (mission-runner admission gate), before ACCEPTING a trading mission:**
1. Resolve required item + qty; query real best-ask depth across reachable stations (market.db / GetReferenceAsk).
2. Estimate total acquisition cost incl. fuel to source+deliver; compare to mission reward.
3. Accept only if reward − cost ≥ margin AND enough cheap depth exists within a price ceiling. Otherwise skip.
4. Mid-execution: cap spend at the ceiling; if remaining supply is only surge-priced, ABANDON rather than overpay into a strand.

Relates to [[project_mission_learning_pool]] (mission-runner logic), [[project_demand_ledger]] / [[reference_market_ohlcv_orderbook]] (price source), [[project_haul_pnl_adjustments]].
