---
name: project_executor_b_rollout
description: "Executor B is merged (2026-07-11) but NOT live: operator rollout steps (fleet redeploys, station confirmation, live smoke) + the reviewed post-merge follow-up list."
metadata: 
  node_type: memory
  type: project
  originSessionId: a8563449-be91-4ce3-ac61-2e2c0f817af8
---

**State 2026-07-11:** Crafting Brain Executor B merged to main (`9406f3e` + `7fd181e`), full suite green (only red = pkg/game espionage drift, see [[project_server_restart_warning_event]]). Nothing pushed yet this session beyond the merge commits sitting on local main — **push when user asks.** The authoritative operator runbook is the "Rollout" section of `docs/superpowers/specs/2026-07-10-crafting-brain-b-executor-design.md`; don't duplicate it here.

**Why:** the code is review-hardened but zero plans have run live; several behaviors are code-verified only.

**How to apply — rollout order (runbook has the exact commands):**
1. Rebuild bin/overmind, bin/worker, bin/craft-dashboard (done once locally 2026-07-11 during Task 15, redo after any new commits).
2. Pre-commit hook was reinstalled 2026-07-11 with 300s pkg/worker timeout (`scripts/setup-pre-commit.sh`) — worktrees share it via the common .git.
3. **mb fleet redeploy — DONE 2026-07-11 18:55** (drain USR1 → TERM → relaunch, new bin/overmind+bin/worker built 18:51 from merged main): 35 workers up incl. NEW `marketbot_001` (resident, Hex Star player station in Dheneb — first entry in mb-fleet.yaml, edit UNCOMMITTED). Workers now carry `--handoff-queue data/overmind/handoff-queue.json` (intended; inert for non-holders). Drain quirk: marketbot_krynn never reported idle (docked+heartbeating at war_citadel, safe) — drain poll ended 33/34, force-TERM as the log suggests. Also cleared the stale-bin/worker facilities `last_seen_utc` gap (that memory deleted).
4. Craft fleet first launch — **DONE 2026-07-12 08:23**: stations live-verified via play_as sweep (4 of 6 provisional ids were WRONG display-form names — fixed to real POI ids: grand_exchange, sol_central, the_core, war_citadel; yaml edit uncommitted). 9 workers up, plans Runner `roster=9 managed=35`. Launch line: `bin/overmind --fleet data/overmind/craft-fleet.yaml --socket data/overmind/craft.sock --worker-bin bin/worker --plan-queue data/overmind/craft-queue --plan-state-dir data/overmind/craft-plans --status-file data/overmind/craft-status.json --history-file data/overmind/craft-history.jsonl --holders-roster data/overmind/mb-fleet.yaml --stagger 10s`. Dashboard live on :8091 (`bin/craft-dashboard`, log craft-dashboard.log).
5. Live smoke — **IN FLIGHT 2026-07-12**: plan `air-recycler-20260712-152838` (air_recycler x2, budget 5000, assembly grand_exchange) dispatched via databot play_as session (`build air_recycler 2 --json` REPL output → JSON extracted by hand → `dispatch <file> --budget=5000`; NOTE flags need `=` form). Runner adopted (7 nodes incl. synthetic xfer-0 producer-courier same-station), mine-6 dispatched to craftsman-10 (mining live). 3 haul nodes parked needs_operator: holders assist-haven (2 flex_polymer) + craftsman-1 (4 ionized_neon, 8 purified_argon) at grand_exchange storage — user must move goods to craftsman-2, then plan_retry each node. Craftsman survey 2026-07-12: wallets 21k–718k EXCEPT craftsman-3 = 1,254 cr (needs top-up; it holds the copper_piping smelt node); ships all mining hulls (Deeprock 400 cargo x2, Drillship 100 x3, Excavator 150, Prospector 50 x2) — fine for now, mine nodes need them; craftsman-7 cargo FULL 100/100, craftsman-8 387/400 (should deposit). Holder storage station ids appear as display-form `grand_exchange_station` in state (capturer id drift, matches known wart).

**LIVE SMOKE FINDINGS 2026-07-12 (air-recycler-20260712-152838, plan now PAUSED with mine-6 parked failed):**
- **mine_qty strands miners fuel-dead (CRITICAL for any --mine plan):** the verb travels to a belt and mines with no fuel guard and no return-to-station; craftsman-10 (r0) then craftsman-2 (retry) each burned to ~5-10 fuel, froze undocked, ate 3 stall-restarts, got QUARANTINED. Fix before un-parking any mine node.
- **Craftsman standing behavior can't recover from a stationless POI:** restarted worker at a belt loops dock→"No station at this location" forever; no travel-home despite the yaml station. Manual recovery: stop fleet → play_as travel+dock → relaunch (done for craftsman-2).
- **Rescue pipeline WORKS end-to-end:** both quarantined craftsmen auto-queued to rescue-queue.json; assist-frontier refueled craftsman-10 (5→100), assist-haven topped craftsman-2 — first live proof of the cross-fleet rescue path.
- Fleet roster note: craftsman-7..10 PULLED from craft-fleet.yaml 2026-07-12 (commented out, uncommitted) for operator maintenance — unload 7/8 cargo, cargo expanders on 9/10; re-add + fleet restart when done. Roster now 5 (craftsman-2..6).
- craftsman-10 is undocked at unknown_edge_mineral_fields (full fuel post-rescue) — operator will fly it back during refit.

**Post-merge follow-ups (from the final fable review, none blocking):**
- Runner Control adoption still has a ms-scale mid-tick lost-update window (re-merge Control under SaveRun's write lock).
- Multi-consumer producers get N full-Qty xfers → noisy fail/park (rule-5 diagnostic exists).
- Task events carry no actual qty/spend — DoneQty/Spent are planner estimates (spec "Deviations" documents it); crash window can double-count one fee (conservative).
- Root-cause pkg/game parseActionResult: NO cargo refresh on withdraw_items/buy/send_gift (each verb compensates with GetCargo); add cases + a pushOnlyResponseTypes drift-guard test (crafting_update bug class).
- HandoffPass stranded-cargo edge: withdraw-ok/gift-fail leaves stock in holder cargo, next pass re-withdraws (never over-gifts); needs a Record in-flight field.
- Plan archival: done/cancelled plans re-ticked+re-saved every 30s forever; dashboard shows them immortally.
- Handoff done-record pruning (only safe keyed to node completion); injectable clock for older pkg/worker verb tests; mine fallback POI list is first-fit not nearest; :8091 binds all interfaces (matches existing practice).
- NEW 2026-07-11 (Dheneb/Hex Star visit): player-station facility capture keys `public_facilities.station_id` by BASE id (`b495c600…`) while all NPC-station rows use POI-style ids — POI-keyed joins miss player stations. Capturer follow-up.

Related: [[project_crafting_brain]], [[project_worker_storage_capture_gap]], [[reference_overmind_launch_commands]], [[reference_login_vs_reconnect_gating]].
