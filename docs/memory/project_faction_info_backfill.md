---
name: project_faction_info_backfill
description: Auto-fetch faction_info for factions seen on observed agents (play_as)
metadata: 
  node_type: memory
  type: project
  originSessionId: a9e877bc-b5aa-417f-b398-a7b8f6f7c432
---

Built 2026-05-31. When a new/stale `faction_id` appears on an observed agent
(via get_nearby / get_system_agents), play_as backfills full faction details
into the knowledge `factions`/members/relations tables — previously we only kept
the inline `faction_id` + `faction_tag` on `seen_players`.

- **Reuses `pkg/faction`**: `parseFactionInfo` + the `Collector`'s Store methods.
  Added `Collector.CollectFaction(ctx, client, factionID)` — fetches
  `faction_info {faction_id, limit:200}` and persists header+members+relations
  (skips intel/station-scoped data, which are own-faction/vantage-only).
- **`pkg/faction/backfill.go`** `FactionBackfiller`: non-blocking `Enqueue`
  (session dedupe via `seen` set, bounded channel), `Start(ctx)` goroutine,
  `process` skips if `FactionCapturedAt` within threshold else CollectFaction;
  `game.SleepQuick` gap between fetches. Refresh threshold = `game.FreshnessFaction`
  (24h, new const).
- **`pkg/knowledge` `FactionCapturedAt(ctx, id) (time.Time, bool, error)`** —
  freshness read on the factions table (missing → ok=false, no error).
- **Wiring**: `agent.WirePlayerObserver(c, kb, enq)` gained a nil-able
  `FactionEnqueuer` (local interface so agent needn't import faction); the
  observer enqueues distinct non-empty faction ids. play_as constructs the
  collector+backfiller and passes it; **auto-explorer passes nil** (sightings
  only, no backfill — easy to enable later).
- GameClient interface untouched → no mock breakage (see
  [[feedback_gameclient_interface_mocks]]). CollectFaction type-asserts to
  *game.Client like Collector.Collect does.
- Player-sightings capture currently runs only in play_as + auto-explorer.
- Design doc: `docs/plans/2026-05-31-faction-info-backfill-design.md`.
  Battle-side/scan faction_ids are an unbuilt follow-up. Relates to
  [[project_play_as_scheduler]].
