# Interior Void Sweep — Design

Status: draft for operator review (brainstormed 2026-11-14).

## Intent

The frontier sweeps (round 1 and round 2, full laps on rings 7,200–8,550 LY)
look for hidden systems at the galaxy's edge. Hidden systems are not only at
the edge. Every one found so far sits at least 366 LY from the nearest
charted system (median ~1,000 LY). Inside the charted galaxy, charted systems
sit ~300 LY from their nearest neighbour, so any patch of space more than
350 LY from every known system could hold one.

The interior void sweep sends the same 10 pathfinder explorers through every
such patch inside the 7,150 LY line. Each explorer takes one void zone at a
time and flies a back-and-forth square wave (a lawnmower pattern) through it
with lanes 100 LY apart, so nothing in the zone is more than 50 LY from a
pass. The goal is the same as the frontier sweep: data. Every new system, POI,
station, resource, hazard, wildlife and hostile goes into the KB.

## Goals

- Cover every void zone inside 7,150 LY with lanes ~100 LY apart.
- Explore the cluster of any hidden system a drift lands in, exactly as the
  ring sweep does.
- Keep a true coverage record: which lanes of which zones have been flown.
- Bring every ship through alive.

## Non-goals

- The edge band (beyond 7,150 LY): the ring laps cover it.
- Re-sweeping a zone once it is marked done.
- Fighting, mining, trading or hauling.

## Anti-goals

- A zone marked done with lanes never flown (interruption, death, restart).
- Two explorers flying the same zone at once.
- A ship dying because it carried a damaged hull or parked on a hazard: the
  sweep safety rules of 2026-11-14 (commit bc5fa5aa) apply unchanged.
- A ship running dry in open space.

## Decided by the operator

- **Void definition:** the wider area. A point is void when its nearest
  known system (any position source: api, measured, estimate; excluding
  non-Sol rows still at 0,0) is more than **350 LY** away.
- **Area:** inside **7,150 LY** of the centre (the edge laps start at 7,200,
  with ±75 LY reach).
- **Zones** are connected regions of void at least **200 × 200 LY** in area.
  At 2026-11-14 that is 58 zones, 38.7M LY², ~387,000 LY of lanes. The
  void map is at https://claude.ai/artifact/M3DFdEbXkHWTPEze3hWtcp.
- **Order:** largest zone first, down the list. **One zone per explorer at a
  time**; a free explorer takes the next unclaimed zone until all are done.
- **Pattern:** square wave through the zone, lanes 100 LY apart.
- **Fuel:** inside the galaxy, refuel at stations (~100 now), not from fuel
  cells. Cells are not part of the plan; a ship that still has some may use
  them as before.
- **Start:** only after every explorer finishes round 2 (all sweep_state.json
  phase `done`).

## Builder's defaults (my calls, operator said "looks good")

- **Zone computation** runs once at sweep start from the live KB with the
  rules above (50 LY grid, 4-connected cells) and is written to a zone file
  that the run then owns, so zones do not shift mid-run as the KB grows. A
  tool regenerates it on demand.
- **Lane geometry:** lanes run parallel to the zone's long axis (the longer
  side of its bounding box), 100 LY apart, starting 50 LY in from one side.
  Each lane is clipped to the zone's own cells: a lane is the span from its
  first to last void cell on that line (one span per lane; a lane crossing a
  gap in a U-shaped zone flies straight over the gap). Lanes alternate
  direction. Lanes with no void cells are dropped.
- **Getting to a zone:** fly by jump lanes to the charted system nearest the
  first lane's start, then launch at the lane start; then fly the lanes.
  Stronghold systems are never used as staging or refuel points without the
  pirate unlock (standing rule).
- **Flying a lane** is one launch along it; the turn at the far end is a
  redirect toward the next lane's start (the 100 LY step), then a redirect
  along that lane. The ring sweep's waypoint test (`sweep.Reached`) and
  bearing math (`sweep.BearingTo`) are reused.
- **Landing mid-lane** (drift captured by a system): record the arrival,
  tour, explore the cluster if it is hidden, with every ring-sweep rule
  (leviathan / repeaters / hazard / charted-system exits, the 30-jump
  explore cap, unroutable targets dropped). Then relaunch toward the point
  on the current lane where it left it, and continue.
- **Fuel:** when fuel left falls to what reaching a station needs plus one
  launch of reserve, the ship stops the lane at its current point, flies to
  the nearest usable station (by lanes if it is in a system, else by a
  pathfinder launch toward the nearest station system), refuels, repairs,
  and resumes the zone at the lane and point where it stopped.
- **Claims and coverage** live in one shared file next to the zone file:
  per zone, the claimant and the index of the next lane to fly and the point
  on it. Updated after every lane. If an explorer dies or is removed, its
  claim is released at its last recorded lane; another explorer resumes it.
- **Done:** a zone is done when its last lane is flown. When no zone is left
  unclaimed, a free explorer docks at the nearest station and stops.
- **Reporting:** each zone records lanes flown, systems found and unfinished
  explores. The void map artifact gets a "covered" overlay from that file.

## Builder check answers (my calls, settled 2026-11-14)

- **Fuel by hull.** "~100" is the number of stations, not a stock limit.
  Tanks differ a lot: Reserve 4,000, Survey Vessel 1,020, Compendium ~350
  (about 7 launches). Zone claiming is hull-aware: the largest remaining zone
  goes to the explorer with the most launches per tank, and small zones go
  to the small tanks. Compendiums keep carrying fuel cells as a top-up, so
  they don't return to a station after every 2–3 lanes. Before claiming, an
  explorer checks it can reach the zone's first lane and a station from it
  with its reserve. If it can't, it skips that zone for the next one; the
  skipped zone stays in the queue for a bigger tank.
- **Dead or removed explorers.** A claim names its agent and is a lease,
  refreshed every time a lane's progress is saved. A restarted explorer
  takes back its own claim. A claim with no progress for 2 hours expires,
  and the next free explorer resumes it at its last saved lane and point.
  There is no heartbeat or overmind hook. Two explorers on one zone is only
  possible if a claim sits with no progress for 2 hours, which lane times
  rule out.
- **Storm deflections.** A deflected lane is re-flown from where the
  deflection happened. It counts as flown only when its end is reached. Each
  deflection is logged as a gap, as on the ring sweep.
- **Start gate.** The operator switches the fleet yaml from ring mode to void
  mode once all 10 sailing explorers are done. The worker does not check
  round 2 itself.
- **Regeneration.** The zone tool refuses to overwrite a zone file while it
  has claims or coverage. It writes a new run's file only on explicit
  request, after the current run is finished or archived.
- **Charted-system intercepts** on lanes and staging legs relaunch with no
  extra handling (the ring sweep's charted-system exit rule).

## Left to the builder

- Whether this is a new worker verb or a mode of `pathfinder_sweep`, and the
  file formats. Reuse of pkg/sweep is expected.
- How the fleet yaml switches explorers from the ring lap to the void
  sweep.
