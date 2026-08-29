---
name: reference_api_v0564_v0565_bounty_station_ids
description: "v0.564.0 pay_bounty + detention changes, v0.564.1 combat-resistance fix, v0.565.1 station Base ID/POI ID aliasing — and the 18-version gap to our client."
metadata:
  node_type: memory
  type: reference
---

Captured 2026-08-27 from operator-pasted patch notes. **Our client declares
`BuiltForAPIVersion = "v0.547.1"` (`pkg/version/checker.go:48`) — the server is
at v0.565.1, so we are ~18 versions behind.** `VersionID` is still `"v0.0.1"`
(`pkg/game/constants.go:131`), which is what goes out in the User-Agent.

## v0.564.0 — bounty/detention overhaul. Dissolves the 0-credit death spiral.

- **New `pay_bounty` command settles an outstanding bounty with an empire FROM
  ANYWHERE** — no longer requires docking in their space.
- Paying an empire that already detains you **releases you immediately**.
- **Detained pilots can use the station exchange**, so an agent can sell cargo
  to raise its own bail.
- **Gifted credits arrive even while detained.**
- Paying one empire does not affect another empire's detention.

This directly unblocks the pattern in [[reference_agent_bounties_not_combat]]
and [[reference_bounty_auto_pays_on_entering_territory]]: previously a
0-credit agent with a bounty was deadlocked (no fuel, and gifted credits got
seized on entering territory). Now gift -> pay_bounty -> refuel works from
wherever the agent is stranded.

**We have ZERO support: `grep -rn pay_bounty --include='*.go'` returns 0 hits.**
Adding it is the [[reference_adding_a_new_game_command]] 5-step path (client
method, interface, runner dispatch, isActionCommand, response struct).

`assets.db` `agent_tax.collection_active` is likely the detention flag — it
reads 1 for the sampled players. Keyed by `player_id`, so it needs an
agent_id -> player_id join to be useful.

## v0.564.1 — combat resistance actually applies now

See [[reference_combat_damage_pipeline]] for the full rewrite. Short version:
adaptive shields were BUGGED, not useless; resistance order is shield skill ->
typed -> flat/adaptive; buckets add to a **75% cap** each with integer
truncation per stage; battle logs now emit per-weapon defense stages.

## v0.565.1 — station Base ID / POI ID aliasing

- Station-aware commands and public market APIs **accept either a station Base
  ID or its station POI ID** where unambiguous.
- **Station directory and detail responses now expose BOTH `base_id` and
  `poi_id`**; `id` remains the canonical Base ID.
- New faction mission station destinations are normalized to canonical Base IDs.

This is the server-side fix for [[reference_station_id_aliases]] (dual-named
stations making joins under-report silently) and
[[reference_public_facilities_player_station_id]] (a POI-keyed join reported 0
for a station holding 231). **Action: capture BOTH `base_id` and `poi_id` at
write time** so our joins stop depending on which name a given reply used.

Related: [[reference_patch_notes_source]] · [[reference_server_api_history]] ·
[[feedback_version_constant]]
