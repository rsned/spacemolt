---
name: project_worker_storage_capture_gap
description: "Worker fleet never wires WireStorageCapture, so scheduling view_storage on a role would store nothing; deferred from craftbrain A2 Task 12"
metadata: 
  node_type: memory
  type: project
  originSessionId: 82fc608b-c0b2-4c87-ad0b-296b44e4a4ff
---

Deferred out of the crafting-brain A2 branch on **2026-07-09** (user decision). Scheduling an hourly `view_storage` sweep on resident roles is **not** a `roles.yaml` one-liner. Three verified blockers:

1. **`view_storage` is not dispatchable.** It appears in neither the `supported` map nor the `Run` switch in `pkg/worker/dispatch.go`. A schedule entry naming it hits `default: unsupported command` on every tick, for every resident worker (~40 live agents). `pkg/worker/roles_test.go`'s `TestSeededCommandsAreDispatchable` enforces this, so it would also turn that test red.

2. **The worker fleet never wires storage capture.** `agent.WireStorageCapture` is called only from `cmd/tools/daily-summary/main.go:822`. Issuing `view_storage` from a worker sends the command and persists nothing — the capture is a passive message handler that nobody installed. Every row currently in `storage_snapshots` came from daily-summary runs.

3. **`WorkerDispatch` has no `agent_id`.** `StoreStorageSnapshot` requires one. `pkg/worker/dispatch.go:168` already carries a comment deferring exactly this plumbing.

**Why:** craftbrain's planner subtracts on-hand fleet inventory from a build's material demand. Fresher snapshots mean fewer redundant mine/buy steps. Nothing is broken today — stale holdings are already flagged (`StatusStale`, `MaxStockAge = 24h`), so the planner degrades honestly rather than silently.

**How to apply:** Cheapest correct route is to call `agent.WireStorageCapture(client, kb, agentID, logger)` at worker startup in `cmd/worker/main.go`, where `agentID` is already in scope. Then the dispatch case is just `case "view_storage": return d.Client.ViewStorage(ctx)`, plus a `supported` map entry and the `roles.yaml` schedule line. The alternative — a new `KBUpdateStorage` in `pkg/worker/capture.go` mirroring `KBUpdateFacilities` — is strictly more code because it *also* forces an agent-id field into `WorkerDispatch`. Either way this changes the live worker binary and needs a graceful fleet redeploy (`kill -USR1 <overmind-pid>`, see [[project_overmind_graceful_drain]]).

Related: [[project_crafting_brain]], [[reference_storage_snapshots_shape]].
