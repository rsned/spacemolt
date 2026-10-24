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

**Smoke findings 10-24 (miningbot_0000, Cobble):** module ids are `ice_harvester_ii` / `gas_harvester_iii` (not
`ice_harvester`). **A Cobble cannot fit the harvester alongside its current fit:** `install ice_harvester_ii` →
`cpu_exceeded: Need 6, have 1` with pulse_laser_i + cargo_expander_iii + mining_laser_iii aboard (utility 2,
weapon 1, defense 1). The rollout plan must decide per group: drop the pulse laser and/or the mining laser for the
harvester (ice/gas groups) or keep the laser (titanium). The harvester now sits in the bot's CARGO (size 10 — eats
hold until moved back to storage). Smoke fleet: `data/overmind/crft-mining-fleet.yaml` (one bot, Altais outer
ice → Frontier Station → trader-8), overmind launched with the standard flags + `--handoff-queue` + `--fleet-name crft-mining`.
**Server rule (live 10-24): "You need an ice harvester module to mine ice here."** Ice fields refuse the mining laser.
Cobble budget: CPU 12 / power 24, utility 2, weapon 1. Stock fit pulse_laser_i (2/5) + cargo_expander_iii (3/3)
+ mining_laser_iii (6/12) = 11/20. Per-group fits for the rollout plan: **ice** = drop mining_laser_iii, fit
ice_harvester_ii (6/12) → 11/20 OK; **gas** = gas_harvester_iii is 9/15 → must drop BOTH the mining laser AND the
pulse laser (3+9 = 12 CPU, 3+15 = 18 power); **titanium** = keep the stock fit. Module ids: `ice_harvester_ii`,
`gas_harvester_iii`; play_as `install <item-id>` / `uninstall <module-id>` (docked).
**⭐🔴 New yaml fields need the overmind RESTARTED, not SIGHUPed.** The running overminds (all launched 10-23 15:07)
are the pre-merge binary: on SIGHUP they parse `pipeline:`/`shopkeep:`/`faction_pull:` as unknown fields, see a
"spec change", and relaunch the worker WITHOUT the new flags (craftsman-10, 10-24 13:45 — no error anywhere). Fix:
SIGTERM the overmind and relaunch it with its exact `ps` args (craft restarted 14:07 on the new bin/overmind; its
2 workers relogged). The other 9 overminds still need this before any pipeline/shopkeep/faction_pull yaml line.
Smoke result 14:03: miningbot_0000 mined to full (117 water_ice of 175), flew back, gifted both cargo lines to
trader-8 at mobile_capital (= Frontier Station's POI id) in two per-line deliveries, no errors.
**⭐ faction_withdraw_items is TICK-DEFERRED (live 10-24 14:10):** the call returns on the ack; the goods land in
cargo on the next tick, and the client cargo cache updates only on `GetCargo`. The first faction_pull deposited
immediately → `item not found in cargo` every pass while each pass withdrew another 6 (stranding them in cargo).
Fixed (commit "faction_pull waits a tick and deposits what landed in cargo"): settle(SleepTick) → GetCargo →
deposit `cargoCount` (drains stranded units too). Same pattern as Deliver's withdraw refresh (deliver.go:244-251).
Any new verb that chains a mutation into a read of ship state needs the same settle+refresh.
