---
name: reference_overmind_launch_commands
description: "Exact relaunch commands for the five overmind fleets (haul / mb / shuttle / assist / craft) PLUS the arbitrage-scanner that feeds the haul fleet — socket, fleet, status-file, history-file flags per fleet"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 868ac572-f106-4923-9823-e82622bf66b9
  modified: 2026-08-14T20:51:05.169Z
---

**⭐ 2026-08-21 UPDATE — the fleet set is now EIGHT: SHUTTLE IS BACK.** johnny_cab finished the
pirate unlock (baseline 10 on all nine factions, smuggling 15) and was returned to
shuttle-fleet.yaml, so unlock is 24 and shuttle is 1. Move procedure that worked: comment the
line out of unlock-fleet.yaml, un-comment it in shuttle-fleet.yaml, `kill -HUP <unlock-overmind>`
(drained in 22s, not the 4min force-stop), then nohup the shuttle overmind. **`--assets-db-path
data/assets.db` is the one flag you must pass explicitly — it defaults to EMPTY, which disables
asset capture for the whole fleet.** Role change does NOT lose an agent's schedule: seeding in
`pkg/worker/standing.go` only ADDS uncovered commands, so johnny_cab kept capture_action_log /
capture_fuel / update_market even though the `shuttle` role lists none of them.

**⭐ 2026-08-14 UPDATE — `docs/COLD_START.md` is stale on the fleet set.**
Current fleets: haul(21, hauler-0 seconded out) / mb(54) / assist(5) / hunt(5, pirate-6..10) /
unlock(24) / craft(9) / mission-learn(40) / shuttle(1). **idle is RETIRED.** Every fleet gets `--assets-db-path data/assets.db`. New per-fleet lines beyond COLD_START:
```
# hunt — standard convention
./bin/overmind --socket data/overmind/hunt.sock --fleet data/overmind/hunt-fleet.yaml \
  --worker-bin bin/worker --status-file data/overmind/hunt-status.json \
  --history-file data/overmind/hunt-history.jsonl --assets-db-path data/assets.db --stagger 10s
# unlock — see project_pirate_reputation_unlock_campaign
./bin/overmind --socket data/overmind/unlock.sock --fleet data/overmind/unlock-fleet.yaml \
  --worker-bin bin/worker --status-file data/overmind/unlock-status.json \
  --history-file data/overmind/unlock-history.jsonl --assets-db-path data/assets.db --stagger 10s
# haul — NO LONGER on the default status/history files, and carries the secondment ledger
./bin/overmind --socket data/overmind/haul.sock --fleet data/overmind/haul-fleet.yaml \
  --status-file data/overmind/haul-status.json --history-file data/overmind/haul-history.jsonl \
  --assets-db-path data/assets.db --secondment-ledger data/overmind/secondments.json --stagger 10s
# fleet-secondment daemon (haul<->unlock loans; relaunch with the fleets)
./bin/fleet-secondment --watch 5m >> data/overmind/secondment.log
```
Cold-start login-pacing order used 2026-08-14 (144 workers, 0 restarts, one fleet staggering at
a time): mb → assist → hunt → craft → unlock → mission-learn → (market cycle + scan gate) → haul.

The three overmind fleets are launched as separate `bin/overmind` processes (no launch script exists except `scripts/start-overmind-status.sh` for the viewer). Shell history does NOT capture them — reconstruct from `data/overmind/*-overmind.log` launch banners + the per-fleet file names. Each runs from repo root `/home/robert/spacemolt/spacemolt` with `nohup ... >> <log> 2>&1 &`.

**Haul** (21 workers; uses the DEFAULT status/history files):
```
./bin/overmind --socket data/overmind/haul.sock --fleet data/overmind/haul-fleet.yaml --stagger 10s
# → writes fleet-status.json + fleet-history.jsonl (defaults); log: haul-overmind.log
```
**ALWAYS pass `--stagger 10s` for the 21-worker haul fleet.** 2026-07-17: relaunching WITHOUT it spawned workers ~5s apart, cramming >10 logins into one minute → tripped the per-IP `/login` rate limit; the first **9** logged in, the other 12 failed AND the overmind never retried them (restarts=0, never spawned again — the [[project_overmind_stall_kill_connect_loop]] no-retry gap). Symptom: fleet stuck at 9/21 for 45 min, the 12 showing "0.0% no / restarts=0" with zero "connecting to game server" log lines. `--stagger 10s` paces 21 logins to ~6/min (same as the mb fleet's 35 @ 10s, which always comes up full). [[reference_login_rate_limits]]

**Marketbots** (35 residents incl. marketbot_001 since 2026-07-11; exact line used at the 2026-07-11 redeploy):
```
bin/overmind --fleet data/overmind/mb-fleet.yaml --socket data/overmind/mb.sock \
  --worker-bin bin/worker --status-file data/overmind/mb-status.json \
  --history-file data/overmind/mb-history.jsonl --market-db-path data/market.db --stagger 10s
# log: mb-overmind.log; workers now also get --handoff-queue data/overmind/handoff-queue.json (Executor B default)
```

**Shuttle** (canary johnny_cab) — RELAUNCHED 2026-08-21, this exact line verified live:
```
nohup ./bin/overmind --socket data/overmind/shuttle.sock --fleet data/overmind/shuttle-fleet.yaml \
  --worker-bin bin/worker --status-file data/overmind/shuttle-status.json \
  --history-file data/overmind/shuttle-history.jsonl --assets-db-path data/assets.db \
  >> data/overmind/shuttle-overmind.log 2>&1 &
```
**Two monitors do NOT pick up a newly-added fleet on their own:**
- `bin/overmind-status` (:8087) reads a hardcoded `defaultSources()`; Shuttle had been dropped when
  the fleet was retired. Re-added 2026-08-21 (uncommitted) — edit + `go build -o bin/overmind-status
  ./cmd/tools/overmind-status` + restart, and launch it DIRECTLY, not via
  `scripts/start-overmind-status.sh` (its pgrep guard false-positives on the compiler).
- `bin/fleet-watch` fixes its watch set at STARTUP on purpose (`main.go:62` — deriving it per pass
  would make a fleet vanish exactly when its overmind died). A fleet started later is unwatched
  until fleet-watch is restarted. Restarting it is free: no game logins.

**Assist** (5 rescuers; added after this memory's original write — verified at the 2026-07-14 relaunch):
```
bin/overmind --socket data/overmind/assist.sock --fleet data/overmind/assist-fleet.yaml \
  --status-file data/overmind/assist-status.json --history-file data/overmind/assist-history.jsonl
# log: assist-overmind.log
```

**Craft** (craftsman-2..10 + the plans Runner; needs `--plan-queue` or the runner stays disabled):
```
bin/overmind --socket data/overmind/craft.sock --fleet data/overmind/craft-fleet.yaml \
  --status-file data/overmind/craft-status.json --history-file data/overmind/craft-history.jsonl \
  --plan-queue data/overmind/craft-queue
# --plan-state-dir (craft-plans) and --holders-roster (mb-fleet.yaml, managed=35) use defaults.
# log: craft-overmind.log; expect a "plan runner enabled: queue=… managed=35" banner.
```

**Mission LEARNING pool** (delivery + exploration; engineer-2 canary, stood up 2026-07-17):
```
rm -f data/overmind/mission-learn.sock
nohup ./bin/overmind --socket data/overmind/mission-learn.sock --fleet data/overmind/mission-learn-fleet.yaml \
  --worker-bin bin/worker --market-db-path data/market.db \
  --status-file data/overmind/mission-learn-status.json --history-file data/overmind/mission-learn-history.jsonl \
  >> data/overmind/mission-learn-overmind.log 2>&1 &
# fleet yaml carries per-worker `mission_categories: [delivery, exploration]` — forwarded to the
# worker as --mission-categories (WorkerSpec → supervisor DefaultSpawn). Without it, workers are
# delivery-only. bin/worker MUST be the exploration build (post-`f43afa2`). Verify:
# grep "mission-categories delivery,exploration" the worker cmdline via /proc. log: mission-learn-overmind.log.
# In overmind-status defaultSources as "Missions" since 2026-07-18 (`1592fca`, NOT pushed) → data/overmind/mission-learn-status.json; viewer runs with no --overmind flags so it shows after a rebuild+restart. [[project_mission_learning_pool]]
```
NOTE: the separate basic delivery-only smoke on `mission.sock` (engineer-1) was **RETIRED 2026-07-18** — its overmind had hung (status frozen ~26h, zero completions ~23h, worker idling on "reposition lookup failed (<nil>, 0 targets)") and it ran pre-fix code (predated the parking-backoff/station-less/mobile_capital fixes). Killed overmind+worker, `rm -f data/overmind/mission.sock`. The learning pool (engineer-2, mission-learn.sock) supersedes it. If you need a delivery-only runner again, relaunch on the CURRENT binary. Only `mission-learn.sock` should be live now.

**⭐ THIS IS NOW WRITTEN UP IN THE REPO: `docs/COLD_START.md`** (committed `dfc4870`, 2026-07-30 — needs the `!docs/COLD_START.md` negation because `.gitignore` ignores `docs/*.md`). Read that first; it has the full procedure, the checkpoint queries with healthy values, and the cold-start traps. The summary below is kept for quick recall.

**⭐ COLD-START ORDER (proven end-to-end 2026-07-30 after a ~6h full outage; 109 workers, 0 restarts).** The command lines below are a reference, not a procedure — this is the order:
1. `rm -f data/overmind/{haul,mb,shuttle,assist,craft,mission-learn}.sock`; check `rescue-queue.json` (a non-`done` record silently holds its worker out at boot); confirm `bin/overmind`/`bin/worker` mtime matches HEAD.
2. Dashboards: `bin/overmind-dashboard` (:8091, needs `frontend/dist`) and `bin/overmind-status` (:8087). Launch overmind-status directly rather than via `scripts/start-overmind-status.sh` — its `pgrep -f bin/overmind-status` guard false-positives on a concurrent `go build`.
3. `arbitrage-scanner` (no logins, safe any time).
4. Fleets, **sequenced, never parallel** — the ceiling is ~10 logins/min: mb (35 @ `--stagger 10s`, ~6 min) → assist (5) + shuttle (1) → craft (9) → mission-learn (38 @ 10s). Leave ~60s between fleets so their stagger windows don't overlap.
5. **haul LAST**, only after marketbots have completed a 10-minute `update_market` cycle AND the scanner has scanned against it — otherwise haulers route on a stale pool.
6. **`market-prune` LAST OF ALL, and staged** — see the 2026-07-30 trap in [[reference_market_db_prune]]; restarting it at `--retain 4h` after a long outage locks the DB and starves the whole fleet.

Checkpoints: each `<fleet>-status.json` gives `workers`/`healthy`/`restarts` + `overmind_commit`; market health = `select count(distinct station_id) from market_orders where captured_at > datetime('now','-15 minutes')` (expect ~34/35); pool health = `select count(*) from arbitrage_opportunities where status='available'` (98 was healthy; 30 was starved).

**arbitrage-scanner** (NOT an overmind — but the haul fleet earns NOTHING without it, so relaunch it with the fleets):
```
setsid nohup ./bin/arbitrage-scanner watch --interval 10m --offset 3m \
  --market-db-path data/market.db >> data/overmind/arbitrage-scanner.log 2>&1 < /dev/null &
# defaults are --interval 30m --offset 5m — the live cadence is 10m/+3m (verified 2026-07-30 from the
# log's own `watch:` banner; this note previously said 15m and was wrong), so pass both explicitly.
# Verify: log shows "scan @ <ts>: expired N available, inserted M" every 15 min at :03/:18/:33/:48.
```
Each scan wipes the pool (`available` → `expired`) and inserts a fresh batch, so **freshness exists only while this process lives**. It is unsupervised — no overmind restarts it, and nothing alerts when it dies. **2026-07-16: it died in the 07-15 ~21:21 reboot and the 23:51 fleet relaunch missed it because this runbook didn't list it — the 21 haulers churned on a frozen pool for ~10h and completed zero hauls for 6.7h.** Symptom to check first when haulers look busy but earn nothing: `ps -eo cmd | grep 'arbitrage-scanner watch'` and `tail data/overmind/arbitrage-scanner.log`.

All 5 relaunched clean at 2026-07-14 after the market.db rebuild ([[reference_market_db_prune]]): `rm -f data/overmind/{haul,mb,shuttle,assist,craft}.sock` first, then nohup each; 73 workers up in ~5 min, no rate errors.

Notes:
- Default flags used (do NOT override unless needed): `--stagger 30s` (per-IP /login pacing — 34 workers ≈ 17 min full spin-up; fresh logins are NOT rate-limited, only reconnects), `--restart-batch 1`, `--tasks data/overmind/tasks.yaml`, `--worker-bin bin/worker`, `--market-db-path data/market.db`.
- **Remove the stale `.sock` file before relaunch** if the prior process died (`rm -f data/overmind/mb.sock`) — `net.Listen("unix")` fails on a leftover socket file.
- Verify with `/proc/*/cmdline` scan, NOT `pgrep -f` (pgrep self-matches the Bash tool's own command line and returns the wrapper shell).
- The viewer `bin/overmind-status` (singleton, `:8087`) reads all six `*-status.json` files (Haul/Marketbots/Shuttle/Assist/Craft/Missions — Assist+Craft added to `defaultSources()` in cmd/tools/overmind-status/main.go on 2026-07-15, Missions on 2026-07-18 `1592fca`; before that only 3 showed); relaunch via `scripts/start-overmind-status.sh`. Since it runs with NO --overmind flags, adding a defaultSources entry + rebuild + restart is all it takes.
- Singleton-guard gotcha: `start-overmind-status.sh` uses `pgrep -f bin/overmind-status`, which ALSO matches a concurrent `go build -o bin/overmind-status ...` — if you kill+rebuild+restart in one shot, the guard can false-positive on the compiler and refuse to start. Let the build finish (or `sleep`) before running the script.
- 2026-06-28: mb + shuttle were found down (frozen at 23:31) and relaunched on the current drain binary (`bin/overmind`, built 16:45). See [[reference_login_vs_reconnect_gating]] and [[project_overmind_graceful_drain]].
- **Kill-signal /proc-scan trap (2026-07-20, cost fighter-4 two restarts):** a Bash-tool script that scans `/proc/*/cmdline` for a pattern that also appears in the SCRIPT'S OWN command line matches its own wrapper shells → `kill -STOP $PID` with a multi-line variable stops your own script, which then wedges past the supervisor's 90s SilenceTimeout and the worker gets silence-killed. Match on the executable prefix (`bin/worker*--agent X*`), take `head -1`, and send signals in a SEPARATE tool call from the scan.
- **Single-worker manual bump procedure (safe window):** worker docked ⇒ stall watchdog can't fire; `SilenceTimeout` = 90s (9×SleepTick, supervisor.go:140) ⇒ SIGSTOP the worker, do ≤60s of play_as movement, logout, SIGCONT. Longer than 90s ⇒ silence-kill + restart (self-healing but counts restarts). play_as invocation: `bin/play_as <agent-id>`, commands on stdin (`autopilot <system> <poi>`), NEVER while the worker process is runnable (session-contention exit(2) loop).
