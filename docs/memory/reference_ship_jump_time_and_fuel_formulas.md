---
name: reference_ship_jump_time_and_fuel_formulas
description: "Authoritative travel/jump time and fuel formulas from spacemolt.com/docs/guides/fuel — jumpTicks = max(1, 7-speed), jump fuel = ceil(scale^1.5 x speed), and the flat per-jump constants they invalidate"
metadata: 
  node_type: memory
  type: reference
  originSessionId: f744e650-ff1a-4add-9401-5a3087024568
  modified: 2026-07-30T15:19:46.331Z
---

Source: **spacemolt.com/docs/guides/fuel** (operator-supplied 2026-07-27).

**Inter-system jump**
- fuel: `ceil(shipScale^1.5 × shipSpeed × 10.0 × 0.10)`, minimum 1. Jump distance is a constant 10.0 regardless of galaxy topology.
- time: **`jumpTicks = max(1, 7 − shipSpeed)`**, minimum 1 tick (10s real). Speed 6 → 1 tick; speed 1 → 6 ticks, the slowest jump that can exist.

**Intra-system travel (POI → POI)**
- fuel: `ceil(shipScale^1.5 × shipSpeed × distanceAU × 0.07)`, minimum 1.
- time: `ceil(distanceAU / effectiveSpeed)`, min 1 tick, where `effectiveSpeed = shipSpeed × (1 + speedBuffBonus)`; towing a wreck applies a penalty.

**Modifiers, applied in order:** module efficiency `fuel × (100 − eff)/100` (max 80% reduction, no cap on penalties), then skill `ceil(fuel × (1 + skillBonus/100))`.

**Cargo mass does NOT affect fuel** (removed in v0.195.0).

**Station refuel price scales with tank fill: 2 cr/unit at ≥90% full up to 20 cr/unit below 10%**, plus empire fuel tax. (So topping off a nearly-full tank is cheap per unit; running dry is 10× worse — relevant to [[project_refuel_timing_endpoint_choice]].)

**Key consequence — speed cuts both ways:** higher speed = fewer ticks but MORE fuel per jump. At scale 1 both ends are cheap (1–6 fuel/jump), which is why the operator's "run couriers in tier 0/1 hulls" holds: `scale` dominates the fuel term (`scale^1.5`), so a scale-1 hull is cheap at any speed, and you can then buy speed for free.

**Live field:** the ship payload carries `speed` (`game.Ship.Speed`); `ships` catalog columns are `scale` and `base_speed`. Catalog `base_speed` spans 1–6 across ALL tiers and does not track tier — a 3600-cargo hull can be speed 1. `haulFuelPerJump` (pkg/worker/haul_fuel.go) implements the fuel formula and prefers the SERVER's `fuel_per_jump` from a `find_route` probe; reuse it rather than recomputing.

**Flat per-jump constants these invalidate:**
- `missionTicksPerJump = 12` — **FIXED `29399a4`**, now `missionJumpTicks(speed)`. It was double the worst case that can exist, and refused a courier at "197 available < 198 needed for 14 jumps" that any hull clears (114 at the slowest). Skip reasons now print the rate.
- `rescue.FuelPerJump = 5` — **FIXED `00d3f62`.** `TransferQuantity` now takes a `perJump` argument and `rescueFuelQty` feeds it `haulFuelPerJump`; the constant survives only as the fallback when the rate is unknown (pass 0). Proven necessary in the field, not just in theory: a rescuer burning ~3/jump reserved 105 of 110 fuel for a 20-hop trip home, reported "nothing to spare", and abandoned a rescue it had already flown 20 jumps to reach. **`fuelForHops`/`FuelForSystem` still use the flat constant** — they run at enqueue time in the overmind, where the strandee's hull is not known; that one is a genuine open gap. See [[project_rescue_pipeline_bugs]].
- `WatchdogConfig.FuelPerJumpCost` (pkg/worker/watchdog.go) — an approximate credit cost per extra jump; check whether it is hull-derived.

`find_route` returns fuel estimates but **no travel time**, so jump ticks must be computed, not read. Actual per-jump time is observable after the fact via `arrival_tick` / `ticks_remaining` on travel/jump replies.
