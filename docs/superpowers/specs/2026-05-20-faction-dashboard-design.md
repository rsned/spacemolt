# Faction Dashboard — Design

**Date:** 2026-05-20
**Status:** Approved (design); ready for implementation planning

## Overview

A comprehensive Faction Dashboard that renders everything known about a faction
as a static, tabbed HTML page: identity and lore, members and roles, diplomacy,
bases and facilities, production facilities, storage across all stations, market
buy/sell orders, missions, and rooms.

Phase 1 delivers two consumers backed by one shared collection library:

1. A standalone `faction-dashboard` CLI tool that collects faction data from
   member agents, persists it to the shared knowledge base, and renders one
   static HTML page per faction plus an index page.
2. An `update_faction_data` command in the `play_as` REPL that runs the same
   collection against the currently logged-in client, ad hoc.

The long-term vision (out of scope here) is dynamic/live updates and a fully
interactive standalone tool that exposes all faction game commands to a
frontend. The data model is structured so that future work — notably
production-chain ("pipeline") visualization — can be derived from what we store.

## Goals

- Persist comprehensive faction state to the shared knowledge base
  (`spacemolt-knowledge.db`) so it is a single source of truth for both
  consumers and any future tooling.
- Render a self-contained, tabbed static HTML dashboard per faction, styled to
  match the existing daily-summary aurora dark theme.
- Reuse one collection library between the standalone tool and `play_as`.
- Be resilient: best-effort collection, partial data is fine, missing sections
  degrade gracefully.

## Non-Goals (Phase 1)

- No live/dynamic updates; static render only.
- No interactive frontend or exposure of faction mutation commands.
- No production-chain ("pipeline") visualization yet — production facilities are
  shown as a flat list. Long-term: chains following recipes toward advanced
  items and ship construction.
- No dated history/diffing in the new tables — current-state only. Day-over-day
  faction diffing remains the job of the existing `daily-summary` tool.
- No agent travel for coverage — best-effort from where agents already are.

## Architecture

```
                 ┌──────────────────────────────┐
   game client → │  pkg/faction (NEW)           │ → knowledge.Base (persist)
                 │  Collector.Collect(client,   │
                 │    factionID) → snapshot      │
                 └──────────────────────────────┘
                      ▲                    ▲
        ┌─────────────┘                    └──────────────┐
┌────────────────────────────┐       ┌──────────────────────────────┐
│ cmd/tools/faction-dashboard │       │ play_as: "update_faction_data"│
│  1. connect member agents   │       │  calls Collector on the       │
│  2. Collect → KB            │       │  current logged-in client     │
│  3. render static HTML+index│       │  (collection only, no render) │
└────────────────────────────┘       └──────────────────────────────┘
                      │
                      ▼
        knowledge.Base ←── reads ── renderer (in cmd tool)
        (spacemolt-knowledge.db)
```

### Packages & components

- **`pkg/knowledge`** — new migration (v35) adding faction tables; new `Base`
  methods to store/load faction data; faction snapshot data types defined here
  (same pattern as the existing `StorageSnapshot` / `MarketSnapshot` types).
- **`pkg/faction`** (new) — the `Collector`. Takes a connected
  `game.GameClient`, gathers all faction data, builds the snapshot, persists via
  `knowledge.Base`. The shared unit both consumers call.
- **`cmd/tools/faction-dashboard`** (new) — connects faction member agents
  (reusing `game.InitializeAgent`, as daily-summary does), runs the Collector,
  then renders one static HTML page per faction plus an index. Renderer lives in
  the cmd tool (daily-summary precedent).
- **`cmd/tools/play_as`** — new `update_faction_data` REPL case alongside
  `update_all`, calling the same Collector against the live client.
- **`pkg/game/serverapi`** — likely add response structs for `faction_rooms`
  and `faction_list_missions` (field names verified against live JSON / docs, not
  assumed, per repo convention).

## Data flow & collection (best-effort, no travel)

For each connected member, the Collector calls:

1. `faction_info` (paginated members) → identity, treasury, members + roles,
   allies, enemies, wars, peace proposals, charter / description / emblem /
   colors, leader, founded date, owned-bases count.
2. `faction_rooms`, `faction_list_missions`, `facility action=faction_list`,
   `view_orders` (→ its `faction_orders` array) → **station-scoped**; captured
   for the station the agent is currently docked at.
3. `view_faction_storage` with `station_id` → queried **remotely** for every
   known faction base (discovered from the KB `bases` table, from facility
   responses, and from the current station).
4. `faction_intel_status`, `faction_trade_intel_status` → coverage stats.

**The KB is the merge point.** Station-scoped data is upserted keyed by
`(faction_id, base_id)`, so when multiple member agents each contribute the
station they happen to be docked at, coverage accumulates without in-memory
merging or travel. The **founder agent** (lowest-numbered member — reusing the
selection idea from daily-summary's `FactionCollector`) is preferred for the
faction-wide `faction_info` call since it has the most permissions.

**Snapshot model: current-state.** The new tables store the *latest* state per
faction/station, each row carrying `captured_utc` for a freshness indicator in
the UI. No dated history.

### Replace-within-scope upsert strategy

To keep current state correct when entities are removed (a cancelled order, a
deleted room, a completed mission):

- **Faction-wide tables** (`factions`, `faction_members`, `faction_relations`)
  are replaced per `faction_id` when `faction_info` is collected.
- **Station-scoped tables** (`faction_facilities`, `faction_storage`,
  `faction_storage_items`, `faction_orders`, `faction_missions`,
  `faction_rooms`, `faction_bases`) are replaced per `(faction_id, base_id)`
  when an agent collects that station.

Rows outside the scope just collected are left untouched (another station's data
remains valid).

## KB schema (migration v35)

Snake_case names, UTC TEXT timestamps, INTEGER booleans — matching existing
migrations (latest is v34, `add_seen_players_tables`).

```sql
CREATE TABLE factions (              -- one row per faction (current state)
  faction_id TEXT PRIMARY KEY, name TEXT, tag TEXT,
  leader_id TEXT, leader_username TEXT,
  treasury INTEGER, member_count INTEGER, owned_bases INTEGER,
  description TEXT, charter TEXT, emblem TEXT,
  primary_color TEXT, secondary_color TEXT, founded_utc TEXT,
  intel_systems INTEGER, intel_trade INTEGER,
  captured_utc TEXT NOT NULL );

CREATE TABLE faction_members (        -- PK (faction_id, player_id)
  faction_id TEXT, player_id TEXT, username TEXT, role TEXT,
  joined_utc TEXT, last_seen_utc TEXT, is_online INTEGER,
  captured_utc TEXT, PRIMARY KEY (faction_id, player_id) );

CREATE TABLE faction_relations (      -- PK (faction_id, target_faction_id, kind)
  faction_id TEXT, target_faction_id TEXT, target_name TEXT, target_tag TEXT,
  kind TEXT,                          -- 'ally' | 'enemy' | 'war' | 'peace_proposal'
  reason TEXT, terms TEXT, our_kills INTEGER, their_kills INTEGER, started_utc TEXT,
  captured_utc TEXT, PRIMARY KEY (faction_id, target_faction_id, kind) );

CREATE TABLE faction_bases (          -- PK (faction_id, base_id)
  faction_id TEXT, base_id TEXT, base_name TEXT,
  system_id TEXT, system_name TEXT, poi_id TEXT, services_json TEXT,
  captured_utc TEXT, PRIMARY KEY (faction_id, base_id) );

CREATE TABLE faction_facilities (     -- PK (faction_id, base_id, facility_id)
  faction_id TEXT, base_id TEXT, facility_id TEXT,
  facility_type TEXT, category TEXT, level INTEGER, status TEXT,
  recipe_id TEXT, details_json TEXT,  -- details_json future-proofs pipeline derivation
  captured_utc TEXT, PRIMARY KEY (faction_id, base_id, facility_id) );

CREATE TABLE faction_storage (        -- per-station header; PK (faction_id, base_id)
  faction_id TEXT, base_id TEXT, credits INTEGER, item_count INTEGER,
  captured_utc TEXT, PRIMARY KEY (faction_id, base_id) );

CREATE TABLE faction_storage_items (  -- PK (faction_id, base_id, item_id)
  faction_id TEXT, base_id TEXT, item_id TEXT, name TEXT, quantity REAL, size INTEGER,
  captured_utc TEXT, PRIMARY KEY (faction_id, base_id, item_id) );

CREATE TABLE faction_orders (         -- PK (faction_id, order_id)
  faction_id TEXT, base_id TEXT, order_id TEXT, side TEXT,  -- 'buy' | 'sell'
  item_id TEXT, item_name TEXT, price_each REAL, quantity REAL,
  captured_utc TEXT, PRIMARY KEY (faction_id, order_id) );

CREATE TABLE faction_missions (       -- PK (faction_id, mission_id)
  faction_id TEXT, base_id TEXT, mission_id TEXT, title TEXT, type TEXT,
  description TEXT, giver_name TEXT, rewards_json TEXT, objectives_json TEXT,
  assigned_player_id TEXT, expiration_utc TEXT,  -- assigned_* links missions→members ("tasks")
  captured_utc TEXT, PRIMARY KEY (faction_id, mission_id) );

CREATE TABLE faction_rooms (          -- PK (faction_id, base_id, room_id)
  faction_id TEXT, base_id TEXT, room_id TEXT, name TEXT, access TEXT, description TEXT,
  captured_utc TEXT, PRIMARY KEY (faction_id, base_id, room_id) );
```

New `knowledge.Base` methods (plus snapshot data types in `pkg/knowledge`):
replace-within-scope store methods (`StoreFaction`, `StoreFactionMembers`,
`StoreFactionStorage`, `StoreFactionOrders`, `StoreFactionMissions`,
`StoreFactionRooms`, `StoreFactionFacilities`, `StoreFactionBases`,
`StoreFactionRelations`), plus `LoadFactionView(factionID)` and
`ListFactionIDs()` for the renderer.

## UI / Layout

Tabbed dashboard, one self-contained HTML file per faction.

- **Persistent header** above the tab bar: tag, name, treasury, member count,
  owned bases, war count.
- **10 tabs:**
  - **Overview** — identity, emblem, color swatches, founded date, leader,
    charter & description (lore prose), KPI tiles, intel coverage chip.
  - **Members** — table: username, role, online status, last-seen, joined;
    "tasks" column links to faction missions assigned to that member where
    `assigned_player_id` is known.
  - **Diplomacy** — allies, enemies, wars (with our/their kills), peace
    proposals.
  - **Bases** — owned bases, location (system / POI), services, facilities per
    base; each base collapsible.
  - **Production** — production facilities with recipe, level, status (future:
    derived chains).
  - **Storage** — per-station rows; each station collapsible to full contents
    (credits + item table).
  - **Market** — faction buy/sell orders per station.
  - **Missions** — posted faction missions.
  - **Rooms** — faction rooms per station (name, access, description).
  - **Intel** — system + trade intel coverage stats.

### Renderer

- Go **`html/template`** (not string-builder) — auto-escaping matters because
  charter, description, and room text are player-authored ("worldbuild")
  content. One embedded template renders the tabbed page.
- **Tabs = minimal vanilla JS** toggling the active `.panel`. No framework.
  Within Storage / Market / Bases / Rooms panels, each station is a collapsible
  `<details>`.
- Reuses the daily-summary **aurora dark theme** CSS for visual consistency.
- Output: `faction-<tag>.html` per faction + `index.html` (cards: tag, name,
  treasury, members, freshness), mirroring daily-summary's index. Output dir
  defaults to `data/reports/factions/`.

## `play_as` command

- New REPL case `update_faction_data` (next to `update_all`) →
  `kbUpdateFaction(client, ctx)` in `kb_update.go`. It guards
  `globalKB != nil` (set via `--db-path`) and calls
  `faction.Collect(ctx, client, globalKB)`. Collects from the live client's
  vantage (current station + remote storage for known bases). Collection only,
  no rendering.

## Error handling

- **Best-effort per command:** any sub-query failing (permission denied, not
  docked, network) logs a warning and continues; partial data is persisted. A
  missing section renders as "not collected / unavailable," never a hard
  failure.
- **Per-agent isolation** in the tool: one agent's connection failure does not
  abort the run (daily-summary pattern).
- Renderer tolerates partial data — empty tabs show an explanatory empty-state.

## Testing

- **Parsing** — table-driven unit tests turning sample faction JSON payloads
  into snapshot types (`pkg/faction`).
- **KB roundtrip** — store→load tests against a temp SQLite DB, including
  replace-within-scope (a removed row disappears after re-collection).
- **Renderer golden test** — render a fixture `FactionView` and compare to a
  golden HTML file (matches the existing `format_*_test.go` style in play_as).
- Live-game collection is verified manually (no live tests).
- Per repo convention: run `go build ./...`, `go test ./...`, and
  `golangci-lint` with no new findings before commit.

## Open items (handled during planning/implementation, not now)

- Confirm real field names for `faction_rooms` and `faction_list_missions`
  responses against live JSON before adding `serverapi` structs.
- Confirm how `assigned_player_id` for missions ("tasks") is exposed, if at all;
  if unavailable, the Members "tasks" column lists faction missions without
  per-member assignment.
- Verify `view_orders` `faction_orders` field shape and that `order_id` is
  present for replace-within-scope keying.

## Known API limitations

- There is no command to read a custom role's permission matrix; only each
  member's role *name* is available (from `faction_info`). The Members tab shows
  role names; a permission matrix is deferred until the API supports it.
- There is no faction "task" concept in the API; "tasks" are interpreted as
  faction missions (linked to members where possible).

## Future vision (not in scope)

- Live/dynamic updates (SSE or polling).
- A standalone interactive tool exposing all faction game commands.
- Production-chain ("pipeline") visualization derived from `faction_facilities`
  (`recipe_id` / `details_json`) as the faction builds chained facilities toward
  advanced items and ship construction.
