# Drone offtake, then fit every marketbot with drones

**Status:** proposed, awaiting operator approval (2026-10-31)
**Related:** `docs/superpowers/specs/2026-08-22-fleet-drone-refit-design.md`
(the August refit), `.beads/003-production-cost-model.md`, the Voss contraband
plan, the Conjunction Junction production lines.

## Why

The ten drone marketbots (010–019) mined about **20,700 units a day each** in
September (66,604 → 480,240 units in two days after the 09-08 deploy), at drone
control 35–37. **Drone control adds +1% yield per level** (operator,
2026-10-31), so yield ∝ 1 + 0.01 × level. They are now at drone control 92–98,
so each mines about **29,700 a day**, about **297,000 a day** for the ten. That
already outruns our use. Several have hit the 100,000-per-item storage limit, so their
drones are idle:

| Bot | Station (system) | To CJ | Capped or near cap | Other large stock |
|---|---|---|---|---|
| 012 | Stellar Leap (tiaki) | 26 | nickel_ore 100,000 | |
| 019 | Hex Wellspring (zosma) | **1** | nitrogen_ice 99,992, water_ice 96,355 | |
| 011 | The Hush Chamber (diphda) | 25 | silicon_ore 99,458 | neodymium 50k |
| 015 | Ironhearth (ironhearth) | 29 | carbon_ore 99,230 | vanadium 66k, rhodium 34k, palladium 33k |
| 016 | Echo Protocol Hub (lilii_borea) | 26 | — | argon 253k, neon 207k, **LH2 158k**, nebula 84k |
| 017 | Plasma Echo (gsc_0010) | 27 | — | argon 252k, neon 210k, **plasma 84k, chlorine 83k** |
| 018 | Carnegie Hall (winterhold) | **11** | — | argon 243k, neon 196k, **LH2 148k**, xenon 81k |
| 010 | Event Horizon Relay (alzirr) | 29 | — | copper_wiring 87k, steel 78k |
| 013 | The Whisper Point (gsc_0034) | 26 | — | copper_wiring 86k, steel 70k |
| 014 | Hex Wellspring (zosma) | **1** | — | lithium 69k, copper_wiring 30k, steel 23k |

**Energy crystals are no longer short.** We hold **166,222** (fighter-2 66,688 at
Cargo Lanes; ~54,600 at BD+20 2457 with fighter-5/6/7 and random-9). The
August campaign was 3,607 short.

Order: **offtake first, then more drones**. New drones on a full storage stall
like 012 has.

## Phase A — offtake (no spend beyond fuel)

**A1. Hex Wellspring (1 jump) — trader-2's home.**
- Add consign rules on marketbot_014 (steel_plate, copper_wiring) and
  marketbot_019 (water_ice, nitrogen_ice), each to trader-2.
- trader-2's existing hourly `deliver steel_plate` / `deliver copper_wiring`
  from Hex Wellspring (`8f8c70ee…`) then has stock to carry. They have found 0
  since 10-30.
- Add ice delivers only if a line uses ice: moonshine needs water_ice at Voss
  (12 jumps from Zosma).
- **Check first:** 014 and 019 must have earned 1,000 credits, or their gifts
  fail with `gift_not_unlocked`. Fix: the `market_participation_buying`
  mission recipe.

**A2. Carnegie Hall (11 jumps) — liquid hydrogen.**
- 018's 148k LH2 is about 1.48M fuel for the Conjunction Junction tank.
- Wait for the Fuel Tank Expansion (+50,000): the tank holds 5,000 today, so
  LH2 only helps once there is room.
- Then a Congregation hauler: 1,900 LH2 per trip, about 19,000 fuel.

**A3. The far bots (25–29 jumps).**
- Hauling bulk ore 25–29 jumps with a 110-fuel Congregation is marginal, so use
  three cheaper levers first.
- **(a) Retarget drones.** Point each capped bot's DroneLang script (one
  template per site, `data/scripts/drones/*.ds`) at an uncapped item its
  station's POIs carry, so the drones keep working.
- **(b) Sell surplus locally.** A shopkeep rule on each bot for its capped ore,
  floored with bead 003's cost model. This turns idle stock into treasury
  income.
- **(c) Haul only what a line needs, ride-along where possible:**
  - 015's carbon goes with explorer-3's platinum run (same station, same
    route).
  - 017's chlorine and plasma gas go to Voss for the contraband line.
  - 011's silicon goes to Conjunction Junction for circuit boards.
  - These are the targets for the long-range hauler (Junk Convoy, 720 fuel)
    once the shipyard exists.

## Phase B — fit the remaining marketbots

**Scope.** About 70 residents without drones: 67 in mb-fleet, minus the 10
drone bots, plus the 13 new marketbots 021–033 after their unlock.
- Each gets 1 `advanced_drone_bay` + 5 `mining_drone`.
- That is ~70 bays + ~350 drones, about **40% of the August campaign**
  (175 bays + 800 drones, 5,679 craft runs).

**Re-costed from the August spec (scaled):**

| | August (175 + 800) | This (70 + 350) |
|---|---|---|
| craft runs | 5,679 | ~2,300 |
| energy_crystal | ~9,700 total (3,607 short) | **~4,200 (2.5% of the 166k we hold)** |
| polymer_extruder runs | 491 | ~200 (own facility at 8 labour/run, not public at 9,000) |

**Where to craft.** Conjunction Junction's own facilities: no rent, labour only.
Bead 003 prices each stage once the drones' BoM is costed.

**Fit constraint.** The bay is a **utility-slot** module (cpu 12, power 15).
Starter hulls (Prospect, Cobble) may not have the slot or the power, so check
each marketbot's hull before crafting its bay.

**Ramp.** A new bot starts at drone control 0, so it yields about **15,200 a
day**. It rises toward about 30,400 at level 100. In September the cohort gained
+18 levels in two days, so expect the 90s in one to two weeks.

**Target choice.** Point each new bot's drones at the ore its station's lines
need. At full skill, ~70 more bots add roughly **2.1M units a day**, far beyond
(for example) the 80,000 iron ore a day that would run the Iron Refinery at
capacity. So aim each bot at a specific need, not maximum volume.

## Transport

Raw drone output far exceeds the haul fleet.
- A Congregation (1,900 hold) on a 25–29 jump route moves about 23,000 units a
  day.
- Moving everything raw would take about 100 hulls. The haul fleet has 21.

Two levers:
1. **Refine at the source.** An L1 facility beside a drone bot cuts the volume
   before it moves:
   - iron ore → steel, 5:2
   - carbon + silicon → circuit boards, 14:3
   - chlorine gas → compound, 8:3

   The facility is rented at that station or built at a nearby player station.
2. **Bigger hulls from our own shipyard:**
   - Congregations (L1 yard) for short routes
   - Junk Convoys (Drydock, 720 fuel) for long routes
   - Conglomerates (2,400 hold, 600 fuel) or Frankenhaulers (2,640 hold) for
     the bulk routes. These need Fleet Yards plus a 20M license.

## Approvals

1. Phase A1 consign rules on 014/019 (no spend).
2. Phase A3 drone retargeting and local shopkeep floors (no spend).
3. Phase B: spending ~4,200 reserved energy crystals and ~2,300 craft runs of
   labour. The crystals are reserved for the drone project, so this needs an
   explicit yes.

## Open questions

- Which uncapped items should each capped bot's drones switch to?
- Sell the surplus locally, or keep stockpiling for future lines?
- Phase B order: drone bots near production first (Hex Wellspring,
  Conjunction Junction's neighbours), or everyone at once?
