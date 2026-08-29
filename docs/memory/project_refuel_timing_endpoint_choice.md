---
name: project_refuel_timing_endpoint_choice
description: "NOT BUILT — buy fuel at the cheaper END of a journey instead of topping off blindly at 50%. Today's needsRefuelForRoute is entirely price-blind; with a 13x galaxy price spread that leaks thousands of credits per refuel"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2cf71781-6ccf-44b8-b879-8971d8d06726
  modified: 2026-07-27T10:10:02.139Z
---

Operator recollection, 2026-07-27: *"check fuel amount and compare the price at both ends of the haul to decide whether to fill up before or after, assuming there was enough fuel to make the journey (we already have the fuel consumption formulas based on ship tier and jumps+travel)."*

**⭐ SHIPPED 2026-07-27 as `bc2adfe`, HAUL FLEET ONLY.** Opt-in per role via
`AutopilotDeps.FuelPriceAt`; `haulAutopilot` is the only caller wired up (it jumps the most
and its realized P&L is already recorded per run). Every other mobile role keeps the old
behavior bit-identical — `needsRefuelForRoute` delegates to `needsRefuelForRouteAt` with a
zero `fuelTiming`, pinned by `TestNeedsRefuelForRouteMatchesLegacy`. Deferrals log
`deferring refuel (X/unit here vs Y at destination)` so the change is observable.
Widening to other roles is the deliberate next step, once this is proven not to strand.

The rest of this note is the design record; it was written before the build and the
proposed rule below is what shipped.

Distinct from [[project_arbitrage_net_of_fuel]] sub-project C (which adds an extra *leg* to reach cheap fuel). This one adds **no legs** — it only chooses *which of the two stations you already visit* you buy at. Much cheaper to build, and strictly safer.

## Why the leverage is large

[[reference_station_fuel_price_spread]]: all-in price spans **2 → 26** (13×) and sol_central is the dearest in the galaxy. On a 380-unit hauler, deferring one half-tank fill from sol_central (26) to a 4/unit station saves **190 × 22 ≈ 4,180 credits per refuel**.

## The server rule that shapes the design (server_docs/openapi.json, /refuel)

> *"Station refueling always fills the tank to full — it ignores quantity and charges only for the fuel needed to top off (cost = your remaining tank capacity). quantity applies only to fuel-cell purchases and ship-to-ship transfers."*

Consequences:
- **A station refuel is BINARY per visit** — no partial fills. You cannot buy 40 units of cheap fuel.
- **Cost = (maxFuel − fuel) × allIn.** Cost scales with emptiness, so *arriving empty at a cheap station is optimal and arriving empty at a dear one is worst-case.*
- Total units consumed over a journey is fixed by the route; the only free variable is **the price you pay per unit**. So the optimal policy is simply: **buy at the cheaper endpoint, and when you buy there, fill completely** (a full tank at the cheap end also pre-pays the next leg).

Also from the same paragraph: station refuel *"draws **free** fuel from your faction's bunker (then allied bunkers) first, then charges **2–20 credits/fuel based on the station's reserve level**, plus any empire fuel tax."*

## Current behavior — price-blind

`needsRefuelForRoute` (`pkg/worker/autopilot.go:41`, `AutopilotRefuelThreshold = 0.5`) refuels when EITHER:
1. `estimatedFuel > fuelAvailable` — cannot make the route; forced, no decision to make.
2. `fuel/maxFuel < 0.5` — pure top-off heuristic, **no price input at all**.

Branch 2 is the leak and it leaks both ways: it fills at 26/unit whenever the tank dips below half, and it *declines* to fill at 2/unit whenever the tank is above half. Called from `Autopilot` via `ensureRouteFuel` (`autopilot.go:56`, invoked at `:126`) — i.e. **shared by every mobile role**, so a change here touches haul/mission/shuttle/assist/craft at once. Scope carefully.

## Proposed rule

Only branch 2 changes; branch 1 (can't make it) must stay forced.

```
E = estimatedFuel for the route, A = fuelAvailable
if E > A:                          refuel here (forced) — unchanged
else if price(dest) unknown:       fall back to current 0.5 threshold  (never gamble on the 29 uncovered stations)
else if price(origin) <= price(dest):  fill to FULL here, even above the 0.5 threshold  (opportunistic cheap fill)
else:                              skip — arrive as empty as possible and fill at dest
```

## Guards that must be in it

- **Stranding is expensive and the recovery path is already buggy** ([[project_rescue_pipeline_bugs]]). Never defer on a bare `E <= A`; require a margin, e.g. `A >= E + reserve` where reserve covers a detour/combat pull. A stranded hauler costs far more than 4,180 credits.
- **Unknown destination price ⇒ do not defer.** Coverage is 35/64 stations; the median fallback is not a real estimate given the 13× spread.
- **Price is reserve-dependent and drifts** ("2–20 based on reserve level"), and captures are hourly — treat a captured price as a reading with an age, not a station constant. Consider a staleness cutoff before trusting it to defer.
- **The route may not end where you think.** Deferring assumes you will actually dock at dest; a re-route (`WaypointCheck`) or an interruption invalidates the plan mid-flight.

## Free faction fuel is under-modelled (separate, easy win)

`haulFreeFuelStations` (`pkg/worker/haul_fuel.go:23`) hardcodes **one** station, `grand_exchange_station`, and its own comment concedes *"a future refinement can data-drive this from a captured ally_fuel signal."* That signal already arrives in `get_base` as `faction_fuel_capacity` / `faction_fuel_reserve` (operator sample 2026-07-27: 50000 / 42824) — and `parseGetBaseFuel` (`pkg/market/capture.go:102`) currently **discards both**. Capturing them would make free/near-free refuel stations data-driven instead of a one-entry allowlist. `station_fuel_prices` would need two columns.

Related: [[reference_station_fuel_price_spread]], [[project_arbitrage_net_of_fuel]], [[project_refueler_ship_roadmap]].
