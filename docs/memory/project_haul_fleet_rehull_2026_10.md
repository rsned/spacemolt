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
