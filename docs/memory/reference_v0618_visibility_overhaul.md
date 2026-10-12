---
name: reference_v0618_visibility_overhaul
description: "v0.618 made the galaxy chart public and gated ship visibility behind scanner modules; get_system_agents returns NOTHING, scan <poi_id> replaces it, and the dispatcher is the only place the dead call can be stopped"
metadata:
  type: reference
---

Server **v0.618.0–v0.618.2**, absorbed 2026-10-12 in `6e9120a4`.
Client constant moved `v0.607.1` → `v0.618.2`.

## What broke for us

- ⭐🔴 **`get_system_agents` returns an empty list**, warns, and is slated for
  removal. It was scheduled on residents as a sensor net and called every hop
  from `KBWaypointCapture`, so it became pure wasted traffic against the per-IP
  budget that has blocked this fleet twice.
  **Fix: the dispatcher is the only chokepoint.** Schedule seeding is
  additive-only ([[reference_schedule_seeding_is_additive_only]]) — the name is
  already written into agents' `schedule.json` and fires forever regardless of
  the role config. So `pkg/worker/dispatch.go` now routes **both**
  `get_system_agents` and `get_nearby` to `GetNearby`.
- `subscribe_observation` no longer sends `system_agents`; `observation_update`
  drops `system_changed`/`system_departed`. Own-POI arrivals still stream.
- The marketbot sensor-net premise (system-wide visibility without travel) is
  **gone** unless bots carry a **ship scanner module** — and `ship_modules` has
  never captured a row ([[reference_ship_modules_never_captured]]), so we cannot
  tell whether any bot has one.
- Battles can only be *started* against a target at **your own POI**. Joining a
  running battle still works from anywhere in the system.
- Unseeable targets now return generic errors (`invalid_target`,
  `player_not_found`); `target_cloaked`, `player_not_here`, `different_location`,
  `target_in_transit`, `not_same_poi` are retired. **We never matched on any of
  them**, so nothing broke.
- An uncharted hidden POI behaves exactly like a nonexistent id in `travel`,
  `jump`, `find_route`, `inspect`, `faction_scan_poi` and drone MOVE.

## What was SAFE (checked, no action)

- **Station fuel capture.** `get_system` lost `fuel_reserve`/`fuel_capacity`/
  `fuel_price`/`faction_fuel_*`, but `parseGetBaseFuel`
  (`pkg/market/capture.go:101`) reads **`get_base`**, which the patch preserves.

## New capability

- ⭐ **`scan <poi_id>`** scans ships at another POI in your system — the actual
  replacement for `get_system_agents`. Ship-scanner gated (power 30/55/95 for
  I/II/III), runs at 10 less power than a scan at your own POI, returns
  `no_scanner` without one. `ScanResponse` was missing **eleven** fields;
  added `contacts`, `npcs`, `pirates`, `leviathans`, `poi_id`, `scan_power`,
  `signature_detected`, `swept`, `ship_name`, `cargo_types`, `message`, plus a
  `LeviathanContact` type that did not exist in Go at all.
- `survey_system` + survey scanner returns `deposits` — ore at **every** charted
  POI in one call. Better than per-POI `get_poi` polling and a direct answer to
  [[reference_poi_resources_remaining_is_unreliable]]. Not yet wired.
- Survey scanners now stack with diminishing returns (each extra adds 50% of the
  previous; disabled modules don't count). Two 60-power scanners give 90, not 120.
- Anomaly detector without survey power finds **wormholes only**.

## The galaxy chart is now public — and our map was already right

`get_map` returns all **506** systems with authoritative `connections`.
Diffed against the KB: see [[reference_connections_phantom_edges]] — the
phantom edges are **gone**, and the only gap was one system (`bnki`). Fixed.

Faction mobile-base commands (`get_/move_/jump_/cancel_/decommission_
faction_mobile_base`) surfaced as uncovered; declared unused since we own 0
faction bases.
