# Manual arbitrage: find_arbitrage / claim_arbitrage / release_arbitrage

**Date:** 2026-07-15
**Status:** Approved design, pending implementation plan
**Depends on:** arbitrage Sub-project B (net-of-fuel hauler economics) — MERGED + DEPLOYED 2026-07-15. Consumes `market.Collector.GetStationFuelPrice` / `MedianStationFuelAllIn`.

## Problem

The hauler fleet continuously scans and works market arbitrage, but a human
operator playing their primary agent through `play_as` has no way to *see* those
opportunities or to *take one without the fleet also grabbing it*. The operator
frequently travels for exploration tasks and would like to make money
opportunistically along the way — buy something near the current position, sell
it at or near the destination — provided the side-trip is a **minimal detour**
from the route they were going to travel anyway.

The primitives already exist: `play_as` wires `globalMarketCollector`
(`*market.Collector`) and `globalAgentID`; the store exposes
`GetOpportunities(status, limit)`, `ClaimOpportunity(id, agentID)`,
`ReleaseOpportunity(id, agentID)`. Haulers select only `status="available"`, so
an operator claim (status → `claimed`) automatically excludes the route from the
fleet. What is missing is the operator-facing surface.

## Goal

Three `play_as` REPL commands:

- `find_arbitrage <dest> [--detour N] [--limit N]` — list available opportunities
  that are on the way to `<dest>`, ranked so the most worthwhile low-detour hauls
  come first.
- `claim_arbitrage <id>` — claim an opportunity so the hauler fleet skips it.
- `release_arbitrage <id>` — release an opportunity the operator previously claimed.

All logic lives in `play_as`; no server change, no schema change, no new
persistence. Data comes from the existing scanner table (operator chose: read the
latest table, do **not** trigger a fresh scan per call).

## `find_arbitrage` design

### Inputs

- `<dest>` — a destination system, given as a system id or a system name.
- `--detour N` — max extra jumps the side-trip may add over the direct route
  (default **3**).
- `--limit N` — max rows to display (default **10**).

### Flow

1. **Resolve endpoints.** Current system from client state (`state.CurrentSystem`);
   error if unavailable (not logged in / no state). Resolve `<dest>` to a system id
   via a name→id map built from the KB; error if unknown.
2. **Load the pool.** `globalMarketCollector.GetOpportunities(ctx, "available", 300)`.
   `GetOpportunities` orders by gross profit descending, so a generous pool (300)
   ensures the small display limit is not starved of on-path but lower-gross
   routes. Only `available` opportunities are fetched, so every displayed row is
   claimable.
3. **Build the graph.** `globalKB.GetSystems` + `globalKB.GetConnections` →
   `navigation.JumpGraphFromConnections`; a `nameToID` map from the systems list
   (mirrors `plan_route` / `passenger_format`). Error if `globalKB` or
   `globalMarketCollector` is absent.
4. **Baseline.** `baseline = BFS(cur → dest)` (one BFS, reused across all opps).
   If `dest` is unreachable from `cur`, report it and stop.
5. **Per-opportunity detour + filter.** Resolve each opp's buy/sell systems via
   `nameToID` (skip and count if either name does not resolve). Compute
   `legs = BFS(cur→buy) + BFS(buy→sell) + BFS(sell→dest)`; skip and count if any
   leg is unreachable (`>= navigation.RouteInf`). Then:

   ```
   detour = legs − baseline
   keep if detour <= budget            (budget = --detour, default 3)
   ```

   `detour` is the extra jumps the side-trip costs beyond going straight to dest;
   `detour = 0` means the haul is exactly on the direct path. `detour` is clamped
   at 0 for display (BFS non-optimality never shows a negative detour).
6. **Net-of-fuel (marginal).** Price the *marginal* fuel the side-trip adds — the
   detour jumps, not the full route:

   ```
   fuelPerJump = currentJumpFuel(client, ctx, destSys)    // operator's ship; probe = dest
   pricePerUnit = priceOf(opp.FromStationID)              // pickup station
   net = opp.GrossProfit − float64(detour * fuelPerJump) * pricePerUnit
   ```

   `priceOf` resolves like Sub-project B: free-pump station
   (`grand_exchange_station`) → 0; else `GetStationFuelPrice(ctx, station)` all-in
   when present; else `MedianStationFuelAllIn(ctx)` (probed once per call); else 0.
   When `fuelPerJump` is unavailable (`ok=false`) or no fuel prices are captured,
   the fuel term is 0 and `net == GrossProfit` — the same graceful degradation as
   B. Using the detour jumps (rather than the full route) is deliberate: it prices
   the *marginal* cost of stopping for this haul versus flying straight to dest,
   which is the correct "is this side-trip worth it" metric.
7. **Sort + trim.** Sort kept rows by `net` descending (deterministic tie-break on
   `detour` asc then `id` asc), then take the first `--limit`.
8. **Render.** A table with columns:
   `#id · item · qty · buy (station @ system) · sell (station @ system) · +detour · gross · net`.
   The header shows `cur → dest` (baseline N jumps) and the active detour budget,
   plus a one-line note if any opps were skipped for unresolved/unreachable
   systems. JSON output mirrors the row fields for `--json` callers.

### The pure ranker

The filter+rank is a pure function so it is unit-testable without a client or DB:

```go
type arbRow struct {
    Opp     market.ArbitrageOpportunity
    Detour  int
    Net     float64
}

// rankDetourArbitrage keeps opportunities whose detour <= budget and orders them
// by marginal net-of-fuel profit descending. graph/nameToID resolve systems;
// fuelPerJump<=0 or a nil priceOf disables the fuel term (net == gross).
func rankDetourArbitrage(
    opps []market.ArbitrageOpportunity,
    curSys, destSys string,
    graph navigation.JumpGraph,
    nameToID map[string]string,
    budget int,
    fuelPerJump int,
    priceOf func(stationID string) float64,
    limit int,
) (rows []arbRow, skipped int)
```

`curSys`/`destSys` are system **ids** in the graph's node space; `nameToID` maps
each opportunity's `FromSystemName`/`ToSystemName` (which the store joins on read)
to those same ids. The handler is responsible for making the current-system value
and the graph nodes agree on that id space (mirroring `plan_route`, which resolves
everything through `globalKB.GetSystems`).

The command handler does the impure work (resolve current/dest, load pool, build
graph, probe fuel, build `priceOf`) and hands everything to `rankDetourArbitrage`,
then renders.

## `claim_arbitrage` / `release_arbitrage` design

Thin wrappers over the store, keyed on `globalAgentID`:

- `claim_arbitrage <id>`: parse `<id>` (int); `ClaimOpportunity(ctx, id, globalAgentID)`.
  On `true` print a confirmation naming the route (item, buy→sell) and that the
  fleet will now skip it; on `false` print that it could not be claimed (already
  claimed by someone, completed, or gone).
- `release_arbitrage <id>`: `ReleaseOpportunity(ctx, id, globalAgentID)`. The store
  enforces the agent-id match, so an operator can only release their own claim; on
  `false` print that there was nothing to release (not held by this agent).

Both require `globalMarketCollector`; error clearly if absent. `<id>` must parse as
an integer (matching `ArbitrageOpportunity.ID int`).

## Files

- **New** `cmd/tools/play_as/arbitrage_cmd.go` — the three handlers
  (`runFindArbitrage`, `runClaimArbitrage`, `runReleaseArbitrage`), the pure
  `rankDetourArbitrage`, and the small `priceOf` builder + free-pump set. Table/JSON
  rendering lives here (or a sibling `arbitrage_render.go` if the file grows).
- **Modify** `cmd/tools/play_as/main.go` — REPL dispatch cases for
  `"find_arbitrage"`, `"claim_arbitrage"`, `"release_arbitrage"`; help text.
- **Test** `cmd/tools/play_as/arbitrage_cmd_test.go` — drives `rankDetourArbitrage`
  with a hand-built graph and synthetic opportunities.

## Error handling / edge cases

- No `globalMarketCollector` (market DB absent) → clear error, no crash.
- No `globalKB` → error (the graph is required).
- Current system unknown (state nil / not docked-or-in-system) → error.
- `<dest>` unresolvable → error, list nothing.
- `dest` unreachable from current → report and stop (no baseline).
- Empty pool, or nothing within the detour budget → friendly "no on-the-way
  opportunities to `<dest>` within an N-jump detour" (not an error).
- Opps with unresolved names or unreachable legs → skipped, counted, surfaced in
  the footer.
- Fuel data absent (no rate, or no captured prices) → `net == gross` (degradation).

## Testing

- **Detour math + budget filter:** a hand-built graph where one opp is exactly on
  the path (`detour = 0`), one is a small detour within budget, one exceeds
  budget; assert the third is dropped and the first two kept.
- **Marginal net-of-fuel:** with a known `fuelPerJump` and a `priceOf` stub, assert
  `net == gross − detour*rate*price` for a kept row; and with `fuelPerJump = 0`
  assert `net == gross` (degradation).
- **Sort order:** two kept opps where the higher-gross one has the larger detour
  such that its marginal net is lower — assert the lower-gross, lower-detour one
  ranks first; assert deterministic tie-break.
- **Skip accounting:** an opp with an unresolved sell-system name and one with an
  unreachable leg both increment `skipped` and are absent from `rows`.
- **Limit:** more kept rows than `--limit` returns exactly `limit`, highest net first.
- `claim`/`release` are thin store wrappers; covered by manual smoke, not unit
  tests (no logic beyond arg parse + a single store call + message).

## Scope boundaries (YAGNI)

- **Table only, no fresh scan** (operator's choice). A future refinement could add
  a `--scan` flag or a staleness-triggered hybrid.
- **No cargo/affordability sizing** — the operator decides how much to buy; the row
  shows the opportunity's own `Quantity`.
- **No autopilot / no route execution** — `find_arbitrage` is informational; the
  operator travels and trades manually. `claim` reserves the opportunity against the
  fleet, not cargo.
- **Detour is BFS-based**, consistent with `plan_route` and the hauler; no
  fuel-optimal or risk-weighted routing here.

## Future work (not this spec)

- `--scan` / staleness-triggered fresh scan.
- A `my_arbitrage` view listing the operator's own current claims.
- Cargo-aware sizing / affordability against the operator's ship and credits.
- Optional autopilot handoff: "route me buy → sell → dest".
