---
name: project_no_fuel_cells_refuel_deadlock
description: "A fuel-dry worker with plenty of credits at a station that sells fuel still cannot refuel: bare client.Refuel(ctx) fails no_fuel_cells and the worker never buys fuel cells, retrying every 30s forever"
metadata: 
  node_type: memory
  type: project
  originSessionId: f744e650-ff1a-4add-9401-5a3087024568
  modified: 2026-07-30T04:23:25.807Z
---

Found 2026-07-29. **Credits do NOT fix this class of stuck worker — do not throw money at it and assume it is solved.**

**The proof (fighter-3, deliberately isolated):** it was restarted to force a fresh login, came back reading **cr=100,000**, **docked** at `sirius_observatory_station`, which carries a fuel price of **8/unit all-in** (`market.db station_fuel_prices`, captured 00:00:55Z). It needed ~159 units ≈ **1,272 cr** it demonstrably had — and the refuel still failed:

```
Station refuel failed: no_fuel_cells: No fuel cells found in cargo.
Buy fuel cells from a market or craft them.
```

So it is NOT credits, NOT station fuel availability, and NOT a stale local credit cache (that hypothesis was TESTED BY THE RESTART AND REFUTED — don't re-derive it).

**Distinguish the two error codes.** `no_fuel_source` = "No fuel cells in cargo **and insufficient credits** for station refueling" — that one IS a money problem and a gift fixes it. `no_fuel_cells` = cargo-only path, credits irrelevant. After a 100k/agent bailout, 5 of 14 recovered and refuelled; 5 stayed stuck on `no_fuel_cells` (fighter-3, fighter-1, explorer-8, random-3, random-4).

**🔴 THE `refueling_pump` HYPOTHESIS IS PROBABLY WRONG — DO NOT CHASE IT.** It was the first guess (the assist fleet "could not refuel ANYONE" until pumps were fitted) but the two mechanics are different, and the error strings say so plainly:
- `no_refueling_pump: You need a Refueling Pump module fitted **to transfer fuel to another ship**.` — ship-to-ship transfer, i.e. the RESCUE case. That is [[project_assist_fleet_refueling_pump_gap]].
- `no_fuel_cells: No fuel cells found in cargo.` — refuelling YOURSELF from cargo. No mention of a module.

So a missing pump does not explain a worker that cannot refuel itself. **Cause still UNKNOWN.** `ship_modules` has 0 rows so per-ship fitment is unverifiable locally anyway ([[project_fleet_asset_snapshots]]).

**⭐ 2026-07-29 — THE BARE `refuel` CALL IS EXONERATED; SUSPECT THE STATION.** A control was run at a *different* station: random-9 (prospector, docked at `the_core`/base `central_nexus`, 100,001 cr, fuel 0, cargo FULL of ore) was sent a **bare `refuel`** with no arguments — the identical call `autopilot.go` makes — and it worked first try: `Refueled at station. 100 units for 400 credits.` So the argument-less form is fine, credits are fine, and a full cargo hold does not block it.

That reframes the error text. `no_fuel_cells: No fuel cells found in cargo. Buy fuel cells from a market or craft them.` is the **cargo fallback path** talking — which is what you would expect when the station offers no refuel SERVICE at all, so there is nothing to fall back from. **Leading hypothesis is now station capability, not the client call:** `sirius_observatory_station` et al. have a `station_fuel_prices` row without an actual refuel service, and that row is probably derived from a `fuel_cell` market listing. Note the price evidence fits — `the_core` charged **4 cr/unit** here (400/100) and later 8 credits for ~93 units on assist-nexus, both far under the 8/unit that `station_fuel_prices` claimed for the failing station.

Next step: for one failing station and one working station, diff the facility/service list (`get_location`, facility sections) for a refuel service entry, rather than trusting `station_fuel_prices`. If confirmed, the worker fix is to treat `no_fuel_cells` as "this station cannot refuel me" → buy `fuel_cell` from the local market, or route to a station that can.

**Code site:** `pkg/worker/autopilot.go:126` and `:338` call `client.Refuel(ctx)` **bare**. The API is `refuel(item_id?, quantity?, target?)`. Fix candidates: (a) fit `refueling_pump` to the affected hulls; (b) when refuel returns `no_fuel_cells`, **buy `fuel_cell` from the local market and retry** — the server error literally instructs "Buy fuel cells from a market" and the worker never does. (b) is the general fix and also kills the retry loop.

**Two compounding defects in the same area:**
- **A failed refuel does not abort the route.** explorer-4 logged `WARNING: Fuel low (1%)`, **auto-undocked for a jump anyway**, failed `Insufficient fuel for jump`, and was left in flight holding a freight package — an undocked ship accrues a GSA recovery fee ([[reference_gsa_ship_recovery]]). Same family as the engineer-5 strand and the flat `rescue.FuelPerJump=5` ([[reference_ship_jump_time_and_fuel_formulas]]).
- **No backoff on a hopeless retry.** fighter-3/random-3/random-4 retry every ~30s indefinitely. This is the THIRD instance of one pattern — the mission accept loop fixed in `d6a739f` was another. Worth a generic "remember the server said no" rather than one call site at a time.

**Scale when found:** 9 of 38 mission-learn workers bricked (credits<100 AND fuel<15%), 14 of 38 broke. It is a closed loop — no credits → no refuel → no movement → no earning. **PRE-EXISTING, not caused by a fleet restart** (engineer-4/explorer-4/explorer-8 already read ~0 credits before any restart that day). Nothing prevents recurrence once the gifted wallets drain.

Also unrelated-but-adjacent: **engineer-4 was DETAINED by the Solarian Confederacy** with a 5,644 cr bounty — *"you will be detained again the next time you dock in their territory"* — so a bounty is a separate blocker from being broke, and Nova Terra is Solarian. Check for detention before diagnosing a stuck agent as merely poor.
