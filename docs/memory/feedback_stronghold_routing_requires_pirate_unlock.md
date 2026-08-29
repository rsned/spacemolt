---
name: feedback_stronghold_routing_requires_pirate_unlock
description: Standing rule — never route an agent to a pirate stronghold (algol, zaniah, alhena, ...) unless its pirate baseline is unlocked (>= 10); unlocked agents may. Strongholds are dead-end systems, so this is an endpoint check
metadata:
  type: feedback
---

**Operator rule, stated repeatedly** (08-15 "algol is a pirate stronghold —
if there is no reputation, death is likely"; 08-23 "as we know Algol is a
pirate Stronghold and requires pirate unlock"; 08-29 "algol is a stronghold",
and "do not route agents to strongholds unless they have unlocked pirate
reputation").

- **The rule is per-agent, not blanket.** A pirate-LOCKED agent (baseline
  <= 0, default -30) must never be sent TO a stronghold — for a rescue, a
  refuel, an arbitrage leg, a mission, a fare, anything. An UNLOCKED agent
  (baseline >= 10 with that stronghold's `pirate_*` faction) may go; the
  unlock giver itself lives at a stronghold, which is why blanket bans break.
- **Strongholds are dead-end systems with no transit through them**
  (operator 08-22), so the check is on the DESTINATION/endpoint, not on
  route hops. Crossing ordinary Lawless space is fine at any reputation —
  [[reference_lawless_transit_vs_idle]].
- The nine strongholds and their systems: [[reference_pirate_base_registry]]
  (algol=dross_citadel, zaniah=mera_sanctum, alhena=voss_redoubt, ...).
  `systems.is_stronghold` marks all of them; `agent_standings.baseline` per
  `pirate_*` faction is the unlock state.
- A locked agent that must reach a stronghold later (unlock campaign) should
  first move to the **nearest EMPIRE station and refuel** (operator 08-13),
  then make the final approach.
- Cheap fuel at a stronghold is NOT a fuel source for a locked agent —
  `NearestFuel`/refuel routing must exclude it.

**Why:** 8 of the 24 hulls lost this month died at strongholds (algol,
zaniah), including assist-sol's 1,500-fuel Capacity tanker on 08-15 — sent
there by the assist role, which has no guard at all.
[[reference_stronghold_guard_is_per_role]] [[reference_assist_tanker_migration]]

**How to apply:** before dispatching, scripting, or hand-flying ANY agent to
a system, check `is_stronghold` and the agent's pirate baseline; refuse if
locked. When building routing, put the gate in the movement layer once
(haul.go already has the correct per-agent logic) instead of per role.
