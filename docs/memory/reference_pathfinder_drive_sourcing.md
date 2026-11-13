---
name: reference_pathfinder_drive_sourcing
description: "pathfinder_drive: buy from Solarian station managers (~276k, daily-reset uniform price) beats building; full BoM needs 18 facilities (~10.6M) and thorium is gone; Obsidian Well (Arneb) rents the whole chain"
metadata:
  node_type: memory
  type: reference
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-13T22:22:37.030Z
---

Researched 2026-11-13 (user deferred buying "for a little while").

**Buy:** station managers list pathfinder_drive at ONE server-set price that resets daily
(505k 11-07, 281k 11-08, 220k 11-09, 276,220 since 11-10). Stock 11-13: mobile_capital 4
(the user bought 6 there), Nova Terra Central 3, Sirius Observatory 2, Alpha Centauri 1.
Bids are 1–22 cr: there is no resale market. Tool: `buy_directed pathfinder_drive QTY STATION MAX_PRICE RECIPIENT`.
Open questions when resumed: count, recipient (CJ officer vs explorers), price cap.

**Build:** recipe build_pathfinder_drive (facility-only, 1800 t) = 3 jump_drive_core,
2 jump_calculator, 2 navigation_core, 1 phase_drive, 8 refined_quantum_matrix, 6 stabilized_exotic.
Cheapest mix (buy parts, build phase_drive + assembly) ≈ 235k/drive + ~6–7M capex; break-even
~150 drives. Full raw build is blocked: thorium_ore (360/drive) nearly unavailable,
raw_focusing_crystal unbuyable, the big facilities need 3,450 dark_matter_cell (1,919 listed).
CJ has none of the 18 facilities. The Obsidian Well (Arneb, BUSY) rents the whole chain
publicly (30k/run assembly). BoM script: scratchpad bom.py over data/game-api/*/catalog_recipes.json.

Related: [[reference_pathfinder_drive_mechanics]], [[project_pathfinder_frontier_sweep]],
[[reference_facility_speed_rules]].
