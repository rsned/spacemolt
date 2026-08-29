---
name: reference_module_wear_removed
description: Item damage/repair was REMOVED from the game — refitting is now consequence-free, so moving modules between hulls costs nothing
metadata:
  type: reference
---

**Item damage and repair were dropped from the game** (confirmed by the operator,
2026-08-28, alongside v0.568.0). Uninstalling a module used to damage it, so
repeated install/remove could eventually destroy it.

**Consequence: refitting is now consequence-free.** The standing argument
against moving modules between hulls is gone. This directly unblocks re-equipping
the **29 agents flying free tier-0 starters while owning better hulls**, against
**170 idle stored hulls** (74 drillship, 45 excavator, 25 deeprock_harvester).

Evidence in the spec (`openapi.20260828.json`, 215 paths, down from 216):
- `/repair_module` **removed**
- `wear`, `wear_status`, `quality_grade` — **0 occurrences**
- `repair_kit` still appears (4x) as an item/recipe

**We still carry the dead fields**: `serverapi` responses + `ShipModule`,
`knowledge/catalog.go`, and real columns on `ship_modules`
(`quality, quality_grade, wear, wear_status`). Inert under `omitempty`; cleanup
is a separate change, not urgent.

**Do NOT infer removal from a path count alone.** `get_location` is absent from
the spec and still works — see [[reference_supported_power_is_derived]] context.
This removal was confirmed by the operator, not by the diff.

Also in v0.568.0: `get_ship` takes an optional `ship_id` (read any owned ship's
fit from anywhere, no docking/travel; reply omits `drone_bay` — see
[[reference_drone_bay_is_agent_wide]]), and `list_ships` returns
`module_type_ids`, which finally makes a stored hull's summed mining power
computable for [[reference_mining_supported_power_gate]].
