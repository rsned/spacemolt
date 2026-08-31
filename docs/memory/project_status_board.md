---
name: project_status_board
description: "READ FIRST on resume — the live ⭐ items (dated, linked, ≤15), plus fleet-ops observations that have no memory file of their own. Rotate old entries to the monthly archive; never let this grow."
metadata:
  type: project
---

Moved out of `MEMORY.md` on 2026-08-29 so the index stays an index. This file
holds the full-text status bullets the index used to carry inline. **Keep the
Live block at ≤15 entries**; when it overflows, roll the oldest into
[[project_status_archive_2026_08]] (or the next month's archive) verbatim.

## Live (as of 2026-08-30)

- **⭐ 08-30 — SERVER v0.572.0: CREW, MARINES, BOARDING, PRIZES.** A (client structs + 3 pushes) and C (DB: `ships` crew/capture cols, `ship_captures`, `seen_prize_events`) absorbed, UNDEPLOYED until the next worker roll; **B = six personnel/prize commands DEFERRED** until A+C are live. Existing hulls seeded at full complement. **A boarding-fit hunter can now take a hauler INTACT — `player_died` will not fire; watch `ship_captures`.** [[reference_v0572_boarding_personnel]]
- **⭐🔴 08-24 — FLEET OVERRIDES ARE REMOVED-SETS.** Commenting out a rotating agent's yaml line makes the reconciler's release a NO-OP: hauler-0 + trader-1 ran in **NO fleet for 14h**, all health checks normal. [[reference_secondment_overrides_are_removed_sets]]
- **⭐🟢 08-24 — UNLOCK: wave 1 graduated 6/6; all 18 nominated haulers now PINNED** (all reach the giver on current fuel — 1-2 fuel/jump starters). **Blocked on operator: SIGHUP both overminds + relaunch `fleet-secondment --max-in-flight 6`.** The 11 unlock natives burn 6/jump at 18-26 jumps — **do NOT pin them** (miner-8 needs 156 of a 150 tank). [[project_pirate_reputation_unlock_campaign]]
- **⭐🔴 08-27 — SERVER IS v0.565.1, CLIENT DECLARES v0.547.1 (~18 behind).** `pay_bounty` (settle from anywhere, releases detention, gifts land while detained) = **0 hits in our Go code**; adaptive shields UN-BUGGED (resist stacks additively, 75%/bucket cap); stations now expose BOTH base_id + poi_id. [[reference_api_v0564_v0565_bounty_station_ids]]
- **⭐🟢✅ 08-30 — FULL FLEET ROLL: all 9 fleets on `1d24c975`** (147/159 live, 0 restarts, 0 new blocks; server restart used as the window). NOW DEPLOYED fleet-wide: `a474d01d` market retry · `2114df0e` MaxRestarts unpark · `6635aa73` settled-mission livelock · v0.572 absorption · sightings/boarding/prize capture · capture_action_log retune (applied at the stop). The 12 non-live agents = the 12 rescue-queue records, all fuel-dead, all need a tanker or hand-flown fuel. [[project_pending_rollout_queue]]
- **08-30 (later) — SECOND server restart → v0.573.1**: fleet frozen via SIGSTOP (156 procs), auto-thawed on 3 server OKs; 93 clean single restarts, 0 blocks. Capacitor REMOVED (v0.572.4) — parity test caught it; struct fixed + absorbed get_base repair ledger & drone_bay view (uncommitted). Repair material bills = titanium-demand intel. [[reference_server_docs_sync]]
- **08-30 (night) — goldcrest wildlife gate BUILT `cd0bb197` + play_as `loot_all` `9e8695dd`**: haul routing now unions danger_zones (level>=5) with strongholds at all 4 choke points, unconditional; goldcrest seeded level 8. Redeployed 19:02, gate VERIFIED FIRING in prod 19:03. BOTH re-equip embargo conditions now met -> buy hulls (junk_convoy 217k/850, bulk_terms 130k/350). [[project_haul_fleet_hull_attrition]]
- **⭐🟢 08-27 — market.db FIXED.** Root cause was `maxRetryAttempts=5`, NOT a locked DB; drained 5.97M stale rows with a **batched** delete while all 153 workers stayed live (rebuild-swap would cost a full fleet relogin). [[reference_market_db_prune]]
- **⭐🟢✅ 08-20 — BATTLE HOLOTABLE P1b SHIPPED**: kb `main` `32bb0f4fa`, **UNPUSHED**; worktree `kb-p1b` kept ON PURPOSE. **NEXT = P2 record sheet.** [[project_battle_holotable_visualizer]]
- **⭐ 08-01 — OPERATOR VISION: make agents interchangeable** so an arbitrage spike can be surged into Haul. **Brainstorm before coding.** [[project_fleet_role_interchangeability]]
- **⭐🔴 08-27 — THE IDLE LOOP RAN 3x PER TICK** (`IdleInterval` = SleepTick/3 = 3.33s) = ~43 passes/sec fleet-wide; the steady-state floor behind the recurring IP blocks (4.5h blocked 08-27). FIXED to one tick — **log volume went DOWN as blocks began, so there is no burst to hunt.** [[reference_idle_loop_ran_3x_per_tick]]
- **⭐🔴 SIZE RESTARTS BY LOGIN HISTORY, NOT FLEET SIZE** — 7 workers 90 min post-deploy blocked all 9 fleets in 6s. **SIGSTOP/SIGCONT freezes the fleet at ZERO logins and preserves game sessions.** [[reference_sigstop_preserves_game_sessions]] · [[reference_login_rate_limits]]
- **⭐🔴 08-22 — 15 OF 22 HAULERS LOST THE FREIGHT HULLS THE OPERATOR BOUGHT THEM**, flying free starters at 60-100 cargo on ~296M idle credits. Kill zones = zaniah (combat) + goldcrest (wildlife). **Do NOT re-equip before fixing routing.** [[project_haul_fleet_hull_attrition]]
- **⭐🔴 CLIENT `Ship.CargoUsed` DRIFTS UPWARD FOREVER; `CargoCapacity` is FREE space.** `cargoFree` goes NEGATIVE in haul + mission sizing. Diagnosed, NOT fixed. [[reference_client_cargo_used_drifts_upward]]
- **⭐🔴 HEALTH CHECKS DO NOT SEE A WEDGED OR LIVELOCKED WORKER** — 6 workers ran 3.5 days on a dead scheduler [[reference_standing_loop_wedge_after_reconnect]]; johnny_cab burned 47h on an "Already docked" loop [[reference_livelock_invisible_to_health_checks]].
- **⭐🔴 capture cadence retune PENDING** — `scripts/retune_action_log_capture.py --apply` at the NEXT fleet stop; live workers revert schedule.json edits, and the seeder can only move cadence FINER. [[reference_capture_cadence_retune_pending]]
- **Client built for v0.547.1** (`BuiltForAPIVersion`). Bump `VersionID` in `pkg/game/constants.go` before tagging. [[feedback_version_constant]] · [[project_api_sync_v0495]]

## Observations without a memory file of their own

These were inline in the index with nothing behind them. Promote any that
matter into a proper memory; delete the rest once acted on.

- **Pirate unlock: 45/161. The bottleneck is NOMINATION** — 101 locked agents were never nominated; 6 finished agents still pinned in the pool wasting slots. Rotation itself works (15 haulers graduated and returned). See [[project_pirate_reputation_unlock_campaign]].
- **Haul fleet: cargo capacity is NOT the binding constraint** — r=+0.27 between hold size and profit/haul; a 60-cargo shard out-earns an 850-cargo junk_convoy. **Do not re-equip before fixing routing.** See [[project_haul_fleet_hull_attrition]].
- **Freight: 3 prime carriers = 85% of delivered value.** Shipping earned **22.1M** (market.db `freight_results.carrier_payout`) vs 8.9M from ALL missions; the action log does NOT record freight payouts.
- **The index itself was the biggest capture gap** (2026-08-29): 55 memory files were reachable from no link at all, and the ⭐ block had grown to 15 bullets of 300–780 chars. Fixed by this restructure — every file is now indexed on one line, and full-text status lives here. `project_assist_fleet_refueling_pump_gap` held the built-in-pump fact since 08-15 and was still re-derived badly on 08-29; that is the failure mode to watch for.

## Rotation rule

Newest entries at the top. When Live exceeds 15, move the oldest to the current
month's archive as-is (do not summarise — the archive is the record). Keep the
archive chain in `MEMORY.md` pointing at every month.
