---
name: reference_prayer_class_freight_hulls
description: "Freight hull economics: compare hulls FITTED, not by base stats — cargo_expander_iii is +100 cargo/slot, so slotless hulls (Prayer 540) lose to slotted scale-1 hulls (floor_price 400+100=500 at 1 fuel/jump); scale^1.5 makes afterburners ruinous on big hulls"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 9acefb51-d097-42a4-bd75-27e0bb652f32
  modified: 2026-08-15T16:24:06.905Z
---

**Never rank freight hulls on base stats — rank them FITTED** (operator
correction, 2026-08-15). Utility slots are cargo:

- **`cargo_expander_iii`: +100 cargo per module**, 3 CPU / 3 power, ~2,000 cr.
- **`afterburner_i`: +1 speed for −10% fuel capacity**, 2 CPU / 3 power.
  `afterburner_ii` is +2 speed for −30% cap — **not worth it**.
- Both are `slot: utility`, so every afterburner costs 100 cargo. CPU/power
  budgets are generous; **utility slots are the binding constraint.**

**Scale is the dominant fuel term.** With fuel/jump `= ceil(scale^1.5 × speed)`
and `jumpTicks = max(1, 7−speed)` ([[reference_ship_jump_time_and_fuel_formulas]]),
scale 1 burns 1×speed while scale 4 burns 8×speed. So an afterburner on a
scale-4 hull is ruinous: Exosphere fitted 7×expander = 1,480 cargo with 40
jumps of range; swap one expander for an afterburner and range collapses to
**18 jumps** for a single tick of speed. Afterburners only make sense on
scale-1/2 hulls, and even there they compete with +100 cargo.

**Fitted efficiency (cargo per fuel-per-jump), best fit shown:**

| hull | fit | cargo | ticks/jump | range | cargo/fuel |
|---|---|---|---|---|---|
| **floor_price** (scale 1) | 1×EXP | 500 | 6 | **120 j** | **500** |
| eldorado (scale 5, pil 50) | 8×EXP | 4,400 | 6 | 50 j | 367 |
| conglomerate (scale 4, pil 30) | 5×EXP | 2,900 | 6 | 75 j | 362 |
| **congregation** (scale 3, **tier 1, pil 0**) | none | 1,900 | 6 | 18 j | 317 |
| **prayer** (scale 1) | **impossible** | 540 | **4** | 33 j | 180 |

**So the Prayer's real edge is SPEED, not efficiency**: 4 ticks/jump where
almost every other freight hull sits at 6. It is the fastest freighter in the
game and needs no fitting budget, which still makes it an excellent cheap
starter/arbitrage hull (~4k cr, tier 1, no piloting or reputation gate, scale 1
so no big-hull dock limits). But `floor_price` + one 2k expander carries
essentially the same load (500 vs 540) at **one third the fuel per jump and
3.6× the range** — for ~10k all-in. We already fly 2 floor_price; listed
2026-08-15 at 8,279 (Treasure Cache) and 8,459 (Gold Run).

**⭐ `congregation` is the sleeper: 1,900 cargo, tier 1, shipyard_tier 0,
piloting 0, no slots needed.** Nearly 4× a Prayer's load with no fitting cost
and no skill gate — the ceiling is its 18-jump range (110 fuel, scale 3).
**Not listed for sale anywhere in the 08-15 captures**; shipyard_tier 0 means
`commission_ship` should be able to build one. Worth pricing.

**Slotless design family** (cheap, tier-1, scale-1, fast, trading-themed):
Prayer 540 · Start Praying 500 · **Ledger 480 @ speed 4** (3 ticks/jump).
None can mine — a mine-and-haul loop needs utility slots (Junk Convoy 850/6
util ~181k) or two ships with a station-storage handoff.

**Fleet opportunity, NOT yet acted on.** ~100 active fleet hulls carry <150
cargo (20× cobble 75, 16× prospector 50, 14× drillship 100, 13× threshold 65,
10× shard 60). Re-hulling is cheap, but **check the interaction first**:
`bookCap = ceil(srcUnits/cargoCap)` grants a BIGGER hull FEWER book slots
([[reference_book_depth_is_the_real_haul_ceiling]]), so a capacity jump may cut
allocation instead of raising throughput. Model before buying.
