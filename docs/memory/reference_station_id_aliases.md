---
name: reference_station_id_aliases
description: "Stations have two live ids (base id vs poi id); 15 differ, incl. all 5 empire capitals. Authoritative map = bases(id, poi_id). Joining base ids against pois.id silently under-reports."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2671530b-761a-4dbe-b378-62f725016c20
  modified: 2026-08-09T10:45:00.572Z
---

**Every station can appear under a BASE id or a POI id, and for 15 of them the
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
