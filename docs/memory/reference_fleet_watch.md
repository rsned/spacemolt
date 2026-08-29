---
name: reference_fleet_watch
description: "bin/fleet-watch — the fleet health watcher; alerts on log SILENCE or disconnects unmatched by reconnects across two passes, plus a census of the unsupervised daemons. Shipped daff4469"
metadata:
  type: reference
---

**SHIPPED `daff4469`.** Relaunch line (it is itself unsupervised — start it with
the fleets):
```
setsid nohup ./bin/fleet-watch --interval 60s --stale 3m --notify \
  >> data/overmind/fleet-watch-daemon.log 2>&1 < /dev/null &
```
- Health snapshot: `data/overmind/fleet-watch-status.json` (`healthy` bool,
  per-fleet `silent_for`, process census, active alerts).
- Alert history: `data/overmind/fleet-watch.log`. Desktop alerts via `notify-send`.
- One-shot check: `./bin/fleet-watch --once --notify=false`.

## Why the rule is two-sided
**Alerting on a disconnect is useless.** The client disconnects and reconnects
constantly under normal load — haul did **22 in half an hour, all recovered**. A
fleet is unhealthy only when:
1. its log goes **silent** past `--stale` (default 3m), or
2. **disconnects exceed reconnects across TWO consecutive passes**. One pass is
   not enough: a disconnect seen moments ago has not had time to reconnect.

It also counts the **unsupervised daemons** (`arbitrage-scanner`, `market-prune`)
— both have died silently before and were noticed only by downstream damage: a
frozen opportunity pool [[project_scanner_outage_expiry_fix]] and a 62 GB
database [[reference_market_db_prune]].

## Traps it was built around
- **Logs are multi-GB and unrotated** (`mb-overmind.log` = **7 GB**, ~20 GB
  total). Every read is a seek to the last 2 MiB — never a scan. **The log growth
  is itself an unaddressed disk risk.**
- **Watched fleets are fixed at startup** from the running overminds' `--socket`
  args. Re-deriving each pass would drop a fleet at the moment its overmind died;
  globbing `*-overmind.log` alerts forever about **retired fleets whose logs
  remain** (idle, mission, shuttle).
- **Processes are matched by `/proc/<pid>/exe`, never cmdline** — a cmdline scan
  matches the tool's own arguments [[reference_overmind_launch_commands]].
- Alerts are **edge-triggered** with recoveries announced, so a standing fault
  notifies once rather than every minute.

## First real catch — 2026-08-18 server instability (three waves)
fleet-watch fired on all 7 fleets and cleared every one, unattended:
- **16:58** — 100 workers dropped at once with raw `read frame header: EOF`
  (NO close frame): a hard server-side kill, not a graceful shutdown.
- **17:14-17:17** — 51 × `StatusGoingAway` close frames: a graceful restart.
- **17:23-17:24** — 41 more `StatusGoingAway`: a second graceful restart.

Recovery was fully automatic — 12 worker restarts total, 0 disconnected
afterwards. Each wave decayed over ~10-13 min as the staggered reconnect gate
worked through the fleet.

**A heartbeat proves NOTHING about connectivity**: `cmd/worker/main.go` logs it
from `client.GetState()`, which is cached, so a disconnected worker keeps
printing plausible system/POI/credits. The real signals are the per-task
`missions|haul|hunt: game connection down` lines and `Status.Disconnected`
(omitempty, so absent = connected in `*-status.json`).

⚠ `TypeServerRestartWarning` (`server_restart_warning`) is **defined in
internal/protocol but has NO handler anywhere** — the graceful restarts at 17:14
and 17:23 may well have been announced in advance and we ignored it. Handling it
would let the fleet drain instead of being cut off. Unbuilt.

⚠ `data/overmind/fleet-status.json` is a STALE artefact from 2026-08-13 — it
still lists salvager-3/9 as unhealthy. Do not read it as live state.
