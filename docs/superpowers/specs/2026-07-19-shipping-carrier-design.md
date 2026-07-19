# Freight Carrier for Mission-Runners — Design

**Date:** 2026-07-19
**Status:** Design approved, pending spec review
**Server feature:** `shipping` freight contracts (v0.517.0, v0.529.0; live server ≥ v0.531.4/v0.532.0)
**Related:** `2026-07-16-mission-runner-fleet-design.md`, `2026-07-17-exploration-missions-design.md`, `project_mission_learning_pool`, `project_idle_agent_income_paths`

## Problem

The server added a `shipping` freight-contract market: station managers post real freight
contracts ("NPC stations ship surplus goods to same-empire stations that need them — a
steady stream of newbie-friendly hauls across all risk tiers"). A carrier accepts a
contract, the sealed package lands in the carrier's storage at origin, and the carrier is
paid a flat `base_reward` (plus optional `speed_bonus`) on delivery, building global carrier
reputation. This is a new income path for our mostly-idle mission-learn pool that does not
depend on the mission board.

Freight is **not** a mission-board category — `/shipping` is a structurally independent
action-dispatched endpoint (its `ShipmentContract` has no relation to `MissionBoardEntry`).
So "mission-runners do shipping" means adding a distinct carrier behavior, not a new mission
category.

**Key risk — breach.** Missing the tick-deadline or opening the seal forfeits payment,
damages carrier standing, and creates **freight-debt that blocks all new acceptances until
paid**. Deadline discipline is the central safety concern.

## Goals

- Give the idle mission-runner pool a freight-carrier income path: list eligible NPC freight
  at the current dock, accept the best one, route it, deliver, earn payout + carrier standing.
- Never breach through our own negligence: a conservative deadline-feasibility gate and a
  `return`/`cancel` escape hatch.
- A clean, tested `pkg/game` shipping client (none exists today) reusable by haulers later.
- Validate on a canary before fleet exposure; measure breach-rate + net.

## Non-Goals (v1)

- **Shipper side** (posting our own surplus as freight: `quote`/`post`) — deferred to a later
  phase.
- **Haulers** — deferred to phase 2 once the mission-runner carrier is validated. The client
  layer (Sub-project A) is built role-agnostic so haulers reuse it.
- **Stacking** freight with other freight or same-destination missions in one trip — v1 is a
  **standalone freight trip** (one accepted contract per run). Rationale: the contract does
  not expose the package's cargo footprint (see Open Questions), so multi-package trip
  planning is impossible until we live-verify sizing.
- **Insurance selection** — priced/chosen shipper-side; a carrier does not choose insurance.

## Architecture — two sub-projects

### Sub-project A — `pkg/game` shipping client (foundation)

`/shipping` is one endpoint, `{"type":"shipping","payload":{"action":"<verb>", ...}}`, with a
discriminated response union keyed on `action` (`ShippingResponse`). No client coverage
exists today (verified: zero `shipping`/`freight`/`contract` references in `pkg/game`).

**Carrier action subset for v1** (of the 11 total): `list`, `get`, `accept`, `deliver`,
`track`, `profile`, `pay_debt`, **`return`, `cancel`** (the last two as the breach-avoidance
escape hatch). Deferred: `quote`, `post`.

Add, following existing `pkg/game` patterns:
- Client methods on `Client` (`pkg/game/client_commands.go`) + the `GameClient` interface
  (`pkg/game/interface.go`, under a new `// Shipping` group). A single
  `Shipping(ctx, action string, payload map[string]any)` low-level dispatcher plus typed
  convenience wrappers (`ShippingList`, `ShippingAccept`, `ShippingDeliver`, `ShippingProfile`,
  `ShippingTrack`, `ShippingReturn`, `ShippingCancel`, `ShippingPayDebt`, `ShippingGet`).
- `serverapi` response structs (`pkg/game/serverapi/responses.go`) with field names copied
  **verbatim** from `server_docs/openapi.json` — do not paraphrase:
  - `ShippingListResponse` { Action; Shipments []ShippingListing; Page, PerPage, Total int;
    EmptyReason, EmptyReasonCode string }
  - `ShippingContractResponse` { Action; Contract ShipmentContract }  (post/get/accept)
  - `ShippingSettlementResponse` { Action; Contract ShipmentContract; CarrierPayout,
    ClaimPaid, DebtCreated, ShipperRefund ... }  (deliver/return/cancel)
  - `ShippingProfileResponse` { Action; Profile CarrierProfile; Capacity CarrierCapacity;
    Progression CarrierTierProgress; Debts []FreightDebt; DebtBlocksAcceptance bool;
    DebtBlockReason string }
  - `ShippingTrackResponse` { Action; Contract ShipmentContract; Events []ShipmentTrackingEvent }
  - `ShippingDebtPaymentResponse` { Action; AmountPaid; Profile; Capacity; Progression;
    DebtBlocksAcceptance; UpdatedDebts; OutstandingDebts }
  - Objects: `ShipmentContract`, `ShippingListing` { Contract; Eligible bool; Reason string },
    `ShipmentActor` { Kind (player|faction|station); ID }, `CarrierProfile`,
    `CarrierCapacity`, `CarrierTierProgress`, `FreightDebt`, `ShipmentTrackingEvent`.
  - `ShipmentContract` carries (exact names from openapi): `id, package_id, shipper,
    recipient, contractor, origin_base_id, destination_base_id, shipping_house_id, visibility,
    service_level, status, posted_at, listing_expires_at, accepted_at, accepted_tick,
    delivered_at, breached_at, settled_at, target_tick, deadline_tick, base_reward,
    max_speed_bonus, service_fee, reward_escrow, speed_bonus_escrow, appraised_value,
    covered_value, premium, reserved_exposure, failure_debt, carrier_payout, claim_paid,
    policy_status, insurable, uninsurable_reason, risk_band, insurer, invited_carrier,
    salvage_owner, reputation_eligible, route_hops, terminal_reason, latest_beacon_at,
    latest_beacon_fingerprint`. Deadlines are **ticks** (`deadline_tick`, `target_tick`), not
    wall-clock timestamps.
- Enums (as string consts / validated): service_level {standard, priority}; status {posted,
  in_transit, delivered, returned, breached, defaulted, canceled}; risk_band / carrier tier
  {probationary, licensed, trusted, prime (+ unpriced for risk_band)}; visibility {public,
  faction, allies, invited}; actor kind {player, faction, station}.
- Adding interface methods breaks `pkg/agent` + `pkg/skills` mocks — regenerate/extend them;
  `go build` misses it, so run `go test ./...` (known gotcha, `feedback_gameclient_interface_mocks`).
- **Bump `BuiltForAPIVersion`** (`pkg/version/checker.go`, currently stale at v0.495.1) to the
  live server version, and note the `get_system` `kind` field drift surfaced during rollout.

### Sub-project B — carrier behavior (`pkg/worker`)

A freight opportunity source evaluated in the mission-runner's existing docked pass, **co-equal
with the mission board** (per approved priority): compute the best freight candidate's net and
the best delivery-mission's net, take whichever is higher; exploration remains the fallback
when neither clears the floor.

**Per docked pass:**
1. **Debt guard:** `shipping profile`; if `debt_blocks_acceptance`, skip freight this pass
   (log the reason). Auto-`pay_debt` is a later choice, not v1.
2. **List:** `shipping list` (docked-only; returns contracts posted here with an `eligible`
   flag + `empty_reason`). Consider only `eligible == true` listings — the server's flag
   already encodes carrier-tier / liability / debt eligibility, so we do not re-derive tier
   logic client-side.
3. **Gate each eligible contract → freight candidate:**
   - **Deadline feasibility (core safety):** estimate `route_ticks` to `destination_base_id`
     from the jump distance (existing route/fuel helpers) and require
     `route_ticks × freightDeadlineSlack ≤ (deadline_tick − now_tick)`, with
     `freightDeadlineSlack = 1.5` (a named, tunable const — conservative ~50% buffer that also
     absorbs GameClock forward-drift and reconnect stalls). If we cannot resolve a route or the
     deadline is already too tight, skip.
   - **Net:** `net = base_reward − route_fuel_cost` must be `≥ freightMinNet` (reuse the
     mission/haul net floor). Count `speed_bonus` toward net only if `target_tick` is beatable
     with margin; otherwise treat it as upside, not a reason to accept.
4. **Co-equal selection:** `best_freight_net` vs `best_mission_net` → pursue the higher.
5. **Execute the freight trip (standalone):**
   - `shipping accept` (carrier = player). The package enters the agent's **personal storage
     at origin**.
   - **Withdraw the package into cargo** (mechanic + the package's cargo footprint are
     LIVE-VERIFY items — see Open Questions). If the package will not fit the hold, do **not**
     transit toward a guaranteed breach: invoke the **escape hatch** `shipping return`/`cancel`
     *before* pickup/transit (verify which avoids debt at pre-transit stage) and release the
     candidate.
   - Route to `destination_base_id` using existing navigation; refuel as the standing loop
     already does.
   - On arrival, `shipping deliver` (deliver mechanic — cargo vs deposit — is a LIVE-VERIFY
     item). Record `carrier_payout`, standing delta, and mark the contract done.
6. **In-flight safety:** each pass while carrying, re-check `deadline_tick − now_tick` against
   remaining `route_ticks`; if the buffer collapses (e.g. after a long disconnect) and the
   contract is now unwinnable, prefer `return`/`cancel` over riding into a breach, if the
   contract state still permits it.

**Captain's-log / restart resilience:** a mission-runner that restarts mid-freight currently
loses its in-memory task (no `captains_log` resume yet — `project_captains_log_task_resume`).
For v1, on (re)connect the carrier reconciles from server state: `shipping profile`
`active_contracts` + `shipping get`/`track` reveal any contract it already holds, so it can
resume the deliver leg rather than orphan the package. This reconciliation is required in v1
because breach risk makes an orphaned in-flight contract expensive.

## Data flow

```
docked pass ─▶ shipping profile (debt guard)
           ─▶ shipping list (eligible freight here)
           ─▶ per contract: route_ticks×1.5 ≤ ticks_to_deadline ? net ≥ floor ?
           ─▶ best_freight_net vs best_mission_net ─▶ take higher (else exploration)
   accept ─▶ package into origin storage ─▶ withdraw into cargo (VERIFY fit)
          ─▶ [won't fit → return/cancel pre-transit, release]
          ─▶ route to destination_base_id ─▶ shipping deliver ─▶ payout + standing
   reconnect ─▶ shipping profile.active_contracts ─▶ resume deliver leg
```

## Error handling

- **List empty / `empty_reason`:** no freight candidates this pass; fall through to mission /
  exploration as today. Not an error.
- **Debt block:** skip freight, log once per pass; the pool keeps doing missions/exploration.
- **Accept fails** (contract taken, no longer eligible): drop the candidate, re-evaluate.
- **Package won't fit / unpriceable to route:** `return`/`cancel` before transit; never breach.
- **`view_market`/route lookups unavailable:** freight gate skips for the pass (fail-open, same
  discipline as the mission availability gate).
- **Server restart / disconnect:** the standing loop already skips passes while disconnected
  (`c71ffed`); on reconnect, reconcile active contracts before taking new work.

## Open questions — resolved by a one-agent `play_as` smoke BEFORE any fleet rollout

1. **Package cargo footprint** — not present in `ShipmentContract` (only opaque `package_id`).
   Smoke: `accept` a small real NPC contract, observe the package in storage, `withdraw`, read
   the cargo delta. Determines whether/where a size pre-check is even possible, and whether the
   fit-fail escape hatch is reachable pre-transit.
2. **`deliver` mechanic** — does `shipping deliver` consume the package from cargo, or require a
   deposit into destination storage first? Verify against the live server.
3. **`return`/`cancel` semantics** — which is the correct pre-transit release, and does it avoid
   freight-debt when done before pickup? (If neither is debt-free pre-transit, the accept gate
   must be stricter — never accept a contract whose package we cannot confirm will fit.)
4. **`accept` carrier field** — confirm `carrier: "player"` vs an actor object; confirm the
   package really lands in *personal* storage at `origin_base_id`.

The smoke is a hard gate: no fleet exposure until 1–4 are answered and the happy path
(list→accept→withdraw→route→deliver→payout) completes once by hand.

## Telemetry

Record a freight result per attempt (contract id, base_reward, carrier_payout, route fuel,
net, outcome ∈ {delivered, returned, canceled, breached}, standing tier before/after) to the
same store the mission/haul results use, so we can watch **breach-rate** (must be ~0) and **net
per freight run** as the canary metrics, and compare freight net vs mission net in the pool.

## Testing

- **`pkg/game`:** struct-decode tests against the openapi example shapes for each response
  (list, contract, settlement, profile, track) — verify field names decode (no silent zero
  values from a misspelled tag).
- **`pkg/worker` freight gate (unit, table-driven):** deadline-slack accept/reject boundary
  (route_ticks×1.5 vs ticks_to_deadline); net-floor reject; debt-block skip; ineligible-listing
  skip; co-equal selection freight-vs-mission (freight wins / mission wins / both below floor →
  exploration).
- **Live smoke (`play_as`, one agent):** the Open-Questions gate above — one real
  list→accept→withdraw→route→deliver cycle, plus a return/cancel of a second contract to verify
  the escape hatch.

## Rollout

1. Land Sub-project A (client + structs + tests) on `feat/shipping-carrier`.
2. Land Sub-project B (gate + carrier trip + reconciliation + telemetry) with unit tests.
3. **`play_as` smoke** resolves the Open Questions and proves the happy path + escape hatch.
4. Canary: enable freight on **one** mission-runner; watch breach-rate + net for a few hours.
5. Enable across the mission-learn pool.
6. Phase 2: extend the carrier behavior to haulers (reuse Sub-project A).
7. Phase 3: shipper side (`quote`/`post` surplus).
