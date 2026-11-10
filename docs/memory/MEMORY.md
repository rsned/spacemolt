# Spacemolt Project Memory

<!-- ROUTER ONLY, keep under ~10 KB / 100 lines. Two tiers: this file (always loaded) → index_*.md hubs (one line per memory) → memory files.
     New memory: add its line to the matching hub, NOT here. Every memory file appears in exactly one hub (Resume-here files appear only here).
     Hard rules: max 15; a line goes here only if acting without it causes damage. Swap one out to add one. -->

## Resume here
- ⭐🔴 [CONTRABAND PIPELINE RESUME POINT 10-25](project_contraband_pipeline_resume.md) — plans 1+2+3 done & merged (main 06cd562b); moonshine line LIVE; dashboard on :8093; day-one metrics from /api/line/moonshine.json ~10-25 19:00
- ⭐🟢✅ [Line dashboard :8093 v2 (11-01): lines DERIVED](reference_line_dashboard.md) — from schedules/fleets/KB/asks; line-overrides.yaml only; why from task-outcomes.json; grouped by station
- ⭐🟢 [Cloverfield home station "Conjunction Junction" (founded 10-25)](project_cloverfield_home_station.md) — CRFT station, record set data/stations/cloverfield, --record-dir/station_snapshot/ledger, 1M floor/1M buy cap, approve every build; ships with 500k faction storage but 0 power/life support
- [Status board](project_status_board.md) — **READ FIRST.** The ≤15 live items, dated and linked. Roll old ones to the archive; never grow it.
- [Standing rules](feedback_standing_rules.md) — never `git add -A` with dirty data/*.json · haul `--stagger 10s` · unpushed? `git log origin/main..HEAD` · after memory edits: `make memory-sync` + commit docs/memory · NEVER edit an applied migration
- ⭐🔴 [NO stronghold routing without pirate unlock](feedback_stronghold_routing_requires_pirate_unlock.md) — per-agent rule; strongholds are dead ends; 8 of 24 losses incl. assist-sol's tanker
- Archives: [Aug](project_status_archive_2026_08.md) · [Jul 22–29](project_status_archive_2026_07.md) · [Jul 4–10](project_current_status.md) — misnamed: the OLDEST archive, 81 KB
- ⭐🟢✅ [Clerk-token login + host-wide connection gate LIVE 10-22](reference_clerk_token_login_and_connection_gate.md) — fleet cold start ~5 min (was 15–30), zero 429s; `--stagger` is dead; key in ~/.config/spacemolt/clerk_api_key; run clerk-sync after new accounts; 4001 parks; play_as lease; **key regen still pending**
- ⭐ [Cold start runbook re-synced 11-03: 11 fleets + fleet-watch](reference_cold_start_runbook_drift.md) — shuttle NOT retired; fleet-watch launches LAST; login gate stays 60/min (headroom)
- [Shipped-feature index](reference_shipped_history.md) — check before rebuilding anything
- [Sunless systems = generation artifact](reference_sunless_systems_intentional.md) — 9 systems, LLM-map slip, do NOT chase; real reveal hook is gsc_0026 faint_signature

## Hard rules (detail in the linked file)
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
- [Energy crystals: reserve LIFTED 11-10](project_drone_project_crystal_reserve.md) — drone project done; crystals are ordinary stock (scarce: sites exhausted)
- [craftsman-1 vacuum bids are INTENTIONAL](reference_craftsman1_vacuum_bid_economics.md) — never cancel
- [NEVER unbatched DELETE on a huge table](feedback_bulk_delete_scale.md)
- [Never `time.Sleep` in pkg/worker](reference_race_test_cost_sqlite_migrations.md) — use the `settle(ctx,d)` seam

- [Faction-storage crafting needs Officer rank; faction_withdraw lands in cargo](reference_faction_storage_crafting_and_withdraw.md) — live 10-24; craft falls back to CRFT storage, output goes there too

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
