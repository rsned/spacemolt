---
name: project_haul_departs_without_enough_fuel
description: "FIXED 4b84ac1b 2026-08-16: autopilot printed 'Not enough fuel! Need N more' and departed anyway, stranding 11+ agents across four fleets in one night; now returns ErrInsufficientRouteFuel without moving"
metadata: 
  node_type: memory
  type: project
  originSessionId: 9acefb51-d097-42a4-bd75-27e0bb652f32
  modified: 2026-08-16T17:06:13.279Z
---

**FIXED `4b84ac1b` 2026-08-16.** `Autopilot` now refuses to depart and returns
`ErrInsufficientRouteFuel` (exported; callers use `errors.Is`) instead of flying
a route it has already computed it cannot finish. The agent stays DOCKED and
recoverable rather than fuel-dead in deep space. This is EVERY mobile role, not
just haul — rolled to all seven fleets 2026-08-16 10:05.

**The bug, for the record.** The route planner printed its own verdict and then
ignored it:

```
Fuel: 15 per jump, ~225 total, 169 available
WARNING: Not enough fuel! Need 56 more.
[Jump 1/15] Jumping to GSC-0016...
```

`ensureRouteFuel` warns but neither blocks the departure nor tops up first, so
a hauler flies a route it has already computed it cannot finish and strands
mid-way. **This is the single biggest source of rescue-queue entries.**

**Live casualty 2026-08-15:** craftsman-1 left Korr Fortress on exactly the
route above and died at `westmark_star` with **4/400 fuel** — after correctly
banking 300,300 cr from the sale ([[reference_sell_leg_dock_gap]]). Same night:
salvager-3 fuel-dead 3/270 at Krynn/blood_arena, engineer-1 3/380 at
Nashira/nashira_cold_forge_seam.

**Why it bites hardest at strongholds:** the departure station is often a
stronghold with a DRY desk (korr_fortress 0/50,000 —
[[project_station_fuel_reserve_capture]]), so there is nothing to buy before
leaving, and the correct behaviour is to route to a wet desk FIRST rather than
depart short. `Collector.NearestFuel` already ranks reachable wet desks and is
wired into the assist role (`assistRetankElsewhere`) but **NOT into the hauler**
— from Gliese 581 it would have found node_gamma (6 jumps, all-in 4) and
the_core (7 jumps, all-in 4, 499k reserve) well inside the 11-jump range 169
fuel actually bought.

**Blast radius, measured 2026-08-16.** Within hours of the first three, EIGHT
more agents fuel-dead at station-less POIs across four fleets — johnny_cab
(Propus star), miner-1 (Maplevale star), miner-4 (Struve 1321 cryobelt),
miner-9 (Taygeta star), miner-10 (Ashford ice shelf), prophet-2 + overmind
(Syrma), trader-5 (Alphecca vapor fields). **This one bug was the dominant
source of rescue-queue entries fleet-wide.**

**Still TODO (the routing half):** on `ErrInsufficientRouteFuel` the agent now
stops safely but does not yet route itself to fuel. `Collector.NearestFuel`
ranks reachable wet desks and is wired into assist (`assistRetankElsewhere`) but
NOT into haul — from Gliese 581 it would have found node_gamma (6 jumps, all-in
4) and the_core (7 jumps, 499k reserve) inside the 11 jumps craftsman-1's fuel
actually bought. Wiring it needs `NearestFuel` added to the `OpportunityStore`
interface plus the test fakes.

**Meanwhile:** a fuel-dead agent at a station-less POI (a star, a belt) is a
textbook GSA tow — leave it undocked 30 min, ~500 cr, then delete its
rescue-queue record ([[reference_gsa_ship_recovery]]). Do NOT send assists; the
overmind had already marked craftsman-1 UNRESCUABLE after five attempts.
