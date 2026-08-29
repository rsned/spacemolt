---
name: reference_ship_modules_never_captured
description: "ship_modules has 0 rows and always has — the StoreShipModules write path exists but nothing calls it. We cannot see what is fitted on any agent."
metadata:
  node_type: memory
  type: reference
---

`spacemolt-knowledge.db.ship_modules` = **0 rows**, fleet-wide, ever. The write path
exists (`pkg/knowledge/sqlite_player.go` — DELETE+INSERT keyed on `ship_id`) but **nothing
in the capture pipeline calls it**. Same shape as `agent_ships` (also 0 rows) in
[[project_fleet_asset_snapshots]].

**Consequence:** we can answer "what hull is this agent flying" (`ship_class` is on every
worker status since 2026-08-22) but NOT "what is fitted to it". So any refit question —
free slot? CPU headroom? does this miner have a gas harvester? — is unanswerable offline.

**The available proxy is hull capacity, not free capacity.** `spacemolt-knowledge.db.ships`
carries `utility_slots`, `weapon_slots`, `defense_slots`, `cpu_capacity`, `power_capacity`.
Joining that against each agent's `ship_class` gives an **upper bound**:

- 153 of 158 agents fly a hull that *could* take an `advanced_drone_bay` (utility ≥1,
  cpu ≥12, power ≥15)
- `floor_price` (cpu 6) and `huffnpuff` (cpu 9) cannot
- `siphon`, `sparrow`, `shadow_dancer` are **absent from the `ships` catalog** — the same
  erasure as [[reference_legacy_ship_classes_erased_by_refresh]]

Treat that 153 as a ceiling. Mining hulls plausibly already run lasers and cargo expanders
in those utility slots, so an unknown share need a *swap*, not an install.

Blocks Phase 4 of [[project_fleet_drone_refit]].
