---
name: project_station_fuel_reserve_capture
description: "SHIPPED 2026-08-15: hourly capture of station fuel-desk reserves via get_base + NearestFuel routing + assist dry-desk re-tanking; first sweep found 6 of 9 pirate strongholds run DRY desks, and faction bunker fields are member-only"
metadata: 
  node_type: memory
  type: project
  originSessionId: 9acefb51-d097-42a4-bd75-27e0bb652f32
  modified: 2026-08-15T12:01:56.235Z
---

**SHIPPED and LIVE 2026-08-15** (`eb02ac91`, rolled to all 7 fleets same night —
159 workers, 0 restarts). `get_base` carries `base.fuel/max_fuel` (public desk
reserve) and `faction_fuel_reserve/capacity` (bunker); the hourly `capture_fuel`
every worker runs now stores both in `station_fuel_prices` (six new columns,
-1 = never measured vs 0 = measured dry; upserts guarded on
`reserve_observed_at` so price-only writes can't fake a reading).
`Collector.MarkDeskDry` records live `station_fuel_empty` refusals between
sweeps. `Collector.NearestFuel` ranks re-tank stops (known-wet desk or allowed
faction bunker ≥ need, fresh ≤6h) by jumps then all-in price, excluding
measured-dry, unreachable, and caller-excluded stronghold systems. Assist
workers use it when the home desk is dry (`assistRetankElsewhere`) instead of
retrying an empty desk forever.

**First-sweep findings (05:00 2026-08-15, 49/53 measured):**
- **Six pirate strongholds run their fuel desks DRY as steady state**
  (voss_redoubt, sable_port, crix_stronghold, korr_fortress, thane_keep,
  kael_arsenal — all 0/50,000 at all-in 20). So even post-pirate-unlock,
  stronghold desks are not fuel sources; NearestFuel's measured-dry exclusion
  covers this regardless of rep.
- **`faction_fuel_reserve` is MEMBER-ONLY in get_base**: marketbot_haven's
  capture at grand_exchange saw the public desk (499,904/500,000, all-in 7 —
  cheapest big desk known) but -1 bunker, despite CRFT's 26,872-unit bunker
  being there (visible to the operator's member view). Bunker coverage needs
  the capturing agent to be in (or maybe allied with?) the owning faction.
- mobile_capital / frontier_station (same base, dual-named): measured 0/200,000
  — the desk that stranded assist-frontier, now visible in data.

**Why:** the assist fleet burned half a day failing refuels at a dry desk that
quoted a normal price ([[reference_station_fuel_price_spread]] prices say
nothing about stock). Ties into [[project_pirate_reputation_unlock_campaign]]
(unlocked agents can drop the stronghold exclusion) and the HELP⇄CRFT alliance
(free ally refuel at grand_exchange).

**How to apply:** dry-desk intel = `select station_id from station_fuel_prices
where fuel_reserve=0`. When adding capabilities, plumb per-agent
stronghold_access into the assist exclusion set. Consider a CRFT-member capture
at grand_exchange to get bunker numbers into the table.
