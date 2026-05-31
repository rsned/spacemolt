# play_as Scheduler (cron-lite) — Design

Date: 2026-05-31

## Goal

Let a `play_as` user schedule recurring API calls at coarse frequencies —
`hourly`, `daily`, `weekly` — e.g. "once an hour run `get_nearby`" or
"daily run `view_storage --station_id treasure_cache_trading_post`". No
crontab-expression parsing; just three named frequencies plus the command line
to send.

## Requirements (confirmed)

- Frequencies: `hourly`, `daily`, `weekly` (weekly = Sundays).
- Anchor at **midnight UTC**: daily fires 00:00 UTC, weekly Sunday 00:00 UTC,
  hourly at the top of each hour. (Game time is UTC.)
- `schedule_add` **runs the command once immediately**, then on schedule.
- **Catch up once** on startup if a boundary was missed while play_as was
  closed; log that we're backfilling. Multiple missed boundaries collapse to a
  **single** run (never N-in-a-row).
- Commands: `schedule_add`, `schedule_remove`, `view_scheduled`.
- Persist across sessions.
- Future (not built): existing `get_chat_history` background pollers could
  migrate under this scheduler once a finer interval frequency exists.

## Data model & timing

Lives in `cmd/tools/play_as` (package `main`) because it reuses
`executeLogicalCommand`. Pure timing logic goes in a game-free, testable file;
the executor is injected as a callback.

```go
type ScheduledTask struct {
    ID        int       `json:"id"`
    Frequency string    `json:"frequency"`  // hourly | daily | weekly
    Command   string    `json:"command"`
    CreatedAt time.Time `json:"created_at"`
    LastRun   time.Time `json:"last_run,omitempty"` // UTC; zero = never
}
```

Persisted as a JSON array at `data/agents/<agent>/scheduled_commands.json`,
owned by a `Scheduler` (load on startup, save after every mutation/run).

The entire model is one pure function (`now` passed in — deterministic):

```go
func currentBoundary(freq string, now time.Time) time.Time
// hourly -> now truncated to the hour
// daily  -> today 00:00 UTC
// weekly -> most recent Sunday 00:00 UTC
```

A task is **due** when `currentBoundary(freq, now).After(task.LastRun)`. This
single rule yields: at-most-once-per-period; automatic catch-up collapse
(LastRun before the *current* boundary => due exactly once); and
run-immediately-on-add (stamp `LastRun = now`).

## Execution, concurrency & commands

Background goroutine mirroring `chatPoller` (context.WithCancel, stopped on
exit). Ticks every 60s (`game.SleepLong`). Each tick calls:

```go
func (s *Scheduler) checkDue(now time.Time) []ScheduledTask // stamps LastRun=now, saves
```

and runs each due task via the injected runner. On startup it runs one
immediate `checkDue(now)`; if anything was missed it logs
`⏰ backfilling N missed scheduled task(s)` first.

Runner: `func(cmd string) { executeLogicalCommand(client, ctx, cmd, format, cfg, agentID) }`.
A shared `sync.Mutex` (execMu) wraps both the REPL's `executeLogicalCommand`
call site and the scheduler's runner so scheduled and foreground commands never
interleave. Scheduled output is prefixed `⏰ [scheduled <freq>] <cmd>` and
printed `\r`-style like the chat poller.

Commands (REPL dispatch, alongside `mbox`):

| Command | Behaviour |
|---|---|
| `schedule_add <hourly\|daily\|weekly> <command…>` | validate freq; join rest as command; id = max+1; run once now; persist. Rejects bad freq, empty command, scheduling a `schedule_*` builtin. |
| `schedule_remove <id>` | remove by numeric id; persist. |
| `view_scheduled` | table: id, freq, next due, last run, command. |

## Testing (TDD, deterministic — now passed in)

- `currentBoundary` hourly/daily/weekly (weekly lands Sunday 00:00; mid-week ->
  previous Sunday).
- `isDue`/`checkDue`: fresh task due; just-ran not due until next boundary;
  3-missed-days -> exactly one due; LastRun stamped.
- Persistence: missing file -> empty; add/remove JSON round-trip; ids stable
  across reload.
- `schedule_add` parsing: good freq accepted, bad freq rejected, flags
  preserved in command.
