---
name: cloudwhale-too-tough-to-hunt
description: "cloudwhale is a \"grazer\" with 3,501 hull that hits back 80/shot; killed pirate-7's Gauntlet 2026-11-26; hunters now refuse quarry over 500 hull"
metadata:
  node_type: memory
  type: reference
  originSessionId: 734f5d40-f51d-46b3-9086-72af3ca8b239
  modified: 2026-11-27T17:56:36.661Z
---

A wildlife role of "grazer" and danger text "does not attack ships first" say nothing about how hard a creature hits back. The Cloudwhale (ids start `lev_`, 3,501 hull) hit pirate-7's Gauntlet (285 hull, 85 shield) 6 times in 8 shots for 480 damage at Birchwood vapor fields on 2026-11-26 12:17 PT. The hunter fled at 33% hull and died fleeing (flee takes full damage for 3+ ticks). The fleet has zero cloudwhale kills. Lumen Oil needs are the ones that ask for it.

Since 2026-11-27 hunters gate quarry on a **danger score** (`pkg/worker/hunt_threat.go`), operator decisions same day: danger = creature max hull / hunter damage per tick * creature damage per tick / (hunter max hull + max shield); refuse over `huntDangerMax = 0.35`. Creature damage = wildlife_attacks damage per shot; hunter damage = its last 50 wildlife_kills (hull / fight ticks), default 18 (Gauntlet) under 10 kills, so a new hull starts at the Gauntlet figure. Gauntlet scores: chlorophage 2%, pilot whale 24%, frost lurker 78%, crusher mantis 223%, cloudwhale 3,200%. Unmeasured species fall back to a 500-hull cap. `huntTooToughSpecies` (cloudwhale) is a never-list; `huntDangerAllowed` is a per-hull allow-list, empty. **Predators** are quarry only when the score passes AND rests on 20+ recorded shots (a Hoarfrost hatchling scored 29% on 6 shots); none passes for a Gauntlet. Operator expects next-tier hulls soon; no code change needed for them.

**Training data** (operator 11-27: "a good learning point to be sure gets into the training data"): the fatal sortie `pirate-7-20261126T195434Z` was recorded `returned_damaged`; I relabelled it `destroyed` with a `corrected` note. A sortie that closes in a different hull now records `destroyed` and `last_engaged`. Also since 11-27 every engagement writes a `target` decision (one option per creature at the ground: species, role, hull, max_hull, admissible, and `refused` = too_tough / in_combat / not_quarry) and a `target` outcome answering it by seq (result, ticks, killed, hull before/after, ship_lost); code in `pkg/worker/hunt_target.go`. No exploration on `target`. Target options also carry danger and both damage figures. Still not captured: the `job` decision (taking the need, phase 5). Action log `combat.battle_ended` carries battle_id plus damage dealt/taken (2,689 hunter battles, 84 without wildlife_attacks rows on 11-27); a LOST battle has no battle_ended, only `combat.ship_destroyed` with no battle_id.

Related: [[hunt-training-data-cutoff]], [[index_combat_wildlife]], [[battle-flee-gate]]
