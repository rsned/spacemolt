# Station #2 pre-flight (Task 1, read-only)

Written 2026-11-16 ~01:30Z from DB captures, the catalog and the live stations API only. No
play_as, no logins, nothing changed in game. Plan: `docs/superpowers/plans/2026-11-16-station-two.md`.

Sources and abbreviations:
- **A** = `data/assets.db`; **K** = `data/spacemolt-knowledge.db`; **cat** = `data/game-api/latest/catalog_*.json`.
- **API** = `GET https://game.spacemolt.com/api/stations` at 2026-11-16T01:25Z (97 stations and outposts).
- **F** = `data/stations/cloverfield/findings.md`.
- Tick ≈ 2,415,224 at 01:25Z (max `poi_resources.last_updated_tick`). The rate is about 8,600 ticks a
  day (2,245,484 at 10-27 08:20Z per F). **Stale (>2 days) = older than ~17,200 ticks.**

## 1. craftsman-2's standing

| fact | value | source |
|---|---|---|
| faction | CRFT (e727c0e9…) | A agent_profile 2026-11-16T01:06:11Z |
| rank | **member** (not officer) | A agent_profile 01:06:11Z; K faction_members 2026-11-14T10:30Z "Member" |
| location | docked at CJ (c976fa7d…), cloverfield | A agent_profile 01:06:11Z |
| wallet | 525,867 cr | A agent_profile 01:06:11Z |
| corporation_management | L14 (crafting 19, refining 11, engineering 12) | A agent_skills 01:06:11Z |
| fleets listing it | craft-fleet.yaml, unlock-fleet.yaml | data/overmind/*.yaml |

The CRFT officers now (A agent_profile, 11-16) are craftsman-1, -3, -4, -5, -8, -10 and
miningbot_0000. The leader is craft_boss.

Role permissions:
- CRFT's own role table is **not captured**. `game-api/latest/faction_info.json` is Data Bots'.
- The default roles there are: officer = manage_bases **true**, promote **false**; leader = everything.
- F line 15 says every station action needs ManageBases (leader/officer), and CJ's officers have built
  facilities. So CRFT officers most likely carry manage_bases.

**Verdict: not ready.** craftsman-2 must be promoted to officer, and only the leader can do that
(officers lack `promote` by default).

## 2. Station cores and outpost kits (A agent_storage_items, 2026-11-16T00:00:07Z)

| holder | base | item | qty |
|---|---|---|---|
| craftsman-1 | c976fa7df53c87f9616165d7d411b41a (CJ) | station_core | 1 |
| craftsman-1 | voss_redoubt_station | station_core | 1 |
| craftsman-1 | c976fa7df53c87f9616165d7d411b41a (CJ) | outpost_kit | 1 |
| craftsman-1 | gold_run_extraction_hub | outpost_kit | 1 |

There are **two outpost kits**; the spec says one. None of these items is in faction storage or in
cargo. The plan stands: craftsman-1 gifts the CJ core to craftsman-2 at CJ.

## 3. Site check

Eligibility: K systems (last_updated 2,373,732 to 2,415,223) and API 01:25Z. Jumps are from K
connections.

| system | jumps from CJ | empire / police | station or outpost now (API) | links |
|---|---|---|---|---|
| ironpeak | 2 (via driftwood) | none / 0 | none | beid castor driftwood markeb |
| beid | 2 (via cocibolca/driftwood) | none / 0 | none | castor cocibolca driftwood ironpeak markeb |
| castor | 2 | none / 0 | none | beid cocibolca driftwood gsc_0023 haedus ironpeak |
| driftwood | 1 | none / 0 | none | beid castor cloverfield cocibolca ironpeak zosma |
| cocibolca | 1 | none / 0 | none | beid castor cloverfield driftwood gsc_0023 zosma |
| markeb | 2 | **outerrim / 30** | none | beid first_step ironpeak void_gate |

**markeb is ineligible** (empire space), as the spec says. The others are eligible per K; this still
needs confirming with `get_base_cost` in game. Each system has a sun, so a stellar_collector_grid is
possible. No hazards are recorded on any POI (K pois.hazard_severity = 0).

### Resource reads (K poi_resources; age = ticks before 2,415,224)

| system | POI | resource | rich | remaining / max | age (ticks) | stale? |
|---|---|---|---|---|---|---|
| ironpeak | ironpeak_belt | iron_ore | 64 | 98,244 / 100k | 41,501 | **stale (~4.8 d)** |
| ironpeak | ironpeak_belt | copper_ore | 44 | 100k / 100k | 41,501 | **stale** |
| ironpeak | ironpeak_belt | gold_ore | 36 | 304 / 25,000 | 41,501 | **stale** |
| ironpeak | ironpeak_weak_superposition | quantum_fragments | 11 | 663 / 663 | 41,501 | **stale** |
| ironpeak | ironpeak_weak_superposition | iron / copper | 20 / 23 | 100k / 100k | 41,501 | **stale** |
| ironpeak | strange_matter_node | exotic_matter | 18 | 800 / 800 | 41,492 | **stale** |
| beid | beid_belt | iron / copper | 54 / 28 | 100k / 100k | 256,962 | **stale (~30 d)** |
| beid | beid_vapor_fields | plasma_gas | 21 | 537 / 10k | 104 | fresh |
| beid | beid_vapor_fields | xenon / argon / neon / H2 | 27 / 36 / 42 / 55 | ~540–810 of 10k–50k | 104 | fresh |
| beid | deep_deuterium_ice_sheet | deuterium_ice | 30 | 2,000 / 2,000 | 10,975 | fresh |
| castor | castor_belt | iron / copper / manganese | 64 / 26 / 23 | 253 / 100k / 15 | 256,890 | **stale** |
| castor | castor_emission_nebula | plasma / ion / neon / H2 | 17 / 27 / 51 / 63 | 174 each | 484 | fresh |
| markeb | markeb_belt | uranium_ore | 15 | 28 / 5,000 | 226 | fresh |
| markeb | markeb_belt | radium_ore | 6 | 953 / 2,000 | 226 | fresh |
| markeb | markeb_belt | lead_ore | 16 | 24 / 20,000 | 226 | fresh |
| markeb | markeb_belt | aluminum / carbon | 62 / 51 | 2 / 1 of 30k | 226 | fresh |
| markeb | markeb_quantum_eddy | quantum_fragments | 17 | 1,016 / 1,016 | 104 | fresh |
| driftwood | driftwood_frost_ring | helium / nitrogen / water ice | 12 / 41 / 54 | 512 / 835 / 774 | 226 | fresh |
| driftwood | driftwood_ghost_resonance | phase_crystal | 7 | 467 / 467 | 34,040 | **stale** |
| cocibolca | cocibolca_belt | iron / copper | 38 / 42 | 35,401 / 30,480 of 100k | 1,621 | fresh |

What the reads show:
- Belt reads in **ironpeak, beid and castor are all stale**, and faction_intel_resources is empty
  (0 rows).
- Markeb's lead and uranium are mined almost to zero. Station #2's lead comes from stock: 17,442
  lead_ore in personal storage across 5 agents (A, 01:26Z).
- Beid is the only candidate with **plasma_gas in system** (the plasma_injector input). CJ also holds
  67k of it.

## 4. Inputs to move from CJ

Faction storage is per station. CJ holdings below are from A faction_storage_items at
2026-11-16T01:19:46Z (131 rows; a missing row means 0).

### Build materials (cat facilities, 2026-11-13)

The scope priced here:
- **base**: locker_bay, faction_workshop, atmospheric_recycler, refueling_dock, and
  h2_fuel_combustor for fuel production. The refueling_dock's own 5,000 tank makes a fuel_tank_expansion
  unnecessary (F, 10-27).
- **power**: one stellar_collector_grid. The plan's base sequence has **no power source**, and a
  fresh station has 0 power. Every line draws 1 power and 1 life support.
- **defense**: shield_projector, reinforced_plating, and three point_defense_battery (L1):
  - one stays at L1;
  - one is upgraded to station_defense_turret (L2);
  - one is upgraded twice, to turret then heavy_defense_battery (L3).

  This is how CJ got its L2 and L3 guns (F 11-09, 11-12); an upgrade charges the target's full
  build_cost. The materials total assumes each upgrade consumes its own listed materials.
- **lead line** (3 facilities) and **plasma line** (4 facilities).

| item | needed to build | CJ holds | CJ upkeep /cycle | gap / note |
|---|---|---|---|---|
| steel_plate | 36,600 | 24,045 | 10 | **short ~12.6k**: 438,732 in personal storage (73 agents); 9,638 in other faction storage |
| copper_piping | 8,400 | 13,445 | 0 | OK |
| control_node | 2,050 | 7,130 | 0 | OK |
| circuit_board | 2,060 | **0** | 7 | **short 2,060**: personal 5,293 (craftsman-1 677 at gold_run, 670 at treasure_cache…). **CJ itself is at 0.** |
| fuel_cell | 2,000 | **36** | 3 | **short**: personal 8,506 (28 agents). CJ has ~12 cycles left. |
| water_recycler | 200 | 13 | 0 | short: craftsman-1 295 at gold_run, 101 at market_prime |
| superconductor | 440 | **0** | 0 | short: personal 846 (craftsman-1 272 at gold_run, 150 mera_sanctum, …) |
| power_cell | 300 | 1,492 | 8 | OK |
| armor_plate | 440 | 1,184 | 2 | OK (craftsman-1 holds ~4.8k elsewhere) |
| targeting_computer | 62 | 5 | 0 | short: personal 545 (craftsman-1 102 market_prime, 101 central_nexus) |

### Station #2 upkeep per cycle (cat maintenance_inputs) and CJ cover

| item | S2 /cycle | CJ /cycle | CJ holds | haul for 100 S2 cycles |
|---|---|---|---|---|
| coolant_fluid | 12 | 17 | 2,726 | 1,200 |
| power_cell | 4 | 8 | 1,492 | 400 |
| circuit_board | 3 | 7 | **0** | 300 (from personal storage) |
| oxygen_gas | 2 | 4 | 1,991 | 200 |
| fuel_cell | 2 | 3 | **36** | 200 (from personal storage) |
| water_ice | 3 | 6 | 339,955 | 300 |
| steel_plate, flex_polymer, focused_crystal, sunspindle, purified_water, catalytic_scrubber_cartridge, liquid_hydrogen, armor_plate | 1 each | 10, 1, 1, 2, 4, 2, 1, 2 | 24,045 / 1,837 / 440 / 2,626 / 44,681 / 561 / 124,976 / 1,184 | 100 each |
| liquid_hydrogen for the station fuel tank | 10 per 100 fuel | | 124,976 | 2,000 (fills a 5,000 tank four times) |

How long a cycle is:
- F measured ~3,000 ticks, about 2.9 cycles a day. F's 11-01 table uses the worst case, 86.4 a day.
- **100 cycles ≈ 5 weeks at the measured rate.**
- Production lines have no maintenance_inputs; they only draw power and life support.

Ammo for the guns (the per-gun load is unknown):

| gun | ammo | CJ holds |
|---|---|---|
| point_defense_battery | ferrous_slug_case | 4,600 |
| station_defense_turret | armor_piercing_rounds_box | 302 |
| heavy_defense_battery | fragmentation_rounds_box | 3,854 |

### Line inputs (cat recipes)

Per finished unit:
- **lead_shielding:** 7.5 lead_ore + 1 steel_plate.
  - Chain: 4 ore → 2 ingot → 3 ingot → 2 sheet; 10 sheet + 4 steel → 4 shielding.
- **plasma_injector:** 5 plasma_gas, 2 palladium_ore, 1 iridium_ore, 6 copper_wiring, 4 silicon_ore,
  2 titanium_alloy and 2 titanium_ore.
  - superconductor = 2 Pd + 1 Ir + 3 wiring.
  - core = 5 plasma + 1 superconductor.
  - ceramite = (4 Si + 2 Ti-alloy + 2 Ti-ore) → 2.

| input | per 100 units | CJ holds | elsewhere |
|---|---|---|---|
| lead_ore (shielding) | 750 | 0 (16 ingot, 238 sheet) | 17,442 personal (5 agents) |
| plasma_gas | 500 | 67,224 | beid in-system |
| palladium_ore | 200 | 5,452 | 882,906 personal |
| iridium_ore | 100 | **0** | 173,791 personal (14 agents) |
| copper_wiring | 600 | 5,000 | 448,871 personal |
| silicon_ore | 400 | 16,989 | |
| titanium_alloy | 200 | 1,113 (CJ uses 1/cycle; its Alloy Foundry makes it) | |
| titanium_ore | 200 | 7,729 | |

CJ already holds **364 lead_shielding and 193 plasma_injector** as finished stock, and no CJ facility
consumes either. These are candidates for the first consignment sales.

## 5. Treasury

| reading | value | source |
|---|---|---|
| CRFT treasury | **3,302,912** | A faction_profile and faction_storage, 2026-11-16T01:19:46Z |
| previous | 3,383,835 | K factions 2026-11-14T10:30Z |

## 6. Budget (L1 build credits, cat 2026-11-13; labor not included)

| block | credits | detail |
|---|---|---|
| founding fee | **5,000,000 (placeholder, CJ's; unknown until `get_base_cost`)** | |
| base sequence | 2,208,000 | locker 500k, workshop 500k, recycler 600k, refueling_dock 500k, h2_fuel_combustor 108k |
| power | 900,000 | stellar_collector_grid (+20); not in the plan's base sequence |
| defenses | 4,380,000 | shield 900k, plating 700k, L1 PDB 180k, L2 (PDB + turret) 600k, L3 (PDB + turret + HDB) 2,000k |
| lead_shielding line | 293,000 | furnace 91k, mill 92k, assembly 110k |
| plasma_injector line | 478,000 | refinery 132k, containment 133k, kiln 108k, assembly 105k |
| **total** | **13,259,000** | 8,259,000 without the fee |

Against the money available:
- **10M top-up − 1M floor = 9.0M**: over by **4.26M**.
- **Treasury now + 10M − 1M = 12.30M**: over by **0.96M**.
- Dropping the L3 gun (−2.0M) gives 11.26M. That fits inside 12.30M with about 1.04M of slack, but
  only if the fee is ≤5M.

Line cycle labor (5–13 cr per run) and the S2 upkeep inputs come from stock, not credits.

## Unknowns the operator must resolve

| # | unknown | how to resolve |
|---|---|---|
| 1 | CRFT officer role carries manage_bases? Promote craftsman-2 | `go run ./cmd/tools/play_as craft_boss` → `faction_info` (read roles), then `faction_promote --player_id craftsman-2 --role_id officer` (live action: operator yes) |
| 2 | Founding fee and site eligibility | After Task 2 frees craftsman-2: `go run ./cmd/tools/play_as craftsman-2` → `get_base_cost` (fee); at ironpeak, undocked at a non-star POI → `get_base_cost` again for eligibility |
| 3 | Fresh ironpeak and beid belt reads (all stale) | Any scout passing through: `go run ./cmd/tools/play_as <scout>` → travel to ironpeak → `get_system`, `get_poi ironpeak_belt`, `get_poi strange_matter_node`, `get_poi ironpeak_weak_superposition`; same for beid_belt. Or craftsman-3 `capture_intel` to fill faction_intel_resources |
| 4 | Power source for S2 (not in the plan) | Operator choice: stellar_collector_grid 900k (2 circuit_board/cycle), or bunker_fed_reactor 500k (55 tank fuel/cycle) |
| 5 | Gun ammo load; whether upgrades consume materials | At build time: `facility types --facility_type heavy_defense_battery` (and station_defense_turret) |
| 6 | CJ is short of circuit_board (0) and fuel_cell (36) **now** | Restock CJ from personal storage before taking any for S2 |

## Gate update 2026-11-15 ~19:10 local
- craftsman-2 (Samuel 'Smith' Sterling) promoted member -> **officer** by the operator via craft_boss (`faction_promote`). Unknown #1 resolved.
