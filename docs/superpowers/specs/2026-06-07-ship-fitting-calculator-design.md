# Ship Fitting Calculator — Design

**Date:** 2026-06-07
**Status:** Approved (design); ready for implementation plan

## Problem

The game has ~309 ship hulls and hundreds of modules, each with CPU/power costs
and slot requirements. Players (and, later, agents) need to answer two questions
offline, without a live game connection:

1. **Forward fit:** Will module X fit on ship Y? How many — 2, 3, …? Accounting
   for the Engineering skill, which reduces module CPU/power usage by 1% per level.
2. **Reverse fit:** "I want to fit N of module X — which ships can do it?"

No fitting/fit-check logic exists today. The only related server command is
`RefitShip`, which resets a ship to its factory defaults. This is net-new
computation built on catalog data.

## Scope & Approach

- **Data source: catalog JSON snapshots** (decided). Read
  `data/game-api/latest/` (a symlink, currently → `20260605`):
  `catalog_ships.json`, `catalog_items.json`, `catalog_skills.json`. Each file is
  a JSON object with a top-level `items` array. This source carries every field
  fitting needs (`slot`, `cpu_bonus`, `power_bonus`, `required_skills`, drone
  stats). The SQLite KB was rejected: its schema drops several of these fields and
  carries extra metadata that would confuse the issue.
- **Interface:** core logic in a reusable `pkg/fitting` package; a thin CLI
  `cmd/tools/fit` on top. Binary builds to `bin/` (per project convention).
- **Skills:** a single `--eng_skill_level` flag (default `0` = no reduction).
  Live-character skill loading (load an agent, as other tools do) is deferred to a
  later iteration.
- **Constraints enforced:** CPU + Power + slot-count-by-type, plus informational
  required-skill warnings. No other per-ship or per-module caps exist in the data.

## Confirmed Assumptions

1. **Bare hull.** Fitting assumes the player clears existing modules and fits from
   scratch — `default_modules` are ignored; the hull's full CPU/power/slots are
   available.
2. **Rounding: ceil (round up).** The server applies the skill reduction to the
   integer usage and rounds **up** to a whole unit. Example: base 25 reduced to
   23.9 → **24** units consumed. So effective usage =
   `ceil(base_usage * (1 - 0.01*engLevel))`.
3. Skill cap (Engineering max level 100 → 100% reduction → zero usage) is a known
   degenerate edge but a non-issue: reaching level 100 takes ~a year of in-game
   time. No special guard.
4. Capacity-adding modules (`cpu_bonus`, `power_bonus`) add to the ship's budget at
   their base value (not skill-modified).
5. Only constraints are slots + CPU + power + (informational) required-skills.

## Data Model (`pkg/fitting`)

Fitting-focused structs — only the fields fitting needs, not the full serverapi
types.

```
Ship {
    ID, Name        string
    CPUCapacity     int   // cpu_capacity
    PowerCapacity   int   // power_capacity
    WeaponSlots     int   // weapon_slots
    DefenseSlots    int   // defense_slots
    UtilitySlots    int   // utility_slots
    Tier            int   // tier (for sorting/output)
    DefaultModules  []string // default_modules (parsed, but ignored by fit math)
}

Module {
    ID, Name        string
    Type            string          // "weapon" | "defense" | "mining" | "utility"
    Slot            string          // "weapon" | "defense" | "utility"
    CPUUsage        int             // cpu_usage
    PowerUsage      int             // power_usage
    CPUBonus        int             // cpu_bonus (capacity-adding, e.g. tow rig)
    PowerBonus      int             // power_bonus (capacity-adding, e.g. reactor)
    RequiredSkills  map[string]int  // required_skills (may be absent)
}
```

**Slot mapping** comes straight from the module's `slot` field:
`weapon → WeaponSlots`, `defense → DefenseSlots`, `utility → UtilitySlots`. (Note:
`type: "mining"` modules carry `slot: "utility"`, so they consume utility slots.)

## Catalog Loader (`pkg/fitting/catalog.go`)

- `LoadCatalog(dir string) (*Catalog, error)` — reads the three JSON files from
  `dir` (default `data/game-api/latest`), unmarshals the `items` arrays into the
  structs above. Items file mixes many categories; keep only entries that are
  modules (have a `slot`/module type). Skills file is read only to obtain
  Engineering's `cpuEfficiency`/`powerEfficiency` (currently both `1`); the
  `--eng_skill_level` value is multiplied by these.
- `Catalog.Ship(id)`, `Catalog.Module(id)`, `Catalog.Ships()` lookups.

## Fitting Engine (`pkg/fitting/fit.go`)

Effective per-module usage helper:
```
effUsage(base, engLevel, effPerLevel) = ceil(base * (1 - 0.01*engLevel*effPerLevel))
```
(`effPerLevel` is 1 for both CPU and power from the skill data; passed in so it
is data-driven, not hardcoded.)

**`MaxFit(ship, module, engLevel) FitResult`** — iterative, because capacity-adding
modules raise the budget as you add them:
```
slotCap = slots for module.Slot
count = 0
loop:
    next = count + 1
    if next > slotCap: stop (slot-limited)
    cpuUsed   = next * effCPU
    powerUsed = next * effPower
    cpuCap    = ship.CPUCapacity   + next * module.CPUBonus
    powerCap  = ship.PowerCapacity + next * module.PowerBonus
    if cpuUsed > cpuCap:   stop (cpu-limited)
    if powerUsed > powerCap: stop (power-limited)
    count = next
return count + breakdown + binding constraint
```
(Iteration is bounded by `slotCap`, so it always terminates.)

**`CheckFit(ship, []ModuleCount, engLevel) FitResult`** — does an arbitrary
loadout fit? Sums slots-by-type, CPU, power across all requested modules (applying
ceil reduction per module), adds capacity bonuses, compares to capacities. Handles
mixed loadouts; the "2× / 3× of one module" case is just a single `ModuleCount`.

**`ShipsThatFit(ships, module, count, engLevel) []Ship`** — runs `MaxFit` for each
ship, returns those with `maxCount >= count`, sorted by tier then name.

**`FitResult`** carries:
- `Fits bool`, `MaxCount int`
- per-resource breakdown: slots used/avail (for the module's slot type),
  CPU used/avail, power used/avail
- `BindingConstraint` string (e.g. "utility slots", "CPU", "power")
- `SkillWarnings []string` (e.g. "requires drone_control 3") — informational only;
  does not block the fit.

## CLI (`cmd/tools/fit`, built to `bin/`)

```
fit check  --ship <id> --module <id> [--count N] [--eng_skill_level 0]
fit ships  --module <id> --count N            [--eng_skill_level 0]
  global: --catalog-dir data/game-api/latest
```

- `check`: prints max-fit for one ship plus the resource breakdown and binding
  constraint. If `--count` given, also prints a pass/fail for that target.
- `ships`: prints every hull that fits ≥ `count`, sorted by tier then name, with
  each ship's max-fit count.
- Both surface any required-skill warnings.

## Testing (TDD)

Small fixture catalog (Cobble hull + `advanced_drone_bay` + a `nuclear_reactor` +
the Engineering skill). Table-driven tests:
- slot-limited fit (drone bays on a low-utility-slot hull)
- CPU-limited fit
- power-limited fit
- reactor raises the power budget (iterative path: more fit than naive division)
- engineering reduction with ceil rounding changes the count at a boundary
  (e.g. base 25 → 24 at the right level)
- `ShipsThatFit` reverse query returns the expected hull set, sorted

## Out of Scope (later iterations)

- Live-character skill loading (load an agent) — only `--eng_skill_level` for now.
- Other skills beyond Engineering's CPU/power efficiency.
- Drone bandwidth/capacity modelling (no ship-wide cap exists; module-local only).
- Validating ceil rounding against the live server (flagged as the main
  correctness risk if behavior differs).
