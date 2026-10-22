---
name: index-gotchas-movement
description: Movement/docking/fuel gotchas: zero fuel, ship-to-ship refuel, connections, poi_resources, jump formulas, docked_at_base, station ids, GSA, gifts of hulls, commissioning
metadata:
  type: reference
---

# Gotchas — movement, docking, fuel

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- ⭐🔴 [Docked at 0 fuel is invisible to the watchdog](reference_docked_zero_fuel_invisible_to_watchdog.md) — `Stalled()` early-returns on Docked; hand-write a rescue record
- ⭐🟢 [Ship-to-ship refuel WORKS dock-to-dock](reference_ship_to_ship_refuel_works_while_docked.md) — client fuel field stays 0 after; confirm via `tank_full` or a jump
- ⭐🟢 [`connections` is CLEAN — resolved 10-12](reference_connections_phantom_edges.md) — matches the public v0.618 chart exactly (506 sys / 2132 edges / 0 phantom); wormholes are hidden POIs, never lanes, so nothing is missing
- ⭐🔴 [`poi_resources.remaining=0` is NOT depletion](reference_poi_resources_remaining_is_unreliable.md) — ore REGENERATES slowly; capital belts sit at 0 from over-mining yet still yield. Rank by `richness`, prefer non-capital. max_remaining fixed `847d47d2`
- ⭐ [Jump time + fuel formulas](reference_ship_jump_time_and_fuel_formulas.md) — `jumpTicks=max(1,7−speed)`, fuel `ceil(scale^1.5×speed)`; every flat constant is wrong
- [Travel is priced before it is measured](reference_travel_priced_before_measured.md) — zero-distance move to your own POI rejected as "Insufficient fuel"
- [Refuel fallback vs measurement](reference_refuel_fallback_vs_measurement.md) — a cargo-cell fallback misreports a dry station as one that sells fuel
- ⭐🔴 [Standing AT a POI is not being DOCKED](reference_sell_leg_dock_gap.md) — resumed haul looped `not_docked`; FIXED `2fea237a`
- [Pin-arrival needs FOUR checks](reference_pin_arrival_check_four_directions.md) — `docked_at_base` is EMPTY while docked
- [docked_at_base lives on the PLAYER](reference_docked_at_base_gotcha.md) — never the ship; dock events historically never recorded it
- [Pinned mission workers never refuel](reference_pinned_mission_workers_never_refuel.md) — refuelling is a ROLE property; FIXED `topUpAtPin`
- ⭐ [Player stations can refuse your dock](reference_player_station_access.md) — access is LEARNED; unverified = closed
- ⭐🔴 [Station ids are dual-named](reference_station_id_aliases.md) — find_item gives POI ids, docked_at_base gives BASE ids: never compare across commands; dronescript took POI-only until v0.598.3, now either
- [Lawless transit is safe; idling is not](reference_lawless_transit_vs_idle.md)
- ⭐ [GSA auto-recovery](reference_gsa_ship_recovery.md) — a drifting ship gets docked FOR you for a fee; a QUARANTINED agent needs its rescue record deleted
- [Jettison → loot transfer flow](reference_jettison_loot_transfer_flow.md) — hand cargo to a ship that cannot dock; verified working
- [Ship replacement workflow](reference_ship_replacement_workflow.md) — insurance pays credits, not a ship
- [Cargo liquidation](project_cargo_liquidation_cut_losses.md) — best bid, else DEPOSIT (never jettison)
- [drone_bay is an agent-wide ledger](reference_drone_bay_is_agent_wide.md) — drones keep mining across a ship switch; capacity fields report 0
- ⭐ [send_gift ship_id transfers a hull FREE](reference_send_gift_ship_transfer.md) — remote send, but the ship STAYS PARKED; recipient must travel to it. Credit gifts need no co-location
- ⭐🔴 [commission_ship has TWO modes](reference_ship_commissioning_is_credits_only.md) — credits-only 38,603 vs provide-materials 5,939 labour+components; BUYING at ~13,300 beats credits-only; outerrim hulls need an Outer Rim yard
- ⭐ [Rank freight hulls FITTED, not by base stats](reference_prayer_class_freight_hulls.md) — `cargo_expander_iii` = +100/slot; congregation = 1900 cargo, tier 1
- [Refueler ship roadmap](project_refueler_ship_roadmap.md) — Tanker step DONE 08-14 for 4 of 5 (see migration); tier-4 hulls next
- [Idle script authoring traps](reference_idle_script_authoring_traps.md) — dead idle_params, stale POI cache after jump, first-belt selection, override path
- [Reconnect wakes on input](project_reconnect_wake_on_input.md) — bounded burst then dormant, woken by REPL input
