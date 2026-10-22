---
name: index-crafting-mining
description: Crafting & mining: supported_power, random yield, ore regen, drone refit, BoM explorer, public_facilities, crafting brain, batch size
metadata:
  type: reference
---

# Crafting & mining

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- ⭐ [supported_power = floor(remaining/20)](reference_supported_power_is_derived.md) — DERIVED, do not store; falls as a deposit depletes
- ⭐🔴 [supported_power gates mining](reference_mining_supported_power_gate.md) — summed module power must be BELOW it; an over-fitted rig is locked OUT
- ⭐🔴 [Mining yields a RANDOM resource from the site mix](reference_mining_yield_is_random_from_site_mix.md) — mining hulls = 100% ore cargo boost; 25 idle deeprock_harvesters
- [Ore deposits REGENERATE](reference_ore_deposits_regenerate.md) — round-trip time beats deposit size; check `last_updated_tick`
- ⭐🟢 [Fleet drone refit](project_fleet_drone_refit.md) — SPEC `898852c1` unpushed; only shortfall = 3,607 energy_crystal
- ⭐🔴 [KB BoM explorer picks recipes ALPHABETICALLY](reference_kb_bom_explorer_alphabetical_default.md) — not cost-aware; one campaign read 13x too expensive
- [public_facilities keys player stations by base_id](reference_public_facilities_player_station_id.md) — a POI-keyed join reported 0 for 231
- [public_facilities prune-at-write](reference_public_facilities_prune.md) — SHIPPED 08-22; safe ONLY because listings are complete
- [circuit_board intel](reference_circuit_board_production.md) — silicon_ore is the bottleneck
- [Crafting brain](project_crafting_brain.md) — A1+A2 SHIPPED
- [Executor B rollout](project_executor_b_rollout.md) — NOT LIVE
- [Craft batch = Crafting skill](project_craft_batch_skill.md) — no longer hardcoded 10
