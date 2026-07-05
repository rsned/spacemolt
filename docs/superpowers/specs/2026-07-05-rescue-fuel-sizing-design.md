# Dynamic Rescue-Fuel Sizing — Design

**Date:** 2026-07-05
**Status:** Approved, pending implementation plan

## Problem

Assist-fleet refuelers rescue fuel-stranded workers by flying to them and
transferring fuel ship-to-ship (`refuel target=<player>`, mode 2 — draws from
the rescuer's own tank, needs a Refueling Pump module). The transfer amount
(`rescue.Record.RescueFuel`) is sized once at *enqueue* time in
`cmd/overmind/rescueops.go` via `rescue.FuelForSystem` → `fuelForHops`
(`pkg/rescue/enrich.go`): `5 × hops_to_nearest_station + 5`, floored to 10.

Most strandees sit in a system that already has a station (hops = 0), so the
transfer floors to **10 fuel**. That is below a single big-hauler jump, so a
large-tank hauler burns it immediately and re-strands. Observed overnight
2026-07-04→05: `salvager-10` (420-fuel tank, cargo full) was rescued **11
times** in a non-converging loop; `trader-1` (240-fuel) **8 times**. 30 rescues
fired, 27 of them exactly 10 fuel.

The flat sizing has two faults:
1. **Too small for big tanks** — 10 fuel doesn't unstick a 420-fuel hauler.
2. **No rescuer self-preservation** — naively raising the amount would drain the
   rescuer's own tank (transfers come from it), stranding the rescuer. Rescuers
   already strand themselves in practice (e.g. `assist-frontier` at 1/120 fuel).

## Goal

Size the transfer *dynamically, at refuel time*, from the rescuer's live fuel:

```
transfer = MIN( strandee_remaining_capacity, rescuer_spare )

  strandee_remaining_capacity = max(0, rec.MaxFuel - rec.Fuel)      // fill the tank
  rescuer_spare               = max(0, rescuerFuel - homeReserve)   // keep enough to get home
  homeReserve                 = FuelPerJump*hopsHome + FuelBuffer   // = 5*hopsHome + 5
```

`rescuer_spare` is a **hard floor of 0**: the rescuer never gives away the fuel
it needs to get home. "The assist can get home after" is an invariant.

## Design

### 1. Pure helper — `pkg/rescue`

A pure, I/O-free, table-testable function holding the MIN/clamp logic. Reuses
the existing `FuelPerJump = 5` and `FuelBuffer = 5` constants in `enrich.go`.

```go
// TransferQuantity sizes a rescue fuel transfer: fill the strandee's remaining
// tank capacity, but never give away more than the rescuer can spare after
// reserving enough fuel to fly home (hopsHome jumps). Both terms clamp at 0.
func TransferQuantity(strandeeMaxFuel, strandeeFuel, rescuerFuel, hopsHome int) int {
    need := strandeeMaxFuel - strandeeFuel
    if need < 0 {
        need = 0
    }
    spare := rescuerFuel - (FuelPerJump*hopsHome + FuelBuffer)
    if spare < 0 {
        spare = 0
    }
    if need < spare {
        return need
    }
    return spare
}
```

### 2. Wiring — `pkg/worker/assist.go` `runRescue`

Computed after `navigate` succeeds and before `RefuelShip`:

1. `rescuerFuel` ← `int(deps.Client.GetState().Fuel)`
2. `hopsHome` ← BFS from `rec.SystemID` to the rescuer's own home system, using
   the same machinery `claimNearestPending` already uses:
   `deps.KB.GetConnections` → `navigation.JumpGraphFromConnections` →
   `navigation.BFSJumps`. The rescuer's home is `assistHomes[deps.AgentID]` or,
   for mobile homes, `assistResolveMobile`.
3. `qty := rescue.TransferQuantity(rec.MaxFuel, rec.Fuel, rescuerFuel, hopsHome)`
4. `deps.Client.RefuelShip(ctx, rec.TargetUsername, qty)`

`rec.MaxFuel` / `rec.Fuel` are the strandee values captured at quarantine
(`rescueops.go` already sets them from `LastStatus`). A stranded worker's fuel
stays ~0, so these are a good estimate of remaining capacity; the server clamps
any overshoot to the strandee's real tank room.

### 3. Degradation & edge cases

- **Fallback to enqueue estimate.** If `GetState()` is nil, `deps.KB` is nil, or
  `hopsHome` is unreachable (`RouteInf`), use `rec.RescueFuel` — today's
  enqueue-side value. Preserves current behavior as a safe floor and keeps
  existing tests that don't wire a KB unchanged. The `enrich.go` enqueue sizing
  stays as-is and simply becomes this fallback.
- **Rescuer can't spare anything (`spare <= 0`).** Skip the transfer and release
  the claim back to `StatusPending` (via `Queue.Transition(..., Claimed →
  Pending, mutate)` where `mutate` clears `ClaimedBy` so the record is freely
  re-electable) so a fuller or nearer rescuer takes it, or the same rescuer retries
  after it re-tanks at home. The rescuer then falls through to
  `assistEnsureHome`. This upholds the home-reserve invariant rather than
  stranding the rescuer to complete a rescue it can't afford.

### 4. Out of scope

- *Why a hauler sits stranded at a station POI with credits and doesn't
  self-refuel* — a strandee-behavior gap, separate from rescue sizing. Flagged
  for later, not fixed here.
- Refueler tank-capacity upgrades (`expanded_fuel_tank` module, Tanker-class
  hulls) — the hardware complement to this software fix; tracked separately as a
  roadmap item.

## Testing

- **`pkg/rescue` table test for `TransferQuantity`:**
  - big-tank strandee + healthy rescuer → capped by `spare`
  - small strandee + healthy rescuer → capped by `need`
  - far/low-fuel rescuer → `spare` clamps to 0 (caller skips)
  - zero/negative `need` → 0
- **`pkg/worker/assist_test.go`:**
  - existing test (no KB wired, asserts `qty == RescueFuel`) validates the
    fallback path unchanged
  - new test: wire `state.Fuel` + a KB connection graph, assert the dynamic
    `qty` passed to `RefuelShip`
  - new test: `spare <= 0` → no `RefuelShip` call, record returns to
    `StatusPending`, rescuer heads home

## Files touched

- `pkg/rescue/enrich.go` (or a sibling file in `pkg/rescue`) — add
  `TransferQuantity`
- `pkg/worker/assist.go` — `runRescue` computes `qty` before `RefuelShip`;
  fallback + skip-and-release handling
- `pkg/rescue/*_test.go` — `TransferQuantity` table test
- `pkg/worker/assist_test.go` — dynamic-sizing + skip-and-release tests
