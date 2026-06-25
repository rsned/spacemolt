# Autopilot Tactical Station-Refuel — Design

**Date:** 2026-06-25
**Status:** approved (scope A only: tactical refuel; fuel-price intelligence + fuel-aware
routing deferred)

## Problem

The 2026-06-25 hauler validation stranded 72× at `jump 1/10 ... Insufficient fuel`.
Root cause: the worker `Autopilot` only ever burns cargo `fuel_cells`
(`autopilotRefuelIfNeeded` / `autopilotUseFuelCells`) — it **never station-refuels**
(pays credits for fuel). Haulers carry trade goods, not fuel cells, so a hauler that
starts a route low on fuel cannot recover and strands on the first jump.

Fuel matters for every mobile role; `Autopilot` is shared by hauler/miner/explorer, so a
fix there unblocks all of them at once.

## Decision

Add a tactical station-refuel step at the start of an `Autopilot` route, before the jump
loop, right after `parseFuelEstimates`:

- **When to refuel:** if the route's fuel estimate says it needs more than is available
  (`estimatedFuel > fuelAvailable`); or, when the estimate is unavailable, if current
  fuel is below `AutopilotRefuelThreshold` (0.5) of capacity.
- **How:** if not already docked, `Dock()`; then `Refuel()` (a no-arg, full-tank top-up).
  Best-effort — if the ship is **not** at a dockable station, `Dock()` fails, so log a
  low-fuel note and proceed (autopilot still falls back to burning cargo fuel cells, as
  today). The first jump auto-undocks.
- Keep the existing per-jump cargo `fuel_cell` burning unchanged.

**Deliberately out of scope (deferred):**
- No price awareness — pays whatever the station charges (`Refuel()` has no amount; it is
  always a full tank). Cost optimization is scope C.
- No fuel-price table / marketbot fuel capture (scope B).
- No *planned* mid-route fuel stops — a route needing more than one full tank can still
  strand mid-route. Scope C (fuel-aware routing) handles multi-tank routes.

## Components

- `needsRefuelForRoute(estimatedFuel, fuelAvailable int, fuel, maxFuel, threshold float64) bool`
  — pure decision fn (route-estimate first, fuel-fraction fallback; false if capacity 0).
- `ensureRouteFuel(ctx, client, out, estimatedFuel, fuelAvailable int) int` — orchestration:
  dock-if-needed + `Refuel()`; returns the (possibly increased) available fuel for display.
- `Autopilot` calls `ensureRouteFuel` after `parseFuelEstimates`, before the jump loop.
- `const AutopilotRefuelThreshold = 0.5`.

## Tests

- `needsRefuelForRoute` table: route-needs-more → true; route-within → false; estimate
  unknown + below threshold → true; estimate unknown + above → false; maxFuel 0 → false.
- `ensureRouteFuel` via `fakeClient`: low + undocked → records `dock` then `refuel`; low +
  docked → records `refuel` only; full → neither; dock failure → records `dock`, no
  `refuel` (best-effort, ship not at a station).
- Existing Autopilot tests stay green (their fake has a full tank → no refuel).

## Validation

Re-run the trader haulers: they should refuel at origin and actually reach their buy
stations (the gate then decides whether to buy).
