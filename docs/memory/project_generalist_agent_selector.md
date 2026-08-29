---
name: project_generalist_agent_selector
description: "Long-term direction — no agent is permanently a mission-runner/miner/hauler; each weighs all income paths per juncture, gated by location and current ship capability."
metadata: 
  node_type: memory
  type: project
  originSessionId: 8c9098dc-7c7c-4768-bb10-b65a2ce84115
  modified: 2026-07-25T09:40:02.926Z
---

Stated by the user 2026-07-20 while scoping [[project_shipping_carrier]] Sub-project B.

**The direction:** roles are not identities. No agent should be *just* a mission-runner, *just*
a miner, *just* a hauler. At each juncture an agent should consider every income path open to
it and pick the best-scoring one. Which paths are *open* is gated by:

- **Location** — at its home base with ships to choose from, more options unlock; out in the
  field, options are limited to what the current hull can do.
- **Ship capability** — e.g. freight needs ≥100 free cargo (sealed package footprint is a flat
  100), so not every hull qualifies; a hauler can size to any hold.

**Why:** the fleet currently hard-partitions agents by role via fleet yamls, which strands
capable agents on low-yield paths and is the root of the recurring "what do we do with the idle
pool?" question ([[project_idle_agent_income_paths]], [[project_mission_learning_pool]]).

**How to apply:** NOT being built yet — deliberately deferred (user chose "narrow B" over
building the generalist selector during Sub-project B, to avoid coupling freight's first live
validation to a refactor of two working income paths). Until it is designed, shape each new
income path as a **cleanly-scored candidate with an explicit capability precondition**, mirroring
`missionCandidate` / `freightCand`, so it drops into the eventual selector without adapters.
Related but narrower: [[project_ship_role_naming_scheme]].

**Endgame (2026-07-25): every agent "trained up" to work in ANY overmind fleet.** The full
vision = each agent has (a) all skill sets leveled to their gates (e.g. smuggling L2/L3
[[project_smuggling_enablement]], mining, trading, combat) AND (b) the full role-ship stable via
capability-driven provisioning [[project_ship_role_naming_scheme]] — so the overmind can drop any
agent into any fleet (haul/mb/shuttle/craft/assist/mission-learn/smuggler) on demand. Two enabling
tracks feed it: **skill training** (run the gate chains/missions to unlock each category — smuggling
is the first worked example) and **ship provisioning** (role→required `inherent_capabilities`→hull).
The generalist selector is what then picks the best open path per juncture once an agent is fully
trained + provisioned.
