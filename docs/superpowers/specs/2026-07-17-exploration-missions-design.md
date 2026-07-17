# Exploration Missions — Mission-Runner Category 2 (Design)

Date: 2026-07-17
Status: DRAFT — live probe in flight (engineer-2 running Federation Trade Route
Prospectus; results section to be filled before build)
Parent: 2026-07-16-mission-runner-fleet-design.md (delivery v1, shipped)

## Goal

Teach the mission runner the `exploration` board category — pure-navigation
missions (`visit_system` / `dock_at_base` objectives) — as the second category
of the mission-learning pool. Exploration missions need no cargo, no item
budget, and no market availability gate: their entire cost is fuel and time,
and their payers include the richest per-jump missions yet observed.

## Wire truth (live board dump, grand_exchange 2026-07-17)

31 missions on the board, 6 typed `exploration`. Objective vocabulary:

- `visit_system` — `{system_id, system_name}`; some carry `quantity: 1`.
- `dock_at_base` — `{system_id, target_base_id, target_base_name}`. The
  "Return to X with market data" flavor is a plain `dock_at_base` at the giver.
- **Type ≠ shape**: exploration-typed missions can carry non-nav objectives —
  `overdue_accounts` embeds a `deliver_item`, `wh_intro_*` a
  `traverse_wormhole`. The shape gate must allowlist OBJECTIVE types, not
  trust the mission type.
- Board `mission_id` is template-ish as with delivery; `complete_mission` /
  `abandon_mission` need the hex active-instance id from
  `get_active_missions` (same resolveActiveMissionIDs machinery).
- Active objectives render `[current/required]` per leg — resume can chase
  only incomplete legs.
- **Objectives complete in ANY order** (operator-confirmed 2026-07-17) —
  optimal-order routing is safe.
- **Objectives can be DYNAMIC** (operator-observed): completing one can append
  a new objective to the active mission (staged story shape: "meet scientist
  A" → done → "go to system C and meet scientists" appears). The accept-time
  objective list is NOT final.
- Expiries observed are LONG: 3 days (survey), 14 days (prospectus) — the
  delivery expiry gate's margins are trivially met; keep the gate anyway.

## Economics (BFS over the live KB graph, from haven)

| Mission | Reward | Optimal jumps | cr/jump |
|---|---|---|---|
| federation_trade_route_prospectus (diff 5) | 20,000 | **12** | **1,667** |
| grand_tour_of_the_five_empires (diff 5) | 12,000 | 81 | 148 |
| deep_space_cartography (diff 4) | 4,000 | 40 | 100 |
| local_sector_survey (diff 2) | 2,500 | 32 | 78 |

Two hard lessons:

1. **Difficulty stars are anti-correlated with value.** The diff-5 Prospectus
   is the best per-jump payer in the game; the "easy" diff-2 survey is a trap
   (its "nearby" systems are 32 jumps of travel). The selector must gate on
   **net-per-jump**, never difficulty or raw reward.
2. **Route optimization is v1, not v2.** Optimal leg order (exact
   permutation search, n ≤ 7 targets ≈ 5k perms over cached BFS maps) differs
   from wire order and changes viability. Pin any trailing return-to-giver
   `dock_at_base` leg last; permute the rest.

Abandon penalty: **zero credits** (measured live: 310,006 → 310,006 across an
abandon). Reputation effects unmeasured.

## Probe findings (engineer-2, Prospectus, COMPLETED 2026-07-17)

- ✅ `dock_at_base` ticks on dock, verified per leg via active_missions.
- ✅ Legs complete OUT OF WIRE ORDER (the Levy flown 4th, wire-listed 7th;
  all 7 ticked). Matches operator intel: objectives are order-free.
- ✅ `complete_mission` at the return-leg dock paid instantly: +20,000 cr
  (310,006 → 330,006) + exploration 75 XP + navigation 60 XP. (No-return
  missions' completion location still unknown; fallback nav-to-giver stands.)
- ✅ Exactly 12 jumps flown — matches the BFS-optimal estimate. Fuel ~24
  units (drillship, 2/jump), trivial vs reward.
- ✅ Wall-clock: ~19 minutes accept-to-complete → **~63k cr/hour while
  touring** (vs ~8.6k/hr measured for delivery missions). Even at partial
  board availability this is the best per-hour activity yet measured for
  idle-class agents.
- Abandon (measured on local_sector_survey): zero credit cost.

## Design

### Selection (pkg/worker/mission_select.go)

- `exploreShape(e)` — ok when: `Type == "exploration"`, no RequiredModules,
  no Warnings, ≥1 objective, EVERY objective ∈ {visit_system, dock_at_base}
  with SystemID set (dock also TargetBaseID). Returns `[]missionLeg{SystemID,
  BaseID}` in wire order.
- Candidate build: order legs by exact-permutation shortest tour from
  `current` (trailing giver-return pinned last); TotalJumps = tour cost.
  Net = Reward − fuelCostFor(TotalJumps). Gates:
  - net ≥ missionMinNet (existing floor)
  - **net/TotalJumps ≥ missionMinNetPerJump (new const, ~300 cr/jump —
    above hauler-leftover tier so exploration never outcompetes real work
    with slop)**
  - expiry ≥ missionMinExpiryTicks + TotalJumps×missionTicksPerJump
  - every leg endpoint not a stronghold; every consecutive leg-pair route
    clear via missionRouteClear (galaxy graph)
- Stacking: an exploration candidate runs SOLO (its own trip). If the best
  net candidate overall is exploration, run it; else deliveries stack as
  today. (Mixed deliver+explore banding is v2.)

### Execution (pkg/worker/mission.go)

For each leg in tour order: `deps.nav(system, poiID)`; `dock_at_base` legs
then Dock (resolve base→POI via KB, the missionStationPOI pattern —
note grand_exchange_station's POI is `grand_exchange`, ids differ);
`visit_system` legs need no dock. **After the known legs, re-read
`get_active_missions`: staged missions append new objectives on completion,
so loop plan→fly→re-read until no incomplete nav objectives remain (bounded,
e.g. 5 stages; a stage that adds a NON-nav objective → abandon, it left our
lane).** Then complete at the giver-return leg if present, else nav back to
the accepting base and complete there (pending probe: completion-from-anywhere
may make this moot). Telemetry: mission_results row, item_id="", qty=0,
jumps=TotalJumps actually flown.

**Market-capture synergy:** at every `dock_at_base` leg, fire the worker's
market capture (same call the marketbots use) — the Prospectus alone pays
20k cr for a tour of 7 Federation stations plus Haven, exactly the docks the
demand ledger and market.db want. Exploration pool = paid coverage sweeps.

### Resume

Active exploration missions: chase only legs whose objective shows
incomplete, in re-optimized order from the current system; complete as above.

### Rollout

Learning pool = a second 1-worker overmind fleet (engineer-2) with a
category allowlist knob (MissionDeps.Categories, default ["delivery"];
learning pool runs ["delivery","exploration"]). Promote to the whole fleet
once soaked.

## Open questions

- visit_system-only missions' completion location (probe only covers the
  return-leg case).
- Whether visit_system ticks require any dwell time in-system or tick on
  arrival.
- Reputation cost of abandons (credits cost measured zero).
