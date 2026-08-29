# Spacemolt Project Memory

<!-- INDEX ONLY. One line per memory — a linked title, then a one-clause hook; aim for ≤150 chars, never past 200. Detail lives in the file, never here. Full-text status lives in project_status_board.md, not in this file. Every memory file must appear here exactly once; a memory with no line here is unrecallable. -->

## Resume here
- [Status board](project_status_board.md) — **READ FIRST.** The ≤15 live items, dated and linked. Roll old ones to the archive; never grow it.
- [Standing rules](feedback_standing_rules.md) — never `git add -A` with dirty data/*.json · haul `--stagger 10s` · unpushed? `git log origin/main..HEAD`
- ⭐🔴 [NO stronghold routing without pirate unlock](feedback_stronghold_routing_requires_pirate_unlock.md) — per-agent rule; strongholds are dead ends; 8 of 24 losses incl. assist-sol's tanker
- Archives: [Aug](project_status_archive_2026_08.md) · [Jul 22–29](project_status_archive_2026_07.md) · [Jul 4–10](project_current_status.md) — misnamed: the OLDEST archive, 81 KB
- [Shipped-feature index](reference_shipped_history.md) — check before rebuilding anything

## Fleet ops — live problems
- ⭐🔴 [Stronghold guard is re-written per role](reference_stronghold_guard_is_per_role.md) — 5 copies, ABSENT from assist/autopilot/hunt/explore/freight; 8 of 24 losses. Fix = one movement-layer gate
- ⭐ [Tanker migration 08-14: how tankers work + hand-flown pickup](reference_assist_tanker_migration.md) — built-in pump, arrives full, 4 listings galaxy-wide, `switch_ship` is a SEPARATE step (nexus trap)
- ⭐ [Assist pump gap + pump-blind claim election](project_assist_fleet_refueling_pump_gap.md) — pre-tanker history; stale `requested_at` defeats distance routing
- ⭐🔴 [no_fuel_cells ≠ no_fuel_source](project_no_fuel_cells_refuel_deadlock.md) — credits do NOT fix it; usual cause is a station with no refuel SERVICE, worker retries forever
- ⭐🔴 [The refuelling fleet is itself dry](reference_assist_fleet_is_dry.md) — 3 of 5 tankers immobile; assist-sol invisible 14 days at 0 fuel. `cargo_used: 0` is normal for a tanker
- ⭐🔴 [Off-map == QUARANTINED, never launched](reference_rescue_queue_blocks_launch.md) — `restoreQuarantine` precedes the supervisor; `restarts:0` + zero `last_seen` is the tell
- ⭐🔴 [Fleet overrides are removed-SETS](reference_secondment_overrides_are_removed_sets.md) — commenting out a rotating agent's yaml line makes release a NO-OP; list rotators in BOTH yamls
- [Secondment drain must outlast RemoveDrainTimeout](reference_secondment_drain_and_rollback.md) — 4m; and must roll back on failure or the agent runs in no fleet
- ⭐🔴 [Health checks miss a wedged worker](reference_standing_loop_wedge_after_reconnect.md) — 6 workers, 3.5 days, dead scheduler; fix = bound every dispatched command
- ⭐🔴 [Health checks miss a livelocked worker](reference_livelock_invisible_to_health_checks.md) — 47h on "Already docked"; find one: grep 'held for next pass' at tick cadence
- ⭐🔴 [MaxRestarts=100 parks an agent FOREVER](reference_crash_loop_cap_parks_agents_forever.md) — dashboard remove/readd does NOT free it; fixed `2114df0e`, undeployed
- [Stall-watchdog kills workers during initial connect](project_overmind_stall_kill_connect_loop.md) — burns MaxRestarts in ~8min when the game is slow; FUTURE
- ⭐🔴 [Settled-mission livelock](reference_settled_mission_livelock.md) — server lists a mission ACTIVE it refuses to complete; `abandon_mission` is the cure
- ⭐🔴 [Idle loop ran 3x per tick](reference_idle_loop_ran_3x_per_tick.md) — SleepTick/3 → ~43 passes/sec fleet-wide, the floor under the IP blocks; fixed to one tick
- ⭐🔴 [SIGSTOP/SIGCONT preserves game sessions](reference_sigstop_preserves_game_sessions.md) — zero logins; the safe tool during an IP block
- ⭐🔴 [CargoUsed drifts upward forever](reference_client_cargo_used_drifts_upward.md) — `deposit_items` never cleared the client cargo list; fixed 08-22, uncommitted
- ⭐🔴 [15 of 22 haulers lost their freight hulls](project_haul_fleet_hull_attrition.md) — kill zones zaniah + goldcrest; do NOT re-equip before fixing routing
- [Haul routes through station-less Lawless systems](project_haul_lawless_routing.md) — stuck→restart; plus a cosmetic stale-docked heartbeat flag; fix queued
- [Haul fleet runs an old bin/worker](project_haul_fleet_worker_update_due.md) — needs drain+relaunch onto current build; requested 07-26, not done
- ⭐🔴 [Capture cadence retune PENDING](reference_capture_cadence_retune_pending.md) — `--apply` at the next fleet stop; live workers revert schedule.json edits
- [Pirate unlock campaign](project_pirate_reputation_unlock_campaign.md) — raise every agent from -30 to 10; 45/161; NOMINATION is the bottleneck, not rotation
- [Mining fleet](project_mining_fleet.md) — created 08-21 as the unlock campaign's graduation destination; two-system loop, ore-selection refinements
- [Spare marketbot accounts for smuggling](reference_smuggling_spare_marketbot_accounts.md) — nine dormant, credentials present, in no fleet yaml
- [Rescue pipeline bug package](project_rescue_pipeline_bugs.md) — failed records never retry, wrong assister claims, gift-locked fees (Distant Light, 07-16)
- [Worker heartbeat credits read stale 0](reference_worker_heartbeat_credits_stale.md) — gifts look failed when they landed; confirm in-game, not the status file

## Data loss / capture gaps
- ⭐🔴 [Six ways we silently drop data](reference_capture_loss_taxonomy.md) — 18 KB tables have NEVER held a row; every mode looks like success. Detector spec'd, NOT built
- ⭐🔴 [Stale status files read as LIVE](reference_fleet_status_fossil.md) — `fleet-status.json` 17 days dead; `mining-status.json` is the same now
- ⭐🔴 [ship_modules has 0 rows, always](reference_ship_modules_never_captured.md) — we cannot see what is FITTED; hull capacity is the only proxy
- ⭐ [Fleet asset snapshots](project_fleet_asset_snapshots.md) — `agent_ships` = 0 rows ever; storage last captured 07-02
- [Worker fleet never wires WireStorageCapture](project_worker_storage_capture_gap.md) — scheduling view_storage on a role stores nothing
- [Action-log capture](project_action_log_capture.md) — 2 canaries live, fan-out pending
- [Per-death loss capture](project_per_death_loss_capture.md) — hull + manifest + cause + insurance; FOLLOW-UP, so detour/PvP losses are measurable
- [Survey anomaly capture](project_survey_anomaly_capture.md) — persist survey_system anomaly hints to the anomalies table
- [Wildlife combat intelligence](project_wildlife_combat_intelligence.md) — capture wired `3dea78d7`, NOT scheduled; Leviathan kills a starter hull in 2 ticks

## Haul & freight economics
- ⭐🔴 [Haul revenue halved](project_haul_revenue_halved_v0547.md) — v0.547.1 moved the fat tier to `trade_authenticator`; real loss is ALLOCATION; `MinProfit: 1000` is the lever
- ⭐🔴 [Book depth is the real haul ceiling](reference_book_depth_is_the_real_haul_ceiling.md) — `bookCap=ceil(srcUnits/cargoCap)`: a BIGGER hull gets FEWER slots
- [Fleet capacity ceiling](reference_haul_fleet_capacity_ceiling.md) — DON'T add haulers; 21 saturate the fat tier, realized = 34.4% of predicted
- [Scanner outage + expiry fix](project_scanner_outage_expiry_fix.md) — scanner is UNSUPERVISED; its death mimics "no opportunities". Check it first
- [Earnings-per-jump dashboard](project_fleet_efficiency_dash.md) — SP1 LIVE `:8087`
- [Stranded-recovery POIs bug](project_haul_stranded_recovery_pois_bug.md) — FindNearest never populates NearestResult.POIs, so the recovery branch is dead
- [Book-coordination follow-ups](project_haul_book_coordination_followups.md) — two deferred items after the 07-18 merge
- [Haul idle-trap gate](project_haul_idle_trap_gate.md) — `52b2cfa`; constraints 1-3 unbuilt
- [Haul departs without enough fuel](project_haul_departs_without_enough_fuel.md) — autopilot said "Need N more" and left anyway, 11+ stranded; FIXED `4b84ac1b`
- [Haul rolling drain on completion](project_haul_rolling_drain_on_completion.md) — FUTURE: zero-abort rolling upgrades
- [Haul P&L adjustments](project_haul_pnl_adjustments.md) — deferred non-trading reconciliation
- [Arbitrage net-of-fuel](project_arbitrage_net_of_fuel.md) — A+B merged 07-15; C (fuel-arbitrage chains) open
- ⭐ [Unpriced freight is prime-carrier-only](reference_freight_unpriced_cargo_prime_gate.md) — accept lands the package in STORAGE AT ORIGIN, not your hold
- [Freight orphan salvage](reference_freight_orphan_salvage_unpack.md) — a DEFAULTED contract's sealed package becomes loot; playbook
- [No active-contracts listing](reference_shipping_no_active_contracts_listing.md) — no shipping equivalent of get_active_missions; track locally
- [Freight load-confirm regression](project_freight_load_confirm_regression.md) — v0.2.1 check is a false negative: 0 deliveries fleetwide from 07-24
- [Freight load-confirm + resume fixes](project_freight_load_confirm_and_resume.md) — MERGED `6e211cc` 07-24, not pushed/deployed
- [Freight withdraw silent failure](project_freight_withdraw_silent_failure.md) — 52 events/18 agents; the withdraw SUCCEEDS, read the request_id-correlated action_result
- [Freight returns only at origin](project_freight_wrong_origin_return.md) — wrong_origin retried forever, breached fighter-1; fixed `3f010dd`
- [Freight probation bootstrap](project_freight_probation_bootstrap.md) — take a loss on first deliveries to escape probationary → 4x tiers; SHIPPED
- [Freight probationary cargo fence](project_freight_probationary_cargo_fence.md) — senior carriers skip probationary-band cargo; DEPLOYED
- [Shipping carrier feature](project_shipping_carrier.md) — A+B+C all MERGED; C `fcc1fda` 07-22
- [Marketbot freight-demand scan](project_marketbot_freight_demand_scan.md) — ~40 parked marketbots as a demand sensor net; FEATURE
- [Empire treasury payout collapse](project_empire_treasury_payout_collapse.md) — mission credits ~37% of advertised from 07-23, dev-confirmed treasury dry; XP still full

## Server API
- ⭐🔴 [v0.564–v0.565: pay_bounty, resistance fix, station ids](reference_api_v0564_v0565_bounty_station_ids.md) — client is ~18 versions behind; `pay_bounty` has 0 hits in our Go
- [v0.549.0 freight + per-crew pirates](reference_v0549_freight_and_percrew_pirates.md) — late delivery replaces default; shipping action=active; package_id addressing
- [v0.536.0 wildlife combat](reference_v0536_wildlife_combat.md) — First Hunt chain, Pirate Bounty gated on weapons L1, scouts engage small hulls
- [Settled API changes](reference_server_api_history.md) — what is already absorbed into the client
- [Patch notes source](reference_patch_notes_source.md) — where they live and the current server version
- [server_docs sync](reference_server_docs_sync.md) — how it stays current and why actionspace drifts
- [API sync v0.495](project_api_sync_v0495.md) — espionage command, ShipClass price removal, prestige cluster
- [API struct drift audit](project_api_struct_drift_audit.md) — DONE 07-08 across v0.398→v0.473; response-struct verification partial
- [API currentness round v0.322](project_api_currentness_round.md) — 3 workstreams, historical
- [server_restart_warning handled](project_server_restart_warning_event.md) — DONE 07-13; pre-0.473 drift unaudited
- [`kind` discriminator drift](project_kind_discriminator_drift.md) — DONE `2b45a9d`
- [v2 API migration gaps](project_v2_api_migration.md) — filed with the server team; blocks v1→v2
- [request_id rollout](project_request_id_rollout.md) — LONG-TERM: fire-and-forget → Submit correlation
- [get_poi retirement](project_get_poi_retirement.md)
- ⭐🔴 [private chat target_id is a conversation key](reference_chat_target_id_conversation_key.md) — `"<recipient>:<sender>"`; killed databot silently for 2 months. FIXED
- [Mission board wire shape](reference_mission_board_wire_shape.md) — NO `requirements`
- [craft is action_result-wrapped](reference_craft_action_result_wrapping.md) — job body arrives next tick in `{command,tick,result}`
- [catalog(recipes) decode quirk](reference_catalog_recipes_shape.md) — full Recipe shape inside items[], not CatalogItem
- [catalog_items omits `tradeable`](reference_catalog_items_tradeable_drift.md) — modules import as tradeable=0; bulk tools must default it
- [Catalog refresh runbook](reference_catalog_refresh_runbook.md) — ships/items/recipes/skills from a scraper snapshot
- [Legacy mining hulls erased by every catalog refresh](reference_legacy_ship_classes_erased_by_refresh.md) — `StoreShipClasses` is DELETE+INSERT
- [ships table migration trap](reference_ships_table_migration_trap.md) — ALTER TABLE ships fails on pre-collapse DBs
- [GameClient interface → mocks](feedback_gameclient_interface_mocks.md) — `go build` misses it; run `go test ./...`
- [Version constant](feedback_version_constant.md) — bump `BuiltForAPIVersion` with every struct/signature change
- [Facility list strips level + rent](reference_facility_list_field_omissions.md) — default level=1 from catalog
- [Facility list has 3 public sections](reference_facility_list_sections.md) — private lines omit production.public
- [Facility rent cycle = 100 ticks](reference_facility_rent_cycle.md) — ≈17 min; 86.4 cycles/day
- [Pirates standing key drift](reference_pirates_standing_key_drift.md) — generic "pirates" retired for nine per-stronghold keys; fixtures hid it
- [faction_list is shuffled](project_faction_list_shuffle.md) — `--seed` loops-until-coverage works around it
- [Faction events](project_faction_events.md) — faction_promote done, faction_demote pending server-side
- [Faction info backfill](project_faction_info_backfill.md) — auto-fetch for factions seen on observed agents
- [Notifications tick](project_notifications_tick.md) — get_notifications as a lightweight refresh after login and in the runner loop
- [server-cmd spec-driven payloads](project_server_cmd_spec_driven_payloads.md) — DEFERRED: route through typed pkg/game wrappers

## Combat & wildlife
- ⭐🔴 [Combat damage pipeline](reference_combat_damage_pipeline.md) — `reach` gates firing entirely; `brace` = 0.25 dmg but 0% offense; the worker has NO combat code
- ⭐ [Module wear/repair REMOVED](reference_module_wear_removed.md) — refitting is consequence-free; 29 agents on starters vs 170 idle hulls
- ⭐ [Module cost scales with Engineering](reference_ship_module_costs_scale_with_engineering.md) — catalogue CPU/power are BASE, −1%/level; trains only at ≥90% fit
- ⭐ [get_battle_log has the FULL reconstruction](reference_battle_log_api_replay_data.md) — positions, stance, loadouts, pipeline, autopilot reasoning; 30 ticks = 1.5MB
- [Battle replay viewer](reference_battle_replay_viewer.md) — spacemolt.com/battles/<id>; don't scrape it
- [Scan bracket = THREAT flag, not role](reference_creature_scan_bracket_threat_flag.md) — FIXED `35ae5f4b`
- [Belt-Grazer grounds](reference_belt_grazer_hunting_grounds.md) — kochab/ironpeak, NOT khambalia
- ⭐🔴 [get_nearby on arrival reads EMPTY](reference_wildlife_arrival_race.md) — 18% false-negative; hunt fleet rejected good grounds. Fixed `238f73a7`; table is `wildlife_surveys`
- [Combat roadmap](project_combat_roadmap.md) — entered via wildlife/First Hunt, not PvP; the spar testbed exists but is dormant
- [Spar testbed](project_spar_testbed.md) — cmd/tools/spar + pkg/spar, controlled PvP harness; BUILT, dormant
- [Ship fitting calculator](project_ship_fitting_calculator.md) — pkg/fitting + cmd/tools/fit; open validation TODO
- [Pirate bands](project_pirate_bands.md) — pirate-1..15 = three 5-agent bands, DORMANT on purpose
- [play_as smart battle handler](project_play_as_smart_battle_handler.md) — DEFERRED: auto-retreat instead of dying passively
- ⭐ [Battle holotable visualizer](project_battle_holotable_visualizer.md) — P1b shipped; NEXT = P2 record sheet; battle API is CORS-open but needs a logged-in session
- [Battle visualization](project_battle_visualization.md) — longer-term top-down radar, after-the-fact then live

## Gotchas — movement, docking, fuel
- ⭐🔴 [Docked at 0 fuel is invisible to the watchdog](reference_docked_zero_fuel_invisible_to_watchdog.md) — `Stalled()` early-returns on Docked; hand-write a rescue record
- ⭐🟢 [Ship-to-ship refuel WORKS dock-to-dock](reference_ship_to_ship_refuel_works_while_docked.md) — client fuel field stays 0 after; confirm via `tank_full` or a jump
- ⭐ [Jump time + fuel formulas](reference_ship_jump_time_and_fuel_formulas.md) — `jumpTicks=max(1,7−speed)`, fuel `ceil(scale^1.5×speed)`; every flat constant is wrong
- [Travel is priced before it is measured](reference_travel_priced_before_measured.md) — zero-distance move to your own POI rejected as "Insufficient fuel"
- [Refuel fallback vs measurement](reference_refuel_fallback_vs_measurement.md) — a cargo-cell fallback misreports a dry station as one that sells fuel
- ⭐🔴 [Standing AT a POI is not being DOCKED](reference_sell_leg_dock_gap.md) — resumed haul looped `not_docked`; FIXED `2fea237a`
- [Pin-arrival needs FOUR checks](reference_pin_arrival_check_four_directions.md) — `docked_at_base` is EMPTY while docked
- [docked_at_base lives on the PLAYER](reference_docked_at_base_gotcha.md) — never the ship; dock events historically never recorded it
- [Pinned mission workers never refuel](reference_pinned_mission_workers_never_refuel.md) — refuelling is a ROLE property; FIXED `topUpAtPin`
- ⭐ [Player stations can refuse your dock](reference_player_station_access.md) — access is LEARNED; unverified = closed
- [Station ids are dual-named](reference_station_id_aliases.md) — joins under-report silently
- [Lawless transit is safe; idling is not](reference_lawless_transit_vs_idle.md)
- ⭐ [GSA auto-recovery](reference_gsa_ship_recovery.md) — a drifting ship gets docked FOR you for a fee; a QUARANTINED agent needs its rescue record deleted
- [Jettison → loot transfer flow](reference_jettison_loot_transfer_flow.md) — hand cargo to a ship that cannot dock; verified working
- [Ship replacement workflow](reference_ship_replacement_workflow.md) — insurance pays credits, not a ship
- [Cargo liquidation](project_cargo_liquidation_cut_losses.md) — best bid, else DEPOSIT (never jettison)
- [drone_bay is an agent-wide ledger](reference_drone_bay_is_agent_wide.md) — drones keep mining across a ship switch; capacity fields report 0
- ⭐ [Rank freight hulls FITTED, not by base stats](reference_prayer_class_freight_hulls.md) — `cargo_expander_iii` = +100/slot; congregation = 1900 cargo, tier 1
- [Refueler ship roadmap](project_refueler_ship_roadmap.md) — Tanker step DONE 08-14 for 4 of 5 (see migration); tier-4 hulls next
- [Idle script authoring traps](reference_idle_script_authoring_traps.md) — dead idle_params, stale POI cache after jump, first-belt selection, override path
- [Reconnect wakes on input](project_reconnect_wake_on_input.md) — bounded burst then dormant, woken by REPL input

## Gotchas — code & data
- ⭐ [storeRawJSON key drift](reference_rawjson_key_drift.md) — cache keys reachable only via the action switch die when a reply omits `action`. Audit the rest
- [Missions() vacuous-test trap](reference_missions_vacuous_test_trap.md) — bare `&game.State{}` early-returns before the code under test
- [GameClock drifts forward](reference_gameclock_forward_drift.md) — syncs FORWARD only; no tight timeouts from it
- [Exploration = content survey](reference_exploration_is_content_survey.md) — the map graph is fixed and known
- [Actual Sleep constants](reference_sleep_constants_actual.md) — CLAUDE.md's table is STALE
- [spacemolt-kb shares the SQLite DB](reference_spacemolt_kb_shared_db.md) — poi_metadata_planets/stars are owned by the kb repo
- [Ironlight Combine = the dev's faction](reference_ironlight_combine_dev_faction.md) — its charter describes our marketbot pattern
- [POI merge provenance](project_poi_merge_provenance.md) — weak scans no longer clobber rich data
- [Worker log tagging](project_worker_log_tagging.md) — `[worker:<id>]` v0.2.3 deployed; carrier-tier logging v0.2.4 committed, not deployed
- [Deploy verification](reference_deploy_verification.md) — SIGUSR1 drain ABORTS if workers stay busy; verify by process start time vs binary mtime

## Market & economy
- ⭐🟢 [Station fuel-reserve capture](project_station_fuel_reserve_capture.md) — SHIPPED 08-15; 6 of 9 strongholds run DRY desks; faction bunkers member-only
- [Fuel price spread = 13×](reference_station_fuel_price_spread.md) — sol_central is the DEAREST; name the station or cr/jump is meaningless
- [Buy fuel at the cheaper end](project_refuel_timing_endpoint_choice.md) — `bc2adfe`, HAUL ONLY
- ⭐🔴 [Unbuyable modules poison the arbitrage board](reference_unbuyable_module_arbitrage_trap.md) — block at GENERATION; FIXED `443c0604`
- [market_orders stored duplicate books](reference_market_orders_duplicate_books.md) — phantom depth; FIXED `08fb75be`, a PARTIAL roll leaves it running
- [market_ohlcv = order-book data](reference_market_ohlcv_orderbook.md) — not trades; 999999 sentinel fix
- [GetReferenceAsk perf](reference_getreferenceask_perf.md) — 3.3s/call on 190M rows; needs the (item,side,station,captured_at) index
- [storage_snapshots is upserted](reference_storage_snapshots_shape.md) — UNIQUE agent+base; quantities REAL
- [empire field semantics](reference_empire_field_semantics.md) — get_system=REGION vs get_map=OWNERSHIP
- [craftsman-1 vacuum bids](reference_craftsman1_vacuum_bid_economics.md) — INTENTIONAL, never cancel
- [TRADING missions NOT market-validated](reference_trading_missions_not_market_validated.md) — reward and qty ignore the market
- [bill_of_materials is LOSSY](reference_bom_table_lossy.md) — ceils fractions; drops has_alternatives
- [Item category = sourcing rule](reference_item_category_sourcing.md) — ONLY `category='ore'` is mineable
- [Customs mechanics](reference_customs_mechanics.md) — scans only pilots who STOP 10 ticks at a border; keep moving
- [Empire tax day](reference_empire_tax_day.md) — weekly; get_tax_estimate shows amount + reason
- [Pirate base registry](reference_pirate_base_registry.md) — ids + systems; stale, in knowledge.db only
- [Market intelligence pkg](project_market_intelligence.md) — pkg/market, single source of volatile data; merged 06-21/22
- [Demand ledger](project_demand_ledger.md) — Station Manager buy orders, fulfill-now + craftable matching
- [find_item command](project_find_item_command.md) — BUILT `e1c73ff`; ranks stations by hops then price
- [price command depth-walk](project_price_command_depthwalk.md) — single-best-ask today; revisit
- [Arbitrage opportunity detail view](project_arbitrage_opportunity_detail_view.md) — FUTURE dashboard drill-down
- [Citizen demand brainstorm](project_citizen_demand_brainstorm.md) — PARKED 08-14: ~25M population, ~30B infrastructure
- [Passenger demand intel](project_passenger_demand_intel.md) — marketbots survey list_station_passengers into a KB table
- [Idle mining marketbots](project_idle_mining_marketbots.md) — short idle-mining loop between scans; canary viable

## Overmind / ops / deploy
- [Overmind launch commands](reference_overmind_launch_commands.md) — all fleets; `rm -f` stale socks; /proc scan not `pgrep -f`; relaunch market-prune too
- ⭐ [fleet-watch health watcher](reference_fleet_watch.md) — alerts on log SILENCE + daemon census; relaunch with the fleets. Logs ~20GB unrotated
- [Pending rollout queue](project_pending_rollout_queue.md) — what is deployed per fleet; haul is the one left behind
- ⭐ [Park an agent: quiesce.json](reference_worker_quiesce_park.md) — survives restarts; `admin remove` force-kills after 4min
- [Graceful drain](project_overmind_graceful_drain.md) — SIGUSR1/USR2/TERM
- [Dynamic exit/join](project_fleet_pool_dynamic_membership.md) — MERGED `8016cd8`
- [Fleet manager](project_overmind_fleet_manager.md) — NEXT = Phase 2
- [Fleet role interchangeability](project_fleet_role_interchangeability.md) — operator vision 08-01: surge any agent into Haul; brainstorm first
- [Generalist agent selector](project_generalist_agent_selector.md) — LONG-TERM: every agent weighs all income paths per juncture
- [Idle-agent income paths](project_idle_agent_income_paths.md) — A mission-runner specced; B mining next; C combat deferred
- [Dashboard v1 on :8091](project_overmind_dashboard_v1.md) — pkg/ovdash; MERGED 07-21
- [System view](project_overmind_system_view.md) — click system → orbital view with live dots; LIVE
- [Activity line](project_ovstatus_activity_line.md) — per-agent current-activity sub-line
- [Dashboard task summary](project_overmind_dashboard_task_summary.md) — FUTURE per-worker task line
- [Fleet version visibility](project_fleet_version_visibility.md) — current/behind per build
- [Login rate limits](reference_login_rate_limits.md) — per-IP per-minute on /login; shapes fleet startup
- [Login vs reconnect gating](reference_login_vs_reconnect_gating.md) — FRESH logins NOT gated; never mass-restart fast
- [market.db pruning](reference_market_db_prune.md) — UNSUPERVISED; died 07-15 → 62GB; retry fix `a474d01d` undeployed
- [Bulk-delete scale lesson](feedback_bulk_delete_scale.md) — NEVER unbatched DELETE on a huge table; 6.5h without committing
- [Verify regen with stash](feedback_verify_regen_with_stash.md) — swap old code in with git stash, never `git checkout`
- [Worker DB pool cap = NEGATIVE RESULT](reference_worker_db_pool_negative_result.md) — didn't cut CPU; real culprit was GetReferenceAsk
- [Agent capability ledger](project_agent_capability_ledger.md) — pkg/assets, 16 commits on feat/agent-capability-ledger, UNMERGED
- [Capability ledger slices 5–6](project_agent_capability_ledger_storage_faction.md) — storage + faction + ovdash panel; reviewed, unmerged
- [Ship role naming scheme](project_ship_role_naming_scheme.md) — FUTURE: overmind re-allocates idle agents by hull name
- [Server consolidation](project_server_consolidation.md) — agent-server features migrated into spacemolt-server; DONE
- [Agent empire-band numbering](project_agent_empire_bands.md) — trailing number = empire band

## Crafting & mining
- ⭐ [supported_power = floor(remaining/20)](reference_supported_power_is_derived.md) — DERIVED, do not store; falls as a deposit depletes
- ⭐🔴 [supported_power gates mining](reference_mining_supported_power_gate.md) — summed module power must be BELOW it; an over-fitted rig is locked OUT
- ⭐🔴 [Mining yields a RANDOM resource from the site mix](reference_mining_yield_is_random_from_site_mix.md) — mining hulls = 100% ore cargo boost; 25 idle deeprock_harvesters
- [Ore deposits REGENERATE](reference_ore_deposits_regenerate.md) — round-trip time beats deposit size; check `last_updated_tick`
- ⭐🟢 [Fleet drone refit](project_fleet_drone_refit.md) — SPEC `898852c1` unpushed; only shortfall = 3,607 energy_crystal
- ⭐🔴 [KB BoM explorer picks recipes ALPHABETICALLY](reference_kb_bom_explorer_alphabetical_default.md) — not cost-aware; one campaign read 13x too expensive
- [public_facilities keys player stations by base_id](reference_public_facilities_player_station_id.md) — a POI-keyed join reported 0 for 231
- [public_facilities prune-at-write](reference_public_facilities_prune.md) — SHIPPED 08-22; safe ONLY because listings are complete
- [circuit_board intel](reference_circuit_board_production.md) — silicon_ore is the bottleneck
- [Crafting brain](project_crafting_brain.md) — A1+A2 SHIPPED
- [Executor B rollout](project_executor_b_rollout.md) — NOT LIVE
- [Craft batch = Crafting skill](project_craft_batch_skill.md) — no longer hardcoded 10

## Agents, play_as, social
- ⭐🔴 [A BOUNTY IS UNPAID TAX](reference_tax_bounties_and_rates.md) — exactly the shortfall; weekly Sunday levy; Crimson 10% income/1% property
- [Bounties are not from combat](reference_agent_bounties_not_combat.md) — every 0-credit agent has one
- [Bounty auto-pays on entering the empire's space](reference_bounty_auto_pays_on_entering_territory.md) — trap: 0 credits → no fuel → stranded
- ⭐🔴 [Citizenship mechanics](reference_citizenship_mechanics.md) — `empire` is ORIGIN; crimson EXCLUSIVE, outerrim NOT; voidborn free
- ⭐ [send_gift + play_as mechanics](reference_send_gift_and_play_as_mechanics.md) — sender DOCKED, recipient = USERNAME, play_as needs `quit`
- [play_as via go run](feedback_play_as_go_run.md) — always `go run ./cmd/tools/play_as`, never bin/
- [play_as scheduler](project_play_as_scheduler.md) — cron-lite hourly/daily/weekly commands
- [Passenger feature](project_passenger_feature.md) — carrying commands + formatters; RawCommand blocking change
- [Treasury + shuttle role](project_treasury_and_shuttle.md) — 5% faction deposit/rescue; shuttle canary pending
- [Smuggling enablement](project_smuggling_enablement.md) — treasure_cache tutorial chain, smuggling-2 courier gate, mission capture
- [Mission learning pool](project_mission_learning_pool.md) — phase 2: categories beyond delivery, exploration first
- [Mission category coverage](project_mission_category_coverage.md) — v1 = single-leg deliver_item only; trade is the biggest rejected bucket
- [Mission scan rate backoff](project_mission_scan_rate_backoff.md) — QUEUED: stop re-polling boards every tick when dry/parked
- [Captains-log task resume](project_captains_log_task_resume.md) — FUTURE: server-persistent notes so workers re-orient after restart
- [Action-log analyzer](project_action_log_analyzer.md) — FUTURE: event timeline from get_action_log
- [mbox spam folder](project_mbox_spam_folder.md) — per-agent blocklist for noisy senders
- [LLM rollout](project_llm_rollout.md) — only miner-1 on ToT today
- [ToT next steps](project_tot_next_steps.md) — prompt tuning, async planning, UI polish
- [ToT prompt improvements](project_tot_prompt_improvements.md) — short-term memory, role-aware context, KB integration
- [Shared knowledge dirs](project_shared_knowledge.md) — FUTURE: per-org shared directories instead of per-agent copies
- [Pairwise jump cache](project_pairwise_jump_cache.md) — FOLLOW-UP: all-pairs distance cache for routing
- [Stronghold reach page](project_stronghold_reach_page.md) — kb did-you-know page SHIPPED 07-26
