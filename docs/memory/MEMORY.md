# Spacemolt Project Memory

<!-- ROUTER ONLY, keep under ~10 KB / 100 lines. Two tiers: this file (always loaded) → index_*.md hubs (one line per memory) → memory files.
     New memory: add its line to the matching hub, NOT here. Every memory file appears in exactly one hub (Resume-here files appear only here).
     Hard rules: max 15; a line goes here only if acting without it causes damage. Swap one out to add one. -->

## Resume here
- [Status board](project_status_board.md) — **READ FIRST.** The ≤15 live items, dated and linked. Roll old ones to the archive; never grow it.
- [Standing rules](feedback_standing_rules.md) — never `git add -A` with dirty data/*.json · haul `--stagger 10s` · unpushed? `git log origin/main..HEAD` · after memory edits: `make memory-sync` + commit docs/memory · NEVER edit an applied migration
- ⭐🔴 [NO stronghold routing without pirate unlock](feedback_stronghold_routing_requires_pirate_unlock.md) — per-agent rule; strongholds are dead ends; 8 of 24 losses incl. assist-sol's tanker
- Archives: [Aug](project_status_archive_2026_08.md) · [Jul 22–29](project_status_archive_2026_07.md) · [Jul 4–10](project_current_status.md) — misnamed: the OLDEST archive, 81 KB
- ⭐🟢✅ [Clerk-token login + host-wide connection gate LIVE 10-22](reference_clerk_token_login_and_connection_gate.md) — fleet cold start ~5 min (was 15–30), zero 429s; `--stagger` is dead; key in ~/.config/spacemolt/clerk_api_key; run clerk-sync after new accounts; 4001 parks; play_as lease; **key regen still pending**
- ⭐ [Cold start: follow docs/COLD_START.md, but it lists 7 of 9 fleets](reference_cold_start_runbook_drift.md) — mining + shuttle + fleet-watch missing; pool ~90 is HEALTHY now, not starved
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
- [Energy crystals RESERVED](project_drone_project_crystal_reserve.md) — never sell/craft without asking
- [craftsman-1 vacuum bids are INTENTIONAL](reference_craftsman1_vacuum_bid_economics.md) — never cancel
- [NEVER unbatched DELETE on a huge table](feedback_bulk_delete_scale.md)
- [Never `time.Sleep` in pkg/worker](reference_race_test_cost_sqlite_migrations.md) — use the `settle(ctx,d)` seam

- [Faction-storage crafting needs Officer rank; faction_withdraw lands in cargo](reference_faction_storage_crafting_and_withdraw.md) — live 10-24; craft falls back to CRFT storage, output goes there too

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
