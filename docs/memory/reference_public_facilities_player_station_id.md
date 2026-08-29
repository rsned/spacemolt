---
name: reference_public_facilities_player_station_id
description: "public_facilities keys PLAYER stations by base_id, NPC stations by POI id. A POI-keyed join reports 0 facilities for a station that has 231."
metadata:
  node_type: memory
  type: reference
---

`public_facilities.station_id` holds a **`base_id`** for player-owned stations but a
**POI-style id** for NPC stations. The same station therefore has two identities:

| The Obsidian Well (arneb) | id |
|---|---|
| `pois.id`, `market_orders.station_id` | `a356fc2c1744c0425cf6cf47f48def92` |
| `public_facilities.station_id` | `cca9e51e6eaf8dada77f698ccc4a09c7` |

Querying by the POI id returned **0 facilities** for a station that actually holds **231
across 219 recipes**. It looked exactly like a capture bug — the marketbot was healthy,
docked, and had run `facilities` an hour earlier. Nothing was broken.

**Any join from `public_facilities` to `pois` (or to `market_orders`) is silently wrong for
every player station** — it under-reports coverage rather than erroring. Same family as
[[reference_station_id_aliases]], which covers the 5 capitals + 7 strongholds whose base id
and poi id differ.

Player-station base ids seen in `public_facilities`: `cca9e51e…` (Obsidian Well),
`b495c600…` (Hex Star, Dheneb), `b866042e…`, `d1c54e3a…`, `a356fc2c…`.

To resolve one, run `facility list` while docked and read `base_id` from the response —
that is the key the capture writes under.
