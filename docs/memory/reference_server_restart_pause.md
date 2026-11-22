---
name: reference_server_restart_pause
description: "server_restart_warning push now pauses new jumps/travel until the reconnect (cap 5m) and autopilot resumes a restart-broken route once; built 2026-11-22, bin/worker rebuilt, NOT committed"
metadata:
  type: reference
---

Built 2026-11-22 after the v0.733.0 restart broke explorer-3 (Adhara) and craftsman-2 (Wazn) mid-route: jumps started in the ~60s after the warning hung the full 5m SleepJumpMaxWait.

- pkg/game: `State.ServerRestart` {At, Target} set by the `server_restart_warning` handler (also logs `🔄 SERVER RESTART:` on the event log), cleared by a successful `Reconnect`; `State.LastRestartAt` kept; `RestartPending(now)` / `RestartRecent(now)`; constant `SleepServerRestartMaxWait = 5m` (anti-wedge cap).
- pkg/worker: `waitOutRestart` (hold_gate.go) runs beside every `waitOutHold` in autopilot; `Autopilot` wraps `autopilotOnce` and, on a failure while `RestartRecent`, logs `⏸ route interrupted by server restart` and resumes once.
- Spec docs/superpowers/specs/2026-11-22-server-restart-pause-design.md, plan docs/superpowers/plans/2026-11-22-server-restart-pause.md.
- Operator: no immediate push/restart; bin/worker rebuilt 11-22 14:41 server time so workers pick it up on natural restarts. Code NOT committed until asked.

Related: [[reference_customs_mechanics]] (same State+gate pattern as customs holds), [[reference_never_restart_a_disconnected_worker]]
