---
name: reference_station_id_aliases
description: "Stations have two live ids (base id vs poi id); canonically 48 of 76 differ. CANONICAL SOURCE = https://game.spacemolt.com/api/stations (no auth, has base_id+poi_id+services). Our bases table is stale: 32 aliases, only 60 of 76 stations."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2671530b-761a-4dbe-b378-62f725016c20
  modified: 2026-08-09T10:45:00.572Z
---

**Every station can appear under a BASE id or a POI id, and for 32 of them the
two differ.** The operator's explanation: legacy names they could not remove,
so both stayed live and the server uses them interchangeably. Confirmed
2026-08-01 against databot, prophet-1 and craftsman-1.

**The authoritative map already exists — `bases(id, poi_id)` in
`data/spacemolt-knowledge.db`.** Do not hand-roll it.

```sql
SELECT id, poi_id, name, empire FROM bases WHERE id <> poi_id;
```

**The five empire capitals — genuine renames, no shared substring:**

| base id | poi id | empire |
|---|---|---|
| `confederacy_central_command` | `sol_central` | solarian |
| `central_nexus` | `the_core` | voidborn |
| `frontier_station` | `mobile_capital` | outerrim |
| `grand_exchange_station` | `grand_exchange` | nebula |
| `crimson_war_citadel` | `war_citadel` | crimson |

Plus **all 7 pirate strongholds** on a mechanical `_station` suffix
(`crix_stronghold_station`→`crix_stronghold`, and dross / kael / mera / nyx /
thane / voss), and 3 hex-id player bases.

**⚠️ `_station`-suffix stripping is NOT a valid shortcut.** It resolves the 7
pirate cases and **none of the 5 capitals**, so it looks like it works.

**Why it bites: the two forms live in different columns of the same row.**
In `pkg/assets`: `agent_profile.home_base` and `.docked_at_base` are BASE ids,
`.current_poi` is a POI id, `agent_hulls.location_base_id` is a BASE id, and
slices 5-6 `agent_storage` will be BASE ids (that is what `view_storage`
returns). `pkg/assets` has no FKs by design, so a bad join does not error — it
just returns fewer rows.

**⭐ Measured cost, craftsman-1, 2026-08-01: 3 of its 4 station ids are absent
from `pois`, so `JOIN pois ON pois.id = location_base_id` returns 2 of 20 hulls
— 10%.** `grand_exchange_station` alone held 12 of them. Reading
`docked_at_base` next to `current_poi` and concluding the agent is in two
places is the other easy misread; they are one place.

**⭐ It bites NAVIGATION too, not just joins.** A freight contract's
`destination_base_id` (and `origin_base_id`) is a BASE id, and `travel` takes a
POI id — so you cannot fly to a contract's destination as given. Worse, the
base id can name a system that exists and is WRONG: `frontier_station` reads
like the `frontier` system, which is real and has two real stations
(`expedition_launch`, `scout_docks`) — but the contract's destination is
`mobile_capital` in **Void Gate**. Autopiloting to the obvious-looking system
strands you a galaxy away holding the package, with the deadline running.

Resolve before flying, every time:

```sql
SELECT b.id, b.poi_id, p.system_id, s.name
FROM bases b JOIN pois p ON p.id = b.poi_id
LEFT JOIN systems s ON s.id = p.system_id
WHERE b.id = '<destination_base_id>';
```

Then `autopilot <system_id>` and `travel <poi_id>`. Caught live 2026-08-09 on
the shield_recharger_ii run [[reference_freight_unpriced_cargo_prime_gate]].

Related: [[project_agent_capability_ledger]] · [[reference_docked_at_base_gotcha]] · [[reference_empire_field_semantics]]

## 2026-09-04 — the population has shifted to PLAYER stations (3 -> 18)

Recount: **32 aliases, of which 18 are hex-id player stations** and only 14 are
named. Player stations are now the majority case, and they are the dangerous
one: an empire or stronghold alias is guessable by eye
(`frontier_station`/`mobile_capital`, the mechanical `_station` suffix), but a
player station has **a hex id AND a name**, so BOTH ids are 32-char hex and the
pairing is undetectable by inspection. All 18 are `public_access = 1`.

**How it bit, concretely.** BD+20 2457's crystal depot was recorded in
`agent_storage_items` under base id `59b102279f50..`. Querying `pois` and
market.db `stations` for that id returned nothing, and it was reported as a
depot "invisible to both catalogues" -- wrong. It is The Veil Anchor, a player
station, catalogued under its POI id `9e0b4dbad76a..`; `bases` had the mapping
the whole time. Go to `bases(id, poi_id)` FIRST when an id resolves to nothing,
before concluding data is missing.

Storage itself is consistent -- `agent_storage_items.base_id` always holds the
BASE id (0 rows key off a poi_id), so holdings are never split across the pair.
The breakage is in the lookup, not the data. Related:
[[reference_player_station_access]] (access is LEARNED; unverified = closed).

## 2026-09-04 — the CANONICAL source, and our table is behind

**`https://game.spacemolt.com/api/stations` — public, no auth, ~87 KB.** Each of
the 76 stations carries `id`, `base_id`, `poi_id`, `name`, `type`
(station/outpost), `faction_id`/`faction_name`/`faction_tag`, `system_id`,
**`services[]`**, `condition`, `satisfaction_pct`, `facility_count`,
`weapon_dps`, `wrecked`. This is the authority for the alias map -- prefer it
over the local `bases` table, and use it to refresh that table.

**Canonically 48 of 76 stations are aliased** (`base_id != poi_id`), against
only 32 in our `bases` table. Our table holds 60 of the 76 and is **missing 16
outright** -- all faction outposts and new faction stations (the `ENDL:*` family,
Argon Bank Range, Ashborne Reach, Copperbelt Range, Fortress Blackthorn,
Frontier Vault, Frost Ring Range, Glintfin Range, Ironbelt Range). Those 16 are
also exactly the stations reporting `services: []`, so they are invisible to us
AND non-functional for refuel/repair -- a bad combination for an autopilot that
routes to the nearest station. See
[[project_no_fuel_cells_refuel_deadlock]] for the services angle.
