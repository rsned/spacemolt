---
name: reference-wildlife-no-swarm-aggro
description: "Wildlife never swarms — only the creature under attack fights back; others at the POI stay passive (operator, 11-16)"
metadata:
  node_type: memory
  type: reference
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-17T01:25:42.697Z
---

Operator rule (2026-11-16): wildlife creatures do NOT attack in swarms. When
you attack one, only that creature fights back; the rest of the herd at the
POI stays out of it.

**Why it matters:** a hunter can work a crowded belt one kill at a time and
size the fight against ONE creature's hull/damage, not the POI's population.
Threat is per target, so the creature-part design (Station Need deliveries)
picks targets by species stats, not by how many are present.

**Caveat:** hostile species that attack on their own (e.g. the Molt Leviathan
that killed explorer-8 and explorer-10) are a separate matter — they start fights,
which the flee gate and default_stance flee cover. See [[reference_battle_flee_gate]].

Related: [[reference_belt_grazer_hunting_grounds]] [[project_combat_roadmap]]
