---
name: project_player_sightings_timeline
description: Per-hop player sightings timeline (seen_player_events, observer + tick) and the worker PlayerObserver wiring — COMMITTED 2026-08-29 `2dfd83e9`, NOT deployed; marketbots still schedule no sighting call
metadata:
  type: project
---

**Built 2026-08-29 after the MoltenOne kills**
([[reference_moltenone_player_hunter_ip_block_kills]]). Operator ask: call
`get_system_agents` at every system an agent passes through, before the next
jump, so any player's movements can be rebuilt (tick, system, poi).

**What shipped in `2dfd83e9` (local main, unpushed, UNDEPLOYED):**
- `cmd/worker` now calls `agent.WirePlayerObserver` — before this the worker
  fleet NEVER persisted a player sighting (only play_as/auto-explorer did);
  every hunt-fleet `get_nearby`, scan and battle event was dropped. Same
  class of gap as [[project_worker_storage_capture_gap]].
- `KBWaypointCapture` (pkg/worker/capture.go) is the single per-hop capture
  behind autopilot's `OnWaypoint`: get_system + get_poi as before, plus
  `get_system_agents` **only when police_level == 0** (rate-limit budget;
  operator: "we can change the lawless-only if we need more data later").
  Six duplicated closures (dispatch/haul/explore/shuttle×3) collapsed to it.
- New table `seen_player_events` (knowledge migration 56): append-only,
  `observer_id`, `tick`, `seen_at_utc`, system/poi/ship/source. The
  hour-bucketed `seen_player_sightings` is unchanged. `ObservedPlayer` and
  `SeenPlayer` gained `Tick`/`ObserverID` (stamped in `observerStamp()`).
- `scripts/sql/initialize_database.sql` regenerated (a test enforces sync).

**DEPLOYED 2026-08-29 11:53-12:26 local to mb/assist/hunt/craft/shuttle** (bin/worker built 11:53:24 from `35561e87`; each fleet TERM → rm sock → relaunch with its exact live line, one at a time, zero block lines; verified oldest worker start > binary mtime). **haul, unlock, mission-learn still run the OLD binary.** Craft was relaunched WITHOUT `--plan-queue`, matching the 02:23 launch (last plan-runner banner 08-27). Full roll = rebuild bin/worker + roll every fleet ([[reference_deploy_verification]],
[[project_pending_rollout_queue]]). Until then the fleet still records nothing.

**Marketbots are NOT a sensor net yet** (operator asked 08-29): the resident
roles schedule kb_update/view_market/update_market/facilities/capture_*
only — no `get_nearby`, no `get_system_agents`; the only worker caller of
`GetNearby` is `hunt_wildlife.go`. Adding a scheduled `get_system_agents`
to the resident roles (~40 parked bots, one call each per interval) needs a
dispatch case for the command name plus roles.yaml lines — a small
follow-up, not yet approved.

**Query the timeline:** `select observer_id, system_id, poi_id, tick,
seen_at_utc from seen_player_events where player_id=? order by tick`.
MoltenOne = `b195177bf33ce1de4d155a57d1ab149e`.
