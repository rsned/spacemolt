---
name: reference_battle_flee_gate
description: "Non-hunt workers never fled combat until 11-11 — explorer-8's survey vessel was killed by a Molt Leviathan at Oxbow Belt B; worker battle gate now sends `battle stance flee`; deaths now logged as SHIP DESTROYED"
metadata:
  type: reference
---

**Incident (2026-11-11 ~01:00 PST):** explorer-8 (survey_vessel, 1,020 fuel,
high-tier survey gear) arrived on its system tour at oxbow_belt_b and was
attacked by a **Molt Leviathan** (predator; juveniles ~600 hull seen there in
Oct). Battle 1m50s, 650 damage, Leviathan 640. The worker re-ran explore every
~17 s, got `in_battle` each time, never fled. Respawned in a Shard (90 fuel,
1 fuel/jump) at war_citadel/krynn with 0 credits; parked in
mission-learn-overrides.json (docked at Node Beta) pending a replacement hull.

**Why it was invisible:** player_died went only to the debug logger
(discarded in production). Fixed (fd59069b): `💀 SHIP DESTROYED cause=…
killer=… ship=… wreck=… respawn=… insurance=…` in the operational log. The
game's own record: play_as `get_action_log` → `combat.ship_destroyed`
(cause, ship_class, wreck_id) — no killer name; the website battle page has it.

**Fix (aeea4562):** `pkg/worker/battle_gate.go` runs before every
standing/scheduled/task command for every role except `hunt`: if
State.InBattle and the command is not a combat action, send `battle stance
flee` and return ErrFleeingBattle (loop-fatal). A "not in a battle" refusal
means the flag is stale → run the command. Log line: `⚔️  battle: in combat
at …`.

**Not covered yet:** explore's system tour does not avoid POIs with known
predators (wildlife_observations role=predator); a long single command
(autopilot route) only meets the gate between commands.

Related: [[reference_environmental_hazards]], [[reference_combat_damage_pipeline]].
