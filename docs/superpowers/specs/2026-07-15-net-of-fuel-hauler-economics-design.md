# Net-of-fuel hauler economics (arbitrage Sub-project B)

**Date:** 2026-07-15
**Status:** Approved design, pending implementation plan
**Part of:** arbitrage net-of-fuel economics (A → B → C)
**Depends on:** Sub-project A (fuel-price feed) — MERGED + DEPLOYED 2026-07-14. Consumes `market.Collector.GetStationFuelPrice`.

## Problem

The hauler's opportunity selection ignores fuel cost. `RankHaulOpportunities`
(pkg/worker/haul.go) orders by `effectiveGross` (raw gross lifted by a stability
streak) and caps only the **approach leg** (`current→buy`, `DefaultHaulMaxJumps=5`).
The **haul leg (buy→sell) is unbounded**, and `haulGate` checks the live spread
(margin + net-profit floor) but never subtracts the credits burned on fuel. So a
hauler can pick a high-gross route whose long haul leg quietly eats a thin margin,
and a 40-jump haul is never penalized relative to a 3-jump one of similar gross.

Sub-project A now records each station's live all-in fuel price
(`station_fuel_prices`, one upserted row per station). This sub-project (B) spends
that data: it prices a candidate route's fuel, subtracts it from gross, ranks and
gates on the **net**, and bounds the haul leg. No new persistence.

## Goal

Haulers rank and gate opportunities by profit *net of fuel*, so nearer/cheaper
routes win over far/fuel-expensive ones of similar gross, and unprofitable long
hauls are rejected. The haul leg is bounded by fuel economics plus a hard
backstop. All changes are in `pkg/worker/haul.go` plus one small `pkg/market`
read helper.

## Fuel model

Computed once per haul decision pass, entirely client-side except a single probe
— **no per-candidate server calls** (see the `plan_route` / `currentJumpFuel`
precedent, cmd/tools/play_as/plan_route.go).

### 1. Rate — `fuel_per_jump`

One `find_route` probe per pass yields the ship's `fuel_per_jump`, which is
ship-constant across a route (mirrors `parseFuelEstimates` reading the `"_last"`
cache and `plan_route`'s `currentJumpFuel`). Prefer the server value; fall back to
the ship-class formula `ceil(scale^1.5 × base_speed × 10.0 × 0.10)` when the probe
is unavailable. If neither yields a value (`fuel_per_jump = 0`), fuel cost is 0
everywhere and ranking/gating **degrade gracefully to today's gross-only
behavior** — a safe no-op, not a failure.

### 2. Jumps — client-side BFS

The known jump graph is already built in `Haul` (`navigation.JumpGraphFromConnections`).
BFS gives exact jump counts between any two systems (unlike `find_route`, which is
current-relative and cannot route `buy→sell` from afar):

- `approachJumps = BFSJumps(graph, currentSys, buySys)` — already computed during ranking.
- `haulJumps = BFSJumps(graph, buySys, sellSys)` — same graph (the flow already
  computes this for metrics at haul.go:609-610).

### 3. Price — pickup station, free pump, median fallback

Fuel is priced at the **pickup (buy) station** (`opp.FromStationID`) — the hauler
docks there anyway and naturally tops up before the haul leg. Resolution
(`priceOf(stationID) → creditsPerUnit`):

1. If `stationID` is in `haulFreeFuelStations` (default `{"grand_exchange_station"}`,
   the databot faction's free ally pump) → **0**.
2. Else `GetStationFuelPrice(ctx, stationID)`; if `ok` → `float64(allIn)`.
3. Else (uncaptured, `ok=false`) → the **galaxy median** of captured all-in prices,
   computed once per pass via a new `MedianStationFuelAllIn` helper (conservative,
   data-driven). If no prices are captured at all, median is 0 → fuel-free
   fallback (same graceful degradation as a missing rate).

`haulFreeFuelStations` is a package-level set, defaulting to the grand_exchange
station id. A future refinement can data-drive it from a captured `ally_fuel`
signal (get_base carries `ally_fuel`); out of scope for B.

### 4. Cost

`legFuelCost(jumps) = jumps × fuel_per_jump × priceOf(pickupStation)`.

## Ranking changes (`RankHaulOpportunities`)

Replace `effectiveGross` with **`effectiveNet`** as the ranking value, per the
approved formula:

```
totalFuelCost = (approachJumps + haulJumps) × fuel_per_jump × priceOf(opp.FromStationID)
effectiveNet  = (opp.GrossProfit − totalFuelCost) × stabilityBoost(opp.CyclesSeen)
```

`effectiveNet` substitutes for `effectiveGross` everywhere in the function: the
near-tie band threshold (`maxGross * (1 − haulNearTieFraction)`) and both sort
comparators. The existing band structure is preserved — opportunities within the
near-tie fraction of the top **net** are still ordered by reposition jumps →
chaining bonus → cycles → id; the rest by net descending. `stabilityBoost` still
lifts durable routes (multiplying the net; a net ≤ 0 opp fails the gate regardless
of rank order, so the sign is immaterial to selection).

**Haul-leg backstop:** drop any candidate whose `haulJumps > HaulMaxHaulJumps`
(new constant, e.g. 20). Fuel cost already penalizes long hauls economically; this
is a hard safety cap for when price/graph data is thin (median≈0). The approach
cap (`DefaultHaulMaxJumps=5`) is unchanged.

`RankHaulOpportunities` gains two inputs — the probed `fuelPerJump int` and the
`priceOf func(stationID string) float64` closure. It already has `graph` and
`nameToID` to compute the haul leg.

## Gate changes (`haulGate`) — forward fuel only

At the pre-buy gate (haul.go:758) the hauler is **already docked at the buy
station**, so the approach-leg fuel is a **sunk cost** and must not affect the
go/no-go. The gate subtracts only the forward **haul-leg** fuel:

```
haulLegCost = haulJumps × fuel_per_jump × priceOf(opp.FromStationID)
net         = (sellBid − liveAsk) × qty − haulLegCost
reject if net < netProfitFloor(cargoCap)
```

The live-spread **margin** check (`haulMinMargin`) is unchanged — margin measures
spread health; fuel is a fixed forward cost applied to the net-profit floor. The
gate receives the precomputed `haulLegCost` (computed at the call site from the
same `fuelPerJump` + `priceOf` + graph), keeping `haulGate` a pure, unit-testable
function.

> **Design point to confirm at review:** ranking counts approach+haul fuel (it
> decides whether repositioning is worth it); the gate counts haul-only fuel
> (approach is already spent by the time it fires). This sunk-cost split is
> deliberate. The alternative — gate on approach+haul too — would let an
> already-paid reposition cost veto an otherwise-profitable buy, which is
> economically wrong.

## Plumbing

In `Haul()`, once per pass, before ranking:
1. Probe `fuelPerJump` (single `find_route`, ship-class fallback) → `haulFuelPerJump`.
2. Precompute the galaxy median (`MedianStationFuelAllIn`) and build the `priceOf`
   closure (captures the market collector, median, and `haulFreeFuelStations`).
3. Pass `fuelPerJump` + `priceOf` into `RankHaulOpportunities`.
4. At the gate call site, compute `haulLegCost` for the claimed opp and pass it to
   `haulGate`.

New `pkg/market` read helper:

```go
// MedianStationFuelAllIn returns the median captured all-in fuel price across all
// stations. ok is false when no prices have been captured.
func (c *Collector) MedianStationFuelAllIn(ctx context.Context) (median int, ok bool, err error)
```

## Edge cases / graceful degradation

- **No fuel rate** (`fuelPerJump=0`) → all fuel costs 0 → gross-only ranking/gating (today's behavior).
- **Uncaptured pickup price** → galaxy median; **no captured prices at all** → 0 → gross-only.
- **Unreachable haul leg** (`BFS ≥ RouteInf`) → the opp is already dropped by the reachability filter; the haul-jump cap is applied only to reachable opps.
- **Free pump** (`grand_exchange_station`) → 0 fuel cost, so routes pumping there rank as fuel-free.

## Scope boundaries

- No exact-at-buy re-check. Even though `find_route(buy→sell)` becomes exact once
  docked at the buy station, B keeps the BFS estimate everywhere for consistency
  and zero extra calls. (Listed in Future Work.)
- No persistence, no schema change. B is a pure decision-logic change over A's data.
- No change to the approach cap or stronghold route-safety filters.

## Testing

- **Fuel cost:** `legFuelCost` = jumps × rate × price; `priceOf` returns 0 for
  `grand_exchange_station`, the captured all-in for a known station, and the median
  for an uncaptured one.
- **`MedianStationFuelAllIn`:** median over an odd/even set; `ok=false` when empty.
- **Ranking flip:** two opps of similar gross — a near/cheap one and a far/expensive
  one — the far one sorts *below* once fuel is subtracted, where it would have tied
  or led on gross. And `fuelPerJump=0` reproduces the gross-only order exactly.
- **Haul-jump cap:** an opp with `haulJumps > HaulMaxHaulJumps` is dropped from the
  ranked output.
- **Gate:** a haul whose spread clears the floor on gross but not after haul-leg
  fuel is rejected; the margin check is unaffected; approach fuel does **not** enter
  the gate (a long-approach/short-haul opp gates the same as a short-approach one
  with identical haul leg).

## Future work (not this spec)

- **Sub-project C — fuel-arbitrage-aware chained routing:** when the destination's
  fuel is dear but a neighbor near the sell station is cheap, extend the route with
  an extra delivery leg to refuel there. Builds on `sellChains` + B's fuel model.
- **Exact-at-buy re-check:** at the gate (docked at buy), a single
  `find_route(buy→sell)` gives exact haul fuel; use it to refine the gate decision.
- **Data-driven free pumps:** capture `ally_fuel` from get_base so free-pump
  stations aren't a hardcoded set.
- **Refuel-stop attribution:** price fuel per actual refuel stop instead of a single
  pickup-station price (only worth it if the single-price model proves too coarse).
