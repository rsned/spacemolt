---
name: project_status_archive_2026_07
description: "Rolled-off Current Status entries from July 2026 — freight regressions, carrier tiers, smuggling phase 1, multi-package freight merge, rescue cascade"
metadata: 
  node_type: memory
  type: project
  originSessionId: 7da8be40-9f9f-4a30-8440-d70ba696b4ee
  modified: 2026-08-06T03:07:23.655Z
---

Status entries that have rolled off the ⭐ Current Status section of MEMORY.md. Newest first. Each was a "resume here" line at the time; kept for the commit SHAs and the reasoning, not because action is pending.

**Why:** the ⭐ section is capped at 8 pointer-length lines so recall stays cheap; older entries live here instead of being deleted.
**How to apply:** read this when you need the history behind a freight/smuggling decision or the SHA of a rollout, not for what to do next.

## 2026-07-29 late — Assist pump gap closed, both quarantined strandees recovered
random-1 rescued by a fitted pump (+17 fuel); random-9 was DOCKED at `the_core` with 100,001 cr the whole time — its rescue record's POI was stale since 07-28, so four rescues chased a phantom; one `refuel` fixed it. Refuted three old notes: gifting is NOT co-location-limited (deposits at the SENDER's station), `play_as` DOES take piped stdin, bare `refuel` exonerated (suspect station capability for `no_fuel_cells`). Confirmed bug: a worker's own successful refuel leaves its cached fuel stale (task #10). → [[project_assist_fleet_refueling_pump_gap]] · [[project_rescue_pipeline_bugs]] · [[project_no_fuel_cells_refuel_deadlock]]

## 2026-07-29 eve — Smuggling chain 1 runs from level 0, replicates 3/3
explorer-2 reached the pirate-baseline unlock (`an_introduction` at voss_redoubt, +2867). L3 gate empirically confirmed: at L2 the mission is NOT OFFERED at the giver, no skip line. Credits are not the constraint — pick canaries ≤2 jumps out. Two gate defects fixed + deployed (`d6a739f`): XP credit was keyed on mission TYPE (a delivery paying 50 smuggling XP was refused over 7 credits); a refused accept wasn't marked attempted, so 3 canaries re-accepted one L1-gated courier every 10s forever. → [[project_smuggling_enablement]]

## 2026-07-25 — Freight load-confirm regression fixed + rolled (`2c1212f`)
The v0.2.1 load-confirm check *itself* was the bug. Use `market.db freight_results` for freight timing, NOT the worker log. → [[project_freight_load_confirm_regression]]

## 2026-07-25 — All 12 explorers carry freight
`removed[]` = craftsman-1 (**operator's facilities — do NOT re-add**) + engineer-2 (smuggling pilot). Overrides only ever REMOVE, never add. → [[project_freight_probationary_cargo_fence]]

## 2026-07-25 — Smuggling phase 1 proven
Part-1 landed on `feat/smuggling-part1-mission-capture`; next step was mapping phase 2. 9 dormant marketbots assigned to thresholds. → [[project_smuggling_enablement]] [[reference_smuggling_spare_marketbot_accounts]]

## 2026-07-25 — Carrier tiers established
licensed = 1, 2, 4, 6; probationary = 5, 7, 8, 9, 10; unknown = 3. A licensed carrier skipping probationary cargo is the fence working, NOT a bug. Worker log tagging v0.2.3/v0.2.4 deployed. → [[project_freight_probationary_cargo_fence]] [[project_worker_log_tagging]]

## 2026-07-24 — Freight probationary-cargo FENCE v0.2.2 (`91b4249`)
STILL OPEN at time of writing: reposition carriers onto empire colonial hubs. marketbot_002 added — a new agent means edit the fleet yaml + SIGHUP. → [[project_freight_probationary_cargo_fence]] [[reference_overmind_launch_commands]]

## 2026-07-24 — Rescue cascade resolved
Fuel-0 in a no-station system can't auto-rescue; manual refuel THEN clear the terminal `failed` record. → [[project_rescue_pipeline_bugs]]

## 2026-07-24 — v0.2.1 load-confirm + restart-resume (`6e211cc`)
Orphan salvage = unpack, then pay the acceptance-blocking debt. → [[project_freight_load_confirm_and_resume]] [[reference_freight_orphan_salvage_unpack]]

## 2026-07-22/23 — Multi-package freight MERGED (`fcc1fda`)
market.db rebuilt 62.7GB → 570MB; probation bootstrap v0.2.0. → [[project_shipping_carrier]] [[project_freight_probation_bootstrap]] [[project_fleet_pool_dynamic_membership]]

## Long-term direction
Agents are generalists, not role-locked. → [[project_generalist_agent_selector]]
Earlier merges: price-gate, disconnect-fix, haul BOOK-coordination, learning pool, play_as arbitrage + net-of-fuel. → [[project_mission_learning_pool]] [[project_arbitrage_net_of_fuel]]

## Rolled off the MEMORY.md status block 2026-07-28
- **2026-07-27 — jettison→loot resupplies a ship that can't dock.** `send_gift` is station-only and a tank transfer needs a pump; carrier jettisons → cannister id → any co-located agent loots it, including the jettisoner. 10-min lifetime. [[reference_jettison_loot_transfer_flow]]
- **2026-07-26 — Freight detour guard** (`b48a288`) NOT deployed; the wrong_origin loop was fixed + rolled (`3f010dd`), returns legal only at origin. [[project_freight_wrong_origin_return]]
- **2026-07-26 — Client synced to v0.547.1** (`f4f8a7c`); `sell_ship` retired server-side. (Also recorded under Server API Updates.) [[reference_server_docs_sync]]
- **2026-07-26 — Stronghold reach page SHIPPED + PUSHED** (sibling repo `spacemolt-kb` @ `a9c0f76ed`); follow-ups + headline numbers in the note. [[project_stronghold_reach_page]]

## Rolled out of ⭐ on 2026-08-01
- **⭐ 2026-07-27 — Freight load-confirm FIXED + PUSHED (`6eb2c10`).** Confirm from the withdraw's request_id-correlated `action_result`, not `Ship.Cargo`; 52 healthy contracts were returned across 18 agents. **The "clobber" theory was tested and DISPROVED — don't re-derive it.** [[project_freight_withdraw_silent_failure]]
- **⭐🔴 2026-07-27 — `server-cmd` was CORRUPTING hex ids; FIXED (`8cda9b5`).** `fmt.Sscanf("%f")` parses a PREFIX with no error, so `158222749cdf…`→`158222749`. **Run `server-cmd --debug` BEFORE theorising about game rules.** [[project_server_cmd_spec_driven_payloads]]
- **⭐🔴 2026-07-26 — v0.549.0 INVERTS the freight premise** — late delivery now beats defaulting, so the doomed-return machinery optimizes a penalty that no longer exists. Pirate standing went PER-CREW. [[reference_v0549_freight_and_percrew_pirates]]
