---
name: reference_circuit_board_production
description: "circuit_board production intel: 7 recipe routes + their facilities/build-costs, silicon_ore is the bottleneck ore, and Nexus Prime is the only single-site with the full fabricate input set (silicon+copper+energy_crystal). Use when siting a circuit_board facility or planning the craft fleet around circuit demand."
metadata:
  node_type: memory
  type: reference
  originSessionId: 86df3835-f10f-4c25-928d-7948d122ecd7
  modified: 2026-07-24T20:21:58.443Z
---

circuit_board is a deep high-demand intermediate (e.g. 300 per control_node batch → facility builds). Data below from `data/spacemolt-knowledge.db` (recipes/recipe_inputs/poi_resources) + `data/game-api/.catalog.json` facilities, 2026-07-24.

**7 routes → circuit_board** (recipe: inputs → output/batch; fac_only):
- `fabricate_circuit_boards`: 3 copper_ore + 2 silicon_ore + 1 energy_crystal → 2  (no)
- `carbon_arc_circuit_etching`: 2 silicon_ore + 12 carbon_ore → 3  (no) — silicon-light (0.67 Si/board)
- `fabricate_gold_circuit_boards`: 3 copper_ore + 3 gold_ore + 1 energy_crystal → 2  (no) — silicon-FREE, burns gold
- `chlorine_circuit_etching`: 2 silicon_ore + 1 chlorine_compound → 3  (**fac_only**)
- `fabricate_silver_circuit_boards`: 2 silicon_ore + 2 silver_wiring + 1 energy_crystal → 2  (**fac_only**)
- `fluorine_nano_etch_circuits`: 2 silicon_ore + 1 fluorine_etchant → **5**  (**fac_only**) — best yield / silicon-efficiency (0.4 Si/board)
- `reclaim_circuit_boards`: 3 rare_salvage + 5 salvage_components → 1  (no) — salvage route, no mining

**Ore scarcity (systems mining it)** — the whole decision hinges on this:
copper_ore 226 (ubiquitous) · chlorine_gas 60 · carbon_ore 35 · energy_crystal 19 · **silicon_ore 13 (BOTTLENECK)** · fluorine_gas 12 (rarest). 6 of 7 routes need silicon.

**Cheapest facility per route** (each has pricier higher-throughput tiers):
- `fluorine_acid_bath` 108k → fluorine route (best yield); facility-only
- `circuit_fabricator` 110k (steel_plate 2500 + copper_piping 950 + control_node 250) → fabricate
- `carbon_arc_furnace` 116k (steel_plate 5800 + copper_piping 2300) → carbon_arc
- `chlorine_etch_chamber` 332k → chlorine
- `auric_electronics_complex` 3.72M → gold (silicon-free)

**Refined-input sourcing (both trace to mineable GAS leaves):**
- chlorine_compound ← `stabilize_chlorine_gas` (the Chlorine Neutralization Vat, fac_only): 8 chlorine_gas(ore) + 1 flex_polymer → 3.
- fluorine_etchant ← `synthesize_fluorine_etchant` (fac_only): 6 fluorine_gas(ore) + 1 ceramite_plating → 2.
So fluorine/chlorine routes are 2-facility chains (gas-processor → circuit etcher).

**⭐ OPTIMAL LOCAL SITE = Nexus Prime (voidborn, station the_core).** It is the ONLY silicon system that also mines copper + energy_crystal — the complete `fabricate_circuit_boards` input set. Build a `circuit_fabricator` (110k) there → zero input hauling. Every other route/site requires hauling: NO silicon system co-locates with carbon or the gases (verified empty), and only Nexus Prime co-locates silicon+copper+energy_crystal. Silicon+copper (no energy_crystal): Haven, Zubenelhakrabi, Netherwick, Emberglow → there use carbon_arc only if you haul carbon in.

**Tie-in:** the chlorine route is the natural downstream of a Chlorine Neutralization Vat — vat makes chlorine_compound → chlorine_etch_chamber turns it into boards. See [[project_crafting_brain]] (planner has NO facility-construction goal — plans build materials only if each is fed as a target; facility build via separate `Facility()`/`build_*` command). Empire=ownership not region in this DB [[reference_empire_field_semantics]].
