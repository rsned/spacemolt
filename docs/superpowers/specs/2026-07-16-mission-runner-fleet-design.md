# Mission-Runner Fleet — Design

**Date:** 2026-07-16
**Status:** Approved for planning
**Sub-project:** A of the idle-agent income initiative (B = mining→crafting vertical, deferred to its own spec; C = combat/bounty economy, deferred pending combat-lab research)

## Problem

Roughly half of our ~160 agent accounts sit idle while the running fleets
(haulers, shuttles, marketbots, craftsmen, assists) generate all income.
Arbitrage hauling realizes only ~34% of predicted profit and compresses the
spreads it harvests; we want an income stream with deterministic payouts that
scales onto idle accounts.

Missions pay fixed rewards: supply/delivery runs are 1,500–4,000 cr each and
stack up to 5 per route; exploration circuits pay 10,000–20,000+ cr per loop.
The official Mission Runner guide estimates 5,000–10,000 cr/hour. Missions
also build empire reputation, which unlocks richer boards over time.

## Goal & success criteria

A new overmind "mission" fleet earning credits from repeatable
supply/delivery missions, expanding to exploration circuits in phase 2.

- **Primary metric:** net credits/hour per worker
  (`credits_earned − item_cost − fuel_cost`), measured via telemetry and
  compared against the haul fleet's realized 220–3,057 cr/jump.
- **Canary:** 3–5 workers from idle trader accounts in one empire band,
  measured before any scale-up decision.
- **Secondary:** empire reputation accrual per agent.

## Key facts (verified)

- Client commands are complete: `GetMissions`, `AcceptMission`,
  `CompleteMission`, `AbandonMission`, `DeclineMission`,
  `GetActiveMissions`, `CompletedMissions` (`pkg/game/interface.go:248`).
- `MissionObjective` carries `item_id`, `quantity`, `target_base_id`,
  `system_id` — enough to compute cargo needs and route compatibility
  (`pkg/game/serverapi/types.go:663`).
- `CompleteMissionResponse.CreditsEarned` exists
  (`pkg/game/serverapi/responses.go:443`), but the client's raw-JSON router
  has no store key for complete_mission responses, so v1 measures realized
  income as the credits delta around `CompleteMission` and records the
  board's expected reward alongside it.
- The hourly `kb_update` capture (`KBUpdateMissions`,
  `pkg/worker/capture.go`) stores only hand-authored templates and **skips
  procedural missions** (empty `template_id`). The repeatable supply/delivery
  missions are procedural, so a central KB-driven scanner would be blind to
  them. Runners must read boards live while docked.

## Architecture: autonomous runners + telemetry

No central mission brain. Each worker self-selects from the live board where
it is docked, executes, and reports results. Rationale: procedural boards
mutate between scan and arrival; central scanners are a single point of
failure (see the 2026-07-16 arbitrage-scanner outage); and boards are only
visible while docked anyway.

### Worker task: `missions` (`pkg/worker/mission.go`)

Loop, modeled on `haul.go`:

1. Dock → `GetMissions` → parse the live board.
2. **Select** a stackable set (up to the 5-slot server cap) — see selector
   rules below.
3. Accept the set. For supply missions, acquire cargo (buy at best ask or
   withdraw from storage).
4. Autopilot to each `target_base_id` in route order; `CompleteMission` at
   each destination.
5. Record a `mission_results` row per mission (including abandons).
6. Read the destination station's board and repeat — chain boards forward
   instead of backtracking.

### Selector rules (v1)

- **Type filter:** supply/delivery mission types only. Smuggling, bounty,
  and combat types are explicitly excluded. (Exploration circuits enter in
  phase 2.)
- **Cargo gate:** total required cargo fits the current hold.
- **Expiry gate:** `expires_at` must leave enough time for acquisition plus
  travel with margin (the arbitrage-expiry lesson: never accept work that
  times out mid-route).
- **Cost gate:** estimated net = `reward.credits − item acquisition cost −
  fuel estimate` must be positive. Item cost estimated with
  `GetReferenceAsk` best-ask from market.db; fuel from the same net-of-fuel
  machinery the haulers use.
- **Stacking:** v1 stacks missions sharing the top-ranked candidate's
  destination system (rank by net credits, greedy fill under budget/cargo
  caps). Route-direction banding across systems is deferred to phase 2.

### Telemetry: `mission_results` (`pkg/market`)

New table in `pkg/market/schema.sql` mirroring `haul_results`: agent_id,
mission_id, template_id, mission_type, station (accepted-at base), target
base, credits_earned, item_cost, fuel_cost, jumps, accepted_at,
completed_at, outcome (completed | abandoned | expired). Written by the
worker at completion/abandon time. The `:8087` efficiency dashboard extends
to read it as a follow-up task (SP-style, not blocking rollout).

## Fleet ops & provisioning

- New role in the worker roles YAML; new "mission" fleet entry in the
  overmind launch runbook (`reference_overmind_launch_commands` — the
  arbitrage scanner was once missed in a relaunch; add this fleet the same
  day it goes live).
- Supervised by the existing overmind watchdog — no new unsupervised
  singleton process.
- **Canary agents:** engineer-1, engineer-2, fighter-1, fighter-2 — all
  band 1–2 (same empire). Plan-time correction: trader-1..10 and
  salvager-1..10 are already employed as the haul fleet, so the canary draws
  from the genuinely idle engineer/fighter pools.
- **Provisioning:** treasury-funded bootstrap per agent — a T2 freighter
  (guide: "a T2 freighter with a weapon mount covers 90% of boards"; the
  weapon is skipped in v1 since delivery-only work has no combat). Insurance
  purchased at bootstrap; premiums are trivial.

## Error handling

- Behind schedule mid-route → `AbandonMission`, record an `abandoned` row so
  losses are visible on the dashboard.
- Empty or unprofitable board → hop to the next station with a board (from
  KB bases data) rather than camping.
- Board read or accept failures → standard worker retry/backoff; never
  re-accept a mission already recorded this session.

## Phases

1. **v1 (this spec):** supply/delivery selector, `missions` worker task,
   `mission_results` telemetry, canary fleet of 3–5 traders in one empire.
2. **Phase 2:** exploration circuits (multi-waypoint routes, fits the 12
   idle explorer accounts); optional board-snapshot capture to the KB so a
   future optimizer can steer workers toward historically rich boards.
3. **Later (out of scope):** wildlife culls / salvage contracts (double as
   cheap combat-mechanics data collection), reputation-aware board
   targeting, dashboard panel.

## Testing & rollout

- Table-driven unit tests for the selector: scoring, stacking, expiry
  gating, cost gating — against fixture board JSON.
- Parse tests for `GetMissionsResponse` fixtures (procedural + templated
  entries).
- Live smoke of the full loop via `play_as` with one agent
  (supervisor-freeze + worker-stop protocol) before the canary.
- Canary runs long enough to compare net cr/hour against haul-fleet
  economics; scale-up is a separate decision informed by the data.
