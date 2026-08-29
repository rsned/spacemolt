---
name: reference_customs_mechanics
description: "Customs only scans pilots who STOP at a border system for 10 ticks — continuous travel = no scan, no confiscation"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 051adb3a-a06c-4dac-aed0-f51137d16814
---

Customs scanning/cargo confiscation (operator-confirmed 2026-07-17): only triggers if the pilot **stops at a border system and waits ~10 ticks** to be scanned. A ship that keeps traveling through is never scanned and never has cargo confiscated.

Implications:
- Worker autopilot never loiters at borders → mission/haul cargo is effectively never at customs risk.
- Smuggling-type missions' contraband is therefore deliverable safely in practice; the mission-runner excludes them via the type allowlist + Warnings gate mainly for **insurance-voiding** warnings, not customs. Smuggling couriers (which pay well and often provide the cargo) are a viable future income lane if we ever relax that gate deliberately.
- **Prerequisite (operator, 2026-07-17): accepting `smuggling`-type missions requires smuggling skill ≥ 1**, obtainable ONLY by completing the initial smuggling chain mission from the station in the `treasure_cache` system. So a smuggling lane needs a one-time onboarding trip per worker (treasure_cache chain) before the allowlist relaxation does anything.
- **Completing the first TWO smuggling chain missions grants PERMANENT pirate reputation → travel to pirate strongholds without being attacked** (operator, 2026-07-17). Implications: (1) the mission-runner's stronghold guard is a per-agent capability, not a universal danger — an onboarded worker could safely run stronghold-destination Trade Runs (e.g. the blocked Dross Citadel run) and route through stronghold systems; (2) this may be how the stronghold-resident marketbots survive; (3) a future "smuggler-onboarded" worker flag could lift both the stronghold guard and the smuggling allowlist per agent. Onboarding = the first TWO mission **chains** at treasure_cache, each chain being a series of **3 missions of increasing objectives** — so ~6 missions total per worker, not 2.
- engineer-1's historic `customs_evaded: 1` stat is consistent with this mechanic.

Related: [[project_idle_agent_income_paths]] [[reference_mission_board_wire_shape]]
