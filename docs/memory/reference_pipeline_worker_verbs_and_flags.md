---
name: reference_pipeline_worker_verbs_and_flags
description: "SHIPPED 2026-10-24 (plan 1 of the contraband pipeline, merged d761593d): mine_qty_at + pipeline_miner role (--pipeline), Lawless gate, whole-hold hand-off, shopkeep (--shopkeep, posts/reprices asks), faction_pull (--faction-pull), stall factor for *_miner, secondment away-fleet filter. Fleet yaml fields pipeline / shopkeep / faction_pull."
metadata:
  type: reference
---

Merged to main 2026-10-24 (14 commits, d83cfaa2..d761593d), built by subagent-driven development from
docs/superpowers/plans/2026-10-24-contraband-pipeline-worker-fleet.md (spec: docs/superpowers/specs/2026-10-24-contraband-production-pipeline-design.md).

**Verbs (pkg/worker, in `supported`):**
- `mine_qty_at ITEM QTY SYSTEM POI TO RECIPIENT` — pinned-POI mining. Refuses: fuel below `MineQtyMinFuelReserve`;
  a stronghold for a locked bot; a **Lawless** system (KB `security_status` contains "lawless") for a bot whose
  pirate baseline < 10 (`pirateUnlocked` = `assets.HoldsPirateUnlock`; no asset store → locked). Empty security
  status fails OPEN with a logged warning (plan departs from spec D2's "or police_level 0" — ruled 10-24).
  Hand-off at TO: the FULL current line of the target item always, plus every other line that grew on this trip;
  `fuel_cell` and `energy_crystal` are always kept; unrelated unchanged lines stay aboard. A nil return does NOT
  mean the target qty was delivered (byproduct-only passes return nil).
- `pipeline_mine` (no args) — reads `--pipeline item,system,poi,to,recipient` and calls mine_qty_at with qty = free hold.
- Role `pipeline_miner` (roles.yaml): idle `pipeline_mine` (= `refuel` then `pipeline_mine`), `idle_ticks: 2`,
  hourly capture_profile/capture_fuel/capture_storage, daily faction/tax/citizenship. Fleet yaml:
  `- { agent_id: X, role: pipeline_miner, station: ..., pipeline: "water_ice,glenhaven,glenhaven_ice_belt,treasure_cache_trading_post,trader-1" }`.
- Supervisor: `StallTimeoutFor` applies `MinerStallFactor` to `miner` and any `*_miner` role; `ReconcileSecondments`
  skips ledger entries whose `away_fleet` is set and differs from the sweep's away fleet (empty = legacy, processed).

**Idle-pass steps (StandingDeps, order: Handoffs → FactionPull → Consign → ConsignOffer → Shopkeep, under ExecMu):**
- `faction_pull: [items]` → `--faction-pull a,b`: per allow-listed item in CRFT faction storage, withdraw
  min(qty, free hold) → cargo → deposit to personal storage; refuses `energy_crystal`; dedupes ids; no reading → skip.
- `shopkeep: ["item:start[:floor]"]` → `--shopkeep`: state in `data/agents/<id>/shopkeep.json`; own asks read via
  **`view_orders`** (raw key "orders"; view_market has no order ids); posts min(held, Lot=50) at the price; stale
  (>6 h) ask → ModifyOrder down 5 % to floor; ask gone → treat as fill, price up 5 %; floor ≥ 1 enforced, default
  max(1, start/2); every price clamped ≥ 1; refuses `energy_crystal`; worker REFUSES TO START if an item is both
  shopkept and in `consign`/`consign_offer`. A `has_more` orders page → skip (never double-posts).
Known deferred minors (final review): prices < 20 never step up; a hand-posted ask of the same item is adopted;
state keyed by item only; ViewStorage+ViewOrders+Save every idle tick (watch command budget); free hold in units
(size-1 assumption); FactionPull has no docked check (logs a skip every pass when undocked).
**Post-merge smoke (plan Task 9) in progress 10-24:** bins rebuilt; craftsman-10 line staged with faction_pull +
shopkeep ("pirate_moonshine:5500:4000"); pipeline bot smoke pending. Plans 2 (rollout) and 3 (dashboard) not written.
