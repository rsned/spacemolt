---
name: project_shipping_carrier
description: Shipping/freight carrier feature — A+B+C ALL MERGED; Sub-project C (multi-package trips) merged to main `fcc1fda` 2026-07-22 (NOT pushed), fleet restarted on it; 12-agent soak (craftsman-1/fighter-4/engineers) with per-hold caps.
metadata: 
  node_type: memory
  type: project
  originSessionId: 853398d3-bb3c-4431-bf12-2caa11653a5d
  modified: 2026-07-23T02:09:36.373Z
---

Freight-carrier feature for idle mission-runners (then haulers). `/shipping` = one action-dispatched endpoint (11 actions), NOT a mission category. Spec/plan: `docs/superpowers/{specs,plans}/2026-07-19-shipping-carrier*`; SDD ledger `.superpowers/sdd/progress.md`. Related: [[project_idle_agent_income_paths]] [[project_mission_learning_pool]] [[project_kind_discriminator_drift]] [[reference_craft_action_result_wrapping]].

## Sub-project A — MERGED to main `947e44a` (2026-07-19, FF; branch deleted, NOT pushed)
Pure **dormant** pkg/game client foundation — nothing calls it yet. 6 code commits, every SDD task reviewed clean, whole-branch final review [opus]=MERGE (14 struct types' JSON tags set-diff'd vs openapi = 0 drift). Delivered: `serverapi/responses_shipping.go` (18 types), `storeRawJSON` caches `shipping_<action>` (11 actions), 10 GameClient methods (Shipping+9 wrappers)+interface+mocks (agent/skills/MCP), `BuiltForAPIVersion`→v0.531.4, Shipping() copies caller's map (no mutate). Stopgap: parked get_battle_summary/get_battle_log/inspect in ignoredCommands (pre-existing v1 drift; owed passthrough coverage).
- **openapi.json symlink is STALE** (→openapi.20260714.json = v0.501.0, NO shipping). Real spec = `server_docs/openapi.20260719.json` v0.531.4. Repoint/full server_docs sync = follow-up (guarded by TestLoadFromOpenAPIContainsAllHardcoded).

## Task 5 live smoke (craftsman-1 self-ship, v0.531.4) — RESULTS
play_as syntax = `shipping --action=<verb> --param=value` (positional→arg1, server rejects). Full details in spec "Live smoke RESULTS" section. Key findings:
- **Q1 (footprint) = FLAT 100.** Sealed pkg cargo `size`=100 regardless of contents (10 iron_ore size-1 → still 100) = container's 100-item capacity reserved whole. ⇒ **B fit-check = constant "≥100 free cargo units"**, not per-contract.
- **Q4 = confirmed.** accept moves pkg to `player_storage@origin` (beacon fingerprint); self-ship contract `eligible:true` at probationary (tier-gate bypass). `carrier=player` correct.
- **Deadline set AT ACCEPT, not post.** Posted listing has NO `deadline_tick`/`target_tick`; on accept (standard, route_hops 3): accepted_tick → target +90 → deadline +180, status→in_transit. ⇒ **B cannot gate deadline pre-accept** — estimate from route_hops+service_level, or accept-then-`return`. (Revises the spec's old `route_ticks×1.5 ≤ deadline_tick−now` gate.)
- **CRITICAL for B: shipping MUTATIONS (accept/deliver/return/cancel/post/pay_debt) are tick-deferred + `action_result`-wrapped** `{command:"shipping", result:{action, contract}}` — NO top-level `action`. Our `storeRawJSON` (keys on top-level action) + `Shipping()`'s `WithAckOnly` await BOTH MISS them. B must unwrap the action_result (mirror pkg/worker craft fix [[reference_craft_action_result_wrapping]]: `WithTerminator(terminateOnActionOrOK)` + a storeRawJSON action_result path, read `result.contract`). READS (list/get/profile/track) = synchronous top-level-action TypeOK, decode fine (confirmed live).
- **Q2 (deliver) = confirmed:** deliver at dest → status `delivered`/`delivered_intact`, `carrier_payout:100`, sealed pkg deposited to recipient storage@dest (consumed from cargo). 3-hop trip = 56 ticks (~19/hop). **Q3 (return) = confirmed debt-free:** status `returned`/`returned_intact`, `shipper_refund:100`, no debt, `returns`++ , liability released, `outstanding_debt` stays 0. Settlement shape: `carrier_payout` on deliver, `shipper_refund` on return. SMOKE COMPLETE.
- Package creation: `craft pack_package` (items array, needs cargo_container + Logistics; escrows from STATION storage). play_as has no pack handler → use `craft --file <json>` (bulk accepts it). package_id = bare hash in contract; storage/cargo item id carries `package:` prefix.
- Carrier tiers: fresh=probationary (liability ≤5000 single/≤10000 agg); →licensed = 5 deliveries + 250 delivered_value. Ineligible board = empty + `no_eligible_shipments`. Self-ship bypasses gate but earns NO delivery/value/tier credit.
- `cancel` = shipper-side, only while posted (pre-accept); `return` = carrier's post-accept escape.

## MUST-FIX before Sub-project B fleet rollout
client_api_monitor spams `[SERVER API CHANGE]` on every shipping call (bare-action keying unaware of `shipping_` namespace; `list` collides with facility). Ties to [[project_kind_discriminator_drift]].

## Sub-project B — MERGED to main `a602afc` (2026-07-20; branch deleted, NOT pushed)
18 commits, 8 SDD tasks + 3 fix waves, 4 review rounds. Freight is **dormant**: `MissionDeps.EnableFreight` defaults false and inertness was verified four independent ways (incl. against the *deleted* code). Shape: `pkg/worker/freight.go` co-equal with the mission board; reconcile runs before EVERY early return; a single three-way ranking (freight / mission trip / exploration) with freight taking ties; `freightStep` three-state enum (Proceed / Released / **Stuck**=abort the pass). `pkg/market` `freight_results` table (schema.sql only — new tables need NO numbered migration; the trap is columns on EXISTING tables).

**The bugs that only existed at integration seams** (per-task review passed each task individually):
- `freightRunTrip` re-withdrew on reconcile-resume, **destroying** the healthy contract the nav-failure path deliberately preserved. Unreachable until Task 8 added the 2nd call site.
- **Exploration preempted freight** — the ranking compared exploration against the mission trip only, so a 12k freight contract lost to a 600 exploration tour. Inverted the spec.
- Freight was unreachable on BOTH dry paths, incl. one that computed a candidate then discarded it.
- **THREE separate tests passed green while asserting nothing** (`System.ID==""` → `Missions()` early-returns before any freight code). Lesson: on this codebase, prove a test discriminates by neutering its target and observing red. Two tests are now proven that way.

## Canary gate — FIXED 2026-07-20 (`2ff2e7e`, pushed)
The original "any `breached` row" stop-condition was DEAD (nothing client-side writes that slug — breach is server-side, unobservable here). Both docs corrected; the plan's **Rollout section is the maintained runbook** (task bodies deliberately NOT retro-edited; a top-of-plan note covers them). Corrected gate: (1) `outcome IN ('breached','return_failed')`; (2) known contract_id with NO terminal row = presumed breach; (3) server ground truth via `shipping --action=profile` (`outstanding_debt`/`breaches`). Runbook also now includes: telemetry-liveness query FIRST (RecordFreightResult errors are swallowed → missing table invisible), log deliver's ack frame once (closes the pending:true inference), and the two known false-alarm artifacts (dock-ordering `returned_infeasible`; total-vs-remaining `RouteHops` on `returned_inflight`).

## Canary ROUND 1 (2026-07-20, fighter-4) — ANSWERED the unknowns, hit a default
One full contract cycle (procyon_colonial_station → nova_terra_central, 3 precision_optic). ANSWERS:
1. **Board read NEVER lists own in_transit contracts — reconcile premise FALSE.** Worker flew away with the package (mission logic took over); the I1 `ActiveContracts` guard contained damage every pass.
2. **Dock race REAL:** deliver issued 14s before the tick-deferred dock resolved → `not_docked`-class error. And **/shipping does NOT auto-dock** — the same-day server auto-dock patch covers craft/buy/storage, but `pay_debt` returned not_docked while undocked, live.
3. **Deadline windows are ~tens of MINUTES, not hours.** Missed deadline → status **`defaulted`** (distinct from breached; profile `breaches` stays 0), flat `failure_debt: 500`, `debt_blocks_acceptance: true`. Late deliver → `shipment_not_deliverable`; return → `shipment_not_returnable`. `track --shipment_id` returns the full custody trail.
4. **Defaulted package is KEEPABLE:** deposit to storage, then `craft --file` recipe `unpack_package` recovers contents + the cargo_container (fighter-4 recovered 3 precision_optic + container; they sit in its storage @ nova_terra_central). Operator paid the 500 debt (user-approved); carrier unblocked.

## Round-1 fixes — PUSHED `bbcca9d` (2026-07-20)
- **Dock settle:** `freightSettleDock` polls `IsDocked()` (SleepQuick cadence, 3-tick budget, one explicit Dock nudge after a full tick) before deliver; failure = leave in flight.
- **Memory-first reconcile:** `missionRunState.heldFreight` (set at accept, kept on failed return, cleared on terminal); `freightReconcileHeld` verifies via synchronous `get`, fail-open on transient get failure; server `defaulted` records outcome `breached` (the round-1 default produced NO freight_results row — closed). Profile+board path = post-restart fallback only, logs UNRECOVERABLE loudly (restart with in-flight contract still needs operator play_as rescue until captains_log resume exists).
- **Monitor:** `auto_docked` added to `action_result` expected fields (new server patch flag).
- All three fixes proven by neuter-and-observe-red. Fleet relaunched once on the fixed binary, fighter-4 still sole `enable_freight: true`.

## Canary ROUND 2 — FIRST CYCLE GREEN (2026-07-20 15:44)
Contract `4753adb…` 1-hop to nova_terra_central: accepted → withdrawn → jumped → dock SETTLED (Fix A visibly held deliver 13s until dock resolved) → **delivered, payout 1402** (fuel 40; est net 1362 ≈ real). freight_results row present w/ decoded settlement (closes unknown #4); stop-query clean; ZERO monitor spam since relaunch. New: **carrier tier gating visible** — boards skip "trusted/licensed cargo requires … standing"; fighter-4 = 1/5 deliveries toward licensed (licensed also needs 250 delivered_value). 100-reward NPC contracts reject at net 20 vs the 500 floor (floor working, board mostly junk at probationary tier).

## Sub-project C — MERGED to main `fcc1fda` (2026-07-22 FF from 52e23b8, branch deleted, NOT pushed) + FLEET LIVE
Multi-package freight trips: held-SET of contracts (`missionRunState.heldFreight` map) + chained dock pass (`freightChainRun` in pkg/worker/freight.go: deliver due → refill headroom → nav nearest held stop; chain emerges from repeating the pass; 25-leg guard). `pkg/worker/freight_chain.go` = pure chain math (nearest-first order, round-trip-through-origin cumulative bound, feasibility = deadline ≥ cum×19.0×1.5, marginal hops pricing). Concurrency = min(floor(cargoFree/100), `--freight-max-packages` cap [WorkerSpec `freight_max_packages`], server active_contract_limit, liability headroom). Spec/plan `docs/superpowers/{specs,plans}/2026-07-22-shipping-carrier-subproject-c*`; SDD ledger has full history (7 tasks, 2 task-level Criticals both plan-code-vs-constraint conflicts, final fable review found 3 more: **C1 cap-1 refill broke v1 equivalence** — refill now gated to cap>1; I1 eager fuel-model on disabled dry path → lazy builder; I2 unbounded refill on load-release → one accept/return cycle per dock max).
- **Rollout 2026-07-22 (operator-directed, supersedes fighter-4-only canary): 12-agent soak with per-hold caps** — craftsman-1=11 (1100 hold), fighter-4=7 (790), engineers 1-10 = 6 each (re-shipped into larger hulls). Caps are CEILINGS: server tier contract limit auto-throttles probationary/licensed agents, so they grow into caps as tiers advance ("larger opportunities as they advance in status" = automatic, no yaml edits needed).
- Fleet restarted on new binaries 2026-07-22 ~19:05: drained 32/32, relaunched 42 workers (all engineers back post cargo-clear; engineer-2/3 too), --stagger 10s.
- Live unknowns the soak should answer: does the server refresh RouteHops on in_transit; does a returned contract re-list on the board.
- Soak watch: `freight: holding N/M packages` log lines; `sqlite3 data/market.db "SELECT outcome,COUNT(*) FROM freight_results WHERE ts > ... GROUP BY outcome"`; stop = `return_failed`/`breached` rows or profile outstanding_debt. `returned_inflight` rows = bound conservatism, cheap, tuning data not defect.

## Next — RESUME HERE
Soak the 12 (tier progression → concurrency unlock). NEXT PROJECT (user-promoted): [[project_fleet_pool_dynamic_membership]]. Queued: server_docs sync + BuiltForAPIVersion bump post-v0.531.4; auto_docked/auto_undocked struct sweep; sell/keep fighter-4's 3 precision_optic @ nova_terra_central; possible pool expansion candidates if user wants later: random-4 (400 hold, needs unload+credits), random-1/2 (150s, full+broke), random-5 (150 empty), explorer-12 (150).

## Historical (pre-B)
Write Sub-project B (brainstorm→spec→plan→build): freight gate + carrier trip + reconciliation + telemetry, encoding all smoke findings — esp. the `action_result` mutation unwrap (WithTerminator + storeRawJSON action_result path for accept/deliver/return/cancel/post/pay_debt), the constant ≥100-cargo fit-check, deadline-known-only-post-accept (estimate from route_hops or accept-then-return), and `return` as the debt-free escape. The client_api_monitor spam fix is now its OWN standalone task ([[project_kind_discriminator_drift]]), land before B fleet rollout. Then canary one mission-runner.

## Session handoff (2026-07-20)
- **git:** main @ `8bfe8a6` (spec findings committed, docs-only). Shipping Sub-A + spec are on LOCAL main, **NOT pushed**. Verify unpushed backlog: `git log --oneline origin/main..HEAD`.
- **Dirty tracked files on main I did NOT touch:** `server_docs/{api.md,openapi.json,openapi.v2.json,skill.md}` — part of the separate server_docs-sync situation (openapi.json symlink still → v0.501.0). Don't assume these are mine; leave for the server_docs sync.
- **craftsman-1 is under manual `play_as` control** (operator drove the whole smoke). Release it back to normal craft-fleet duty when done (stop the play_as session). Smoke residue: the `smoke_iron_10` package (pkg id `ed9edd4346ed071f3c890ca73f9456b2`) is in craftsman-1's storage @ treasure_cache_trading_post — unpack via `craft --file` with recipe_id `unpack_package` (the plain `craft unpack_package` form drops `--package_id`; must use --file/bulk) to recover 10 iron_ore + the cargo_container, or leave.
- **play_as shipping syntax:** `shipping --action=<verb> --param=value`. pack/unpack packages: `craft --file <json>` (bulk passes the items array / package_id verbatim; the inline `craft <recipe>` form can't).
