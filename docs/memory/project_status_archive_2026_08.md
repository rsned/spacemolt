---
name: project_status_archive_2026_08
description: "Archived Current-Status entries from MEMORY.md, August 2026 — full text, rolled off the index to keep it small"
metadata: 
  node_type: memory
  type: project
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-24T22:35:02.540Z
---

Full text of status entries rolled off `MEMORY.md`'s Current Status section.
Newest first. The index keeps a one-line hook; the detail lives here.

## 2026-08-08 — ✅ Asset ledger is LIVE

89/110 workers capture on schedule; 86 agents in `data/assets.db`. Five fleets
rebuilt+restarted (mb/assist/craft/mission-learn/shuttle); **haul deliberately
NOT rolled** — still on `f85a4ca`. All 10 roles now schedule the three
captures. `capture_storage`/`capture_faction` are daily with a **00:00 UTC**
boundary, so the first fleet-wide storage+faction pull lands 2026-08-09T00:00Z
— expect `agent_storage` ~86 and `faction_storage` **4** (CRFT, DB, YSMT,
XPLR); grep for `unparseable faction storage hint` after, since only CRFT's
wording is verified. **craftsman-1 is NOT on a worker** (craft fleet is
craftsman-2..10) so it shows permanently stale in the coverage panel.
[[project_pending_rollout_queue]] · [[project_agent_capability_ledger]]

## 2026-08-06 — 🟢 Ledger slices 5-6 built + fully reviewed

Storage, faction, flap fix, ovdash panel: 20 commits on
`feat/agent-capability-ledger`, final review 0 Critical / no blockers,
UNMERGED + UNPUSHED + inert. Live canary settled the wire questions: **the
storage hint is AGENT-GLOBAL** (one call from anywhere lists every base), does
**not** truncate, and `"No items in storage at any station."` is a sentinel the
spec's own parser turned into a base named `any station.`. **Review found a
real data-loss bug by building and running the scenario** — the agent's own
docked base was deleted when the hint omitted it. **Six known limitations are
committed into the spec (`d667e43`) — read them before trusting
`faction_storage.fuel_reserve` or `agent_storage.credits`, which are only valid
for the captor's own dock.** [[project_agent_capability_ledger_storage_faction]]

## 2026-08-05 — ✅ Full cold start #2 clean

110/110 workers, restarts=0, all on `f85a4ca`. Host hard-stopped 2026-08-02
09:39 (all logs end same second). Ran `docs/COLD_START.md` verbatim; binaries
rebuilt from HEAD; 10.3M stale `market_orders` rows cleared via the
batched-delete loop DURING mb spin-up (zero update_market failures); scanner
pool came back at 463 available (3 days of drift = fat pool); hauler-0 claimed
an opp seconds after connect. **New trap found + added to the runbook: stale
pre-outage `*-status.json` files make spawn-count waiters pass instantly —
check `overmind_commit` first** (doc edit UNCOMMITTED). arbitrage-scanner
source is `cmd/arbitrage-scanner`, NOT cmd/tools/. mb log shows
`[SERVER API CHANGE] help response: new field` — minor drift, unhandled.

## 2026-08-01 — 🔴✅ Mission payouts collapsed to ~37%

The empire treasury is broke (dev-confirmed, not a bug). XP still paid in
FULL, credits are not — 427 completions, 964,366cr short, all mission types.
Realized-ratio gating SHIPPED (`3d66909`); smuggling below L3 exempt. **The
XP-witness technique and `get_action_log` are how it was cracked — reuse
them.** [[project_empire_treasury_payout_collapse]]

## 2026-07-31 — ✅ Full cold start from a 6h outage, then a market.db rebuild

110 workers, 0 restarts. `docs/COLD_START.md` (`dfc4870`) is the runbook.
market.db **18.56GB → 2.04GB**, 47GB WAL gone (it only needed a clean close —
`wal_checkpoint(TRUNCATE)` in 4ms), 116GB of stale backups deleted; disk
**170G → 342G free**. Order matters: delete-then-vacuum gave 2.04GB,
vacuum-then-delete only 12.7GB. **Restarting `market-prune` at `--retain 4h`
after a >4h outage = an unbatched whole-table DELETE that starves the fleet;
batch it.** [[reference_market_db_prune]] · [[reference_overmind_launch_commands]]

## 2026-07-31 — ✅ Mission resume dropped the reward

`cf7243b`: resumed missions recorded `expected_reward=0`, so the gate scored
them as pure losses and the shortfall diagnostic could never fire. Also
shipped: shortfall logging that echoes the server's `complete_mission` body
verbatim (`bfdd3da`, cap 1024→4096 after it truncated the useful tail).
[[project_empire_treasury_payout_collapse]]

## 2026-07-30 — ✅ Four rescue defects fixed and deployed

`00d3f62`; assist fleet 5/5 + overmind on it, drained 5/5 in 10s, restarts=0.
failed→re-queue with FailedBy/Attempts + terminal alert; takeover ladder
measures `PendingSince` not `RequestedAt`; `TransferQuantity` takes the
measured per-jump rate (**`rescue.FuelPerJump=5` is no longer live except as a
fallback** — but `fuelForHops`/`FuelForSystem` still use it at enqueue time);
`RefuelAndSync` re-reads state after a refuel. [[project_rescue_pipeline_bugs]]
· [[reference_ship_jump_time_and_fuel_formulas]]

## Rolled off the status board 2026-08-13

- **2026-08-06 — Ledger slices 5-6 reviewed, UNMERGED/UNPUSHED/inert** on
  `feat/agent-capability-ledger`; six known limitations are in the spec
  (`d667e43`) — read them before trusting `faction_storage.fuel_reserve` or
  `agent_storage.credits`. [[project_agent_capability_ledger_storage_faction]]
- **✅ 2026-08-01 — MISSION PAYOUTS ~37%: the empire treasury is broke**
  (dev-confirmed, not a bug). XP full, credits short. Gating SHIPPED `3d66909`.
  **XP-witness + `get_action_log` cracked it — reuse both techniques.**
  [[project_empire_treasury_payout_collapse]]
- **✅ 2026-08-12 — PIRATE UNLOCK deployed, wave 1 banked, all 9 stronghold
  marketbots pinned** (`d56bbef9`). 7→11 unlocked. **The strand fear is a HULL
  property** — scale-1 starters burn 1-2 fuel/jump, so a 27-jump pin lands with
  46 spare. Superseded 08-13 when all nine graduated to mb-fleet (`46122c8a`).
  [[project_pirate_reputation_unlock_campaign]]

## Rolled from Current Status 2026-08-17
- **⭐🟢 2026-08-15 20:45 — HAUL FLEET rolled to `2fea237a`** (sell-leg dock fix): 16/21 healthy, restarts=0, deploy-verified. Five held out are pre-existing rescue records, not roll casualties. Other six fleets remain on `eb02ac91`. [[reference_sell_leg_dock_gap]] · [[project_pending_rollout_queue]]
- **⭐🟢✅ 2026-08-16 — FUEL GATE SHIPPED `4b84ac1b` + rolled to all 7 fleets**: autopilot now REFUSES a route it cannot finish (`ErrInsufficientRouteFuel`) instead of warning and flying; haul no longer resumes EXPIRED claims (which flew released agents straight back to the stronghold that stranded them). This one bug stranded 11+ agents across 4 fleets in a night. [[project_haul_departs_without_enough_fuel]]
- **⭐🟢 2026-08-08 — ASSET LEDGER IS LIVE**: 89/110 workers capturing, 86 agents in `data/assets.db`; **haul NOT rolled**; craftsman-1 permanently stale (not on a worker). [[project_agent_capability_ledger]] · [[project_pending_rollout_queue]]
- **⭐🔴 2026-08-08 — THE `pirates` STANDINGS KEY IS RETIRED** (nine `pirate_*` keys). Broke `stronghold_access` + play_as status; **stale fixtures kept both suites green**. [[reference_pirates_standing_key_drift]]
- **⭐🔴 2026-07-31 — A DRAIN IS NOT A DEPLOY.** `SIGUSR1` is time-bounded and aborts if workers stay busy; worker count and `overmind_commit` both LIE. Check process start time vs `bin/worker` mtime. [[reference_deploy_verification]]


## Rolled from MEMORY.md Current Status on 2026-08-23 (superseded by later events)

- **⭐🟢✅ 2026-08-18 01:15 — FULL COLD START after an OOM crash (fleets died 08-17 15:06, down 9.4h): all SEVEN fleets on HEAD `3dea78d7`, 160 workers, ZERO restarts, ZERO rate-limit errors** (order mb54→assist5→hunt5→craft9→unlock25→mission-learn40→haul22, ~40 min). Dashboards :8087/:8091, arbitrage-scanner, fleet-secondment all relaunched. **`capture_action_log` FANNED OUT to all 161 agents + `capture_wildlife_attacks` to the 5 hunters** — 156 agents capturing within minutes. explorer-7 released from quarantine. [[reference_overmind_launch_commands]]
- **⭐🔵 2026-08-17 — ACTION-LOG CAPTURE SHIPPED but only on 2 canaries; the fan-out to 158 agents needs a schedule entry each + a restart. Read [[project_action_log_capture]] before resuming.**
- **⭐🟢✅ 2026-08-17 — FLEET ALL-GREEN: 160/160 workers healthy, RESCUE QUEUE EMPTY** (first time in days). Fuel gate `4b84ac1b` rolled to all 7 fleets 10:05; every one of 14 queued 'stranded' agents had already been GSA-towed to a station — the queue was stale coordinates. craftsman-1 now flies in the HAUL fleet (22 workers). [[project_haul_departs_without_enough_fuel]] · [[reference_gsa_ship_recovery]]
- **⭐🟢✅ 2026-08-14 — FULL COLD START after workstation crash (12:02 PDT): all SEVEN fleets (144 workers) on HEAD `2150a282`, restarts=0, deploy-verified.** Haul has asset capture + secondment ledger; all 53 marketbots `update_market=ten_minutely`. shuttle+idle retired; docs/COLD_START.md stale on hunt/unlock/haul flags — see [[reference_overmind_launch_commands]] · [[project_pending_rollout_queue]]
- **⭐🟢✅ 2026-08-13 — THE NINE STRONGHOLD MARKETBOTS GRADUATED to mb-fleet** (`46122c8a`), all `resident`+pinned, 45/45 healthy; alhena went 0/130 → 130/130 on the move. unlock now 25. [[project_pirate_reputation_unlock_campaign]]
- **⭐🔴 2026-08-13 — HAUL SECONDMENT'S FIRST LIVE TRIP ORPHANED TWO AGENTS** (ran in NO fleet): 90s wait < the 4m graceful drain, plus no rollback. Fixed `490ec641`. [[reference_secondment_drain_and_rollback]]
- **⭐🔴 2026-08-13 — "INSUFFICIENT FUEL" CAN MEAN "YOU ARE ALREADY THERE"** — travel is priced before it is measured. Fixed `1df1b9d1`. [[reference_travel_priced_before_measured]] · a refuel error that gets RECORDED must not use the cargo fallback [[reference_refuel_fallback_vs_measurement]]
- **⭐🟢✅ 2026-08-21 — SHUTTLE FLEET IS BACK, fleet set is now EIGHT.** johnny_cab graduated the unlock (baseline 10 × all nine pirate factions, smuggling 15) and returned to shuttle-fleet.yaml; unlock is 24, 160 workers total. **overmind-status `defaultSources()` and fleet-watch's watch set BOTH needed a restart to see the new fleet** — neither discovers fleets at runtime. [[reference_overmind_launch_commands]] · [[project_pirate_reputation_unlock_campaign]]
- **⭐🟢✅ 2026-08-23 — MINER STALL FIX + ROSTER DEDUP SHIPPED** (`ad3f4488`, `7a01a1c1`): `statusProgressed` counts system/POI/credits/docked and a working miner changes NONE of them, so the watchdog killed miners every 15 min (92 restarts vs 0 for craft). Miner role now gets a 6x window — 28 stall kills before, **0 after**. unlock/haul rosters each listed 22 agents the other owned.
- **⭐🟢✅ 2026-08-22 — FULL 9-FLEET REDEPLOY, 160/160 healthy, 0 over-capacity, ship_class on all 160.** Shipped deposit cargo accounting + ship-class plumbing + the 30-min command timeout. Rollout gotcha: cycling fleets back-to-back drove 3 workers into the pre-Hello stall-kill loop (johnny_cab hit 100 restarts). Recovery = dashboard `admin remove` then `readd` per agent. Budget >15min for a 54-worker fleet at `--stagger 10s`. [[project_overmind_stall_kill_connect_loop]]
- **⭐🟢✅ 2026-08-21 — MINING FLEET CREATED, fleet set is now NINE.** The 7 pirate-unlock graduates released from unlock into it; the `miner` role had NO idle script until then. 3 run the two-system loop — mine a STATIONLESS system, haul back to a measured-fuel home. Ore selection (`mine <ore_x>`) still deferred. [[project_mining_fleet]]
- **⭐🟢✅ 2026-08-23/24 — UNLOCK CAMPAIGN WAVE 1 GRADUATED 6/6** after pinning fixed the stall (15 of 17 ran unpinned; unpinned NEVER unlocks — pinned 8/8 vs unpinned 0/45). 7 were fuel-dead at 0 credits, gifted 100k each. pirate-13/prophet-1/pirate-5/miner-6/miner-5/hauler-0 all completed `an_introduction` within ~4h of reaching the giver. [[project_pirate_reputation_unlock_campaign]]
- **⭐ 2026-08-21 — 12 agent ship losses on record, none since 08-19 23:55.** Two standing kill zones: **zaniah (6, combat)** and **goldcrest (4, wildlife — salvager-7 twice)**. All 161 agents currently hold an active hull. Blind spot: 6 agents' action-log cursors stale >24h, incl. salvager-1 and salvager-3, and salvagers are 7 of the 12 dead.
