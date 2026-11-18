# Frontier Full Explore — Design

Date: 2026-11-18. Status: awaiting operator approval.

## Intent

The pathfinder sweeps (rings, then interior voids) found about 238 frontier
systems, in 80 lane-connected groups. Most were only landed on and toured
once, with little or no survey power, so their hidden POIs are still hidden
and their relics have never been interacted with.

This run sends eight explorers back to work those systems completely. Why
now: a recent server patch makes faction intel terminals capture frontier POI
resource data when a member scans it, so every survey now feeds the faction's
intel, and CJ holds 30 Survey Scanner IIs.

## Goals

- Every frontier system surveyed at **90 survey power** in this run, so every
  hidden POI is revealed in one visit.
- Every POI in those systems visited, with resources, hazards, wildlife and
  hostiles recorded.
- `interact_poi` run once at every relic, and every reply recorded, so the
  relics that responded with results can be listed.
- Lanes between frontier systems walked, so the KB's cluster map is real
  rather than inferred.

## Non-goals

- No new sweep for undiscovered systems. A system found by accident (drift
  capture, a new lane) is added to the run, but nothing searches for them.
- No in-space refuelling between our own ships in this version, although the
  Reserves have a built-in refuelling rig.
- No fighting. These ships carry no weapons or defence modules.
- explorer-7 is not part of the run (operator is testing a hazard solution
  with it).

## Anti-goals

- A ship stranded without fuel in the frontier. explorer-5 and explorer-6
  both ended the last run that way and needed GSA tows.
- A ship looping on a system it cannot finish (hazard, hostile, unreachable
  POI).
- Two ships working the same cluster.
- A second trip to any system because the first had too little survey power.

## Decided by the operator

- **Roster:** explorers 1, 2, 5, 6, 8, 9, 11, 12 (Reserves: 1, 9, 11;
  Compendiums: 2, 5, 6, 8, 12). 5 and 6 fly home to CJ first to refit.
- **Fit:** every ship carries the pathfinder drive and **two** Survey Scanner
  IIs. All three are utility modules, which fills all three utility slots.
  Survey power is 60 + 30 = 90.
- **Start:** one ship per compass direction: N, NE, E, SE, S, SW, W, NW. Each
  picks a frontier system in its direction and begins there. No parallel
  ring paths.
- **Working a cluster:** the per-system work is what the `play_as` `explore`
  command does today. The run uses that same logic rather than a separate
  reduced version.
- **Relics:** at a relic POI, run `interact_poi` once the ship is there, and
  record every reply that came back with results.
- **Relic missions:** relics are not expected to offer missions. If a
  relic's prompt shows one, the ship skips that relic and logs it.
- **Hazardous POIs:** the ship enters, takes one dose, runs its commands
  there, and leaves.
- **Repair kits:** every ship packs **10 repair kits** from CJ faction
  storage (203 held; the run takes 80). A kit restores a small part of the
  hull when used.
- **Hull floor:** a ship at or below **50% hull** skips the POI and moves to
  the next one, until it has repaired from the kits in its hold. Every
  skipped POI is logged.
- **Moving on:** when the connected set is done, pathfinder-jump to the
  nearest frontier system not freshly explored, and continue.

## Builder's defaults (my calls)

### Fit

- CPU is the limit: drive 18 + two scanners 10 = 28. A Reserve has 28, a
  Compendium 32. So a Reserve keeps **no** other powered module and a
  Compendium at most 4 CPU's worth. Engineering skill trims module cost by
  about 1% per level, which may free a point or two; the fit step reads the
  live numbers rather than assuming.
- The fit step runs docked at CJ: read the ship (`get_ship`), withdraw two
  scanners from faction storage, unfit modules until the three utility slots
  and the CPU budget allow drive + two scanners, fit, and deposit what came
  off into CJ faction storage. It is idempotent: a ship already carrying the
  drive and two scanners does nothing.
- What gets removed, in order: other utility modules first, then weapons,
  then defence modules, lowest CPU-for-value last. The step logs the full
  before and after fit. **The operator sees the planned removals for all
  eight ships before any ship is refitted.**
- The run refuses to launch a ship whose `survey_system` power reads below
  90.

### One explore, shared

- `play_as explore` lives in `cmd/tools/play_as` (package `main`), so the
  worker cannot call it. The worker has its own shorter tour
  (`pkg/worker/explore_tour.go`) that skips several things `play_as` does:
  the survey-first ordering, faint signatures, survey anomalies, the wildlife
  second look, hostile capture and player sightings.
- The per-system logic moves into a shared package, `pkg/explore`, with
  output behind an interface. `play_as explore` / `auto_explore` and the
  worker both call it. `play_as` keeps its commands, flags and printed output
  unchanged. The worker's `systemTour` is replaced by the shared one, so the
  existing `explore` and `pathfinder_sweep` roles gain the same coverage.
- The relic step is added to the shared logic, so `play_as explore` gains it
  too.

### Per-system work (the shared explore, in order)

1. `survey_system`. Save newly revealed and already-revealed POIs, faint
   signatures, anomalies and the wildlife census.
2. `get_system`, save the system and its lanes.
3. Visit every POI, nearest-neighbour order, including POIs the survey just
   revealed. At each: `get_nearby` (players, creatures, hostiles),
   `get_poi` / update, a second wildlife look before leaving.
4. At a station the ship may dock at: dock, capture, refuel, undock.
5. At a relic: the relic step below.

Wildlife is recorded, never scanned or engaged: `scan` is a tick-costing
mutation that draws aggro, and these ships cannot fight.

### Relics

- A POI is a relic when its type is `relic`, or when its `get_poi` reply
  carries an `interaction` block (the server's own signal that
  `interact_poi` applies). The second rule catches archives under other
  types.
- The ship runs `interact_poi` once per relic per run.
- Every attempt is saved to a new KB table, `relic_interactions`: POI,
  system, agent, tick, the prompt text from `get_poi`, the reply message,
  `data_copied`, items recovered, any mission offered (raw JSON), and the
  error text if it was refused. "Responded with results" means
  `data_copied`, items recovered or a mission offered; the table keeps the
  empty and refused replies too, so "tried, nothing" is distinguishable from
  "never tried".
- Before interacting, the ship reads the relic's prompt (`get_poi`
  `interaction`). If the prompt carries a mission, the ship does **not**
  interact: `interact_poi` may accept the mission and board passengers in
  the same call. The relic is saved with outcome `mission_offered_skipped`
  and the prompt and mission JSON, for the operator to handle by hand.
- Items recovered stay in the hold and come home with the ship.

### Clusters and the ledger

- A **frontier system** is a KB system whose position source is not `api`
  (measured or estimated), plus any system the run discovers.
- A **cluster** is a set of frontier systems connected by lanes. It is
  worked breadth-first from the system the ship lands in. Lanes found while
  working extend the cluster. A lane that leads into charted (api) space is
  recorded and not followed.
- One shared ledger file, `data/sweep/frontier-run.json`, generated once
  from the KB by a small tool (as `void-zones` did for the void sweep) and
  then owned by the run. Per system: status (`todo`, `claimed`, `done`,
  `incomplete`), claimant, survey power used, POIs seen / visited, relics
  tried, the reason when incomplete, and timestamps. Writes take a file
  lock, as the void ledger does.
- A ship claims a whole cluster when it starts on it. Two ships that reach
  the same cluster from different ends: the second finds it claimed and
  leaves for its next target.
- **Freshly explored** means: surveyed at ≥ 90 power with every listed POI
  visited, in this run. Earlier visits do not count.
- **Incomplete** covers: a POI skipped at the hull floor and still unvisited
  when the ship left the system, fled a hostile, a POI the server refused
  travel to, or the per-system time cap hit. The skipped POIs are named in
  the ledger entry. A hazardous POI that was entered, worked and left after
  one dose counts as visited. An incomplete
  system is not retried by the same run; it is reported for the operator.
- The tool also prints run status (per direction, per ship, done / todo /
  incomplete counts) and the relics that responded.

### Directions and starting systems

- Bearings are measured from the galaxy centre, counter-clockwise from +x
  (the sweep code's convention): E 0°, NE 45°, N 90°, NW 135°, W 180°,
  SW 225°, S 270°, SE 315°. Each direction owns the 45° wedge centred on it.
- Each ship starts at the unclaimed `todo` system in its wedge **nearest the
  empire** (smallest distance from centre), so the outward leg is short and
  the first refuel point is close.
- Assignment: Reserves (4,000 fuel) take the wedges whose frontier systems
  lie farthest from a refuelling station; Compendiums (350 fuel) take the
  rest. The ledger tool computes this and prints the proposed assignment for
  the operator to confirm or edit before launch.
- "Next closest" after a cluster is the nearest `todo` system by straight
  distance from the ship, own wedge first. When its own wedge is finished, a
  ship takes the nearest `todo` system anywhere, so the run ends when the
  ledger has no `todo` left rather than when the first wedge empties.

### Travel

- To a cluster: by lanes to the charted system nearest the target, then a
  pathfinder launch at the target's recorded position, reusing the sweep's
  staging, launch, redirect and arrival code. Frontier positions are
  estimates; a launch that is captured by a different system works that
  system instead (added to the ledger if new) and tries again afterwards.
- Within a cluster: ordinary lane jumps.
- A target that cannot be reached after two launches is marked incomplete
  with the reason.

### Fuel and safety

- Before leaving a system, the ship must hold enough fuel to reach the
  nearest usable refuelling station plus one pathfinder launch in reserve.
  Below that, it goes to refuel first and resumes its cluster afterwards.
  This is the rule the void sweep used, and it is checked before every
  pathfinder launch and every lane jump inside the frontier.
- Compendiums carry fuel cells in the hold as a second reserve (number set
  at launch from CJ stock). The hold is otherwise empty.
- **Hazardous POIs are entered.** The ship travels in, runs the POI's
  commands (nearby, update, wildlife second look, relic step) and leaves
  after the first dose. It does not wait out a second dose to finish; what
  was not captured is logged.
- **Hull floor and repair kits.** Before travelling to each POI the ship
  reads its hull. At or below 50% it uses repair kits from the hold until it
  is above 50% or the kits run out. Still at or below 50%, it skips the POI,
  logs `skipped <poi>: hull N%`, and moves to the next. POIs skipped this way
  are retried once at the end of the system's tour if the hull allows by
  then. The floor applies to every POI, not only hazardous ones: a ship that
  low should not be travelling into the unknown.
- Kits are also used after a dose whenever the missing hull is at least one
  kit's worth, so a ship does not sit at 55% with ten kits aboard.
- A ship with no kits left and hull at or below 50% stops its cluster, goes
  to the nearest usable station to repair and restock, and resumes.
- Unchanged from the sweeps: flee hostiles
  (`battle stance flee`); never route through a stronghold without the
  pirate unlock; a ship that does strand calls a GSA tow rather than waiting
  for an assist.
- A known-lethal system (the Repeater Monolith at Gloaming, recorded
  leviathans) is entered in the ledger as `incomplete: hostile` at
  generation time and not visited.

### Worker role

- A new role, `frontier_explore`, in `sweep-fleet.yaml`, with one option:
  the ship's direction. State lives in
  `data/agents/<id>/frontier_state.json` (current cluster, queue, phase), so
  a restart resumes in place.
- Phases: `verify` → `stage` → `launch` → `work` → (`refuel`) → `next` →
  `done`. At `done` the ship flies home to CJ and docks.
- **Fitting is a pre-launch operator step, not a phase of the role**
  (builder check, my call). A fit tool has a dry-run mode that prints the
  planned removals per ship, and an apply mode run per ship after the
  operator has seen the plan. The role's first phase only verifies: drive
  present, two scanners present, `survey_system` power ≥ 90; otherwise it
  stays docked and says why. The wedge assignment is likewise written into
  `sweep-fleet.yaml` by the operator from the ledger tool's proposal.
- Ships launch as they become ready. The six at CJ do not wait for
  explorer-5 and explorer-6; a late ship takes its wedge when it joins.

## Left to the builder

- The exact package boundary when moving `play_as` explore into
  `pkg/explore`, provided `play_as` output and behaviour are unchanged and
  its tests still pass.
- The per-system time cap value and the fuel-cell count per Compendium.
- Whether a repair kit can be used undocked in space is assumed from the
  existing repair-kit code; confirm on the first ship before relying on it.
- The KB migration number and column types for `relic_interactions`.
- Ledger file layout and the tool's name and subcommands.
