---
name: reference_intel_center_autosync
description: "CJ Intel Center L2 (built 11-13): server auto-writes verified faction intel on member jump/get_system/get_poi/survey_system/deep-core mining — no client submitter needed; intel holds NO ship/creature sightings"
metadata:
  node_type: memory
  type: reference
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-14T01:40:36.215Z
---

Upgraded 2026-11-13 ~17:40 PST by miningbot_0000 (play_as `facility upgrade
--facility_id=<intel_terminal id> --facility_type=intel_center`; materials and
750k came from CJ faction storage/treasury; 78 ticks to build).

**L2 auto-sync:** jump or get_system → system structure; get_poi / deep-core
mining → ore at that POI; survey_system → ore at every listed POI. Each reading
carries read_at_tick (verified). Snapshots, except POIs with an online member
(live). L2 also unlocks faction_query_intel filters resource_type, poi_type, empire.
Hand submissions (faction_submit_intel, play_as `--file`) are always unverified.

**Not covered:** ships, NPCs, leviathans, repeaters from scan/faction_scan_poi —
the intel schema has no fields for them. Faction sensor (`sensor_dome`, 600k +
300 steel_plate, 150 circuit_board, 40 sensor_array) is NOT built as of 11-13.

Related: [[project_pathfinder_frontier_sweep]], [[project_cloverfield_home_station]].
