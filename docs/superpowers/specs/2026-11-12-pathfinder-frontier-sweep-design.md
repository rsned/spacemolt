# Pathfinder Frontier Sweep — Design

Status: draft for operator review (brainstormed 2026-11-12).

## Intent

The galaxy has grown past its original radius. Nineteen "frontier" systems
appeared in two clusters 5,055–8,817 LY from the centre, with no jump-gate
connection to the old map and no coordinates in the API. Most of that band
has never been looked at. A Pathfinder Drive lets a ship travel by **bearing**
instead of by connection, so a ship can drift through open space and find
systems nobody can reach by jumping.

The sweep sends 10 explorer ships around the whole galaxy, clockwise, each
following its own concentric ring inside that band. Whenever a ship's drift
lands it in a system, it explores everything connected to that system, then
returns to its ring and carries on. The goal is **data**: every new system,
POI, station, resource, hazard, wildlife and hostile logged in the KB, so the
faction knows the new frontier before anyone else does.

## Goals

- Find and record unknown systems in the frontier band, all the way round.
- At every system a drift reaches, explore its connected cluster as fully as
  fuel allows, capturing everything the existing explore tour captures.
- Record each system's coordinates (measured where possible) so it can be
  reached again by pathfinder later.
- Bring every ship home alive: no ship ever runs dry in open space.

## Non-goals

- Fighting anything. Hostiles and dangerous wildlife are logged and avoided.
- Covering the whole band in one round. Round 1 is one 450 LY strip; later
  rounds reuse the same machinery with different rings.
- Mining, trading or hauling on the way.

## Anti-goals (would be a failure even if it "works")

- A ship self-destructing or dying adrift because it planned a launch it
  could not fuel.
- A ship parking in a hazard or next to repeaters (a Repeater Monolith group
  killed craftsman-1's Survey Vessel at Gloaming on 2026-11-12).
- Rings silently skipped or swept twice after an interruption (explore run,
  tow, restart): the sweep's coverage claim must be true.

## What is already known (measured)

- Pathfinder: `jump <numeric bearing>` with a pathfinder_drive fitted starts
  an open-ended drift. Bearing = degrees counter-clockwise from +x in KB map
  coordinates. Speed 10 map units (LY) per tick, straight line.
- Fuel is a **flat charge per launch or mid-drift redirect** (5× the hull's
  jump fuel; 45 on a Survey Vessel). No per-tick drain while drifting.
- A mid-drift redirect reply carries `origin_x`/`origin_y`: the ship's exact
  current position. The client already returns `JumpResult{Drifting:true}` for
  `pathfinder_jump`/`pathfinder_redirect`, and handles `pathfinder_arrival`
  (system, POI, XP, `poi_hazard`).
- fuel_cell restores 20 fuel (size 1); premium_fuel_cell restores 50 (size 2).
  They can be used mid-drift.
- pathfinder_drive: utility slot, size 10, 18 CPU, 30 power. Highest-fuel T3
  hulls that fit it: Reserve 4,000, Survey Vessel 1,020 (+25% fuel
  efficiency), Odyssey 1,000.
- The API gives **no coordinates** for frontier systems. The operator wrote
  estimated positions for all 19 into the KB `systems` table on 2026-11-12.
- Frontier hazard rate is ~30× the old galaxy's (6 of 19 systems). All six are
  in the south-west cluster. Repeater stats (Monolith 6,400 hull / 9,600
  shield, 8× Heavy Pulse Laser at 45 energy each) are in the `hostile_*` KB
  tables.
- GSA tows a ship that sits idle in a **system** for more than 20 minutes,
  even in frontier space. Running dry inside a system is survivable; running
  dry adrift in open space is not.
- Existing worker `explore` verb (`pkg/worker/explore*.go`): picks the next
  system by jump distance (undiscovered connection endpoints first), tours
  every POI with capture, docks/refuels at stations, skips POIs with moderate+
  hazards. The worker hazard gate (`pkg/worker/hazard_gate.go`) leaves or
  refuses hazardous POIs.

## Round 1 (decided by the operator)

- **Ships:** 10 explorer agents ("any 10 for now"), each on a T3 hull with a
  pathfinder_drive, holds stocked with fuel cells.
- **Rings:** 10 concentric rings 50 LY apart, radii 7,500, 7,550, … 7,950 LY
  from the galaxy centre (0,0). One ship per ring. This band is the densest
  part of the north-west cluster and passes just inside the south-west one.
- **Direction:** clockwise — the ship's angle around the centre *decreases*.
- **Steering:** every time a ship has covered 5° of arc since the start or its
  last course change, it re-aims to follow the ring (72 course changes per
  lap). At ~8,000 LY a 5° straight chord strays ~8 LY from the true circle.
- **Start:** from an already-explored frontier system in the band. Suggested:
  Facing Away (7,582 LY) or Still Point (7,620 LY), north-west cluster, no
  recorded hazards. Each ship first drifts radially to its own ring, then
  begins its lap.
- **Lap length:** ~50,000 LY ≈ 5,000 ticks ≈ 14 h per ship. Fuel: 72 course
  changes × 45 ≈ 3,240 per lap on a Survey Vessel, about 3× its tank, so each
  ship needs roughly 110 fuel_cells (or ~44 premium cells) on board.
- **Ending:** when the ship's fuel (tank + cells in the hold) falls to 75 or
  less, it makes a final jump toward the **nearest empire station system** and
  ends there.

## Behaviour at a system (decided by the operator)

When a drift brings the ship into a system that has jump connections:

1. Run an **explore run**: visit every system reachable through normal jump
   connections from there until no unvisited connected system is left, with
   the full capture the explore tour already does.
2. Fuel need not be held back during an explore run. If the ship runs dry
   inside a system, GSA tows it; it then refuels where it lands and pathfinders
   back to its saved ring position.
3. A partially explored cluster is acceptable: any recorded system can be
   reached again later by pathfinder.
4. Then **rejoin the ring**: drift in or out as needed from wherever the
   explore run ended to the ring, and continue the lap.

Once a system's coordinates are known (estimated or measured), a ship can
compute a drift bearing that intersects it directly. That is how a ship
returns to a saved position, rejoins its ring, or revisits a system later.

## Hazards and hostiles (decided by the operator)

A ship that **experiences a hazard leaves quickly**. A ship that finds
repeaters (or other hostiles) at its POI also leaves; it does not fight.

## Later rounds

Same scheme, rings stepped further in or out (still 50 LY apart) to cover the
rest of the 5,055–8,817 LY band.

## Builder's calls (my suggestions, not yet operator-confirmed)

- **Rejoin point:** rejoin the ring at the angle the ship had reached when it
  stopped, so nothing is skipped or swept twice.
- **Explore-run scope:** skip systems the KB already holds with full, fresh
  data; don't spend jumps re-capturing known ground. The explore run applies
  the same hazard/hostile leave rule.
- **Refuel when possible:** if an explore run reaches an empire station, dock
  and refuel before rejoining the ring.
- **Unfinished clusters:** log, per ship, each system left with unvisited
  connections, so a later run (or another ship) can follow up.
- **Drift fuel reserve:** before every launch or redirect, require tank +
  cells ≥ cost of that launch + one more + the final station jump. This is
  what keeps a ship from dying adrift; the 75-fuel rule is its end-of-lap
  form.
- **Position:** read from `get_location` while drifting; dead reckoning only
  if a reading fails. On arrival, write the system's measured position (the
  last drift reading) to the KB when it has no coordinates or only an
  estimate.
- **Leaving a hazard or hostiles:** the leaving launch simply resumes the
  ring (re-aim to the ring at the current angle); it is one of the lap's
  launches, not an extra detour. Detection: check `get_nearby` repeaters and
  the arrival `poi_hazard` on every arrival, and treat any `battle_started`
  event as an immediate leave (`battle stance flee`, the existing flee gate).
- **Pathfinder storms:** a `pathfinder_storm` deflection triggers a re-aim
  back toward the ring at the ship's current angle; any arc swept off-ring is
  logged as a coverage gap rather than re-swept.
- **Final jump home:** budget the reserve as one launch; if the homeward drift
  stops early in a system, the ship continues home by normal jumps (or waits
  for the GSA tow). After it ends, the agent returns to its normal schedule.
- **Agents:** the 10 explorers are taken off their normal work for the
  duration of their lap (dedicated, not interleaved with schedules).
- **Mechanics left to the builder:** how the sweep is driven (a worker verb,
  an overmind role, schedules), how ring state is persisted across restarts,
  how fuel cells are bought and loaded, and how ships are coordinated.

## Hull options (estimated 2026-11-12)

Launch cost = 5 × jump fuel; jump fuel = ceil(scale^1.5 × speed), less the
hull's fuel-efficiency bonus. Only the Survey Vessel is measured (45); the
formula gives 60 for it, so the others below are scaled by the same 0.75 and
must be confirmed on each hull's first launch. A lap is ~72 launches.

| Hull | For sale | Price | Tank | Cargo | Est. launch | Lap fuel | Cells needed per lap |
|---|---|---|---|---|---|---|---|
| Reserve | 3 | 99,764 | 4,000 | 150 | ~41 | ~2,950 | none |
| Compendium | 6 | 103,461 | 350 | 625 | ~23 | ~1,650 | ~65 (room for 600+) |
| Survey Vessel | 2 | 543,712 | 1,020 | 305 | 45 (measured) | ~3,240 | ~110 |
| Perigee | 4 | 683,569 | 620 | 270 | ~41 | ~2,950 | ~120 |
| Odyssey | 0 | — | 1,000 | 160 | ~28 | ~2,000 | ~50 |

Pathfinder drives: ~735 listed at 276,220 (Sirius Observatory, Nova Terra,
Alpha Centauri, Mobile Capital). Drift speed (10 LY/tick) is not believed to
depend on hull speed; confirm on first launch.

Suggested fleet: 3 Reserves + 6 Compendiums + 1 Survey Vessel = 10 hulls,
~1.4M in hulls + ~2.8M in drives. explorer-3 already flies a Compendium.

## Answered by the operator (2026-11-12)

- **Drifts stop when they intersect a system.** A ship finds only systems on
  its own path; the space between rings is covered by later rounds, not
  round 1. Each stop costs one extra launch to resume, so budget lap fuel
  for the 72 course changes plus one launch per system reached.
- **`get_location` reports x/y mid-drift.** Position is read, not estimated:
  dead reckoning is only a fallback, and ring angle/radius is computed from
  the live reading. A ship that has just finished an explore run launches
  toward its saved ring point and reads its position from the drift.
- **Hull mix approved:** 3 Reserves + 6 Compendiums + 1 Survey Vessel, each
  with a pathfinder_drive. Funding done: explorer-1 gifted 1,000,000 cr each
  to explorer-4, -7, -8, -10 and -11, so all 12 explorers can afford a hull
  and drive. Any 10 of the 12 may sail.
