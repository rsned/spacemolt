---
name: project_api_sync_v0495
description: "2026-07-13 openapi sync to server v0.495.1 — espionage command, ShipClass price/required_skills removal, prestige-unlock cluster, 7 additive fields"
metadata: 
  node_type: memory
  type: project
  originSessionId: aa93e47f-4e5a-48b3-bf98-dbc6dbaa1204
---

**DONE 2026-07-13, uncommitted on `main`.** Synced the client from `v0.473.0` to server **`v0.495.1`**, driven by a path- *and* field-level diff of the openapi snapshots (`openapi.20260705.json` → `openapi.20260713.json`), not a snapshot match. Build, full `go test ./...`, and `golangci-lint` all green.

**What the diff actually contained** (it was small — 1 new path, 1 removal, the rest additive):

- **New command `espionage`** (faction, mutation). Wired end-to-end: `serverapi.EspionageResponse`, `Client.Espionage`, `GameClient` interface, `MCPGameClient`, `actionResponseTypes`, runner dispatch, `calllog` mutations, actionspace (preconditions: docked + faction), and a play_as `espionage` command + `formatEspionage`. **This cleared the standing red `TestServerCommandsCoveredByClient`.** It blocks the player ~90s server-side, so it awaits on a new `SleepEspionageMaxWait = 18 * SleepTick` (3min) rather than the ordinary 30s mutation timeout.

- **`ShipClass.price` and `ShipClass.required_skills` were REMOVED by the server — do not reintroduce them.** Confirmed against live data: `data/game-api/latest/catalog_ships.json` has no `price` key at all, and 0 of 333 ships carry `required_skills` (250 carry the scalar `piloting_required` instead). Both were still declared in `serverapi.ShipClass`, `knowledge.ShipClassDef`, and `cmd/data/import-catalog-ships`, so every catalog re-import silently wrote **0/nil** — and `GetShipClassesByCategory` did `ORDER BY price ASC`, which had degenerated into an arbitrary order. Now removed from Go; ordering is `tier ASC, name ASC`. The DB columns survive (defaulted) but are neither read nor written.
  **Ships are built, not bought** — `build_materials` + `shipyard_tier` are the sourcing story now. Relevant to [[project_crafting_brain]].

- **Prestige/unlock cluster added to ShipClass**: `required_achievement`, `required_faction_achievement`, `required_faction_leader`, `prestige_lock`, `default_loadout_version` (plus `required_reputation`, which the DB already had). New DB columns added via `ensureShipClassPrestigeCols`, **not** a numbered migration — see [[reference_ships_table_migration_trap]], which is the non-obvious part of this change.

- **7 additive fields**: `Base.auto_buy_fuel`, `GetBaseResponse.life_support` (new `LifeSupport` type), `ListPassengersResponse.onboard_service`, `ListStationPassengersResponse.transit_lounge` (new `TransitLounge` type), `NearbyPlayer.docked`, `PlayerStats.deaths_by_wildlife`, and the achievement catalog on `CatalogResponse` (new **`AchievementDefinition`** type — deliberately distinct from the existing `Achievement`, which is the per-player *progress* view from `get_achievements`; the catalog carries criteria/credits/skill_xp instead). `StationConfigResponse.auto_buy_fuel` was skipped on purpose: its `station` command is in `ignoredCommands` as not-implemented.

**Gotcha worth keeping:** openapi documents `ShipClass.build_materials` as an *object keyed by item ID*, but live data returns an **array** of `{item_id, quantity}`. Live shape wins (per CLAUDE.md) — the struct already modeled it correctly.

**Not done / still open:** pre-`v0.473.0` drift remains unaudited ([[project_api_struct_drift_audit]]). The achievement catalog decodes but has no DB import. `espionage` has had no live smoke test — the ~90s timeout and the `action_result` unwrap ([[reference_craft_action_result_wrapping]]) are both untested against a real server.
