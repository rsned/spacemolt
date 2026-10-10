---
name: project_haul_fleet_rehull_2026_10
description: "Re-hulling the haul fleet 2026-10-10: congregation 1900 cargo costs ~6k (3.0 cr/unit) so hulls are now CONSUMABLE; 74% of big-hull profit comes from trips >65 units"
metadata:
  type: project
---

**The embargo in [[project_haul_fleet_hull_attrition]] is obsolete on its own
terms.** It said do not re-equip because replacement freight hulls cost
130k-217k and would feed the kill-zone grinder. On 2026-10-10 `congregation`
(1900 cargo, tier 1, shipyard_tier 0, piloting 0) is listed at **5,657-7,099
cr = 3.0 cr per unit of cargo**. A loss is now noise, not capital.

## Realized economics settle the bookCap argument

`bookCap = ceil(srcUnits/cargoCap)` (pkg/worker/haul.go:570) is real — a
bigger hull gets fewer concurrent book slots — but it does NOT offset the
hold. From `market.db` `haul_results`, 21 days to 2026-10-10:

| hull | cargo | profit/trip | profit/jump |
|---|---|---|---|
| shard | 60 | 7,978 | 517 |
| cobble | 75 | 10,802 | 634 |
| prayer | 540 | 18,186 | 1,078 |
| junk_convoy | 1350 | 21,422 | 1,403 |
| congregation | 1900 | **29,033** | **1,935** |

**The decisive cut: across the three big-hull agents, 74% of profit came from
trips ABOVE 65 units** (buckets: <=65 → 26%, 66-100 → 6%, 101-540 → 27%,
541-1900 → 39%). A small hold does not earn a proportional share; it earns a
quarter. Trip size is heavy-tailed and the money is in the tail.

**Do NOT rank hulls on credit-balance deltas.** I tried that first and it
appeared to show the big hulls LOSING money. It is confounded by strandings,
insurance payouts and gift/recapitalization flows. `haul_results` is the only
clean source.

## Beware the stale-position trap

Haulers move constantly. An assignment precomputed from a status snapshot is
wrong by the time the agent drains — trader-4 was 1 jump from a listing at
plan time and 11 jumps away at execution. **Assign from the live roster at the
moment of execution**, one agent at a time.

## Free upgrades exist in storage

`agent_hulls` (assets.db, join `agents` on player_id — NOT agent_id) shows
idle hulls. `deeprock_harvester` is **400 cargo** and salvager-2 + salvager-8
each own one idle; `excavator` is 150 and salvager-1/2/8 + trader-7 own those.
Switching is free. Check storage before buying.

## congregation tradeoffs

0 module slots (no cargo expanders, no defenses), 100 hull, 0 shield, speed 1
(6 ticks/jump), scale 3 → 6 fuel/jump → **18-jump range against a ~15-jump
average trip**. Tight but proven: 4 already fly it. See
[[reference_prayer_class_freight_hulls]] — prayer (540, speed 3, 1,726 cr) is
the cheap fast alternative and captures ~59% of the distribution.

## OUTCOME 2026-10-10: 8 hulls bought, 47,449 cr total

| agent | was | now | paid |
|---|---|---|---|
| trader-2 | prospect 100 | congregation 1900 | 6,399 |
| salvager-8 | shard 60 | congregation 1900 | 5,790 |
| salvager-9 | cobble 75 | congregation 1900 | 6,813 |
| trader-4 | theoria 70 | congregation 1900 | 5,665 |
| salvager-7 | shard 60 | congregation 1900 | 7,043 |
| salvager-5 | threshold 65 | congregation 1900 | 7,043 |
| trader-9 | cobble 75 | congregation 1900 | 7,008 |
| salvager-6 | threshold 65 | prayer 540 | 1,688 |

Fleet went from 4 congregations / 12 small holds / 9,865 total cargo to **10
congregations / 4 small holds / 23,160 total cargo**. Spend was 0.009% of the
fleet's 514M. Mechanics in [[reference_buying_a_hull_by_hand]].

**Still on starter holds:** salvager-1, salvager-2 (prospect 100), trader-7,
trader-8 (shard 60). No congregation listings remained. `commission_ship
congregation` is open: shipyard_tier 0, no skill/piloting gate, build_time 320
ticks, inputs = aluminum_sheet 50, cargo_container 12, flex_polymer 14,
fuel_tank 5, hull_plating 3.

**Cost of the operation:** the goldcrest gate had to be re-raised first (it sat
at 3 against a threshold of 5, so it was filtering nothing) and killing the
orphan supervisor unlinked haul.sock, forcing a full haul overmind relaunch —
see [[reference_duplicate_haul_overmind_corrupts_status]].

## base_value is NOT a market price — roughly 10x the real bid

Pricing explorer-8's mined stash I read `items.base_value` and overstated it
by ~26x. darksteel_ore base_value 350 vs best live bid **35**; most stations
bid 2. Always price from `market_buy_orders.price_each` (latest row per
station/item) AND sum `quantity` — the books are shallow, so depth caps the
realizable total far below units x best bid. 27,816 units of exotic ore had
only ~9,250 units of demand above 2 cr (~350k realizable, not 9.26M).

Crafting did not help: `forge_darksteel_plating` (2 ore -> 1) has ~536 units of
plating demand, worse than selling ore raw; `temper_crimson_fury_alloy` burns
1 titanium_alloy per unit ([[project_titanium_alloy_war_demand]]) for a book
that bids 1-4 units at a time. Check the OUTPUT's book depth before crafting.
