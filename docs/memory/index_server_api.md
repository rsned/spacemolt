---
name: index-server-api
description: Server API: version changes v0.322-v0.618, struct drift, catalogs, KB schema/migrations, withdraw_items, chat target_id, mission chains, facilities, factions
metadata:
  type: reference
---

# Server API

<!-- HUB. One line per memory, ≤200 chars. Every memory file appears in exactly one hub. -->

- ⭐🔴 [v0.618: chart is PUBLIC, ship visibility is scanner-gated](reference_v0618_visibility_overhaul.md) — `get_system_agents` returns NOTHING (dispatcher reroutes it; schedules are additive-only); `scan <poi_id>` replaces it; get_base fuel capture SAFE; absorbed `6e9120a4`
- ⭐ [v0.572.0 crew/boarding/prizes](reference_v0572_boarding_personnel.md) — A+C absorbed 08-30 (structs, 3 pushes, `ship_captures`, `seen_prize_events`); B = 6 commands DEFERRED; capture is a NEW loss mode
- ⭐🔴 [v0.564–v0.565: pay_bounty, resistance fix, station ids](reference_api_v0564_v0565_bounty_station_ids.md) — client is ~18 versions behind; `pay_bounty` has 0 hits in our Go
- [v0.549.0 freight + per-crew pirates](reference_v0549_freight_and_percrew_pirates.md) — late delivery replaces default; shipping action=active; package_id addressing
- [v0.536.0 wildlife combat](reference_v0536_wildlife_combat.md) — First Hunt chain, Pirate Bounty gated on weapons L1, scouts engage small hulls
- [Settled API changes](reference_server_api_history.md) — what is already absorbed into the client
- [Patch notes source](reference_patch_notes_source.md) — where they live and the current server version
- [server_docs sync](reference_server_docs_sync.md) — refreshed 08-29 (v0.571): repair_module dropped; 31 OpenAPI commands uncatalogued, no test for that direction
- [API sync v0.495](project_api_sync_v0495.md) — espionage command, ShipClass price removal, prestige cluster
- [API struct drift audit](project_api_struct_drift_audit.md) — DONE 07-08 across v0.398→v0.473; response-struct verification partial
- [API currentness round v0.322](project_api_currentness_round.md) — 3 workstreams, historical
- [server_restart_warning handled](project_server_restart_warning_event.md) — DONE 07-13; pre-0.473 drift unaudited
- [`kind` discriminator drift](project_kind_discriminator_drift.md) — DONE `2b45a9d`
- [v2 API migration gaps](project_v2_api_migration.md) — filed with the server team; blocks v1→v2
- [request_id rollout](project_request_id_rollout.md) — LONG-TERM: fire-and-forget → Submit correlation
- [get_poi retirement](project_get_poi_retirement.md)
- [mbox backfill stopped at the first known row; private never backfilled](reference_mbox_backfill_holes.md) — a June DM invisible 3 months; FIXED 08-30, run `mbox backfill` once per parked pilot
- ⭐🟢 [withdraw_items source+target: `faction`->`self` is STORAGE-to-STORAGE, no hull limit](reference_faction_withdraw_pulls_personal_storage.md) — 38k liquid_hydrogen in ONE call ≈ 19k fuel cells; `storage`-><player> gifts from storage; faction has COMPARTMENTS; `pending` ack ≠ success
- ⭐🔴 [private chat target_id is a conversation key](reference_chat_target_id_conversation_key.md) — `"<recipient>:<sender>"`; killed databot silently for 2 months. FIXED
- ⭐🔴 [Mission chain maps](reference_mission_chain_maps.md) — `chain_next` comes ONLY from the complete_mission reply and play_as drops it; smuggling chain mapped, Grand Circuit breaks at `frontier_extension` (never observed, craftsman-1 is 2 steps away)
- [Mission board wire shape](reference_mission_board_wire_shape.md) — NO `requirements`
- [craft is action_result-wrapped](reference_craft_action_result_wrapping.md) — job body arrives next tick in `{command,tick,result}`
- [catalog(recipes) decode quirk](reference_catalog_recipes_shape.md) — full Recipe shape inside items[], not CatalogItem
- [catalog_items omits `tradeable`](reference_catalog_items_tradeable_drift.md) — modules import as tradeable=0; bulk tools must default it
- [Catalog refresh runbook](reference_catalog_refresh_runbook.md) — ships/items/recipes/skills from a scraper snapshot
- [Legacy mining hulls erased by every catalog refresh](reference_legacy_ship_classes_erased_by_refresh.md) — `StoreShipClasses` is DELETE+INSERT
- ⭐🟢 [Live KB at v60; chain collapsed to a v59 baseline](reference_live_kb_schema_drift.md) — applied 09-08 15:34 via `bin/kb-migrate` (54 ms, fleet live); backup = `VACUUM INTO`, never shell `.backup`; seen_players SQLITE_BUSY ~6/min is CHRONIC. NEVER edit an applied migration
- [ships table migration trap](reference_ships_table_migration_trap.md) — MOOT since the 09-08 collapse (pre-59 DBs are refused)
- ⭐ [Race suite 11m→2m (09-08)](reference_race_test_cost_sqlite_migrations.md) — SQLite migrations 3.5s/open under -race → `knowledgetest.Path(t)`; worker sleeps → `settle(ctx,d)` seam; `Client.Close` held the mutex = 5s per close. Never `time.Sleep` in pkg/worker
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
