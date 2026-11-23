# Spacemolt Project Memory

<!-- ROUTER ONLY, keep under ~10 KB / 100 lines. Two tiers: this file (always loaded) → index_*.md hubs (one line per memory) → memory files.
     New memory: add its line to the matching hub, NOT here. Every memory file appears in exactly one hub (Resume-here files appear only here).
     Hard rules: max 15; a line goes here only if acting without it causes damage. Swap one out to add one. -->

## Resume here
- ⭐🔴 [CONTRABAND PIPELINE RESUME POINT 10-25](project_contraband_pipeline_resume.md) — plans 1+2+3 done & merged (main 06cd562b); moonshine line LIVE; dashboard on :8093; day-one metrics from /api/line/moonshine.json ~10-25 19:00
- ⭐🟢✅ [Line dashboard :8093 v2 (11-01): lines DERIVED](reference_line_dashboard.md) — from schedules/fleets/KB/asks; line-overrides.yaml only; why from task-outcomes.json; grouped by station
- ⭐🟢 [Cloverfield home station "Conjunction Junction" (founded 10-25)](project_cloverfield_home_station.md) — CRFT station, record set data/stations/cloverfield, --record-dir/station_snapshot/ledger, 1M floor/1M buy cap, approve every build; ships with 500k faction storage but 0 power/life support
- ⭐🟢 [Station #2 in beid (founded 11-16)](project_station_two_beid.md) — lead craftsman-2; LH2: miningbot_0000 gifts trucker-100 daily, trucker-100 hauls hourly; feeds combustor (bunker hit 0 on 11-20); marketbot_home2 drones live; spare bay+5 drones held at beid for a 2nd drone host
- 🟡 [Route fleet trucker-100..109 (11-20)](project_route_fleet_truckers.md) — workers started before 11-21 00:21 lack switch_hull; restart first; route.sock overmind, in COLD_START.md §3g since 11-21; gift unlock done; 10 spare congregations in CJ garage held for unlock truckers; first finisher collects junk_convoy at lhs_1140 (11-22)
- 🟡 [Purified water SM arbitrage (11-20)](project_purified_water_sm_arbitrage.md) — 10k @60 at CJ via miningbot_0000 faction_pull+shopkeep; haulers match stronghold SM bids; judge by haul_results; shopkeep one-ask/lot + deposit-sweep gotchas
- [Status board](project_status_board.md) — **READ FIRST.** The ≤15 live items, dated and linked. Roll old ones to the archive; never grow it.
- [Standing rules](feedback_standing_rules.md) — never `git add -A` with dirty data/*.json · haul `--stagger 10s` · unpushed? `git log origin/main..HEAD` · after memory edits: `make memory-sync` + commit docs/memory · NEVER edit an applied migration
- ⭐🔴 [NO stronghold routing without pirate unlock](feedback_stronghold_routing_requires_pirate_unlock.md) — per-agent rule; strongholds are dead ends; 8 of 24 losses incl. assist-sol's tanker
- Archives: [Aug](project_status_archive_2026_08.md) · [Jul 22–29](project_status_archive_2026_07.md) · [Jul 4–10](project_current_status.md) — misnamed: the OLDEST archive, 81 KB
- ⭐🟢✅ [Clerk-token login + host-wide connection gate LIVE 10-22](reference_clerk_token_login_and_connection_gate.md) — fleet cold start ~5 min (was 15–30), zero 429s; `--stagger` is dead; key in ~/.config/spacemolt/clerk_api_key; run clerk-sync after new accounts; 4001 parks; play_as lease; **key regen still pending**
- ⭐ [Cold start runbook re-synced 11-03: 11 fleets + fleet-watch](reference_cold_start_runbook_drift.md) — shuttle NOT retired; fleet-watch launches LAST; login gate stays 60/min (headroom)
- [Shipped-feature index](reference_shipped_history.md) — check before rebuilding anything
- [Sunless systems = generation artifact](reference_sunless_systems_intentional.md) — 9 systems, LLM-map slip, do NOT chase; real reveal hook is gsc_0026 faint_signature

## Hard rules (detail in the linked file)
- ⭐🔴 [Monitor reporting: actionable only](feedback_monitor_reporting_volume.md) — no per-landing/fragment/new-system posts; they flood the screen and bury losses + questions
- [NEVER restart a disconnected worker](reference_never_restart_a_disconnected_worker.md) — only the gate reconnects it
- [IP block: SIGSTOP/SIGCONT, stop overminds BEFORE workers](reference_sigstop_preserves_game_sessions.md) — a relaunch is a login; [runbook](reference_ip_block_20260909_freeze_and_staged_thaw.md)
- [Schedule seeding is ADDITIVE ONLY](reference_schedule_seeding_is_additive_only.md) — removing from roles.yaml never removes from agents
- [Fleet overrides are removed-SETS](reference_secondment_overrides_are_removed_sets.md) — list rotators in BOTH yamls
- [Off-map == QUARANTINED](reference_rescue_queue_blocks_launch.md) — check the rescue record before assuming a crash
- [Status files + heartbeat credits lie](reference_fleet_status_fossil.md) — trust /proc and in-game reads; [credits](reference_worker_heartbeat_credits_stale.md)
- [play_as statusline last field = GAME TICK, not credits](reference_play_as_statusline_last_field_is_the_tick.md)
- [play_as via `go run ./cmd/tools/play_as`](feedback_play_as_go_run.md) — never bin/
- [Station ids are dual-named](reference_station_id_aliases.md) — POI id ≠ base id; never compare across commands
- [Combat escape = `battle stance flee`](reference_combat_damage_pipeline.md) — `battle retreat` never exits
- ⭐🔴 [Battle flee gate (11-11)](reference_battle_flee_gate.md) — explorer-8 killed by a Molt Leviathan at Oxbow; non-hunt workers now `battle stance flee`; deaths log as SHIP DESTROYED
- ⭐🔴 [Environmental hazards v0.687 (11-10)](reference_environmental_hazards.md) — POI hazards 1/3/8/25% hull per dose; worker gate leaves/refuses; route planning NOT hazard-aware yet
- [Energy crystals: reserve LIFTED 11-10](project_drone_project_crystal_reserve.md) — drone project done; crystals are ordinary stock (scarce: sites exhausted)
- [craftsman-1 vacuum bids are INTENTIONAL](reference_craftsman1_vacuum_bid_economics.md) — never cancel
- [NEVER unbatched DELETE on a huge table](feedback_bulk_delete_scale.md)
- [Mining loops: hold-full check FIRST, unload the whole hold](feedback_mining_loop_hold_full_first.md) — snapshot-diff hand-off livelocked miningbot_0023 at 180/180 (11-11)
- ⭐ [Pathfinder drive mechanics (11-12)](reference_pathfinder_drive_mechanics.md) — flat 5x-jump fuel per launch/replot, no drift drain, 10 u/tick; range = hop count; T3 hull fit list
- [Pathfinder drive sourcing (11-13)](reference_pathfinder_drive_sourcing.md) — buy from Solarian station managers ~276k (daily price); building needs 18 facilities, thorium gone; buy deferred
- [Never `time.Sleep` in pkg/worker](reference_race_test_cost_sqlite_migrations.md) — use the `settle(ctx,d)` seam

- [Faction-storage crafting needs Officer rank; faction_withdraw lands in cargo](reference_faction_storage_crafting_and_withdraw.md) — live 10-24; craft falls back to CRFT storage, output goes there too; storage is PER STATION

- ⭐🟢✅ [Pipeline worker verbs SHIPPED 10-24](reference_pipeline_worker_verbs_and_flags.md) — mine_qty_at/pipeline_miner, Lawless gate, shopkeep, faction_pull; yaml fields pipeline/shopkeep/faction_pull; smoke in progress

## Area hubs — read the hub before working in that area
- [Fleet ops](index_fleet_ops.md) — 52: IP blocks/rate limits, stronghold gates, rescue/quarantine, tankers/refuel, secondment, wedge/livelock, MaxRestarts, haul hulls, marketbots, drones, faction facilities
- [Data capture gaps](index_data_capture.md) — 13: silent data loss, stale status files, ship_modules, asset/storage/action-log capture, wildlife observations
- [Haul & freight](index_haul_freight.md) — 25: revenue, book depth, capacity ceiling, market.db index, scanner, freight contracts/probation, arbitrage
- [Server API](index_server_api.md) — 40: version changes v0.322–v0.618, struct drift, catalogs, KB schema/migrations, withdraw_items, chat, mission chains, facilities, factions
- [Combat & wildlife](index_combat_wildlife.md) — 16: damage pipeline, reload/ammo, modules, battle log/replay, wildlife scans, spar, pirate bands
- [Movement, docking, fuel](index_gotchas_movement.md) — 25: zero fuel, refuel, connections, poi_resources, jump formulas, docked_at_base, GSA, hull gifts, commissioning
- [Code & data gotchas](index_gotchas_code.md) — 12: SimpleHandler drops pushes, rawjson keys, test traps, GameClock, Sleep constants, shared kb DB, deploy verification
- [Market & economy](index_market_economy.md) — 24: fuel prices/reserves, arbitrage traps, market_orders/ohlcv, perf, customs, tax, find_item, demand
- [Overmind, ops, deploy](index_overmind_ops.md) — 26: launch commands, fleet-watch, rollouts, quiesce/park, drain, dashboards, login limits, prune, capability ledger
- [Crafting & mining](index_crafting_mining.md) — 12: supported_power, random yield, ore regen, drone refit, BoM explorer, public_facilities, crafting brain
- [Agents, play_as, social](index_agents_social.md) — 22: bounties/tax, citizenship, send_gift, play_as, passengers, shuttle, smuggling, missions, mbox, LLM/ToT

Not in a hub? `grep -ril '<keyword>' .` in this directory — every file's `description:` line is searchable.
- [Public stations API](reference_public_stations_api.md) — game.spacemolt.com/api/stations: every station/outpost live; find player stations without a marketbot; mobile pins OK
- [Gift unlock + officer storage split](reference_gift_unlock_and_officer_storage_split.md) — send_gift needs 1,000 earned credits; gifts land in officer's own storage → faction_deposit sweeps
- [shopkeep+consign sharing an item = silent crash loop](reference_shopkeep_consign_overlap_is_fatal.md) — worker log.Fatalf goes to stderr, not the overmind log; parks at 100 restarts
- [Facility speed + rent rules](reference_facility_speed_rules.md) — recipe time = hand baseline; L1 5x, +3x/upgrade; admin +15%/tier (2 tiers); recruit +5%/lvl; rent 3%/day (x2 pirate); hand-craft queues are per agent, so spread them
- 🟡 [Hull costs page merged 11-03](project_hull_production_costs.md) — KB private hulls.html; migration 66 (quote/license capture) NOT applied to live DB yet → run bin/kb-migrate first
- 🟢 [CRFT owns congregation license (11-02)](reference_congregation_is_outerrim_exclusive.md) — 1M paid; build at CJ, 10% royalty (~662) to outerrim; treasury now ~1.0M = AT floor
- [Haul leftovers → Cloverfield (11-01)](reference_haul_leftovers_to_cloverfield.md) — haulers carry failed-arbitrage cargo, gift to miningbot_0000 at CJ within 3 jumps; faction_deposit_all sweeps it
- [pgrep self-match + sole-member fleets](reference_pgrep_self_match_and_sole_member.md) — use `[b]in/...` patterns; a fleet's only member can't be override-removed, SIGSTOP it for play_as
- [POI rename left stale KB rows](reference_poi_rename_stale_rows.md) — ~10-13 themed renames; old rows double-count belts and nebulae; trust live get_system
- [Commission: supply the materials](reference_commission_supply_materials.md) — anyone can commission; cash-price builds stall if mats are not on the market
- [Transit hub passenger strategy](reference_transit_hub_passenger_strategy.md) — LT1428 hint: 150-seat concourse + ~9 speed-6 ships feeding it; CJ lounge L1 built 11-08; shuttle worker has no lounge support yet
- 🟡 [Stronghold demand: reactor cores (11-08)](project_stronghold_demand_reactor_cores.md) — station_reactor_core bid 1.24M at 7 strongholds, ZERO asks; ~535k input cost, neutronium-limited; salvage_metal 4,370 bids
- [Schedule due batch not cancellable](reference_schedule_due_batch_not_cancellable.md) — removing a task mid-batch does not stop it; one-off buys fire twice
- ⭐ [Uncharted-system fragments (11-13)](reference_uncharted_system_fragments.md) — "Unknown source" hex on system chat; Fibonacci cipher; system history is per-SYSTEM so only push capture keeps them; sweep workers save to mbox
- [Pathfinder frontier sweep (11-12)](project_pathfinder_frontier_sweep.md) — spec+plan approved; MERGED 11-13 + migration 75 applied; ops pending: 7 drives in CJ faction storage, buy hulls
- ⭐🔴 [RESUME: interior void sweep rollout 11-15](project_interior_void_sweep.md) — round 2 done; void run started for ready ships (1,2,5,7,9,11,12); held: -8 Winterhold (assist-frontier refuel), -6 quarantined frontier, -4 undecided; 4 commits unpushed
- [Drones mine radioactives (11-15)](reference_drones_mine_radioactive.md) — no special gear; but no-station rad mining = miningbots w/ rad_harvester + lead_lined_cargo from an outpost, flee on hostiles
- [Faction intel → KB (11-15)](reference_faction_intel_tables.md) — faction_intel_* tables (mig 76), capture_intel twice_daily on craftsman-3; hazards merged into pois
- [Intel Center L2 auto-sync (11-13)](reference_intel_center_autosync.md) — CJ intel auto-collects verified POI/system data from member reads; no ship sightings; sensor_dome not built
- [Repair kits from cargo](reference_repair_kits_from_cargo.md) — repair works undocked, one kit per call (user-confirmed 11-18)
- [Hand crafting is free](reference_hand_crafting_is_free.md) — no labor fee by hand; do not gate hand-craft tasks on credits (operator 11-19)
- [Catalog ship stats include default modules](reference_catalog_ship_stats_include_default_modules.md) — Frankenhauler speed 2 is its default afterburner; bare hull speed 1 (operator 11-19)
- 🔴 [attack re-issued mid-battle locks the action queue ~50 min (11-21)](reference_attack_mid_battle_locks_queue.md) — reload refused "pending (attack)"; no cancel; play_as now sends `battle target` in battle; never disconnect to clear
- [Repeater weaponless EM rows = REFLECTION (11-21)](reference_repeater_reflection_damage.md) — damage sent back at the attacker; pane flat ≤15, spire % of incoming; exclude from repeater offense
- 🟡 [Marketbot crafts paused for 100k storage cap (11-22)](project_marketbot_storage_cap_crafts_paused.md) — 13 crafts removed on 010/013/016/017/018; capped drone hosts STALL (retargeted 8, parked stock with trucker-100)
- [war_wagon hauler fit (11-22)](reference_war_wagon_hauler_fit.md) — strip defaults; 7x CX III + expanded_fuel_tank = 1500 cargo/340 fuel; watched big-hold hull list
- [Server restart pause (11-22)](reference_server_restart_pause.md) — restart warning pauses jumps until reconnect, autopilot resumes once; bin/worker rebuilt, uncommitted
- 🟡 [Modulated laser chain (11-22)](project_modulated_laser_chain.md) — CJ=refined/components, Beid=modules/weapons; sinter+optics placed at CJ; all 3 placed 11-22; next: feeds (neodymium ore, glass) + daily CJ->Beid magnet/optic haul
- 🔴 [Schedule periods = UTC calendar (11-21)](reference_schedule_periods_are_utc_calendar.md) — weekly re-fires at Sun 00:00 UTC (16:00 PT Sat); remove one-offs right after they land
- [reload replaces the whole magazine (11-22)](reference_reload_replaces_whole_magazine.md) — one ammo item per reload, loaded rounds lost; reload only when empty/below half; max_weapon_reach counts dry guns
- [Take an agent out for play_as (11-23)](reference_take_agent_out_for_play_as.md) — overrides removed-set + SIGHUP; SIGSTOP gets replaced in ~90s, play_as alone collides with the live worker
- [Wildlife loot drops vary (11-23)](reference_wildlife_loot_drops_vary.md) — quantities differ per kill; count the part in the hold, never kills
