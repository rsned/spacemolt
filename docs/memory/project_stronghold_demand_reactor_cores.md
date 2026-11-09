---
name: project_stronghold_demand_reactor_cores
description: "2026-11-08 stronghold buy books (marketbots sit in all 9 strongholds) — station_reactor_core bid up to 1.24M with ZERO asks anywhere; salvage_metal 4,370 bids; station_core books are 1-21 cr placeholders"
metadata:
  node_type: memory
  type: project
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-09T01:07:55.882Z
---

Operator tips 2026-11-08: players report stronghold demand for station cores and, since
v0.674.0 (strongholds build the best ships they can afford), for high-end ship parts.
**We have a marketbot in every stronghold** (operator), so the books are covered.

Stronghold buy books, latest capture 2026-11-09 ~01:00Z, bids ≥ 1,000:
- **station_reactor_core**: 7 strongholds, top bid **1,235,706**, 401 units (~327M book).
  **No sell orders anywhere.** Recipe `build_station_reactor_core` (facility-only:
  `reactor_core_production_line` L1, 806k): 5 durasteel_plate + 5 neutronium_ingot + 2
  power_cell + 5 superconductor → 1. Input cost ≈ 5 × neutronium (min ask 106,395, only
  545 listed) + ~1.6k → **~535k/core vs 1.24M bid**. Neutronium is the constraint:
  `synthesize_neutronium` (facility-only) = 3 durasteel + 2 power_core + 1
  weapons_grade_plutonium.
- salvage_metal: 9 strongholds, top 4,370, 198k units (~749M); asks from 2,187 (8,770 listed).
- starshine / nerve_burn / void_dust / ghost_rounds: our contraband lines (bids 2–3.7k).
- targeting_computer 2,387 (asks from 1,932), galactic_standard_alloy 3,752 (asks 2,034).
- station_core: bids 1–21 cr only (placeholder books); one ask anywhere at 15M.

Not yet done: sizing a reactor-core line at CJ, neutronium sourcing, a test sale.
See [[project_cloverfield_home_station]] · [[project_contraband_demand_shock]].

**Plutonium chain (traced 2026-11-08):** weapons_grade_plutonium ← 1 polonium_ore + 1
reactor_grade_plutonium (`polonium_doping_cell` L1 140k). reactor_grade_plutonium: buy
49–69k (224 listed) or breed 2 reactor_fuel_assembly + 3 thorium_fuel_rod (fuel assembly ←
2 low_enriched_uranium + 1 thorium rod; rod ← 2 processed_thorium; LEU ← 2
uranium_hexafluoride + 2 liquid_nitrogen) — bred ≈ bought (~47k). power_core: BUY (5,986+;
crafting uses energy crystals = reserved). neutronium facility `neutronium_compression_chamber`
L1 314k. Made neutronium ≈ 62k vs 106k bought → core ≈ 312k vs 1.24M bid; ~44 cores before
listed RGPu runs out. Drone ore held: polonium 4,456, uranium 4,598, thorium 2,661, radium 4,326.
Plan: one test core from bought inputs to confirm the bid before building the chain.
