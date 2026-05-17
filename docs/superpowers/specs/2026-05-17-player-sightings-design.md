# Player Sightings — Design

**Date:** 2026-05-17
**Status:** Draft — pending implementation plan

## Goal

Persist every player encountered in-game (via any command or push event that
carries player records) into the shared SQLite knowledge base, with enough
history to answer:

- *Who has been seen, and when last?*
- *What ships have they been observed flying?*
- *Where (system + POI) and how often have they been sighted?*

The data must be useful for three audiences:

1. The `play_as` REPL — ad-hoc intel queries.
2. Autonomous agents — future decision logic (avoid-known-hostiles, find-traders, etc.).
3. Long-term analytics — population, faction movement, ship-loadout census.

## Non-Goals

- No CLI query commands on top of the new tables in this change. Follow-up.
- No agent strategy reading from `seen_players` yet. Follow-up.
- No retention/pruning policy. Tables grow unbounded for now; revisit later.
- No anti-impersonation logic.

## Architecture

```
┌──────────────────────────────────┐
│ pkg/game/Client (handleResponse) │
│   parses NearbyPlayer /          │
│   BattleParticipant / etc.       │
└───────────────┬──────────────────┘
                │ notifyPlayers(source, players, poiID)
                ▼
        ┌───────────────────────┐
        │ PlayerObserver        │  (registered via SetPlayerObserver)
        │ func([]ObservedPlayer)│
        └─────────┬─────────────┘
                  │ adapter: []game.ObservedPlayer → []knowledge.SeenPlayer
                  ▼
        ┌──────────────────────────────┐
        │ knowledge.SQLiteKB           │
        │   .RecordSightings(batch)    │
        │   single tx, 3 UPSERTs/row   │
        └──────────────────────────────┘
                  │
                  ▼
        ┌──────────────────────────────────────┐
        │ seen_players / seen_player_ships /   │
        │ seen_player_sightings (new tables)   │
        └──────────────────────────────────────┘
```

The `pkg/game` package never imports `pkg/knowledge`. It exposes an
`ObservedPlayer` value type and a `PlayerObserver` callback. Each consumer
(`cmd/tools/play_as`, agent runners) writes a ~10-line adapter that converts
to the knowledge-side value type and invokes `RecordSightings`.

## Schema (new migration in `pkg/knowledge/sqlite_migrations.go`)

All timestamps are RFC3339 UTC strings, matching the existing convention.

```sql
-- One row per player ever seen.
CREATE TABLE seen_players (
    player_id        TEXT PRIMARY KEY,
    username         TEXT NOT NULL,
    faction_id       TEXT,
    faction_tag      TEXT,
    clan_tag         TEXT,
    primary_color    TEXT,
    secondary_color  TEXT,
    status_message   TEXT,        -- most recent value
    anonymous        INTEGER NOT NULL DEFAULT 0,
    first_seen_utc   TEXT NOT NULL,
    last_seen_utc    TEXT NOT NULL,
    sighting_count   INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX seen_players_username  ON seen_players(username);
CREATE INDEX seen_players_faction   ON seen_players(faction_id);
CREATE INDEX seen_players_last_seen ON seen_players(last_seen_utc);

-- One row per (player, ship_class) ever observed.
CREATE TABLE seen_player_ships (
    player_id       TEXT NOT NULL,
    ship_class      TEXT NOT NULL,
    first_seen_utc  TEXT NOT NULL,
    last_seen_utc   TEXT NOT NULL,
    sighting_count  INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (player_id, ship_class)
);
CREATE INDEX seen_player_ships_class ON seen_player_ships(ship_class);

-- One row per (player, system, poi, hour-bucket).
-- poi_id is NULL for system-scope sightings (e.g. get_system_agents without
-- a POI focus). SQLite treats NULL as distinct in PK comparison, so a
-- NULL-poi row and a populated-poi row for the same player/system/hour
-- coexist intentionally.
CREATE TABLE seen_player_sightings (
    player_id         TEXT NOT NULL,
    system_id         TEXT NOT NULL,
    poi_id            TEXT,
    bucket_hour_utc   TEXT NOT NULL,    -- 'YYYY-MM-DDTHH:00:00Z'
    ship_class        TEXT,
    source            TEXT NOT NULL,    -- get_nearby|get_system_agents|battle_alert|...
    in_combat         INTEGER NOT NULL DEFAULT 0,
    first_seen_utc    TEXT NOT NULL,
    last_seen_utc     TEXT NOT NULL,
    observation_count INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (player_id, system_id, poi_id, bucket_hour_utc)
);
CREATE INDEX seen_sightings_system ON seen_player_sightings(system_id, bucket_hour_utc);
CREATE INDEX seen_sightings_last   ON seen_player_sightings(last_seen_utc);
```

The `seen_` prefix avoids colliding with the existing `players` / `ships`
tables used for the local agent's own state.

### Upsert semantics

`seen_players`:
```sql
INSERT INTO seen_players (player_id, username, faction_id, faction_tag,
    clan_tag, primary_color, secondary_color, status_message, anonymous,
    first_seen_utc, last_seen_utc, sighting_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(player_id) DO UPDATE SET
    username        = excluded.username,
    faction_id      = COALESCE(NULLIF(excluded.faction_id, ''), faction_id),
    faction_tag     = COALESCE(NULLIF(excluded.faction_tag, ''), faction_tag),
    clan_tag        = COALESCE(NULLIF(excluded.clan_tag, ''), clan_tag),
    primary_color   = COALESCE(NULLIF(excluded.primary_color, ''), primary_color),
    secondary_color = COALESCE(NULLIF(excluded.secondary_color, ''), secondary_color),
    status_message  = COALESCE(NULLIF(excluded.status_message, ''), status_message),
    anonymous       = excluded.anonymous,
    last_seen_utc   = excluded.last_seen_utc,
    sighting_count  = sighting_count + 1;
```

Empty-string fields on a fresh observation never overwrite an existing
non-empty value — important because identity-only sources (chat) lack many
fields the nearby-list sources provide.

`seen_player_ships`: similar — bump count, refresh `last_seen_utc`.

`seen_player_sightings`: bump `observation_count`, refresh `last_seen_utc`
and `in_combat`, keep original `first_seen_utc`.

## Capture flow

### `pkg/game/observed_player.go` (new file)

```go
type ObservedPlayer struct {
    PlayerID       string
    Username       string
    ShipClass      string   // "" if source doesn't report it
    FactionID      string
    FactionTag     string
    ClanTag        string
    PrimaryColor   string
    SecondaryColor string
    StatusMessage  string
    Anonymous      bool
    InCombat       bool

    SystemID string    // stamped from c.state at capture time
    POIID    string    // "" for system-scope sources
    Source   string    // get_nearby | get_system_agents | battle_alert | chat_message | ...
    SeenAt   time.Time // time.Now().UTC() at capture
}

type PlayerObserver func([]ObservedPlayer)
```

### `pkg/game/client.go` additions

- Field `playerObserver PlayerObserver` on `Client`.
- Method `SetPlayerObserver(fn PlayerObserver)` (mirrors existing `SetOnChatMessage`).
- Internal helper `notifyPlayers(source string, players []serverapi.NearbyPlayer, poiID string)`
  that builds the `[]ObservedPlayer`, stamps `SystemID` from the current
  state (under the existing `c.mu` read), then invokes the observer
  *outside* the lock. Silent no-op if no observer is registered.
- Variant `notifyPlayersFromBattle(source string, parts []serverapi.BattleParticipant)`
  for combat sources.
- Variant `notifyPlayerFromChat(msg serverapi.ChatMessage)` for chat — emits
  a one-element slice with `ShipClass`/`POIID` empty.

### Wire-in points in `handleResponse()`

`handleResponse` works mostly on `map[string]any` payloads rather than typed
structs, so each wire-in point unmarshals just the relevant slice into the
appropriate `serverapi` slice type before calling the notifier.

| Payload key detected         | Source tag            | Notifier call                                                                |
|------------------------------|-----------------------|------------------------------------------------------------------------------|
| `nearby` + `poi_id` present  | `get_nearby`          | `notifyPlayers("get_nearby", players, poiID)`                                |
| `agents` + `system_id`       | `get_system_agents`   | `notifyPlayers("get_system_agents", players, "")`                            |
| `online_players` on system   | `get_system`          | `notifyPlayers("get_system", players, "")`                                   |
| `participants` on battle msg | `battle_alert` / `combat_update` | `notifyPlayersFromBattle(source, parts)`                          |
| `chat_message` event         | `chat_message`        | `notifyPlayerFromChat(msg)`                                                  |

The detection uses the same `payload[key]` checks the response router
already uses (see `pkg/game/client.go:3746` for the `agents`/`system_id`
pattern); the new capture taps slot in alongside the existing `storeKey`
branches. Each tap unmarshals via `json.Unmarshal(rawSlice,
&[]serverapi.NearbyPlayer{})` (or `BattleParticipant`) and passes the
result to the notifier.

### Recorder side — `pkg/knowledge/seen_players.go` (new file)

```go
// SeenPlayer mirrors the game-side ObservedPlayer but lives in the knowledge
// package so pkg/knowledge does not import pkg/game.
type SeenPlayer struct {
    PlayerID, Username, ShipClass                              string
    FactionID, FactionTag, ClanTag                             string
    PrimaryColor, SecondaryColor, StatusMessage                string
    Anonymous, InCombat                                        bool
    SystemID, POIID, Source                                    string
    SeenAt                                                     time.Time
}

func (kb *SQLiteKB) RecordSightings(obs []SeenPlayer) error
```

`RecordSightings` opens one `BEGIN IMMEDIATE` transaction and, for each
record with a non-empty `PlayerID`:

1. UPSERT `seen_players`.
2. UPSERT `seen_player_ships` — skipped if `ShipClass == ""`.
3. UPSERT `seen_player_sightings` — skipped if `SystemID == ""`.
   Bucket key: `SeenAt.Truncate(time.Hour).UTC().Format(time.RFC3339)`.

Returns the first error encountered; transaction rolls back on error.

### Wiring sites

- **`cmd/tools/play_as/main.go`** — after `globalClient` and `kb` are
  constructed, register an observer:
  ```go
  globalClient.SetPlayerObserver(func(obs []game.ObservedPlayer) {
      seen := convertObservedPlayers(obs) // local adapter
      if err := kb.RecordSightings(seen); err != nil {
          log.Printf("[seen] record: %v", err)
      }
  })
  ```
- **Agent runners** that construct their own `GameClient` and `knowledge.Base`
  follow the same pattern. Agents without a KB simply don't wire the observer.

## Error handling

- Observer errors never break the response path — recorder logs and
  swallows; the game client only knows about a `func` callback.
- Empty `player_id` records are dropped at the top of `RecordSightings`
  (defensive against NPC placeholders the server occasionally emits).
- Migration runs through the existing `runMigrations` chain; pre-existing
  databases get the new tables on next open with no manual step required.

## Performance

- Hourly bucketing caps `seen_player_sightings` growth at roughly
  `players_seen × hours_active × distinct_locations` — small for any
  realistic session.
- Observer fires from the already-serial `handleResponse` path. No new
  goroutines, no channels.
- One transaction per batch: a 50-player `get_system_agents` response is
  one fsync, not 150.

## Testing

### `pkg/knowledge/seen_players_test.go`

- Open in-memory `SQLiteKB`.
- Insert a fresh batch → assert one row each in all three tables.
- Insert the same player again with new `SeenAt` in the same hour bucket →
  `sighting_count` incremented to 2; sightings `observation_count` = 2;
  one sightings row total.
- Insert again in a new hour bucket → two sightings rows.
- Insert with `ShipClass=""` → no `seen_player_ships` row.
- Insert with `SystemID=""` (identity-only / chat) → no
  `seen_player_sightings` row; `seen_players` still upserted.
- Insert with empty `PlayerID` → silently dropped, no rows.
- Insert with empty `faction_tag` over an existing populated `faction_tag` →
  existing value preserved (COALESCE behavior).

### `pkg/game/client_test.go`

- Register a capturing `PlayerObserver` that appends to a slice.
- Feed synthetic JSON responses through `handleResponse`:
  - `get_nearby` with 3 players → one batch of 3, all stamped with the
    expected `Source`, `POIID`, `SystemID`.
  - `get_system_agents` with 5 players → batch of 5, `POIID == ""`.
  - `battle_alert` with 2 participants → both have `InCombat=true`,
    `Source == "battle_alert"`.
  - `chat_message` → single-element batch with empty `ShipClass`/`POIID`.
- No mocks; uses real types and the real `handleResponse` switch.

## Open questions

None. All design choices were resolved in the brainstorming session.
