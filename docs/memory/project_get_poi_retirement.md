---
name: project_get_poi_retirement
description: Server retired get_poi (2026-06-24); local migration to get_location/get_system done but ON HOLD/uncommitted pending possible server revert
metadata: 
  node_type: memory
  type: project
  originSessionId: d0ff5d47-0ac1-44a0-af82-ed3c9c9ff8d5
---

Server retired the `get_poi` command on 2026-06-24 ("has been replaced; use get_system / get_location / get_base"). Local migration is DONE in the working tree but **uncommitted and ON HOLD** — the dev team may revert get_poi temporarily to give players time to update.

**Key API finding (no single command replaces get_poi):**
- `get_location` is the ONLY source of live POI **resources** (`resources[]{item_id,item_name,richness,remaining}`) + nearby players/NPCs/pirates (incl. `role:"police"` on `nearby_empire_npcs[]`).
- `get_system` carries **class/position/base/fuel** per POI but has NO resources.
- `get_nearby` also carries police (`empire_npcs[].role=="police"`).
- Genuinely lost from all commands: per-POI `description`, `hidden`, `reveal_difficulty` — but each has its own provenance (KB merge preserves description; hidden/reveal_difficulty come from `survey_system`). The old `GetPOIResponse.PoliceDrones/PoliceWarning` fields had ZERO consumers.

**Changes made (only the AUTOMATIC call sites; GetPOI plumbing left intact → resilient if server reverts):**
- `pkg/worker/capture.go`: new `GetLocationPOI()` helper (runs get_location, maps item_id→resource_id, enriches class/position/base from cached state). `KBUpdatePOI` uses it; added `KBUpdatePOIData()` returning the POI.
- `cmd/tools/play_as/kb_update.go`: update_poi uses KBUpdatePOIData; faction-intel file rebuilt as `{"poi":{…}}` from captured POI.
- `cmd/auto-explorer/main.go`: POI-detail step uses GetLocationPOI.
- `pkg/agent/runner.go`: `get_poi` action routes to get_location (old name still accepted); added get_location to no-tick query set.
- `cmd/auto-random`, `cmd/data/data-scraper`: swapped get_poi→get_location.
- `cmd/tools/play_as/main.go`: `poi`/`get_poi` REPL cmd runs get_location, renders via existing formatGetLocation.

`go build`, `go test ./...`, `golangci-lint` all clean.

**Deferred cleanup (pending user decision after revert question settles):** strip dead `PoliceDrones`/`PoliceWarning` from `GetPOIResponse`; drop `get_poi` from MCP tool list (`pkg/game/mcp_game_client.go:115`).
