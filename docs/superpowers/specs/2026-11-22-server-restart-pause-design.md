# Pause and resume routes across a server restart

## Intent

The server pushes `server_restart_warning` about 60 s before a version deploy
(payload `message`, `seconds_until_restart`, `target_version`). Today the client
only writes it to the debug log. On 2026-11-22 (v0.733.0) the warning came at
14:23:21, the restart at about 14:24:21, and jumps started in that window
(explorer-3 to Adhara, craftsman-2 to Wazn) hung for the full 5-minute
`SleepJumpMaxWait` and failed at 14:29. Their deliver tasks then waited out the
hourly retry with cargo still in the hold, which blocked the next hauls. The
fleet reconnected about a minute after the restart.

Operator: pause is easiest; "or at least they should log something to know
they were interrupted and can try to resume smartly".

## Goals / non-goals

- Goal: a worker that has the warning does not START a jump or travel until
  the restart is over (pause).
- Goal: a route broken by the restart says so in the log and resumes from
  where the ship is, once, instead of failing the task.
- Goal: the warning shows on the event log, not only the debug log.
- Non-goal: a fleet-wide marker file.
- Anti-goal: a worker wedged forever by a restart that was announced and never
  happened. Every wait is bounded.

## Approaches

(1) Visible only. (2) Visible + pause before new jumps/travel. Operator picked
(2), plus the interrupted-route log and resume.

## Design

Follow the customs-hold pattern (`State.ActiveHold` + `pkg/worker/hold_gate.go`
`waitOutHold`), so no `GameClient` interface change is needed.

**pkg/game**
- `State` gains `ServerRestart *ServerRestart` with `At time.Time` (warning
  time + `seconds_until_restart`, 60 s when absent) and `Target string`.
  Set by the `TypeServerRestartWarning` handler; cleared by a successful
  `Reconnect` (a reconnect after the warning means the restart is over).
  `State.LastRestartAt time.Time` keeps `At` after the clear, for interruption
  detection.
- `func (s *State) RestartPending(now time.Time) bool`: `ServerRestart` set and
  `now` before `At + SleepServerRestartMaxWait`.
- The handler also writes a `🔄 SERVER RESTART: <message>` line with
  `c.logEvent`.
- New constant `SleepServerRestartMaxWait = 5 * time.Minute` in
  `constants.go`: the longest anything waits on an announced restart.

**pkg/worker**
- `waitOutRestart(ctx, out, client stateReader)` next to `waitOutHold`: no-op
  unless `RestartPending`; logs `⏸ server restart to vX: pausing before the
  next jump/travel`; waits one `SleepTick` at a time (via `settle`), bounded
  by `SleepServerRestartMaxWait / SleepTick` waits; logs when it resumes.
- Autopilot calls `waitOutRestart` wherever it calls `waitOutHold` (before each
  `Jump` and the final `Travel`).
- Resume: when a `Jump` or `Travel` in Autopilot fails and a restart was
  announced around it (`RestartPending`, or `LastRestartAt` within
  `SleepServerRestartMaxWait` of now), Autopilot logs
  `⏸ route interrupted by server restart at jump i/n (<step>); resuming from
  the ship's position`, waits out the restart, and runs itself once more to
  the same target (re-plans from the current system). A second failure is
  returned as today. The resumed run is marked so it cannot resume again.

## Left to the builder

Exact log wording; whether resume lives in a thin `Autopilot` wrapper or a
flag in `AutopilotDeps`; test placement next to `hold_gate_test.go`.
