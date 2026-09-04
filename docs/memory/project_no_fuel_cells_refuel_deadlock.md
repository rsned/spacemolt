---
name: project_no_fuel_cells_refuel_deadlock
description: "A fuel-dry worker with credits at a service-less station cannot refuel: refuel falls back to burning a fuel_cell from CARGO, and nothing in the worker ever BUYS one. Cause confirmed 2026-09-04; buying the cell is the fix"
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

## 2026-09-04 — CAUSE CONFIRMED by the operator; the fix is to BUY the cell

**Operator:** "if you buy a fuel_cell, it is used when you run 'refuel' if there
are no fuel services."

That settles the station-capability hypothesis above -- it was right. The
mechanic in full:

- Station HAS a refuel service -> `refuel` bills credits, no cargo needed.
- Station has NO refuel service -> `refuel` falls back to consuming a
  `fuel_cell` **from cargo**. Carrying none is what raises `no_fuel_cells`.

**So amend the headline of this file.** "Credits do NOT fix this" is true only
of handing over credits and walking away: nothing in the worker spends them on
a cell. Credits + a market selling `fuel_cell` + something that BUYS one is a
complete fix. Do not read the original claim as "money is irrelevant here".

**Half the fix now exists, and it is the wrong half.** `pkg/worker/refuel_sync.go`
detects a dry desk (`deskIsDry`: `no_fuel_source` / `station_fuel_empty` /
"reserves are depleted") and calls `client.RefuelFromCargo(ctx, "fuel_cell", 1)`
-- naming the item explicitly, because a bare `refuel` cannot reach cargo cells
while docked. Its own comment calls `fuel_cell` "the id a gift or a market buy
produces". But **no code anywhere buys one**: grep for a Buy of fuel across
pkg/worker returns nothing. So a worker docked at a station listing thousands of
cells, holding credits, still strands.

**Live cost, 2026-09-04:** fighter-9 / random-2 / random-5 / random-8 sat at 0
fuel and 0 credits at The Veil Anchor (BD+20 2457) and had to be recovered by
tanker. **These four were NOT a service problem** -- the canonical endpoint
shows The Veil Anchor carrying a full `refuel` service. They were simply broke,
which is the `no_fuel_source` branch (credits), not `no_fuel_cells` (cargo).
Do not cite them as evidence for the cargo-fallback bug; they are evidence that
a 0-credit worker at a fully-serviced station still needs a tanker.

**🟢 THE SERVICE LIST IS NOW DIRECTLY QUERYABLE — this file's "next step" is
unblocked.** `https://game.spacemolt.com/api/stations` (public, no auth) returns
every station with an explicit `services` array, so "does this station actually
refuel?" no longer has to be inferred from `station_fuel_prices`. As of
2026-09-04: **61 of 76 stations carry `refuel`**. The 15 that do not report
`services: []` outright -- 13 faction `outpost`s plus 2 newly founded stations
(Ashborne Reach, Fortress Blackthorn) -- so a service-less station is a faction
outpost, not a normal trade station. Check this endpoint BEFORE diagnosing a
refuel failure. See [[reference_station_id_aliases]] for the same endpoint's
authoritative base_id/poi_id map.

Remaining work: on `no_fuel_cells` (or a dry desk with an empty hold), buy
`fuel_cell` from the local market and retry, then fall back to a rescue record.
Related: [[reference_ship_to_ship_refuel_works_while_docked]] (the tanker path
that does work), [[reference_station_fuel_reserve_capture]].
