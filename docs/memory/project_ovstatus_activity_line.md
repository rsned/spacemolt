---
name: project_ovstatus_activity_line
description: "Overmind status page per-agent current-activity sub-line — what each active worker is doing (mission/opportunity/passengers/rescue)"
metadata:
  node_type: memory
  type: project
  originSessionId: 051adb3a-a06c-4dac-aed0-f51137d16814
---

**2026-07-18 SHIPPED — PUSHED + FULLY DEPLOYED (`499dda3` on origin/main).** Rolled out fleet-by-fleet (one overmind down at a time; backward-compat omitempty made this safe and kept ~90% of the fleet earning). Rebuilt bin/worker + bin/overmind + bin/overmind-status; restarted viewer + all 6 fleets (shuttle/assist/craft/mb/haul/mission-learn) each with its EXACT captured cmdline on the new binary. Paced the two 6/min staggered fleets (mb, haul) so their logins never overlapped (waited for each to hit ~near-full before the next) — no /login rate trips. LIVE-VERIFIED end to end: haul fleet-status.json showed 15 concurrent `Opportunity #<id> <qty> <item> from A to B` lines. mb shows 0 activity (correct — marketbots run update_market/mining, not the 4 activity roles). **Original build note below.**

**2026-07-18 BUILT + committed local main (`499dda3`).** Adds a greyed sub-row under each active worker's row on the overmind status page (`:8087`) describing its current unit of work, per operator spec:
- Shuttle → `Hauling 2 passengers from STATION_A to STATION_B`
- Haul → `Opportunity #100042 24 power_cell from STATION_A to STATION_B`
- Missions → `Mission <title>` (delivery + exploration; set at accept AND resume)
- Assist → `Rescuing <agent_id>`
Haulers now show TWO sub-rows: activity line ABOVE the existing lifetime line (`385 hauls · … cr/jump`).

**Plumbing (one field end to end):** new `Activity string` on `control.Status` (wire) → supervisor `WorkerInfo.LastStatus` → `recordBalances` (`cmd/overmind/main.go`, the `LiveRecord{}` literal) → `balances.LiveRecord` → status.json → `renderActivityLine` (`pkg/ovstatus/ovstatus.go`, reuses the `eff-line` sub-row style). The role goroutine publishes via a shared `atomic.Pointer[string]` on `WorkerDispatch` (`SetActivitySink`, set in `cmd/worker/main.go` next to `activeTaskID`); the heartbeat goroutine reads it in `buildStatus`. Each `*Deps` got a `SetActivity func(string)` field (nil in tests); `publishActivity(fn, s)` is the nil-safe setter. Roles set at the work-commit site (`haul.go` claimBest→`haulActivityLabel`, `shuttle.go` boardBestDestination, `mission.go` accept→`missionActivityLabel` + resume, `mission_explore.go` run+resume, `assist.go` runRescue) and CLEAR ("") at role-function entry so idle passes report blank. Tests: `pkg/ovstatus/activity_test.go`, `pkg/worker/activity_label_test.go`.

**DEPLOY SCOPE IS FLEET-WIDE (heavier than a single-fleet restart):** the field is only PRODUCED by a new `bin/worker`, only MAPPED by a new `bin/overmind` (`recordBalances`), and only RENDERED by a new `bin/overmind-status`. To see it, rebuild all three and restart every fleet overmind (haul/mb/shuttle/assist/craft/mission-learn) + the viewer. Backward-compatible (omitempty; old workers just omit it), so a partial/rolling restart is safe — fleets show activity only after their overmind+workers are on the new binary. Launch lines: [[reference_overmind_launch_commands]].

Related: [[project_fleet_efficiency_dash]] (the lifetime line this sits above) [[project_overmind_dashboard_task_summary]] (this effectively IS that queued per-worker task line).
