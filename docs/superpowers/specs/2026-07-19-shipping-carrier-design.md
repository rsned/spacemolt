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

### Live findings (2026-07-19 — openapi v0.531.4 docs + partial `play_as` smoke on craftsman-1)

The `/shipping` endpoint description (openapi.20260719.json) plus the item catalog **resolve
the mechanics of all four open questions**; the smoke is now a confirmation run, not
exploration. Answers Sub-project B must encode:

- **Q4 (accept / carrier / where the package lands) — RESOLVED.** `carrier=player|faction`
  selects who "permanently owns the consequences"; it is a bare enum, never an arbitrary faction
  id. Acceptance **deposits the sealed package into the carrier's personal (or faction) storage
  at `origin_base_id`, bypassing the ordinary package cap**, so it is withdrawn for hauling
  later. `carrier=player` is the v1 value. LIVE-confirmed: `list` and `profile` responses decode
  cleanly into `ShippingListResponse`/`ShippingProfileResponse` (fields exact, `storeRawJSON`
  keys `shipping_list`/`shipping_profile` as built).
- **Q2 (`deliver` mechanic) — RESOLVED (confirm cargo-consume live).** `deliver` deposits the
  **still-sealed** package directly into destination storage (even over the recipient's package
  cap). The carrier hauls the sealed package in cargo; **opening the seal breaches the contract**.
  So: withdraw → haul sealed → `deliver` at destination. (Live: confirm it consumes the package
  from cargo rather than requiring a manual deposit first.)
- **Q3 (escape hatch / debt) — RESOLVED.** The carrier's release is **`return`** — "always
  available to surrender freight back to the origin station," and it creates **no freight-debt**
  unless the seal is opened or the deadline is missed (breach/default = 500 cr uninsured, or
  covered value +10%, 100-cr min). **`cancel` is shipper-side and applies "only while still
  posted"** (pre-acceptance) — it is NOT the carrier's post-accept escape. ⇒ Sub-project B's
  fit-fail / unwinnable escape hatch = **`return`**, debt-free as long as it precedes a breach.
- **Q1 (package cargo footprint) — the ONE genuine live unknown remaining.** Packages are built
  by `craft pack_package` (consumes one `cargo_container`, packs ≤100 total item-size, requires
  Logistics). A `cargo_container` is catalog size 4. `ShipmentContract` exposes no footprint
  (only opaque `package_id`), so the accept→`withdraw`→(cargo delta) observation is still needed
  to learn whether a withdrawn sealed package occupies just the size-4 container or scales with
  packed contents. If it is a constant (one container), B's fit-check is a constant, not a
  per-contract calc.

**Carrier tier gate (live, craftsman-1 profile):** fresh carriers start `probationary`
(single-package liability ≤5000, aggregate ≤10000); `probationary → licensed` needs **5
successful deliveries AND 250 delivered_value**. A docked `list` where every posting exceeds the
carrier's standing returns **empty + `empty_reason_code=no_eligible_shipments`** (it does not
list ineligible runs). ⇒ B's gate needs a "no eligible freight at this tier/station" skip, and
bootstrapping standing requires low-value public contracts (or self-ship, which bypasses the
tier gate but earns no progress).

**Smoke bootstrap:** self-shipping (post a `pack_package` to a different station, `accept
--carrier=player`) "bypasses standing and tier liability-limit gates" (unpaid debt still blocks;
no delivery/value/tier credit) — the deterministic way to exercise accept→withdraw→deliver→return
without depending on the NPC freight market.

### Live smoke RESULTS (craftsman-1 self-ship, 2026-07-19, server v0.531.4)

Ran the FULL cycle live: pack_package(10 iron_ore) → post → list → accept → withdraw →
travel(3 hops) → **deliver** → re-post → accept → **return**. All four open questions + the
implementation finding confirmed on the wire. Concrete findings:

- **Q2 (deliver) — RESOLVED.** `deliver` while docked at destination: `status`→`delivered`,
  `terminal_reason:"delivered_intact"`, `carrier_payout:100` (top-level on
  `ShippingSettlementResponse` AND on the contract), `reward_escrow`→0, beacon →
  `player_storage:<dest>:player:<id>` — the sealed package is deposited into the recipient's
  storage at the destination (consumes it from cargo). Trip timing: 3 hops took **56 ticks**
  (~19/hop) inside the 180-tick window — a data point for B's pre-accept route-time estimate.
- **Q3 (return) — RESOLVED = debt-free.** After a fresh accept, `return` (pre-transit):
  `status`→`returned`, `terminal_reason:"returned_intact"`, **`shipper_refund:100`** (full reward
  escrow refunded), **no `debt_created`**, `outstanding_debt` stays 0 — the package returns intact
  to `origin_base_id` storage. ⇒ B's escape hatch = `return`, costs only the already-spent
  `service_fee`. (`ShippingSettlementResponse`: `carrier_payout` on deliver, `shipper_refund` on
  return.)
- **Self-ship earns NO tier credit (confirmed):** after the Q2 delivery, `profile` still showed
  `successful_deliveries:0` / `delivered_value:0`. Bootstrapping a real carrier to `licensed`
  requires genuine third-party contracts. `active_liability` == the accepted contract's
  `appraised_value` (40), drawn against the 10000 aggregate cap — that's what B's capacity check
  consumes.
- **Q1 (footprint) — RESOLVED = FLAT 100.** A sealed package's cargo `size` is **100**
  regardless of contents (10 iron_ore, each catalog size 1 = 10 units of goods, still showed
  `size:100` / `used:100` in `get_cargo`). It equals the container's 100-item packing capacity,
  reserved whole — NOT contents-summed, NOT the empty-container size (4). ⇒ **Sub-project B's fit
  pre-check is the constant "≥100 free cargo units,"** not a per-contract calc. (`pack_package`
  caps packed goods at 100 total item-size, so 100 is the ceiling for the current container.)
- **Q4 — RESOLVED live.** On accept the beacon fingerprint moved from
  `shipping_house_escrow:…` to `player_storage:grand_exchange_station:player:<id>` — package
  lands in the carrier's **personal storage at `origin_base_id`**. `withdraw
  package:<pkg_id> 1` moves it into cargo. `contractor` becomes the carrier; self-ship sets
  `reputation_eligible:false`. The `list` contract came back `eligible:true` at `probationary`
  standing — self-ship tier-gate bypass confirmed.
- **Deadline is set AT ACCEPT, not at post.** The `posted` listing has NO `deadline_tick` /
  `target_tick`. On accept (standard service, route_hops 3): `accepted_tick T` →
  `target_tick T+90` → `deadline_tick T+180`; `status` → `in_transit` immediately. ⇒ **the
  deadline-slack gate cannot run pre-accept.** B must estimate the window from
  `route_hops`+`service_level` (one live point: standard 3-hop = 180-tick window / 90-tick
  on-time target — unknown yet whether the window scales with hops or is fixed per level), OR
  accept-then-verify-and-`return`. **Revise the spec's earlier `route_ticks×1.5 ≤ deadline_tick−now`
  gate accordingly.**
- **CRITICAL for B — shipping MUTATIONS are tick-deferred and `action_result`-wrapped.**
  `accept` (and by symmetry `deliver`/`return`/`cancel`/`post`/`pay_debt`) first returns a bare
  `TypeOK` `{command, message, pending:true}` ack, then on the NEXT tick a separate
  `action_result` message shaped `{command:"shipping", result:{action:"accept", contract:{…}}}`
  — **no top-level `action`**. Consequences: (a) our `storeRawJSON` case keys on the top-level
  `action` and will NOT capture the mutation result under `shipping_<action>`; (b)
  `Client.Shipping`'s `WithAckOnly` await returns on the `pending` ack, not the contract. **B must
  handle the `action_result` unwrap** (mirror the pkg/worker craft fix — terminate on the
  action_result, read `result.contract`), and the mutation client methods likely need
  `WithTerminator(terminateOnActionOrOK)` rather than `WithAckOnly`, plus a `storeRawJSON`
  action_result path. READS (`list`/`get`/`profile`/`track`) are synchronous top-level-`action`
  `TypeOK` and decode fine as-is (confirmed live). This is the same class as
  the craft `action_result` gotcha (docs/… reference_craft_action_result_wrapping).
- **Struct fidelity confirmed on the wire:** full `ShipmentContract` (44 fields incl.
  `appraised_value`, `reward_escrow`, `service_fee`=25 floor, `failure_debt`=500 uninsured,
  `route_hops`, `risk_band`, `{kind,id}` actors), `ShippingListResponse`,
  `ShippingProfileResponse`, and `get_cargo` all decoded with no drift. `package_id` is the
  BARE hash in the contract; the storage/cargo item id carries a `package:` prefix.

### Follow-up discovered — MUST-FIX before Sub-project B fleet rollout

The client API-drift monitor (`pkg/game/client_api_monitor.go`) keys on the bare `action` value
and is unaware of shipping's namespaced `shipping_<action>` `GetRawJSON` scheme, so it emits a
spurious `[SERVER API CHANGE]` on **every** shipping response (`action:"list"` collides with the
`facility` list; `action:"profile"` is "unhandled"). Harmless in `play_as`, but it would spam
fleet logs once B runs. Register the shipping actions (or teach the monitor the `shipping_`
namespace) before rollout. Tracks the same area as the broader "new `kind` field in generic
response shapes" drift the monitor is flagging.

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
