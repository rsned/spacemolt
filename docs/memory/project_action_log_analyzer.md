---
name: project_action_log_analyzer
description: "FUTURE FEATURE — get_action_log summarizer/analyzer: reconstruct an agent's recent activity as a scannable event_type + summary timeline, drill into data on demand."
metadata: 
  node_type: memory
  type: project
  originSessionId: 853398d3-bb3c-4431-bf12-2caa11653a5d
---

**Requested 2026-07-19 (user). NOT STARTED.** `get_action_log` returns a large, verbose,
paginated server-side action log. Build an analyzer that reconstructs recent activity in a
scannable form — start with just `event_type` + `summary` per entry (a timeline), and let
the operator drill into the full `data` block on demand.

**Fetch plumbing already exists** (analyzer is a NEW layer on top): `GameClient.GetActionLog`
(`pkg/game/client_commands.go:919`, `interface.go:246`), response `serverapi.GetActionLogResponse`,
stored under key `action_log` with an `entries` array; MCP variant `MCPGameClient.GetActionLog`.

**Wire shape (per user, 2026-07-18):** response has `entries[]` + pagination
(`page`, `page_size` (50), `total`, `total_pages`, `has_more`). Each entry:
```json
{ "id": 61187083, "category": "navigation", "event_type": "navigation.jumped",
  "created_at": "2026-07-18T16:53:16Z",
  "summary": "Jumped from Dheneb to Proxima Centauri",
  "data": { "from_system": "dheneb", "to_system": "proxima_centauri",
            "arrival_poi": "proxima_centauri_frost_ring", "first_visit": false, ... } }
```

**Design sketch:**
- CLI tool (e.g. `cmd/tools/action-log`) or `play_as` subcommand: fetch all pages
  (follow `has_more`/`total_pages`), print a compact `created_at  event_type  summary`
  timeline, newest-or-oldest first.
- Filters: by `category` / `event_type` prefix (e.g. `trade.*`, `navigation.*`), time window,
  agent. `--verbose` expands the `data` block for matched rows.
- Rollups: counts per category/event_type; spend/earn reconstruction from trade events
  (this is exactly how the fighter-4 iron_ore @2000 overpay was diagnosed by hand —
  see [[reference_trading_missions_not_market_validated]]).
- Pairs well with [[project_captains_log_task_resume]] (what the agent *intended*) —
  action-log is what it *did*.

**When building:** confirm `GetActionLogResponse` field names + the request payload
(pagination params) against the live server before coding — do NOT assume. superpowers
brainstorming → SDD.
