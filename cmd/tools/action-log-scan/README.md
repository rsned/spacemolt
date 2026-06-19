# action-log-scan

Paginate through an agent's server-side action log (`get_action_log`) and report
the distinct `event_type`s seen, with per-type counts, the owning category, and
an example summary — handy for discovering the full event vocabulary the server
emits (the log has 30-day retention).

The tool **persists every entry it pulls** to a per-agent JSONL store, so later
runs fetch only records newer than the highest stored id and then report the
cumulative picture across everything captured so far.

## Usage

```sh
# First run: full pull into data/agents/craftsman-1/action_log.jsonl, then report
action-log-scan --agent=craftsman-1

# Later runs: fetch only newer entries, report the cumulative store
action-log-scan --agent=craftsman-1

# Force a complete re-page (backfills any gaps in the store)
action-log-scan --agent=craftsman-1 --full

# Group the output by category
action-log-scan --agent=craftsman-1 --sort=category

# Only one category (incremental stop is disabled when a filter is set)
action-log-scan --agent=craftsman-1 --category=combat

# Exact event_type (e.g. a faction's production-run history)
action-log-scan --agent=craftsman-1 --event-type=faction.production_cycle

# A faction's log (kept in its own store file)
action-log-scan --agent=craftsman-1 --faction-id=<id>

# Machine-readable output
action-log-scan --agent=craftsman-1 --json

# One-off scan that neither reads nor writes the store
action-log-scan --agent=craftsman-1 --no-store --max-pages=4
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--agent` | *(required)* | Agent ID for authentication |
| `--category` | *(all)* | Category filter (`combat`, `trading`, `ship`, `crafting`, `faction`, `mission`, `skill`, `salvage`, `storage`, `other`) |
| `--event-type` | *(none)* | Exact `event_type` filter (e.g. `faction.production_cycle`) |
| `--faction-id` | *(none)* | Scan a faction's log instead of the personal log (separate store file) |
| `--page-size` | `100` | Entries per page (max 100) |
| `--max-pages` | `0` | Stop after N pages (0 = all pages until `has_more` is false) |
| `--sort` | `count` | Output order: `count` (desc), `name` (event_type), `category` |
| `--json` | `false` | Emit per-type stats as JSON instead of a table |
| `--store` | *(per-agent default)* | Path to the JSONL entry store (`data/agents/<agent>/action_log[.<faction>].jsonl`) |
| `--full` | `false` | Re-page the entire log instead of stopping at the newest stored id (backfills gaps) |
| `--no-store` | `false` | Don't read or write the store (one-off scan of this run only) |
| `--transport` | `ws` | Transport: `ws` (WebSocket) or `mcp` (MCP HTTP) |
| `--debug` | `false` | Enable debug logging |

## Persistent store

- Default path: `data/agents/<agent>/action_log.jsonl` (one JSON object per
  line; gitignored). A `--faction-id` scan uses `action_log.<faction>.jsonl`.
- **Incremental by default**: entries come back newest-first, so the scan stops
  as soon as it reaches an id already in the store. The first run with an empty
  store therefore pulls everything; subsequent runs are cheap.
- `--full` disables the early-stop and re-pages the whole log, appending any
  entries the store was missing (already-stored ids are skipped — no duplicates).
- The early-stop is **disabled automatically when `--category`/`--event-type` is
  set**, since a filtered query no longer bounds the older matching records by
  the store's newest id. New matching entries are still appended.
- The final report (table or `--json`) is computed over the **whole store**, not
  just this run; each type's `example`/`last_seen` come from its newest entry,
  `first_seen` from its oldest.

## Notes

- Progress lines go to **stderr**; the final table / JSON goes to **stdout**, so
  `action-log-scan ... > out.txt` captures just the report.
- The game server tends to close the WS connection after each query, so the tool
  reconnects + re-logs in before every page (and retries a page once on a
  mid-call drop). A full first scan of a few thousand entries takes a couple of
  minutes; incremental runs are near-instant.
