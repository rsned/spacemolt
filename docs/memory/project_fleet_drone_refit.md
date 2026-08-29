---
name: project_fleet_drone_refit
description: "Fit all ~153 agents with 1 advanced_drone_bay + 5 mining_drones (175 bays / 800 drones). Spec committed 2026-08-22; only shortfall is 3,607 energy_crystal."
metadata:
  node_type: memory
  type: project
---

**Spec:** `docs/superpowers/specs/2026-08-22-fleet-drone-refit-design.md` (commit `898852c1`, UNPUSHED).
Drones mine autonomously once fitted, so this is a one-time capital project.

**The whole campaign fits in three numbers:** 5,679 craft runs, 3,607 energy_crystal short,
~114,000 cr unrecoverable. Everything else is already in fleet storage. The two KB BoM
exports said 258,000 raw units — that was [[reference_kb_bom_explorer_alphabetical_default]],
not reality.

**Decisions locked (2026-08-22):**
- `optical_fiber_bundle` = `spin_optical_fiber` (silicon, hand), NOT diamond. Diamond needs
  1 crystal/bundle vs silicon's 2 — tempting, since crystals are the only shortfall — but
  **`draw_diamond_optical_fiber` exists at exactly ONE station and arneb is 22 jumps from
  haven.** Revisit only if a second one is found near haven/sol.
- `reinforced_glass` from stock (hold 33,067, need 750). `titanium_alloy` via
  `onboard_alloy_synthesis` (hand, no facility).
- **grand_exchange is the hub**: craftsman-2 lives there, 63,767 silicon on site, 2 jumps
  from gold_run's 119,659 carbon, CRFT builds there, craftsman-1 parks its parts there.
- Crystals: **mine then buy**. Two nebulas hold 5,000 each (`forgotten_prism`/ivorygate,
  `the_quiet_shimmer`/gsc_0002, richness 22, 12-14 jumps from sol). Nebula = needs a gas
  harvester; craftsman-1 holds 300+ at grand_exchange. Market top-up only at The Obsidian
  Well (49,232 @ 10,001; next best station lists 500).
- Execution: **hand-driven `play_as` wave 1 first**, then decide what to automate.

**✅ PHASE 0 DONE 2026-08-22:** `faction_build polymer_extruder` succeeded at
grand_exchange_station — facility `b659c3602da933e414c9fa91a072968a`, rent **123/cycle**
(~10,600 cr/day), build 120 ticks. A `polymer_refinery` (makes flex_polymer, NOT
nanoplastic) was built first by mistake and is also accruing 123/cycle — decide
dismantle-or-keep; `faction_dismantle` refunds 100% of materials but costs one
cargo_container per package and CRFT held only 3.

**Why Phase 0 was the high-value move:** `extrude_nanoplastic` is a MONOPOLY — one public
facility (confederacy_central_command, player-owned) at **9,000/run**; 491 runs = 4.42M.
Our own `polymer_extruder` costs **`labor_cost` 8/run** (1,125x cheaper) and `dismantle`
refunds **100% of build materials**, so only the 110,000 cr build fee is sunk. CRFT storage
at grand_exchange covers credits + copper_piping; gaps are 50 steel_plate and 300
control_node, both closable on-station (craftsman-1 has 255 nodes there; craft the last 45).
Ongoing cost is **credits only** — facilities have NO material maintenance; just
`rent_per_cycle` per 100-tick cycle (86.4/day). The sibling polymer_refinery reported
123/cycle ≈ 10,600 cr/day. (The API's "draws maintenance from faction storage, goes
offline when undersupplied" line is about SERVICE/INFRASTRUCTURE facilities, not
production ones — corrected by the operator 2026-08-22.)
See [[reference_facility_rent_cycle]].

**Blockers:** `ship_modules` = 0 rows fleet-wide, so "153 hulls can take a bay" is hull
capacity, NOT free capacity — Phase 4 can't be sized until modules are captured
[[reference_ship_modules_never_captured]]. Bulk-craft throughput (`craft jobs=[...]` = one
mutation or N ticks?) unknown. Executor B reads `storage_snapshots` (stale since 07-02);
all spec numbers came from `assets.db`.

⚠ `mine_qty` strands miners fuel-dead — do not drive Phase 1 through it unfixed.
⚠ `circuit_board` has ZERO margin: need 2,314, hold exactly 2,314.

Related: [[reference_public_facilities_player_station_id]] · [[project_crafting_brain]] ·
[[reference_worker_quiesce_park]]
