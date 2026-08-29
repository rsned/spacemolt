---
name: project_overmind_dashboard_task_summary
description: "FUTURE feature: per-worker current-task summary line on the overmind dashboard (haulers + taxis/shuttles)"
metadata: 
  node_type: memory
  type: project
  originSessionId: d5de5721-9ac4-4983-9ff4-4532bee17979
---

FUTURE feature request (user 2026-07-15): show a short current-task summary line per worker on the overmind fleet dashboard (the `:8087` `bin/overmind-status` viewer, and/or the `:8091` dashboard).

- **Haulers:** `hauling <item_id> @ <qty> units from <system_id> to <system_id>`
- **Taxis (shuttles):** `carrying <X> <passengers> from <system_id> to <system_id>`

Notes for build:
- The data exists worker-side: hauler's claimed opportunity carries item/qty/from/to (see `haulMetrics` + `market.ArbitrageOpportunity` FromSystemName/ToSystemName/ItemID/Quantity in `pkg/worker/haul.go`); shuttle passenger state in `pkg/worker/shuttle.go`.
- Mechanism: worker publishes a `task_summary` string in its status heartbeat (the `*-status.json` files the overmind writes / the viewer reads), and the dashboard renders it per row. Check how workers currently report status to the overmind (control socket + status-file writer) and add a summary field.
- Idle workers should show idle/standing state (e.g. "resident @ <station>", "idle").
- Scope: overmind dashboard(s) only; no game-server change. Brainstorm→spec→plan when picked up.
- Related: [[project_battle_visualization]] (other dashboard/telemetry work), [[reference_overmind_launch_commands]] (viewer is `bin/overmind-status` singleton :8087).
