# Conjunction Junction defense, round 1: design

Date: 2026-11-08. Station: Conjunction Junction (CJ), base `c976fa7df53c87f9616165d7d411b41a`,
poi `862e07fa76571d0a76cab3cbaf4046e7`, CRFT's home station in Cloverfield.

This is the concrete build list for the first defense round. The standing design, its rules
and its background are in `2026-11-07-cj-station-defense-design.md`; everything there still
applies unless this document says otherwise.

## Why (operator, 2026-11-08)

Server v0.674.0 doubled pirate stronghold fleets (14 elite raiders + 24 raiders in 8 squads
per stronghold, 6 elites escorting the boss) and has them build the best hulls they can
afford. Server updates keep making pirates more aggressive and competent. CJ is the weakest
armed station on the public list: `weapon_dps` 30 (one L1 point defense battery) against
242 for most armed player stations. The goal is to stop being the softest target, not to be
untouchable.

## Goal: the round-1 target (operator)

At least **2 shields, 2 armor and 6 guns** at CJ. CJ already has 1 armor
(`reinforced_plating`) and 1 gun (`point_defense_battery` L1), so this round adds:

| Add | Count | Credits each | Build materials each | Power each | Upkeep each / cycle |
|---|---|---|---|---|---|
| `shield_projector` (L1) | 2 | 900,000 | 1,200 steel_plate, 200 superconductor, 300 power_cell | 10 | 4 power_cell, 2 coolant_fluid |
| `reinforced_plating` (L1) | 1 | 700,000 | 2,000 steel_plate, 240 armor_plate | 0 | 1 armor_plate |
| `point_defense_battery` (L1) | 5 | 180,000 | 400 steel_plate, 60 circuit_board, 4 targeting_computer | 2 | 1 coolant_fluid; ammo `ferrous_slug_case` |

**All five new guns are L1 point defense batteries** (operator). No turrets this round.

Power (operator: top up from the_banker and build a second collector grid):

| Add | Credits | Build materials | Power supply |
|---|---|---|---|
| `stellar_collector_grid` (second copy) | 900,000 | 2,000 steel_plate, 1,200 circuit_board | +20 (upkeep 2 circuit_board / cycle) |
| `bunker_fed_reactor` | 500,000 | 2,000 steel_plate, 400 power_cell | +12; burns `maintenance_fuel` 55 per cycle from the station fuel reserve that also refuels ships |

Supply goes 20 → 52. Draw with everything built: 2 shields × 10 + 6 guns × 2 + the existing
cell labs and other powered facilities. Read the live draw from `get_base` before the shields;
the 11-07 estimate was about 34.

Source for every number: `kb/data/snapshots/20261108/catalog_facilities.json`.

## Already built

`fire_control_center` (from the 11-07 first round) is already at CJ (facility
`cbaaa308c5d0c41d4e31aa40cfc1641c`), so it is not in this round's list.

## Non-goals (operator)

- **Battery storage** (`battery_bank` / `capacitor_bank`) is deferred to the next round.
- **L2 upgrades** are the next round too: after round 1 is complete, upgrade **2** of the
  point defense batteries to `station_defense_turret` (L2, 420k, +2 power each, ammo
  `armor_piercing_rounds_box`). The 52 supply already covers them.
- No L3 guns, no shield bastion, no bulwark plating.

## Rules that carry over (operator, standing)

- **Every facility build needs the operator's approval, one at a time.** Ask before each
  `facility faction_build`; never batch or pre-authorise. A build without that approval is a
  failure even if it works.
- CRFT treasury floor **1,000,000** credits. Consumable purchases ≤ 1M total.
- **Stock a facility's upkeep inputs at CJ before building it** (the Locker Bay went
  CRITICAL from missing flex_polymer).
- Energy crystals are reserved; no recipe that consumes them.
- craftsman-1 is the operator's own play_as account. Do not log into it from a tool.
  Builds are run by **miningbot_0000** (CRFT Officer, resident at CJ) via play_as, as with
  the alloy foundry and the first point defense battery.

## Repair reserve (operator)

This round builds from the full CJ stock. The 11-07 rule (keep ~30% of each facility's build
materials at CJ for repairs) is **topped up after round 1 is complete**, for the new
facilities too — it is not a gate on these builds.

## Money

| Item | Credits |
|---|---|
| 2 × shield_projector | 1,800,000 |
| 1 × reinforced_plating | 700,000 |
| 5 × point_defense_battery | 900,000 |
| stellar_collector_grid | 900,000 |
| bunker_fed_reactor | 500,000 |
| **Builds** | **4,800,000** |
| ~1,000 power_cell bought (see Materials) | ~290,000 |
| Labor (charged per build; small) | — |

CRFT treasury was 5,486,978 at the 2026-11-08 21:34Z capture. **the_banker tops up the
treasury before the first build** (operator decision). the_banker holds ~128M. Amount: my
call, **2,000,000**, which leaves about 2.3M after the round. Path used before:
the_banker `send_gift miningbot_0000 credits <n>` (the_banker docked) →
miningbot_0000 `faction_deposit_credits <n>` at CJ.

## Materials at CJ (faction storage, 2026-11-08 capture)

| Material | Round needs | CJ holds |
|---|---|---|
| steel_plate | 2,400 + 2,000 + 2,000 + 2,000 + 2,000 = 10,400 | 36,430 |
| superconductor | 400 | 633 |
| targeting_computer | 20 | 38 |
| circuit_board | 300 + 1,200 = 1,500 | 14,439 |
| armor_plate | 240 | 1,302 |
| **power_cell** | **600 (shields) + 400 (reactor) = 1,000** | **53** |
| ferrous_slug_case (ammo) | — | 5,000 |
| coolant_fluid (upkeep) | — | 444 |

Only power cells are short. **Make them at CJ** (my call, revised 11-08 when the operator pointed out CJ's cell labs): CJ holds 142,500 argon_gas and 222,698 neon_gas, `purify_argon` is a hand recipe, and the wire mill supplies wiring. Buying is the fallback: cheapest asks 290 at
`node_alpha_processing_station` (35,510 listed) and 291 at base
`0321b3e4406021575337b7be26e53dd7` (4,200 listed). Making 1,000 at CJ would need ~1,500
purified_argon (CJ holds none) and 1,000 copper_wiring, so buying is faster. Buy ~1,100 to
cover the builds plus the first days of shield upkeep. The cells must end up in **CJ faction
storage** (a hauler carries them to CJ and either deposits as a CRFT member or gifts to
miningbot_0000, whose half-hourly `faction_deposit_all` sweeps them in).

## Upkeep, ongoing

Measured CJ maintenance cycle is **~3,000 ticks (~8 h, ~3 cycles/day)**, not the 100 ticks
in the 11-07 design (`data/stations/cloverfield/findings.md`, "Maintenance cycle"). Per day
for round 1's additions, at ~3 cycles/day:

| Input | Per day | Source |
|---|---|---|
| power_cell | ~24 (2 shields × 4 × 3) | bought now; later the cell labs (see below) |
| coolant_fluid | ~27 (shields 12 + 5 new guns 15) | miningbot_0000 schedule id 49 (hourly `synthesize_coolant_fluid 6`, ~144/day) already covers it |
| armor_plate | ~3 | CJ stock 1,302 |
| circuit_board | ~6 (new grid) | CJ stock 14,439 |
| ferrous_slug_case | unknown, fight-dependent | CJ stock 5,000; recipe `manufacture_ferrous_slugs` (3 iron_ore + 2 steel_plate → 5) |

If the 3,000-tick figure is wrong for these facilities and the cycle really is 100 ticks,
power cell use is ~30× higher (~700/day). **Watch power_cell stock for the first two days
after the shields go up** and recompute.

Self-making cells later: the argon cell lab needs `copper_wiring`. The copper_drawing_mill at
CJ makes **piping, not wiring**. Wiring comes from the hand recipe `basic_copper_processing`
(8 copper_ore → 1; CJ has 47,066 ore) or a `copper_wire_mill` (L1, 90k, 4 ore → 2; build
mats 2,600 steel_plate + 950 copper_piping + 250 control_node, all on hand; draws 1 power and
1 life support). **The copper_wire_mill is part of this round (operator, 2026-11-08)**,
approved and built ahead of the defense builds, so the cell labs have a wiring source
for shield upkeep. **Built 2026-11-08 14:04 PST.** miningbot_0000 schedule id 50 = hourly
`craft process_copper_wiring 50 prefer_own` (operator).

## Reactor fuel (my call)

The reactor's 55 fuel per cycle is ~165 fuel/day at ~3 cycles/day. CJ's H2 combustor runs
hourly `craft manufacture_fuel_basic 500` (miningbot_0000 id 22) and CJ holds ~60,000
liquid_hydrogen, so production is far above the draw. Accept the shared tank. Before
building the reactor, check the station fuel reserve in `get_base` is at least 2,000.

## Fallback if upkeep runs hot (my call)

If power_cell stock at CJ falls more than ~100/day after the shields go up (i.e. the cycle is
closer to 100 ticks than 3,000), stop and bring it to the operator before buying more:
options are buying on, running the argon/neon cell labs on the new wiring, or taking a shield
offline. Do not exceed the 1M consumable cap without the operator.

## Build order (my call; operator approves each step)

0. the_banker → treasury top-up (2M). Buy ~1,100 power cells and get them into CJ faction
   storage.
1. `stellar_collector_grid` (supply 20 → 40).
2. `bunker_fed_reactor` (needs the 400 cells) → 52.
3. `point_defense_battery` × 5 (cheap, no cells; confirm ammo is in faction storage where
   the guns read it).
4. `reinforced_plating`.
5. `shield_projector` × 2, last, after reading live power supply/draw and confirming ≥ 1 day
   of cell upkeep on hand.

Before each build: read `get_base` (power supply/draw, life support) and faction storage;
after each build: `facility list` to confirm status and record it in
`data/stations/cloverfield/` (play_as `--record-dir data/stations/cloverfield`).

## Done when

`facility list` at CJ shows 2 shield_projector, 2 reinforced_plating, 6 point_defense_battery,
2 stellar_collector_grid and 1 bunker_fed_reactor, all with maintenance OK, and `get_base`
power supply ≥ draw. Record the new `weapon_dps`, shield and hull values from `get_base` and
the public stations list.

## Left to the builder

- Which hauler buys and carries the power cells.
- Exact top-up mechanics if the gift → deposit path has changed.
- Whether point defense batteries need their ammo in a specific storage bucket.
