---
name: project_ore_to_component_conversion
description: "Miners and drone bots convert surplus ore into components hourly (refine_steel, process_copper_wiring, draw_copper_piping) — deployed 2026-09-12 to beat the 100k-per-item-type storage cap"
metadata:
  node_type: project
  type: project
---

## Why

Storage is capped at **100,000 per ITEM TYPE**. Ore was piling up against it:

| agent | iron | copper |
|---|---:|---:|
| **overmind** (mining) | **99,887** | **99,983** — capped on BOTH |
| miner-9 | 73,937 | 33,308 |
| random-clark | 73,270 | 29,386 |
| marketbot_010 | 68,396 | 37,604 |
| **fleet total** | ~420k | **773,572** |

`marketbot_010` gained **21,431 iron in one day**, so the cap was days away.
Converting frees space twice: fewer ore units, and the remainder sits in a
different item type with its own separate cap. The outputs are also bulk
components used across facilities and recipes, so they are worth more than the
ore and haulers can distribute them.

## The set (hourly, on `resident` / `resident_gas` / `resident_ice` / `miner`)

| recipe | inputs | output | per hour |
|---|---|---|---|
| `refine_steel` | 5 iron_ore | 2 steel_plate | 500 out = 1,250 iron |
| `process_copper_wiring` | 4 copper_ore | 2 copper_wiring | 600 out = 1,200 copper |
| `draw_copper_piping` | 12 copper_ore + **2 steel_plate** | 4 copper_piping | 200 out = 600 copper + 100 steel |

≈30k iron + 43k copper per day per productive agent, against ~21k/day intake
each, so backlogs FALL rather than hold. Net steel +400/hr after piping.

⭐ **`draw_copper_piping` consumes the steel `refine_steel` makes.** It is
ordered last in the schedule so steel exists first; on an agent with copper but
no steel it fails, costing one request.

## ⭐ Gotchas learned building this

- **`quantity` is OUTPUT items**, not runs and not inputs — the server rounds up
  to whole runs. `craft refine_steel 1000` consumes 2,500 iron. Reversing that
  spends 5x the ore on an irreversible conversion, so `craft` errors on a
  missing/non-positive quantity rather than defaulting.
- **There was no plain `craft` command** in the worker script language, only
  `craft_node` (the crafting-CHAIN executor: RECIPE NUM_OUTPUTS STATION
  FACILITY, may travel). Added `craft RECIPE QUANTITY` in `a4005f58`.
- ⭐ **Our KB's `facility_only` flag is unreliable.** It reads 1 for
  `refine_steel` and `process_copper_wiring`, but both hand-craft fine —
  confirmed by the operator and by api.md, which says craft "auto-routes to
  your own/faction facility, **or hand-crafts at the Station Workshop**".
- ⭐ **A NEW hourly task does not fire on startup.** The scheduler stamps
  `last_run = created_at`, so the first run is a full hour after the worker
  restarts. Do not conclude the wiring is broken when nothing happens
  immediately — check `data/agents/<id>/schedule.json` for the entry instead.
- `roles.yaml` validation only rejects an EMPTY command, so a typo loads
  cleanly and then fails once per agent per interval forever. Guarded now by
  `TestRolesYAMLCommandsAreAllDispatchable`, which checks the first token of
  every scheduled command against the dispatcher.

## Follow-ups

- **Verify the first runs** after the hour elapses, then check the next storage
  capture: if the backlog is not falling, raise the quantities. There is no
  history to fit a rate to (`agent_storage_items` keeps only the newest row per
  player/base/item), so this has to be done by comparing captures.
- **`craft_fuel_cell` (2 liquid_hydrogen + 1 steel_plate, no facility)** could
  break the `no_fuel_cells` refuel deadlock now that steel is being produced —
  but liquid_hydrogen is scarce (2,972 fleet-wide, none in miner hands).
  `crack_plasma_fuel_cells` (4 plasma_gas + 1 steel_plate, no facility) is the
  better route: `marketbot_017` holds 14,149 plasma gas. See
  [[project_no_fuel_cells_refuel_deadlock]].

Related: [[project_drone_marketbots]] · [[project_drone_project_crystal_reserve]]
