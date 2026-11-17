---
name: reference-hunt-fleet-is-miners
description: "Hunt fleet (pirate-1,5..10,13) flies LEGACY mining hulls (Prospector/Drillship) with gunnery/weapons ~1; idles on \"difficulty 2 over cap 1\" (11-16)"
metadata:
  node_type: memory
  type: reference
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-17T01:07:44.316Z
---

As of 2026-11-16 the hunt fleet is hunters in name only. pirate-6..10 fly
**Prospector**, pirate-5 and pirate-13 **Drillship**: operator-confirmed **legacy
hulls** (not in catalog_ships.json, so class lookups by name find nothing).
Skills (assets.db agent_skills): piloting 27–32, deep_core_mining 17, mining 12,
but gunnery/weapons/tactics 0–3. pirate-1 is fuel-dead at Ironhearth (quarantined).

Every hunter idles on its mission board: the only combat missions are
pirate_bounty at difficulty 2, over the hunt cap of 1. agent_ships in the KB is
empty, so fittings are unknown without a live get_ship.

Fleet ships and skills: assets.db `agent_profile.active_ship_id` +
`agent_skills`; ship names show on the worker "Ready! ... Ship: X" login line.

Related: [[reference_battle_flee_gate]] [[reference_combat_damage_pipeline]]
