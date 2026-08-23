# Fleet Drone Refit — Design

**Goal:** fit every agent in the fleet with 1 `advanced_drone_bay` and 5 `mining_drone`.
Target build: **175 bays, 800 drones**. Drones work autonomously once fitted, so this is a
one-time capital project that raises passive yield across the whole fleet.

**Status:** design agreed 2026-08-22. **Phase 0 built the same day** — `polymer_extruder`
`b659c3602da933e414c9fa91a072968a` at grand_exchange_station, rent 123/cycle, build time
120 ticks (~20 min). The `extrude_nanoplastic` monopoly no longer binds this campaign.

---

## 1. The headline finding: recipe choice, not mining, was the cost

The campaign was scoped from two KB Bill-of-Materials exports. Those exports called for
~258,000 raw units and ~31,400 craft runs, which read as a multi-day mining marathon.

That number is an artifact. **The KB explorer defaults to alphabetical recipe selection**
when an item has several producers. The `advanced_drone_bay` export carried explicit `r=`
overrides and so picked good recipes; the `mining_drone` export was generated without them
and picked the diamond optics chain — `draw_diamond_optical_fiber` (`draw…` sorts before
`spin…`) and `fuse_diamond_glass` (`fuse_diamond…` before `fuse_reinforced…`).

Correcting the selection and subtracting what the fleet already holds:

| | as exported | final plan |
|---|---|---|
| raw materials to source | ~258,000 | **3,607** |
| craft runs | ~31,400 | **5,679** |
| facility rent | ~4.4M cr | **3,928 cr** |

Three swaps do it, and each replaces a facility-only recipe with a hand-craftable one:

| item | exported choice | chosen | why |
|---|---|---|---|
| `optical_fiber_bundle` | `draw_diamond_optical_fiber` (facility) | `spin_optical_fiber` (hand) | diamond costs 2 synthetic_diamond = 60 carbon per bundle; silicon costs 3 silicon. Removes 192,000 carbon ore. |
| `reinforced_glass` | `fuse_diamond_glass` (facility) | **from stock** | we hold 33,067 against a need of 750. No crafting at all. |
| `titanium_alloy` | `forge_titanium_alloy` (facility) | `onboard_alloy_synthesis` (hand) | iron+titanium direct, no steel_plate dependency, no facility. |

### Why the diamond path stays rejected

It was re-evaluated after discovering cheap facilities at The Obsidian Well
(`compress_synthetic_diamond` fee 32, `draw_diamond_optical_fiber` fee 185, backlog 0).
Diamond fiber uses 1 energy_crystal per bundle against silicon's 2, which would cut the
crystal shortfall from 3,607 to 2,308 for ~306k in fees — attractive, since crystals are
our only shortfall.

It loses on geography, not price: **`draw_diamond_optical_fiber` exists at exactly one
station in the known galaxy, and arneb is 22 jumps from haven.** The carbon leg is cheap
(gold_run → haven is 2 jumps), but the single-facility dependency means a 44-jump round
trip carrying diamonds out and fiber back. Silicon needs no travel at all: craftsman-2
already sits on 63,767 silicon at grand_exchange.

**Revisit only if** a second `draw_diamond_optical_fiber` facility is discovered near
haven or sol, or if crystal mining proves far slower than projected.

---

## 2. Final bill

**Only one raw material is short: `energy_crystal`, 3,607 units.** Every other input is
already in fleet storage — we hold 171k carbon, 292k iron, 165k silicon, 400k nickel.

### Craft runs (5,679)

| recipe | runs | where |
|---|---|---|
| `spin_optical_fiber` | 1,299 | hand |
| `build_drone_chassis` | 892 | hand |
| `onboard_alloy_synthesis` | 880 | hand |
| `build_mining_drone` | 800 | hand |
| `assemble_laser_focus_array` | 750 | hand |
| `extrude_nanoplastic` | 491 | **facility (ours, see Phase 0)** |
| `build_micro_thruster` | 302 | hand |
| `build_advanced_drone_bay` | 175 | hand |
| `carbon_arc_circuit_etching` | 54 | hand |
| `extract_xenon` | 36 | hand |

Existing intermediates consumed rather than crafted: 3,100 titanium_alloy, 2,400
steel_plate, 2,314 circuit_board, 2,266 flex_polymer, 750 reinforced_glass, 497
purified_xenon, 376 optical_fiber_bundle, 350 power_cell, 313 nanoplastic_composite, 289
micro_thruster_array, 258 drone_chassis, 50 laser_focus_array.

⚠️ **`circuit_board` has zero margin** — the plan consumes 2,314 and we hold exactly
2,314 fleetwide. Phase 0 additionally needs ~46 for control_node crafting. Expect to run
`carbon_arc_circuit_etching` beyond the 54 planned runs.

---

## 3. Architecture: grand_exchange is the hub

Nearly the whole campaign is local to one station.

- **craftsman-2** is stationed there
- holds **63,767 silicon** (need 3,897), 37,489 iron, 8,886 flex_polymer, **8,711
  reinforced_glass** (need 750), 1,289 steel_plate
- **2 jumps from gold_run**, which holds 119,659 carbon ore
- **CRFT already builds facilities there** — this is where our own extruder goes
- **craftsman-1** holds its extruder materials and 300+ gas harvesters there

Material is spread across ~30 holders per base, and crafting escrows from the *crafting
agent's* station storage, so **consolidation is the campaign's real work** — not mining
and not crafting.

---

## 4. Phases

### Phase 0 — Build our own Polymer Extruder

`extrude_nanoplastic` is the only remaining facility step, and it is a monopoly: exactly
one public facility exists (`confederacy_central_command`, player-owned) charging
**9,000/run**. 491 runs = 4.42M credits.

Building our own `polymer_extruder` costs **`labor_cost: 8` per run** — 3,928 credits for
the same 491 runs, a 1,125× saving. `dismantle` returns **100% of build materials**, so
the only unrecoverable cost is the 110,000 cr build fee.

**Build requirements vs CRFT faction storage at grand_exchange:**

| need | CRFT has | gap |
|---|---|---|
| 110,000 cr | 617,360 | ✓ |
| `copper_piping` 950 | 1,350 | ✓ |
| `steel_plate` 2,700 | 2,650 | −50 |
| `control_node` 300 | 0 | −300 |

Both gaps close on-station with no hauling:

1. craftsman-1 deposits **255 control_node + 50 steel_plate** from personal storage at
   grand_exchange into CRFT storage
2. hand-craft the remaining **45 control_node** (`assemble_control_node`, 23 runs) — all
   four inputs are present locally (196 circuit_board, 6,664 copper_wiring, 148
   gold_wiring, 48 silver_wiring)
3. `faction_build polymer_extruder`, build_time 120 ticks

**Ongoing obligation is credits only.** A facility has **no material maintenance** — the
only recurring cost is `rent_per_cycle`, charged per **100-tick cycle** (~16.7 min, 86.4
cycles/day). The comparable `polymer_refinery` build reported **123/cycle**, i.e. roughly
**10,600 cr/day**; the extruder's own figure comes back in its `faction_build` response.

(The API text about facilities drawing maintenance from faction storage and going offline
when undersupplied applies to **service/infrastructure** facilities — power, life support,
services — not to a `category=production` facility like this one.)

**Do not** try to source the build from the seven `package:` entries in CRFT storage
(dismantled Faction Quarters / Market Runner / Trade Ledger materials). A package is
accepted only if it holds *exactly* what the build still needs of an item, "no more" — an
exact match against a 50-plate / 300-node gap is improbable, and a rejection is up front.

### Phase 1 — Source 3,607 energy_crystal

Decision: **mine what we can, buy the remainder.**

`energy_crystal` is `category='ore'` and therefore mineable. Two rich sites carry more than
the whole requirement between them:

| site | system | remaining | richness | jumps from sol / haven |
|---|---|---|---|---|
| `forgotten_prism` | ivorygate | 5,000 | 22 | 12 / 18 |
| `the_quiet_shimmer` | gsc_0002 | 5,000 | 22 | 14 / 20 |

Both are **nebula** POIs, so miners need a gas harvester fitted. craftsman-1 holds **203
`gas_harvester_i`, 100 `gas_harvester_iii`, 23 `gas_harvester_ii` at grand_exchange** —
ample for the 7-agent mining fleet.

Market top-up: **The Obsidian Well** (arneb) lists 49,232 units at 10,001 cr — by far the
deepest book; the next best station lists 500. Buying the full shortfall would cost ~36M.

⚠️ `mine_qty` **strands miners fuel-dead** (live finding 2026-07-12: no fuel guard, no
return-to-station; two craftsmen were quarantined). Do not drive Phase 1 through that verb
until it is fixed.

### Phase 2 — Consolidate

Move the required intermediates into craftsman storage at grand_exchange. Most already sit
there or elsewhere in fleet storage; this is transfer work, not acquisition.

### Phase 3 — Craft waves

5,679 runs, all hand-craftable except the 491 extruder runs. Sharded across the 9
craftsmen. **Wave 1 runs hand-driven via `play_as`** to establish real mechanics before any
automation.

### Phase 4 — Fit and distribute

175 bays + 800 drones out to the fleet. `advanced_drone_bay` is a **utility-slot** module
(cpu 12, power 15); `mining_drone` is category `drone`, size 5, and **one bay holds 5
drones** — so 5 drones per agent is exactly one bay's capacity.

Hull screening across the 158 agents with a known `ship_class`:

- **153 hulls can accept a bay** (utility_slots ≥ 1, cpu ≥ 12, power ≥ 15)
- **2 cannot**: `floor_price` (cpu 6), `huffnpuff` (cpu 9)
- **3 unknown**: `siphon`, `sparrow`, `shadow_dancer` are absent from the `ships` catalog —
  the same erasure pattern as the legacy mining hulls

153 fittable × 5 = 765 drones, so the 175/800 target is correctly sized with spares.

---

## 5. Open risks

1. **`ship_modules` is empty fleet-wide (0 rows).** The write path exists in
   `pkg/knowledge/sqlite_player.go` but nothing in the capture pipeline calls it. So the
   "153 fittable" figure is **hull capacity, not free capacity** — we cannot see what is
   already fitted. Mining hulls plausibly already run lasers and cargo expanders in those
   utility slots, meaning some agents need a *swap*, not an install. Phase 4 cannot be
   sized until this is captured.
2. **Bulk craft throughput is unknown.** Whether `craft jobs=[...]` is one mutation or N
   ticks determines wave sizing entirely. Phase 3 wave 1 answers it.
3. **Executor B reads stale inventory.** The crafting-brain planner reads
   `storage_snapshots` in the knowledge DB, last captured **2026-07-02**. Every number in
   this document comes from `assets.db`, captured live. Any dispatch through Executor B
   subtracts the wrong stock and must be fixed first.
4. **Facility monopoly risk.** Until our extruder is built, `extrude_nanoplastic` depends
   on one player-owned facility that could raise its fee or go private without warning.
   Note the reverse once ours is running: rent accrues per cycle whether or not it is
   producing, so idle time between waves has a real (if small) cost.
5. **`circuit_board` has no margin** (see §2).

---

## 6. Data-integrity notes discovered during design

- **Player stations are keyed by `base_id`, not POI id, in `public_facilities`.** The
  Obsidian Well is POI `a356fc2c…` in `pois` and `market_orders`, but `base_id`
  `cca9e51e…` in the facility capture. A POI-keyed join reports **0 facilities** for a
  station that actually has **231**. Any query joining `public_facilities` to `pois` is
  silently wrong for every player station.
- **The KB BoM explorer's default recipe selection is alphabetical**, not cost-aware. Any
  export without explicit `r=` overrides should be treated as an upper bound, not a plan.
