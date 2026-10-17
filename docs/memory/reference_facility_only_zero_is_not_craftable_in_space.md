---
name: reference_facility_only_zero_is_not_craftable_in_space
description: "recipes.facility_only=0 means no FACTION FACILITY needed, not craftable anywhere; crafting still needs a station, and refining steps need onboard modules"
metadata:
  type: reference
---

**`recipes.facility_only = 0` does NOT mean "craftable in open space."** It
means only that the recipe needs no faction FACILITY. Crafting still requires
being **docked at a station**, and refining/gas-processing steps additionally
need the matching **onboard module** — which the ship usually does not have.

Got this wrong 2026-10-16 while craftsman-1 sat stranded at
`groombridge_1618_frozen_drift` on 8 fuel. I read the whole ice-to-fuel chain
(`process_water_ice` -> `electrolyze_water` -> `compress_hydrogen_gas` ->
`craft_fuel_cell`, all `facility_only=0`) as a self-rescue craftable in the
hold, and recommended it over a tanker. Operator: *"self-rescue is the dumbest
idea. it requires modules the ship doesnt have plus parts the ship doesnt carry
in cargo, and no place to craft in the middle of space."*

Three independent blockers, any one fatal:
1. **No station** in the system at all — nowhere to craft, so the chain cannot
   start regardless of inputs.
2. **No refining modules.** `build_onboard_hydrogen_refinery` is itself
   `facility_only=1`. The module requirement was visible in my own query output
   and I did not connect it.
3. **No `steel_plate` in cargo**, and nothing in the ice chain produces it.
   A Survey Vessel is an Explorer hull and may carry no mining laser either.

**The rule: a recipe's reachability is hull + location + cargo, never the
`facility_only` flag alone.** Check for a station, the required module, AND the
inputs before proposing any craft. `craftable` filters against cargo but says
nothing about modules or docking.

This is the same error shape as reading a missing `crt_` prefix as "not a
creature" and 0 capture rows as "the creature never fired" — treating the
absence of a flag as permission. See
[[reference_wildlife_update_20261014_lev_ids_variable_hull]].

A fuel-starved agent in a station-less system has exactly ONE recovery: a
tanker. Undocked belt-side transfer is the well-tested path
([[reference_ship_to_ship_refuel_works_while_docked]]).
