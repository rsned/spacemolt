---
name: project_fleet_efficiency_dash
description: "Fleet earnings-per-jump on the overmind status page; SP1 (net-of-fuel panel + per-worker lifetime stats line) MERGED to local main 2026-07-15 (4053d09, unpushed, restart overmind-status to see it); SP-task/SP-loss/SP2/SP3 still sequenced"
metadata: 
  node_type: memory
  type: project
  originSessionId: d5de5721-9ac4-4983-9ff4-4532bee17979
---

Multi-sub-project effort: surface "how much are we earning per jump" for the hauler (and later shuttle) fleet, on the **existing overmind-status page** (`pkg/ovstatus`, singleton served :8087 via `scripts/start-overmind-status.sh` — reads every overmind's `*-status.json`; user already runs it). Brainstormed 2026-07-15.

## Key context discovered
- **Haulers already log every completed haul** → `haul_results` (market.db): `realized_profit`, `jumps_traveled` (= approach cur→buy + haul buy→sell, `pkg/worker/haul.go:654`), per-leg wall+tick timing. 6490 rows / 21 agents since 2026-06-27. Written by `recordHaulResult` (`haul.go:1039`) → `market.RecordHaulResult`. Read via `GetHaulResults` / `HaulResultTotals`. Fleet gross ≈ **2,391 cr/jump**, 0 losses.
- **huddash** (`pkg/huddash` + `cmd/tools/haul-dashboard`) already computes cr/jump but is a **dormant one-shot HTML generator** — nothing runs it, no served page, cr/jump is GROSS of fuel. Not the vehicle; overmind-status is.
- **overmind-status reads status JSON ONLY** — never opens market.db today. `ovstatus.Render(sources, refresh, now)` single caller in `cmd/tools/overmind-status/main.go`.
- **`LiveRecord`** (`pkg/overmind/balances/balances.go:27`, the per-worker status JSON) has role/system/poi/credits/hull/fuel/cargo/`ActiveTaskID`/healthy/restarts — but NO current-opp detail, item/qty, route, hauls, jumps, cr/jump, or losses.
- **Ship losses NOT tracked anywhere** — no deaths table, `player_died` not persisted. → render `—`, never fake `0`.
- **Fuel:** real `station_fuel_prices.fuel_price_all_in` = 4–8 cr/unit (avg ~6); ~9 fuel/jump observed. User's "5cr/unit" flat approximation validated. Fuel drag ~50 cr/jump vs ~2400 gross (~2%).

## Decomposition (user chose "both, monitor first"; then reshaped)
- **SP1 — earnings-per-jump on the status page** — **BUILT + MERGED to local main 2026-07-15 (ff `4edc27c..4053d09`, main@`4053d09`, UNPUSHED).** Spec `10575e4`, plan `4edc27c`. Built via SDD (3 tasks, per-task reviews + opus whole-branch review = merge YES, full `go test ./...` green). Commits: `10d211b` (market.HaulEfficiencySince), `3b053e2`+`b9d6036` (ovstatus panel+per-worker line rendering + Render hs param), `4053d09` (overmind-status main wiring). Scope: (a) fleet **net-of-fuel** headline panel (windowed 48h, flat fuel `jumps×9×5cr`) + per-agent net/jump ranked desc; (b) per-worker **lifetime GROSS** line under each haul row: `hauls · jumps · — losses · avg cr/jump`. Aggregator `market.HaulEfficiencySince(ctx, since)` (zero since=all-time; excludes jumps=0). `ovstatus.Render` gained `hs *HaulStats` (nil=page unchanged). Flags `--market-db-path`(default data/market.db) `--eff-window`(48h) `--fuel-per-jump`(9) `--fuel-cr-per-unit`(5). CSS class is `eff-line` (NOT effline — that token collided with static styleBlock). NO worker/schema/redeploy/backfill. **NOT LIVE until overmind-status restarted** (running proc = old binary; stop + re-run `scripts/start-overmind-status.sh`). Two accepted Minors: market.Open runs no-op migration on already-migrated DB; formatCredits truncates.
- **SP-task** — per-worker current-task line + `buy --*--> sell` route-progress bar (user's mockup: "Handling opp #804122 - 204 Trade Authenticators · gold_run--*-->iron_heart"). NEEDS worker to publish active opp (id/item/qty/buy/sell/progress) into heartbeat + `LiveRecord` fields + fleet redeploy. = the queued [[project_overmind_dashboard_task_summary]].
- **SP-loss** — minimal per-agent death counter (persist player_died / ship-destroyed) to fill the `—`. Ties to queued [[project_per_death_loss_capture]].
- **SP2** — sweetspot/outlier gate: characterize cr/jump distribution (IQR around fleet median net/jump), flag opps (find_arbitrage rows / hauler's next pick) far above/below. Builds on SP1's net cr/jump.
- **SP3** — shuttle fare recorder (`shuttle_results`, NEW — shuttles log NO fare outcomes today; `RecordPassengers` is just an identity catalog) + "Shuttle fleet efficiency" panel + per-worker line.

Related: this grew out of the user asking whether haulers log completed opportunities + want profit/jump. [[project_arbitrage_net_of_fuel]] (haulers' net-of-fuel ranker is separate — `worker.RankHaulOpportunities`, unaffected).
