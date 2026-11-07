# Conjunction Junction station defense: design

Date: 2026-11-07. Station: Conjunction Junction (CJ), base `c976fa7df53c87f9616165d7d411b41a`,
CRFT's home station in Cloverfield.

## Why

CJ is one of 32 stations with no real defense. Nothing has attacked it; this gets ahead
of an attack. A wrecked station has to be repaired before it works again, and the repair
costs about 30% of each facility's original cost, in credits and in that facility's
build materials. Those materials must be at the station. Without them CJ stays down,
with all 19 facilities, including life support and power, until someone hauls them in.

## Goals

1. **Prevention.** Make CJ a harder target than the undefended stations. It does not
   need to be untouchable.
2. **Readiness.** Keep a repair reserve at CJ, so a wreck is a quick recovery, not a
   long outage. The reserve also covers other rainy-day needs.
3. **Power for a fight.** Shields and guns drain power and battery in combat, so CJ
   needs supply and stored charge to keep them running.
4. **Self-supply.** Defense upkeep (coolant, power cells, armor plate) is made from
   resources the fleet already produces, not bought.

## Non-goals and anti-goals

- Not defending the other 31 stations.
- Not an automatic builder. **Every CJ facility build and upgrade needs the operator's
  approval, one at a time.** A plan step that would spend credits or materials on a
  build without that approval is a failure even if it works.
- The CRFT treasury floor of 1,000,000 credits stands.
- **Energy crystals are reserved.** No recipe that consumes them (for example
  `build_power_cell`) is part of this.
- Do not consume the repair reserve for anything else without the operator's say.

## Current state (2026-11-07)

- 19 faction facilities at CJ, about 8.0M credits in catalog build cost at their
  current level (upgrades below their level are not counted, so the real figure is
  higher). One defense facility: `point_defense_battery` L1. `get_base` shows hull
  60,000, shield 12,000, `weapon_dps` 30.
- Power: supply 20 from the `stellar_collector_grid`, **battery capacity 0**, current
  draw 0.
- Life support: supply 12, demand 7 (one plant), so 5 spare. Each cell lab draws 1.
- **Guns need ammo, and CJ holds none.** Each gun names an `ammo_item`:
  `ferrous_slug_case` (point defense battery), `armor_piercing_rounds_box` (turret),
  `fragmentation_rounds_box` (heavy battery). Their degraded text says the magazines
  are empty, so the current `point_defense_battery` likely does nothing in a fight.
- CRFT treasury: 3,975,827 credits at the last capture (11-07). The planned builds cost
  about 5.8M plus the reactor and labs, against a 1M floor.
- Faction storage at CJ already covers most of the 30% repair reserve (for example
  42,926 `steel_plate` against about 15,000). Shortfalls: `ceramite_plating` 450 needed
  vs 1, `fuel_tank` 240 vs 213, `copper_wiring` 135 vs 20, `water_recycler` 60 vs 33.
- A Storage Extension bucket named `Maintainence` exists (empty, 100,000 per item,
  1,000 package cap).
- Drone-fitted marketbots now run hourly crafts that produce the inputs CJ needs:
  gas bots make `purified_argon`, `ionized_neon` and `liquid_hydrogen`; ice bots make
  `purified_water` and `liquid_nitrogen`.

Sources: `data/game-api/20261106/catalog_facilities.json`, CJ events in
`data/stations/cloverfield/events.jsonl`, faction storage in `data/assets.db`.

## Decided by the operator

**Defense order.**
1. Upgrade the point defense line to its longest reach: `point_defense_battery` (L1,
   180k, power 2, 1 coolant per cycle) → `station_defense_turret` (L2, 420k, power 4,
   2 coolant) → `heavy_defense_battery` (L3, 1.4M, power 8, 3 coolant).
2. `reinforced_plating` (700k): armor that cuts incoming hull damage by its amount,
   with automatic repair after combat. No power draw. Upkeep 1 `armor_plate` per cycle;
   repairs use `armor_plate`.
3. `shield_projector` (900k, power 10, 4 `power_cell` + 2 coolant per cycle) for more
   shield, and `fire_control_center` (1.2M, 2 `circuit_board` per cycle) for better
   weapon hit rate, once power cells flow.
4. Battery storage (`battery_bank` or `capacitor_bank`, 500k each, 1 `power_cell` per
   cycle) so shields and guns run through a fight.

**Power supply.** `bunker_fed_reactor` (500k, +12 supply, 55 fuel per cycle) burns the
fuel CJ already makes. Supply 20 → 32. This was already chosen on 2026-11-01.

**Power cells are made at CJ**, not bought: an `argon_cell_lab` (63k, 3
`purified_argon` + 2 `circuit_board` + 2 `copper_wiring` → 2 `power_cell`) and/or a
`neon_cell_lab` (98k, same with `ionized_neon`).

**Coolant** is made from ice-bot output: `synthesize_coolant_fluid`, 3
`liquid_nitrogen` + 2 `purified_water` → 3 `coolant_fluid` (hand recipe).

**Haulers carry it.** Haulers pick up the bots' gas and ice products and deliver them
to CJ. Three of the gas stations are in Nebula space, 3–4 jumps from CJ, so the runs
are short.

**Repair reserve lives in a bucket.** Rename the `Maintainence` bucket to
`station_supplies` (or similar) and stockpile the repair and rainy-day items there.
The API lets faction builds and upgrades source their materials from a named bucket.

## Numbers that shape the order

- A cycle is 100 ticks; 86.4 cycles a day.
- `bunker_fed_reactor` needs **400 `power_cell`** to build, and `battery_bank` /
  `capacitor_bank` need 600 each; CJ holds 53. So power cells
  must flow before the reactor, and the cell lab comes first.
- A `shield_projector` alone uses about 346 `power_cell` a day, which is about 520
  `purified_argon` a day (about 865 raw `argon_gas`).
- `heavy_defense_battery` uses about 260 `coolant_fluid` a day; CJ holds 309.
- `reinforced_plating` uses about 86 `armor_plate` a day; CJ holds 1,122.
- Power draw at full build: heavy battery 8 + shield projector 10 + cell labs 1 each,
  against supply 32 after the reactor.
- Life support: production facilities draw life support at CJ, and the 11-01 notes call
  life support the bottleneck. New production facilities (the cell labs) must fit.

## Left to the builder

- The exact build order inside the operator's defense order, given the power-cell
  dependency above, life support headroom and treasury.
- Which cell lab(s): argon, neon or both.
- How the haul pickup is scheduled, which haulers run it, and the delivery destination
  (faction main store or the bucket).
- Where coolant is crafted (CJ or at the ice bots) and by whom.
- How `copper_wiring` is supplied. CJ has 304,136 copper ore, but its copper drawing
  mill makes `draw_copper_piping`, not wiring, so wiring has no source at CJ yet.
- How the repair reserve is measured and topped up, including counting upgraded levels.
- How to rename the bucket (the API exposes facility custom names; unconfirmed for
  buckets).
- How progress is shown to the operator.
