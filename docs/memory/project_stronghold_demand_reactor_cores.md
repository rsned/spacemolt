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
