---
name: project_ship_role_naming_scheme
description: FUTURE overmind feature — documented ship custom-naming scheme so the overmind can re-classify/re-allocate idle agents to role-appropriate hulls
metadata: 
  node_type: memory
  type: project
  originSessionId: 853398d3-bb3c-4431-bf12-2caa11653a5d
  modified: 2026-07-25T09:39:27.778Z
---

Requested 2026-07-19. FUTURE overmind capability (not started; needs its own brainstorming → spec → plan cycle).

**Goal:** agents perform many task types, and each wants a different hull + fitting — a mine wants a mining hull with cargo-hold bonuses, a shuttle wants passenger berths, a hauler wants cargo space, a hunter/fighter wants a fast combat hull with weapons, a Tow mission needs a hull with a `tow` module (not carried in general). These are generally *distinct* ships (an ice miner ≠ a gas harvester), every empire has its own variety of each, and we may pick non-obvious choices too.

**Core ask:** define and DOCUMENT a stable ship custom-naming scheme (server command `name_ship <new_name>`; names are **3–32 chars**) keyed per role/class/category, so a multitasking agent (e.g. the many mission-runners idling with no missions — see [[project_mission_learning_pool]]) can be re-classified/re-allocated: the overmind swaps the agent to an appropriate ship where possible, including swapping to a hull fitted with a special module on demand (e.g. `tow` for Tow missions).

**User's own naming examples:** `hauler`, `ice_miner`, `gas_harvester`, `taxi`. Roles the user has since enumerated: miner, freighter, explorer, mission_runner, **smuggler**, etc. (many hull options per role per empire → hard to track which hull is for what; role-names make switching easy).

**Smuggler role (2026-07-25, ties to [[project_smuggling_enablement]]):** a `smuggler`-named hull should be a **scan-resistant** ship — catalog `ships.inherent_capabilities` exposes a `scan_resistance` capability w/ numeric value (e.g. probable_cause 40 + fuel_efficiency, solipsism 50, dust_devil 30). Shuttle-to-stronghold wants `reticence`/`cloister` (scan_resistance + passenger berths). So the overmind can swap an idle mission-runner onto its scan-resistant smuggler hull for chain/courier runs, then back. (Still out of scope here: buying/fitting — assume the named hull already exists.)

**Scope (narrowed 2026-07-19 by user): the plan does NOT include ship buying or fitting.** In scope: (1) the canonical role→name mapping (naming-scheme doc); (2) overmind re-classification/re-allocation logic that reads the name scheme to switch an idle/multitasking agent onto an already-owned, role-appropriate named ship. OUT of scope: acquiring hulls, installing modules — assume the appropriately-named/fitted ships already exist; the overmind only names (`name_ship`) and switches among them. Adjacent (informational only): [[project_refueler_ship_roadmap]], [[project_ship_fitting_calculator]], [[reference_ship_replacement_workflow]].

**Longer-term horizon (2026-07-25):** the ultimate goal is **fleet provisioning** — buy or BUILD (and fit) a role-appropriate ship for EACH role for EACH agent, so every agent owns a full stable (miner, freighter, explorer, mission_runner, smuggler, taxi, …) and the overmind just switches among them.

**Provisioning is capability-driven (2026-07-25):** as an agent onboards a role/skill set it acquires the ship whose **`inherent_capabilities`** satisfy that role — one data-driven role→required-capability→catalog-ship rule, not per-role special cases. Mappings: shuttle→`passenger_*_berths`, smuggler→`scan_resistance`, miner→mining/cargo bonuses (ice_miner/gas_harvester), refueler→fuel capacity/efficiency, tow-mission→a `tow`-module hull. `ships.inherent_capabilities` JSON is the queryable source (values seen: scan_resistance, integrated_cloak, passenger_economy/business/first_berths, fuel_efficiency_bonus, integrated_survey_scanner, rad_harvesting). That re-includes the buy/fit/build that the near-term naming plan excludes; ties to the crafting brain (`ship_build_materials`) [[project_crafting_brain]], [[project_ship_fitting_calculator]], [[project_refueler_ship_roadmap]], [[reference_ship_replacement_workflow]]. Phasing: (near) name+switch owned hulls → (long) provision the hulls per role per agent.

**Why:** idle mission-runners (structurally near-zero mission acceptances — buy-at-dock limitation) are wasted capacity; role-tagged ships let the overmind repurpose them.
**How to apply:** when starting this, run a fresh brainstorming cycle; verify `name_ship` command shape + 3–32 char limit against live server/openapi before designing.
