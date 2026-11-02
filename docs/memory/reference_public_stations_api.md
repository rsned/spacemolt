---
name: reference_public_stations_api
description: https://game.spacemolt.com/api/stations lists every station and outpost live (faction, system, services, mobile, wrecked) — use it, not the KB bases table, to find player stations
metadata:
  type: reference
---

`curl -s https://game.spacemolt.com/api/stations` (operator, 2026-10-30) returns `{empires, stations}`;
each station has base_id, poi_id, name, type (station|outpost), faction_id/name/tag, system_id,
services[], mobile, wrecked, condition. On 10-30: 91 bases = 42 empire stations, 28 player
stations, 21 player outposts. The KB `bases` table knew only 23 of the 28 player stations.

- Player stations are the ones with a faction_id and type=station. Outposts are members-only,
  so a marketbot cannot dock at one.
- Mobile stations are fine as resident pins: marketbot_frontier is pinned to `mobile_capital`
  (Frontier Station, moves) and docks there normally. The old assist-frontier travel livelock
  ([[project_assist_frontier_mobile_capital_livelock]]) was an arrival-check bug, not a rule.
- 10-30 plan: marketbots 021-033 run the pirate unlock, then each is pinned (mb-fleet) to a
  player station with no CRFT resident. Re-pull this list at each graduation.
