# In-Flight Claim Watchdog — Design

**Date:** 2026-06-27
**Status:** Approved design (decisions locked); implementation plan to follow.

## Goal

While a hauler transits to sell a claimed arbitrage haul, detect when the
destination's demand has degraded **below break-even for the cargo on board** and
react — re-route to a better live market, else continue and sell, else post a
cost-price sell order — so haulers stop delivering into markets that evaporated
mid-journey.

## Problem / Context

Haulers claim an arbitrage opportunity (buy @ source → sell @ destination, often
many jumps away), buy the cargo, then transit. The sell side is only validated at
**claim time**. Over a multi-jump trip the destination's demand can vanish or get
sold out underneath the hauler, turning a quoted profit into a real loss — the
exact dynamic behind observed losses (e.g. salvager-1 at −29.7k real net).

Marketbots already keep `market.db` fresh per system, so the destination's live
demand is observable mid-haul. The haul sell-leg autopilot already fires an
`OnWaypoint` callback at every jump (`pkg/worker/haul.go:676`) — a natural hook.

## Design Decisions (locked)

1. **Trigger = below break-even.** React when destination demand (best bids ×
   absorbable qty) no longer covers `buy cost + remaining fuel` for the held qty.
   Mere margin shrinkage that is still ≥ 0 does **not** trigger. Qty-aware
   (handles partial / sliced holdings and sell-book depletion).
2. **Reaction = tiered:** prefer re-route to the best live destination when it
   clearly beats continuing; else continue and sell at market; else arrive and
   post a cost-price sell order.
3. **Mechanism = phased:** Phase 1 the hauler polls the shared `market.db` at each
   waypoint (no new infra). Phase 2 (later, optional) adds a targeted marketbot
   scan-boost ("subscribe-notify") if per-jump freshness proves too coarse.

## Architecture

A watchdog that re-derives whether the claimed destination still clears
break-even for the cargo actually on board, and if not runs the reaction ladder.
All reads come from the shared `market.db` (marketbot-fresh); the watchdog adds no
new process or channel in Phase 1.

**Where it runs (grounded in the code):** the existing sell-leg autopilot
`OnWaypoint` closure (`haul.go:676`) only closes over `deps.KB` — it has no access
to the opp, destination, or metrics, and `Autopilot` cannot be aborted from it
(waypoint errors are non-fatal). So:

- **Step 1 evaluates once, on arrival** at the sell station (inside `haulSellLeg`,
  after transit + cargo check, before the sell), where the full `opp`/`sellSys`/
  `deps`/`m` context is in scope. No autopilot change. Covers the "arrived into a
  dead market — don't dump at a loss" case.
- **Step 2 adds per-jump monitoring**: thread a watchdog closure (with opp/dest
  context) into the sell-leg autopilot, and extend `Autopilot` so a waypoint check
  can request an early stop → `haulSellLeg` re-plans to a new destination. This is
  what saves the *remaining* jumps by re-routing before arrival.

```
marketbots ──► market.db ──► watchdog (per jump, OnWaypoint)
                               │  GetStationOrders(dest, item)  → break-even eval
                               │  if projectedNet < 0:
                               │     FindBestPrices(item,"buy")  → re-route finder
                               ▼
        ┌────────────────────── reaction ladder ──────────────────────┐
        │ Tier 1 re-route:   repoint haul to better live dest          │
        │ Tier 2 continue:   finish to original dest, sell at market   │
        │ Tier 3 cost-order: arrive, create_sell_order @ buy price     │
        └──────────────────────────────────────────────────────────────┘
```

## Components

1. **Break-even evaluator** (`pkg/worker`, pure): given the opp, held qty, and
   remaining fuel-to-dest, walk the destination's live buy book
   (`market.GetStationOrders(toStation, itemID)`), sum `min(remainingQty,
   orderQty) × orderBid` down the book (capped at held qty), and compare to
   `buyCostPaid + remainingFuelCost`. `viable = projectedNet ≥ 0`; trigger when
   `projectedNet < 0`.
2. **Re-route finder / find-a-market** (`pkg/worker` over `pkg/market` +
   navigation, pure given inputs): from the current system,
   `market.FindBestPrices(itemID, "buy", N)` → candidate buyer stations; for each,
   estimate reachability (nav jumps from the current system) and projected net
   after the extra fuel; return the best alternative whose projected net beats
   continuing by the margin, else none. Reusable standalone for stale-cargo
   liquidation ("find-a-market").
3. **Reaction ladder** (the watchdog, in the haul loop): on trigger →
   - **Tier 1 — re-route:** finder returns an alternative beating continue-by-margin
     → repoint the haul (release/retarget the claim, set new sell system + station,
     let autopilot continue to the new destination).
   - **Tier 2 — continue & sell:** no better destination → proceed to the original
     destination and sell at market (today's `haulSellLeg`; may be a small loss).
   - **Tier 3 — cost-price sell order:** a market sell would be a heavy loss / no
     buyers → on arrival, `create_sell_order` at the buy price (passive recovery)
     and release the claim.
4. **Recorder:** unchanged — it already records the actual outcome (now truthful)
   whichever tier fires; a re-route updates the recorded destination.

## Trigger & margin defaults (tunable)

- **Break-even:** `projectedNet = Σ_book(min(remainingQty, orderQty) × orderBid)
  − buyCostPaid − remainingFuelCost`. React when `projectedNet < 0`.
- **Re-route "clearly beats":** alternative `projectedNet ≥ continueProjectedNet
  + max(15% × buyCost, floor)` **and** reachable within `originalRemainingJumps +
  K` jumps. Prevents thrashing on marginal differences.

## Phasing / build order

- **Step 1 — safety net (on-arrival):** break-even evaluator + a `GetStationOrders`
  read wired into `haulSellLeg` before the sell, with Tiers 2/3. No autopilot
  change. Stops dumping into a dead market on arrival; establishes the evaluator.
- **Step 2 — reoptimizer (per-jump + re-route):** autopilot early-stop + threaded
  watchdog closure for per-waypoint checks, the re-route finder (find-a-market),
  and Tier 1. Re-routes before arrival to save the remaining jumps.
- **Step 3 — later, optional:** marketbot targeted scan-boost on claim (the
  subscribe-notify channel) for fresher destination data.

## Error handling & freshness

- The watchdog **never aborts the haul on its own errors**: a failed market read
  or re-route attempt logs and falls through to continue (Tier 2).
- **No / stale order data → do not trigger a re-route** (optionally log). Phase-1
  reads are only as fresh as the last marketbot scan of the destination; Phase 3
  addresses freshness.
- Re-route must be **atomic w.r.t. the claim**: never end up unclaimed-but-holding
  cargo without a sell plan.

## Testing

- Evaluator and re-route finder are **pure functions over injected market data** —
  table-driven unit tests including loss, partial-sellout, and no-data cases.
- The watchdog hook is tested via the existing `fakeClient` pattern (script
  `GetStationOrders` / `FindBestPrices` responses; assert the chosen tier).

## Out of scope (this spec)

- The marketbot subscribe-notify channel (Phase 3).
- Changing claim/scan cadence.
- Multi-item cargo (assume one claimed item per haul, as today).
