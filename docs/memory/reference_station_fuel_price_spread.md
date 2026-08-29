---
name: reference_station_fuel_price_spread
description: "Station fuel all-in price varies 13x across the galaxy (2 to 26); sol_central is the MOST expensive. Our gates price a whole multi-jump route at the ORIGIN station's rate, which swamps most other economic factors"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2cf71781-6ccf-44b8-b879-8971d8d06726
  modified: 2026-07-27T08:58:57.924Z
---

Operator, 2026-07-27: *"fuel costs are not static across the galaxy."* Quantified against `station_fuel_prices` in `data/market.db` the same day.

## The shape of a `get_base` fuel block

```
"fuel_price": 2,            # commodity
"fuel_tax_per_unit": 5,     # tax, frequently LARGER than the commodity
"fuel_price_all_in": 7,     # what you actually pay -- ALWAYS use this one
```

## Live spread — 35 captured stations, 2026-07-27

| all-in | stations |
|---|---|
| **2** | `98eba8b1a7ad0520d6a7c8ea44b2d6aa`, `a356fc2c1744c0425cf6cf47f48def92` (hash-id POIs, tax 0) |
| 3 | ramens_rest, void_gate_outpost, first_step_memorial, unknown_edge_waystation, mobile_capital |
| 4 | node_alpha / node_beta / node_gamma, the_core, synchrony_hub, the_experiment |
| 6–8 | war_citadel, grand_exchange, treasure_cache_trading_post, the_levy, cargo_lanes, procyon, sirius, alpha_centauri, nova_terra, the_crucible |
| 9–12 | starfall_salvage, the_anvil_arsenal, ironhearth, factory_belt, blood_forge, traders_rest, the_rampart |
| 15 | market_prime_exchange |
| 21–25 | deep_range_outpost, iron_reach_mining_colony, gold_run_extraction_hub |
| **26** | **sol_central — the most expensive fuel in the galaxy** |

**13× spread end to end.** The commodity alone ranges 2→20 and the tax 0→6, so neither component is a safe proxy for the other.

## What this breaks

1. **Route cost is priced at the ORIGIN station's rate for the whole trip.** `pkg/worker/mission.go` (~:399) and `haulFuel.legCost` (`pkg/worker/haul_fuel.go:46`) both compute `jumps * fuelPerJump * priceOf(oneStation)`. The same 13-jump run at 8 units/jump costs **208 cr** priced from a 2/unit POI and **2,704 cr** priced from sol_central. That factor swamps most other terms in a mission/haul accept decision — a worker's accept behaviour depends heavily on *where it happens to be standing when it evaluates*.
2. **Only 35 of 64 stations are captured (55%).** Everything else falls back to `MedianStationFuelAllIn`, a single scalar — and this distribution shows there is no meaningful central tendency to fall back to. See the coverage section below: capture is WORKING, the gap is purely geographic.
3. sol_central being the dearest is a live trap: agents parked there (engineer-5 had 751k credits sitting there 07-27) systematically over-price every route they consider.

## What is CORRECT and should not be "fixed"

The all-in plumbing is right, verified 2026-07-27 — do not re-investigate:
- `pkg/market/collector.go` stores `fuel_price`, `fuel_tax_per_unit`, AND `fuel_price_all_in`.
- `GetStationFuelPrice` selects `fuel_price_all_in`; `MedianStationFuelAllIn` medians the same column.
- `buildPriceOf` (`pkg/worker/haul_fuel.go:70`) returns 0 for `haulFreeFuelStations`, else captured all-in, else median.

So we are NOT underestimating by reading the bare `fuel_price`.

## Coverage: capture WORKS, the gap is geographic (verified 2026-07-27)

Marketbots **are** capturing fuel on schedule — do not go looking for a broken collector:
- `capture_fuel` is a real dispatch command (`pkg/worker/dispatch.go:378`): `GetBase` → `market.CaptureFuelFromClient`.
- Scheduled `{ every: hourly, command: "capture_fuel" }` in `data/overmind/roles.yaml` (4 roles) and in **78 per-agent `schedule.json` files, 36 of them marketbots**.
- **29 marketbots landed a capture within the same hour** (08:00–08:01Z, 2026-07-27), plus roamers `random-6` (2 stations), `fighter-4`, `explorer-5`.

`station_fuel_prices` is **upserted on `station_id`, one row per station, never grows** (`pkg/market/types.go:25`). So row count ≈ number of distinct stations some agent has been *docked at*. Resident marketbots each cover exactly one station, hence the plateau at 35.

**The 29 uncovered stations are precisely the fleet's blind spots:**

| group | stations |
|---|---|
| 9 pirate strongholds | crix_stronghold, dross_citadel, kael_arsenal, korr_fortress, mera_sanctum, nyx_nexus, sable_port, thane_keep, voss_redoubt |
| Frontier pair | expedition_launch, scout_docks |
| 18 hash-id stations | e.g. `0321b3e4…` Ironlight Crossroads (lhs_1140), `1e2d6bd5…` Stellar Leap Station (tiaki) |

This is the **same list** as the operator's already-planned expansion (2026-07-26: *"adding 9 pirate stronghold market agents plus player owned station agents"*). That expansion would close the fuel-price coverage gap as a side effect — worth counting as a benefit when weighing it against the market.db write-load concern that has so far held it back.

## Units vs credits

The hull-intrinsic quantity is **fuel UNITS per jump** (`haulFuelPerJump`, probed from a live `find_route`), which is comparable galaxy-wide. engineer-1's mission hauler measured **8 units/jump** on 2026-07-27. Credits per jump = units × local all-in, so **any cr/jump figure is meaningless without naming the station**. A 56 cr/jump and a 176 cr/jump observation for the same ship are both correct at different stations — that is not a calibration error.

Related: [[project_smuggling_enablement]] (hull-vs-gate economics), [[project_fleet_asset_snapshots]], [[reference_haul_fleet_capacity_ceiling]].
