---
name: project_play_as_scheduler
description: play_as cron-lite scheduler — recurring hourly/daily/weekly commands
metadata: 
  node_type: memory
  type: project
  originSessionId: a9e877bc-b5aa-417f-b398-a7b8f6f7c432
---

Built 2026-05-31. Lets a `play_as` user schedule recurring API calls at coarse
frequencies. Commands: `schedule_add <hourly|daily|weekly> <command...>`,
`schedule_remove <id>`, `view_scheduled`.

- **Timing model** is one pure function `currentBoundary(freq, now)` in
  `cmd/tools/play_as/schedule.go`: hourly→top of hour, daily→00:00 UTC,
  weekly→most recent Sunday 00:00 UTC. A task is due when
  `currentBoundary(freq, now).After(LastRun)` — this single rule gives
  once-per-period firing AND automatic catch-up collapse (N missed boundaries →
  one run).
- **Run-immediately**: `schedule_add` runs the command once now and stamps
  `LastRun=now`, so it won't refire until the next boundary.
- **Startup catch-up**: `startLoop` does an immediate `checkDue` pass, logging
  `⏰ backfilling N missed scheduled task(s)`; then ticks every `game.SleepLong`
  (60s). Mirrors the `chatPoller` goroutine pattern.
- **Persistence**: `data/agents/<agent>/scheduled_commands.json` (array of
  ScheduledTask), atomic write. ids = max+1.
- **Concurrency**: a shared `execMu sync.Mutex` wraps both the REPL's
  `executeLogicalCommand` dispatch (main.go) and the scheduler's runner, so
  scheduled + foreground commands never interleave.
- **Executor reused**: `executeLogicalCommand(client, ctx, cmdString, format,
  cfg, agentID)` — the same per-line executor `runScript` uses; handles flags
  and loops.
- Design doc: `docs/plans/2026-05-31-play_as-scheduler-design.md`.

**Future**: the existing `get_chat_history` background pollers (poll every few
seconds, finer than hourly) could migrate under this scheduler once an
interval/"every N min" frequency is added. Not built yet. Relates to
[[project_mbox_spam_folder]].
