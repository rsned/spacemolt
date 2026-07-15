# Fuel-price feed (arbitrage Sub-project A)

**Date:** 2026-07-15
**Status:** Approved design, pending implementation plan
**Part of:** arbitrage net-of-fuel economics (A → B → C, see Future Work)

## Problem

The arbitrage/haul system does not know what fuel costs. `RankHaulOpportunities`
caps only the **approach leg** (hauler → buy station, `DefaultHaulMaxJumps=5`);
the **haul leg (buy → sell) is unbounded**, and `haulGate` checks the live
spread but never subtracts the credits burned refueling. So a hauler can take a
long haul whose fuel cost quietly eats a thin margin, and nothing accounts for
it.

True fuel cost is not a fixed route property: fuel *units* depend on ship
tier/modules (the server computes this per-ship via `find_route` →
`fuel_per_jump`/`estimated_fuel`), and fuel *price per unit* is live and
per-station (`get_base.fuel_price_all_in`, with low-supply profiteering). To
compute net-of-fuel profit for a route, a hauler must be able to look up the
all-in fuel price at a station it is **not currently docked at**. That price is
captured nowhere today.

This sub-project (A) builds only the **data feed**: marketbots capture each
station's latest all-in fuel price into market.db. Consuming it (net-of-fuel
ranking + gate) is Sub-project B; fuel-arbitrage-aware chained routing is C.

## Goal

A single, always-current, per-station all-in fuel price in market.db, readable
by any hauler for any station, with a freshness stamp. No hauler behavior
change in A.

## Data source

`get_base` (issued while docked at a station) returns, at the top level of
`GetBaseResponse`:

```json
"fuel_price": 2,          // base price per unit
"fuel_tax_per_unit": 5,   // per-unit tax
"fuel_price_all_in": 7    // ← what the hauler actually pays per unit
```

`fuel_price_all_in` is the per-unit cost to the hauler and is the value B
consumes. `GetBaseResponse` already has `FuelPrice`; the other two fields must
be added to the struct (`pkg/game/serverapi/responses.go:64`).

## Schema (market.db)

A new table, **one row per station, upserted in place — no time growth**:

```sql
CREATE TABLE station_fuel_prices (
    station_id        TEXT PRIMARY KEY,
    fuel_price        INTEGER NOT NULL,   -- base price per unit
    fuel_tax_per_unit INTEGER NOT NULL,   -- per-unit tax
    fuel_price_all_in INTEGER NOT NULL,   -- all-in per-unit cost to the hauler (B uses this)
    captured_at       TEXT NOT NULL,      -- freshness stamp on the single row
    captured_by       TEXT                -- agent id that captured it
);
```

Upsert is `INSERT … ON CONFLICT(station_id) DO UPDATE …`, so each capture
**replaces** the station's row. The table is permanently bounded by the number
of stations in the galaxy (~49 rows). This is deliberately the same
replace-per-capture shape as the existing `stations` catalog table (40 rows,
`collector.go:upsertStation`) and the opposite of the append-per-capture
`market_orders` firehose that reached 47 GB. `captured_at` is a freshness stamp
on the one row, NOT a new row per capture; nothing here ever needs pruning.

Added via a new market migration (`pkg/market/migrations.go` pattern);
`initialize_database.sql` regenerated if that repo convention applies to
market.db.

## Capture

1. Add `FuelPriceAllIn int64 \`json:"fuel_price_all_in,omitempty"\`` and
   `FuelTaxPerUnit int64 \`json:"fuel_tax_per_unit,omitempty"\`` to
   `serverapi.GetBaseResponse`.
2. New `market.Collector.UpsertStationFuel(ctx, StationFuel)` performing the
   ON CONFLICT upsert (mirrors `upsertStation`).
3. New worker dispatch command **`capture_fuel`**: calls `GetBase`, reads the
   three fuel fields from the response (via the client's raw-JSON store, the
   same way `update_market → market.CaptureFromClient` reads captured data),
   and upserts the row stamped with the worker's agent id. No-op gracefully
   (log, no error) when not docked at a base or when the collector is nil —
   mirrors `update_market`'s "no market collector configured" handling.
4. Register `capture_fuel` in the dispatch `supported` set
   (`pkg/worker/dispatch.go`).

A marketbot is a resident docked at its home station, so it captures **that
station's** price. Coverage grows exactly as the resident fleet spreads — direct
synergy with the home-station-enforcement work.

## Read API (for B)

```go
func (c *Collector) GetStationFuelPrice(ctx context.Context, stationID string) (
    allIn int, capturedAt time.Time, ok bool, err error)
```

B's single consumption point. Returns `ok=false` when no price has been captured
for the station yet (B falls back to its origin-price proxy in that case).

## Wiring + deploy

- Add `{ every: hourly, command: "capture_fuel" }` to the `resident`,
  `resident_gas`, and `resident_ice` role schedules in
  `data/overmind/roles.yaml`. (Craftsman is out of scope; leave unchanged.)
- Activation is operator-gated: rebuild `bin/worker` and redeploy the mb fleet
  (staggered). Can ride the same restart as the home-station-enforcement
  activation. Until then the feed is inert — no hauler consumes it yet (B).

## Scope boundaries

- A stores the **true** captured price. The **grand_exchange free-pump
  exception (hauler cost = 0) is a Sub-project B concern**, applied when B
  computes hauler fuel cost — NOT a capture-time special case.
- No hauler behavior change in A. No trend history. No pruning logic (bounded
  table).

## Testing

- Migration test: `station_fuel_prices` exists with the expected columns/PK.
- `UpsertStationFuel` / `GetStationFuelPrice` round-trip: insert, re-upsert the
  same station (row count stays 1, values + `captured_at` updated), lookup
  returns the latest value and `ok=true`; unknown station returns `ok=false`.
- `capture_fuel` dispatch test: fake client returns a `get_base` payload with
  the three fuel fields → one row written with the right `fuel_price_all_in` and
  `captured_by`; not-docked / nil-collector path is a logged no-op returning
  nil.
- `TestSeededCommandsAreDispatchable` picks up the new scheduled `capture_fuel`
  command and enforces it is in `supported`.

## Future work (not this spec)

- **B — Net-of-fuel hauler economics:** compute
  `net = gross − est_fuel_units × fuel_price_all_in` (units from `find_route`,
  price from `GetStationFuelPrice`; grand_exchange = 0) for approach + haul
  legs; use it in `RankHaulOpportunities` and add a fuel term to `haulGate`;
  bound/penalize the currently-unbounded haul leg.
- **C — Fuel-arbitrage-aware chained routing:** when a destination's fuel is
  dear but a neighbor station (near the sell station) is cheap, extend the route
  with an extra delivery leg to refuel there. Builds on the existing
  `sellChains` chaining in `haul.go` and B's fuel-cost model. A's per-station
  latest price is already sufficient to support C — no schema change needed.
- Optional fuel-price **trend** history for profiteering analysis would be a
  separate, explicitly-bounded rollup (like `market_ohlcv`), never an unbounded
  append table.
