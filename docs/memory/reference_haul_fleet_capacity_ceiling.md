---
name: reference_haul_fleet_capacity_ceiling
description: "Measured answer to 'should we add more haulers?' — 21 haulers already saturate the fat arbitrage tier; adding agents earns 220 cr/jump junk. Don't expand the haul pool."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2a5c2a37-a408-4c0d-9aa8-9ecda2d08824
---

**Do not add haulers to a second haul pool.** Measured 2026-07-16 against `market.db` (`arbitrage_opportunities` + `haul_results`). The haul economy is a fixed, small pie and the existing ~21 haulers already saturate the profitable end of it.

**Efficiency collapses off the fat tier** (realized, from `haul_results` joined to the source opp):

| predicted net-of-fuel | realized cr/jump | share of available pool |
|---|---|---|
| 20k+ ("fat") | **3,057** | tiny (8 of 327 when saturated) |
| 5k–20k | 680 | ~17% |
| 1k–5k | **220** | ~80% — this is what a new hauler would eat |

- **The fat tier is already oversubscribed.** In steady state a 15-min scan surfaces only **4–20 distinct 20k+ gaps (~12 avg)** against 21 haulers. Proof they're saturating it: claimed opps average ~33.6k net while the available *residue* averages ~4.1k — haulers strip the fat instantly and leave junk.
- **A big fat count means the fleet is BROKEN, not that there's headroom.** After the 10h scanner outage the first fresh scan showed 66 fat gaps — that's a backlog from nobody arbitraging for 10h, not steady-state supply. Don't read a fat pool as a reason to add haulers; read it as "haulers aren't working."
- **Marginal returns are already falling at constant fleet size**: avg realized/haul slid 44k (07-11) → 38k (07-10) → 25k (07-15) → 21k (07-16) with ~21 agents throughout. The pie is shrinking.
- **Scanner predictions are ~3x optimistic**: realized = **34.4%** of predicted net-of-fuel. Discount any `gross_profit - fuel_cost` figure accordingly.
- **Fat gaps concentrate in ~4 items** (power_cell, structural_frame, liquid_oxygen, processing_core), so extra haulers would self-compete and close their own spreads faster.
- **The scout/coverage lever is closed too.** The universe is **40 stations** and the 44 marketbots already cover ~31 of them in live opportunity data. There is no meaningful market left to survey, so idle agents don't pay as marketbots either.

Healthy baseline for comparison: ~20 hauls/agent/day, ~419k cr/agent/day, ~23 hauls/hour fleet-wide.

**Implication for the ~50 idle agents** (explorers/miners/engineers/fighters): hauling is not where they pay. Any expansion needs a revenue source that isn't this fixed 40-station spread pie.

Related: [[project_arbitrage_net_of_fuel]] · [[reference_overmind_launch_commands]] (the scanner is unsupervised — a dead scanner mimics "no opportunities") · [[reference_market_ohlcv_orderbook]]
