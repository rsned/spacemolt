---
name: index-fleet-ops
description: Fleet ops: IP blocks/rate limits, stronghold gates, rescue/quarantine, tankers/refuel, secondment, wedge/livelock, MaxRestarts, schedules, haul hulls, marketbots, drones
metadata:
  type: reference
---

# Fleet ops — live problems

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- ⭐🔴 [Faction storage reads REMOTELY; facility detail does NOT](reference_faction_capture_remote_vs_docked.md) — `facility` has no station param, so rent/level/status need a pinned resident; capture_faction is elected to ONE member (MIN player_id) and silently no-ops for the rest; `agent_storage_items` is PERSONAL only
- ⭐🔴 [Facility rent: per-station, INVISIBLE until you build](reference_faction_facility_rent_mechanics.md) — 11 of 14 CRFT facilities repossessed 10-12; `faction_deposit_credits` needs storage AT that station (23,562 fails); `faction_build` ignores personal storage; gift the parked leader
- ⭐🟢 [ROOT CAUSE of the IP blocks — FIXED](project_execute_loop_has_no_pacing_floor.md) — mine timeout desyncs w/ server pending → unpaced `loop -f` retries into instant "already pending" at 51/min vs a **30/min PER-SESSION** cap. Tick is 10s so 6/min is useful
- ⭐🟢 [Mission-query loop FIXED, undeployed](project_mission_query_loop_burns_the_ip_budget.md) — 44% of traffic; board never refreshes on request so slow-poll is safe. **IP-block counter is SHARED across the address** (per-bot game_query is 300/min, never close)
- [Player sightings timeline](project_player_sightings_timeline.md) — `2dfd83e9` DEPLOYED 08-29 to mb/assist/hunt/craft/shuttle; haul/unlock/mission-learn OLD binary; marketbot sensor net `2340e637` awaits mb restart
- ⭐ [MoltenOne: PLAYER hunter, KILLED 08-29 by hand-flown craftsman-1](reference_moltenone_player_hunter_ip_block_kills.md) — 10 losses in IP-block windows; wreck cc2128e8 has our ore; new `player_kill` event undecoded
- ⭐🔴 [Stronghold guard is re-written per role](reference_stronghold_guard_is_per_role.md) — 5 copies, ABSENT from assist/autopilot/hunt/explore/freight; 8 of 24 losses. Fix = one movement-layer gate
- ⭐🔴 [Stronghold gate false-locks on a COLD session](reference_stronghold_gate_false_lock_on_cold_state.md) — empty Standings reads as LOCKED, so autopilot-as-first-command is refused for an unlock the agent HAS; seed `get_status` first
- ⭐ [Tanker migration 08-14: how tankers work + hand-flown pickup](reference_assist_tanker_migration.md) — built-in pump, arrives full, 4 listings galaxy-wide, `switch_ship` is a SEPARATE step (nexus trap)
- ⭐ [Assist pump gap + pump-blind claim election](project_assist_fleet_refueling_pump_gap.md) — pre-tanker history; stale `requested_at` defeats distance routing
- ⭐🔴 [`facility_only=0` is NOT craftable in space](reference_facility_only_zero_is_not_craftable_in_space.md) — still needs a STATION plus the onboard refining module; reachability = hull + location + cargo, never the flag
- ⭐🟢 [GSA tow = 500cr / 15min; use it instead of a tanker](reference_gsa_tow_beats_the_tanker_fleet.md) — `distress_signal` is implemented and NO fleet code calls it; the tanker fleet's real niche is BROKE agents, so gift credits first
- ⭐🔴 [no_fuel_cells ≠ no_fuel_source](project_no_fuel_cells_refuel_deadlock.md) — cargo-cell fallback vs credits; nothing ever BUYS a cell. Service list is live at /api/stations: 61 of 76 refuel
- ⭐🟢 [Assist fleet re-hulled 09-09 — RESOLVED](reference_assist_fleet_is_dry.md) — all five now fly real tankers (sol Capacity, krynn Morningstar); the fleet is BROKE though, and a 0-credit tanker gets detained
- ⭐🔴 [Off-map == QUARANTINED, never launched](reference_rescue_queue_blocks_launch.md) — `restoreQuarantine` precedes the supervisor; `restarts:0` + zero `last_seen` is the tell
- ⭐🔴 [Fleet overrides are removed-SETS](reference_secondment_overrides_are_removed_sets.md) — commenting out a rotating agent's yaml line makes release a NO-OP; list rotators in BOTH yamls
- [Secondment drain must outlast RemoveDrainTimeout](reference_secondment_drain_and_rollback.md) — 4m; and must roll back on failure or the agent runs in no fleet
- ⭐🔴 [Health checks miss a wedged worker](reference_standing_loop_wedge_after_reconnect.md) — 6 workers, 3.5 days, dead scheduler; fix = bound every dispatched command
- ⭐🟢 [assist-frontier livelock FIXED 09-18](project_assist_frontier_mobile_capital_livelock.md) — 1.26M find_route (a QUERY, ~1/tick, not 11.5/min) vs dual-named MOBILE capital; restart doesn't fix, re-pin does; SIGHUP reloads a fleet yaml without restarting the overmind
- ⭐🔴 [Health checks miss a livelocked worker](reference_livelock_invisible_to_health_checks.md) — 47h on "Already docked"; find one: grep 'held for next pass' at tick cadence
- ⭐🔴 [MaxRestarts=100 parks an agent FOREVER](reference_crash_loop_cap_parks_agents_forever.md) — dashboard remove/readd does NOT free it; fixed `2114df0e`, undeployed
- [Stall-watchdog kills workers during initial connect](project_overmind_stall_kill_connect_loop.md) — burns MaxRestarts in ~8min when the game is slow; FUTURE
- ⭐🔴 [Settled-mission livelock](reference_settled_mission_livelock.md) — server lists a mission ACTIVE it refuses to complete; `abandon_mission` is the cure
- ⭐🔴 [Idle loop ran 3x per tick](reference_idle_loop_ran_3x_per_tick.md) — SleepTick/3 → ~43 passes/sec fleet-wide, the floor under the IP blocks; fixed to one tick
- ⭐🔴 [Rate-limit buckets, prose names, escalation](reference_rate_limit_buckets_and_escalation.md) — 1000 agents/IP is fine for others so SIZE IS NOT THE CAUSE; buckets named in PROSE; send_tally names the over-issued command
- ⭐🔴 [IP block 09-09 at 170 workers: freeze + staged-thaw runbook](reference_ip_block_20260909_freeze_and_staged_thaw.md) — STOP overminds BEFORE workers (a supervisor relaunch is a login); heartbeats/craft-spam ruled OUT; the fleet is just bigger than the per-IP budget
- ⭐🔴 [SIGSTOP/SIGCONT preserves game sessions](reference_sigstop_preserves_game_sessions.md) — zero logins; the safe tool during an IP block
- ⭐🔴 [CargoUsed drifts upward forever](reference_client_cargo_used_drifts_upward.md) — `deposit_items` never cleared the client cargo list; fixed 08-22, uncommitted
- ⭐ [15 of 22 haulers lost their freight hulls](project_haul_fleet_hull_attrition.md) — kill zones zaniah + goldcrest; the re-equip embargo is LIFTED (see next line)
- ⭐ [Gift-sourced hauling: consignment + consigned routes](project_consignment_haul_relay.md) — send side + engine SHIPPED, generator OPEN; claim must precede the gift; never model it as buy_price 0 (poisons the pool)
- ⭐🟢 [Re-hulling 10-10: congregation 1900 cargo @ ~6k = CONSUMABLE](project_haul_fleet_rehull_2026_10.md) — 74% of big-hull profit is from trips >65 units; rank on haul_results, never credit deltas
- ⭐ [Buying a hull by hand](reference_buying_a_hull_by_hand.md) — buy_listed_ship AUTO-switches + stores the old hull; autopilot lands you in SPACE; apostrophe POI names fail, use the id
- ⭐🔴 [congregation is OUTERRIM-EXCLUSIVE](reference_congregation_is_outerrim_exclusive.md) — only 7 outerrim shipyards; the ships table shows tier 0/no gate and does NOT reveal the lock, so commission_quote FIRST
- ⭐🔴 [TWO haul overminds for 7 days; the worker-less one corrupts haul-status.json](reference_duplicate_haul_overmind_corrupts_status.md) — status file + dashboard roster both lie; trust /proc PPid only
- [Haul routes through station-less Lawless systems](project_haul_lawless_routing.md) — stuck→restart; plus a cosmetic stale-docked heartbeat flag; fix queued
- [Haul fleet runs an old bin/worker](project_haul_fleet_worker_update_due.md) — needs drain+relaunch onto current build; requested 07-26, not done
- [Capture cadence retune APPLIED 09-08](reference_capture_cadence_retune.md) — 128 agents hourly→twice_daily; it did NOT stick from 08-30, so dry-run the script at EVERY fleet stop
- ⭐🟢 [Pirate unlock campaign — UNBLOCKED 10-02](project_pirate_reputation_unlock_campaign.md) — 5 code bugs, not mechanics; `an_introduction` SUPPLIES its cargo + grants stronghold passage + IS the whole unlock (baseline→10 on all 9 keys); `chain_next` absent from get_active_missions
- ⭐ [Marketbot unlock hot-swap rotation](project_marketbot_unlock_hotswap_rotation.md) — 09-09: stand-in GRADUATED but still parked in unlock; rotation #1 never started; 54 of 64 marketbots still locked
- ⭐🔴 [A long play_as command starves the scheduler](reference_long_foreground_command_starves_the_scheduler.md) — auto-explore held execMu ~3h: 2 of 4 hourly runs LOST to dropped ticks, then a double-fire; fixed by a per-hop `yieldExec()`; workers unaffected
- ⭐🔴 [Schedule seeding is ADDITIVE ONLY](reference_schedule_seeding_is_additive_only.md) — deleting a command from roles.yaml never removes it from agents; it fires forever. Pruning-by-absence is UNSAFE (per-agent tasks like capture_action_log exist)
- ⭐ [Ore→component conversion, hourly](project_ore_to_component_conversion.md) — refine_steel + copper wiring/piping on miners & residents; beats the 100k/item cap (overmind was capped on iron AND copper). `quantity`=OUTPUT items; new hourly tasks fire an hour AFTER restart
- ⭐ [Drone marketbots 010-019](project_drone_marketbots.md) — 09-11: drone_control 35-37 (+18 in 2 days), storage 66k→480k units. Watch **iron ore on 010 at 47% of the 100k PER-ITEM cap**; storage table keeps only the newest capture
- [Energy crystals RESERVED for drone project](project_drone_project_crystal_reserve.md) — ~6.1k held; never sell/craft them without asking
- [Titanium-alloy war demand](project_titanium_alloy_war_demand.md) — OPTION 08-30: galaxy-wide repair demand; 374k top-tier at grand_exchange; sol_central 242-ask arbitrage first; krynn pipeline pre-positioned
- [Mining fleet](project_mining_fleet.md) — created 08-21 as the unlock campaign's graduation destination; two-system loop, ore-selection refinements
- [Outer Rim capital is MOBILE; marketbot homes ≠ names](reference_outer_rim_mobile_capital_and_marketbot_homes.md) — Frontier has no station now; station-less residents never move; market_prime sits in Haven
- [Spare marketbot accounts for smuggling](reference_smuggling_spare_marketbot_accounts.md) — nine dormant, credentials present, in no fleet yaml
- ⭐🔴 [NEVER restart a disconnected worker](reference_never_restart_a_disconnected_worker.md) — a restart cannot reconnect it, only the gate can; 70 workers behind one paced gate ALWAYS outlast a 30min grace. FIXED a35f94c5
- ⭐🔴 [False rescue records: max_fuel=0 = STATE-READ FAILURE](reference_stale_rescue_records_outlive_the_strand.md) — the reconnect WEDGE manufactures them with no block at all (haul 16→0 in 7h); 6-for-6 "real" strandees were docked+BROKE, 1.5k credits total. Never xargs a username
- [Rescue pipeline bug package](project_rescue_pipeline_bugs.md) — failed records never retry, wrong assister claims, gift-locked fees (Distant Light, 07-16)
- [Worker heartbeat credits read stale 0](reference_worker_heartbeat_credits_stale.md) — gifts look failed when they landed; confirm in-game, not the status file
