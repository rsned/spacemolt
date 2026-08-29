---
name: project_mission_category_coverage
description: "Mission-runner v1 only handles single-leg deliver_item; breakdown of rejected mission categories (2026-07-23) to prioritize what to build next — trade biggest (29.5%), smuggling 2nd (17%, needs treasure_cache XP unlock)"
metadata:
  node_type: memory
  type: project
  originSessionId: 86df3835-f10f-4c25-928d-7948d122ecd7
  modified: 2026-07-23T21:35:56.678Z
---

**Mission-runner v1 (`pkg/worker/mission_select.go`) only runs single-leg `type=="delivery"` `deliver_item` missions; every other category is rejected.** Breakdown from mission-learn overmind log, 6h20m window 2026-07-23 07:54→14:14 (the 42-worker fleet sitting near-idle).

**Rejection layers (by raw volume):** freight below-floor price-gate ~8,500/h + freight no-candidate ~6,000/h dominate (freight IS handled, just refusing unprofitable/empty — the [[project_freight_probation_bootstrap]] deadlock). Mission board: "no acceptable missions" ~12,600/h (per-poll summary), "no board entries here" ~1,200/h. Completions ~0/h (1 in the whole window).

**Rejected mission CATEGORY composition (3,084 per-entry `missions: skip <template_id>` lines; prefix = category):**
- **trade (buy→sell arbitrage) 29.5%** ← biggest unhandled bucket
- **smuggling (contraband courier) 17.0%**
- faction 12.7% · survey/exploration 10.2% · mining/extraction 7.4% · combat/hunting 6.0% · procurement 5.8% · story/named one-offs 5.1% · tutorial/intro 3.7% · market_participation 2.7%

**"What's next" ranking (value/effort):**
1. **TRADE first** — biggest (29.5%) AND cheapest: it's buy-low/sell-high arbitrage, which the HAUL fleet already does ([[reference_haul_fleet_capacity_ceiling]], [[reference_trading_missions_not_market_validated]]) — mostly routing arbitrage logic through the mission wrapper. NOTE: trade missions are NOT market-validated by the server → must price-gate (existing depth-aware gate).
2. **SMUGGLING second** — needs a per-agent one-time XP unlock (user, 2026-07-23): agent travels to **treasure_cache** station, runs the initial smuggling chain mission → grants smuggling XP → **level 2 → can accept smuggling missions ANYWHERE**. Same bootstrap shape as [[project_freight_probation_bootstrap]]. **Chain ENTRY mission = "No Questions Asked" (template `no_questions_asked`) and it is COMPLETELY SAFE to complete in empire space** (user, 2026-07-23 — no customs/contraband risk on the onboarding run). It already appears on boards + is currently rejected (skip count 5 in the window). Log confirms treasure_cache is a smuggling origin (`smuggling_courier_treasure_cache_trading_post_*`). Smuggling deliver-shaped missions carry warnings (contraband) → currently rejected by `has warnings` guard even if L2. Customs risk only if you STOP 10 ticks at a border ([[reference_customs_mechanics]]) → continuous-travel couriers viable.
3. Rest (faction/survey/mining/combat) smaller + each needs distinct capability (combat = different capability entirely).

Relates to [[project_mission_learning_pool]] [[project_freight_probation_bootstrap]] [[project_marketbot_freight_demand_scan]] [[reference_mission_board_wire_shape]] [[reference_v0536_wildlife_combat]] (combat category).
