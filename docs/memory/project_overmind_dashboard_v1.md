---
name: project_overmind_dashboard_v1
description: "Overmind Dashboard v1 (pkg/ovdash + cmd/overmind-dashboard :8091 + React Overmind view) — MERGED to main 2026-07-21, NOT pushed; plus the 07-21 fleet-wide maintenance outcome and follow-ups"
metadata: 
  node_type: memory
  type: project
  originSessionId: 8c9098dc-7c7c-4768-bb10-b65a2ce84115
  modified: 2026-07-21T17:39:16.363Z
---

**SHIPPED 2026-07-21 (merged to main, NOT pushed).** One live ops dashboard: `pkg/ovdash` (galaxy loader, snapshot merger, Diff+SSE Hub, accounting) + `cmd/overmind-dashboard` (:8091, READ-ONLY — never talks to game server or control sockets; serves /api/overmind/{systems,agents,accounting,stream} + frontend/dist) + React "Overmind" view (accounting strip, fleet rail w/ agent cards, live galaxy fleet overlay with move animation, SystemPanel). Spec: docs/superpowers/specs/2026-07-20-overmind-dashboard-design.md; plan: docs/superpowers/plans/2026-07-20-overmind-dashboard-v1.md; README: cmd/overmind-dashboard/README.md.

Run: `bin/overmind-dashboard --addr :8091 --kb data/spacemolt-knowledge.db --market-db data/market.db --status-dir data/overmind --dist frontend/dist` (launch with `setsid nohup` — a plain background launch died when its parent shell was reaped). Dev: vite :5173 proxies /api/overmind→:8091 (entry must precede the `/api`→:8090 entry).

Key builds/fixes on the branch: SSE SetKeyframe-BEFORE-Broadcast invariant; fleet palette single-sourced (Go pkg/ovdash Fleets ↔ TS FLEETS in useFleetStream.ts); ghosting root cause = duplicate connections → duplicate React keys → orphaned `<line>` DOM nodes (`c00b9a1`) + source-side dedupe in galaxy.go; off-map agents routed by system_id in the SSE hook (final-review Important, `1c6ddbb`); GalaxyMap systems-prop priority swap (props now win over internal fetch); `hideInfoPanel` prop kills the duplicate zoom/info panel on the Overmind page (`9a2e97b`).

**2026-07-21 maintenance outcome** (details → [[feedback_bulk_delete_scale]], [[reference_getreferenceask_perf]], [[reference_empire_field_semantics]]): full fleet relaunched on fixed binaries; market.db rebuilt fresh 62.7GB→~570MB — archive at `data/market.db.old-20260721` (durable tables copied into the new DB: haul/freight/mission_results, fleet_timeseries, stations, items, station_fuel_prices, analyses; delete the archive when no longer needed); prune daemon RESIDENT (`bin/market-prune -db-path data/market.db -retain 4h -interval 30m`). Results: load 14→5, worker spinners 100%→0.8% CPU, GetReferenceAsk 3.3s→2ms, KB empire count = 70 and FROZEN with the fleet live (old-binary haulers had re-rotted 69 tags overnight; re-cleaned once post-relaunch).

**Follow-ups:** supervise scanner + pruner (both unsupervised singletons whose silent deaths caused multi-day damage); accepted-minor list in `.superpowers/sdd/final-review-report.md` (FleetRail onClick vs React.memo, filter case, orbit overlap at busy hubs, `_busy_timeout` repo-wide ticket for pkg/mbox + pkg/overmind/checkpoint); SystemMap orbital reuse deferred (needs observer-shaped per-system endpoint); future pages Ship/Storage/Facilities/Missions/Economy are nav placeholders.
