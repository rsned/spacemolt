# faction-dashboard

Collects comprehensive per-faction data from member agents into the shared
knowledge base and renders a tabbed static HTML dashboard per faction, plus an
index page.

## Usage

    # Collect from all agents in data/agents/ and render to data/reports/factions/
    go run ./cmd/tools/faction-dashboard

    # Render only (no collection) from existing KB data
    go run ./cmd/tools/faction-dashboard -render-only

    # Limit to specific agents
    go run ./cmd/tools/faction-dashboard -agents craftsman-1,explorer-1

## Flags

- `-kb`          shared knowledge base path (default `data/spacemolt-knowledge.db`)
- `-output`      output directory (default `data/reports/factions`)
- `-agents`      comma-separated agent filter (default: all with credentials.json)
- `-delay`       seconds between agent connections (default 3)
- `-render-only` skip collection; render from existing KB data
- `-debug`       game client debug logging

## Ad hoc collection from play_as

In the `play_as` REPL (started with `--db-path <kb>`), run `update_faction_data`
to collect the current agent's faction into the same KB.

## Data

Tabs: Overview, Members, Diplomacy, Bases, Production, Storage, Market,
Missions, Rooms, Intel. Collection is best-effort and station-scoped (no agent
travel); the KB merges per-station data across members. Current-state only —
day-over-day diffs remain the job of `daily-summary`.

## Known limitations

- Custom role permission matrices are not readable via the API; only member role
  names are shown.
- "Tasks" are interpreted as faction missions (linked to members when the API
  exposes an assignment).
