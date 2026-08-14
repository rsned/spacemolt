# Cold Start — bringing the fleets up from fully stopped

How to restart everything after a host reboot, a crash, or a deliberate full stop.

Last proven end-to-end: **2026-08-14**, recovering from a workstation crash (~75 min outage) —
seven fleets, 144 workers up, 0 restarts, all on one commit. The Checkpoints numbers come from
that run; the step-5 prune incident numbers are from the 2026-07-30 run that hit it.

The fleet set as of 2026-08-14: **haul / mb / assist / hunt / craft / unlock / mission-learn**.
Shuttle and idle are retired (johnny_cab now lives in the unlock fleet).

**Read this in order.** The order is the point: the fleets have a dependency chain
(marketbots feed `market.db` → the scanner reads it → haulers route on what the scanner
found), and two of the steps will actively break the others if run early.

---

## 0. Preflight

Run all of these before launching anything.

**a. Confirm nothing is still alive.** Scan `/proc`, not `pgrep -f` — `pgrep` matches the
command line of the shell running the scan and reports itself.

```bash
for d in /proc/[0-9]*; do c=$(tr '\0' ' ' < $d/cmdline 2>/dev/null); \
  case "$c" in *overmind*|*bin/worker*|*arbitrage-scanner*|*market-prune*|*fleet-secondment*) \
  echo "$(basename $d) ${c:0:100}";; esac; done
```

**b. Confirm it was a hard stop, and how long ago.** The last timestamp in each
`data/overmind/*-overmind.log` is the moment of death. If every log ends at the same second
with no `shutdown complete` banner, the processes were killed rather than drained — expect
in-flight work (jumps, freight, transits) to have been interrupted mid-action.

**The gap length matters**: if it exceeds the prune retain window (4h), see step 5.

**c. Remove stale sockets.** `net.Listen("unix")` fails on a leftover socket file, so a
relaunch silently dies without this.

```bash
rm -f data/overmind/{haul,mb,assist,hunt,unlock,craft,mission-learn}.sock
```

**d. Check the rescue queue.**

```bash
cat data/overmind/rescue-queue.json     # `[]` is what you want
```

Any record whose status is not `done` causes `restoreQuarantine` to hold that worker out of
the fleet at boot — **silently, with no log line**. The tell afterwards is a worker showing
`0.0% no restarts=0` with zero spawn/connect lines. Restarting the fleet never fixes it;
re-arm or clear the record first (see the rescue-pipeline notes for the flock protocol).

A record stuck at `failed` with every assist worker in `failed_by` is not always a pipeline
fault: a strand at a **pirate stronghold** (e.g. Xamidimura) fails all rescuers by design,
because the station refuses the assists' dock. Those need a manual resolution, not a retry.

**Also check the secondment ledger** (`data/overmind/secondments.json`): an agent whose entry
is `phase=seconded` is *supposed* to be absent from its home fleet (it runs in the away fleet
via the overrides sidecars). `phase=failed` plus a non-empty `removed` list in the home
overrides plus no process = orphaned in no fleet; restore via the overrides + SIGHUP.

**e. Confirm the binaries are the build you think they are.**

```bash
ls -la bin/overmind bin/worker && git log -1 --format=%H
```

If they predate HEAD, rebuild before launching — a cold start is the cheapest possible moment
to roll a new binary, since nothing has to be drained.

---

## 1. Dashboards first

They are read-only observers of the status files, so they are safe to start before anything
exists to observe, and having them up means you can watch the rest of the sequence land.

```bash
go build -o bin/overmind-dashboard ./cmd/overmind-dashboard
go build -o bin/overmind-status    ./cmd/tools/overmind-status

setsid nohup ./bin/overmind-dashboard \
  >> data/overmind/ovdash.log 2>&1 < /dev/null &                      # :8091

setsid nohup ./bin/overmind-status --addr ":8087" --refresh 300 \
  >> data/overmind/overmind-status.log 2>&1 < /dev/null &             # :8087
```

- `overmind-dashboard` needs a built `frontend/dist`; it logs `505 systems loaded, serving on :8091`.
- `overmind-status` logs the sources it found:
  `[Haul, Marketbots, Assist, Craft, Missions, Hunt, Unlock]`. The list is `defaultSources()`
  in `cmd/tools/overmind-status/main.go` — a new fleet is invisible on :8087 until it is added
  there and the viewer rebuilt.
- **Do not use `scripts/start-overmind-status.sh` right after a build.** Its singleton guard is
  `pgrep -f bin/overmind-status`, which also matches a concurrent `go build -o bin/overmind-status`,
  so it refuses to start and blames a process that is really the compiler.

**Checkpoint:** both return HTTP 200.

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8091/
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8087/
```

---

## 2. The arbitrage scanner

No game logins, so it costs nothing to start early — and the haul fleet earns **nothing**
without it. It is unsupervised: no overmind restarts it and nothing alerts when it dies.

```bash
setsid nohup ./bin/arbitrage-scanner watch --interval 10m --offset 3m \
  --market-db-path data/market.db \
  >> data/overmind/arbitrage-scanner.log 2>&1 < /dev/null &
```

The binary's own defaults (`30m`/`5m`) are **not** the live cadence — pass both flags
explicitly. If in doubt about the current cadence, read the `watch:` banner the scanner prints
at startup in its own log rather than trusting any note.

Its first scan or two may fail with `SQLITE_BUSY` if the marketbots are mid-burst; it says
`(will retry at next boundary)` and recovers on its own. Persistent failure is step 5's trap.

---

## 3. The fleets, sequenced

**The constraint is login pacing, and it is the single easiest way to ruin a cold start.**
The per-IP `/login` limiter tolerates roughly 10 logins/minute. Exceed it and the overwhelming
failure mode is silent: the first N workers log in, the rest fail, and **the overmind does not
retry them** — they sit at `restarts=0` with no connect lines, and the fleet is stuck at a
fraction of its roster until you notice.

`--stagger 10s` paces one fleet to ~6 logins/min. That means **one fleet at a time**: two
staggering concurrently is 12/min and back over the line. Leave ~60s between launches so the
stagger windows do not overlap.

Every fleet carries `--assets-db-path data/assets.db`. The overmind forwards it to each worker
it spawns, which is what turns on the scheduled `capture_profile` / `capture_storage` /
`capture_faction` passes. **Omitting it fails silently**: the captures still fire on schedule
and still log `⏰ [scheduled hourly] capture_profile`, but a nil store makes each one a no-op,
so the ledger stays empty while the logs look healthy. The file is created on first write — a
missing `data/assets.db` is not something to pre-create. Confirm it took with
`ASSET LEDGER FRESHNESS` on :8091, or `agents` counts in
`/api/overmind/agents` → `asset_coverage`.

Launch in this order — marketbots first, because everything downstream needs the market data
they produce: **mb → assist → hunt → craft → unlock → mission-learn**, then haul (step 4).

### 3a. Marketbots (54) — ~9 min

```bash
setsid nohup ./bin/overmind --fleet data/overmind/mb-fleet.yaml --socket data/overmind/mb.sock \
  --worker-bin bin/worker --status-file data/overmind/mb-status.json \
  --history-file data/overmind/mb-history.jsonl --market-db-path data/market.db \
  --assets-db-path data/assets.db --stagger 10s \
  >> data/overmind/mb-overmind.log 2>&1 < /dev/null &
```

### 3b. Assist (5) and hunt (5)

Assist is the fuel-rescue fleet — bring it up early so it is available if anything strands.
Hunt is the wildlife-cull pool (pirate-6..10). The shuttle fleet is retired — do not relaunch it.

```bash
setsid nohup ./bin/overmind --socket data/overmind/assist.sock --fleet data/overmind/assist-fleet.yaml \
  --status-file data/overmind/assist-status.json --history-file data/overmind/assist-history.jsonl \
  --assets-db-path data/assets.db --stagger 10s >> data/overmind/assist-overmind.log 2>&1 < /dev/null &

# ~60s later:
setsid nohup ./bin/overmind --socket data/overmind/hunt.sock --fleet data/overmind/hunt-fleet.yaml \
  --worker-bin bin/worker --status-file data/overmind/hunt-status.json \
  --history-file data/overmind/hunt-history.jsonl \
  --assets-db-path data/assets.db --stagger 10s >> data/overmind/hunt-overmind.log 2>&1 < /dev/null &
```

### 3c. Craft (9)

`--plan-queue` is required or the plan runner stays silently disabled.

```bash
setsid nohup ./bin/overmind --socket data/overmind/craft.sock --fleet data/overmind/craft-fleet.yaml \
  --status-file data/overmind/craft-status.json --history-file data/overmind/craft-history.jsonl \
  --plan-queue data/overmind/craft-queue --assets-db-path data/assets.db --stagger 10s \
  >> data/overmind/craft-overmind.log 2>&1 < /dev/null &
```

Expect the banner `plan runner enabled: queue=… state=… roster=9 managed=54`. No banner means
no runner.

### 3d. Unlock (25 of 46) — ~4.5 min

The pirate-reputation unlock pool. The roster carries 46 specs, but
`unlock-overrides.json` removes the 21 haul agents (`by: secondment-activation`) —
they only enter this fleet one at a time when the secondment daemon loans them in.
25 launched is the correct count, not a fault.

```bash
setsid nohup ./bin/overmind --socket data/overmind/unlock.sock --fleet data/overmind/unlock-fleet.yaml \
  --worker-bin bin/worker --status-file data/overmind/unlock-status.json \
  --history-file data/overmind/unlock-history.jsonl \
  --assets-db-path data/assets.db --stagger 10s \
  >> data/overmind/unlock-overmind.log 2>&1 < /dev/null &
```

### 3e. Mission-learn (40 of 41) — ~7 min

```bash
setsid nohup ./bin/overmind --socket data/overmind/mission-learn.sock \
  --fleet data/overmind/mission-learn-fleet.yaml \
  --worker-bin bin/worker --market-db-path data/market.db \
  --status-file data/overmind/mission-learn-status.json \
  --history-file data/overmind/mission-learn-history.jsonl \
  --assets-db-path data/assets.db --stagger 10s \
  >> data/overmind/mission-learn-overmind.log 2>&1 < /dev/null &
```

The roster is 41 but the launched count is lower — `mission-learn-overrides.json` holds a
`removed` list (1 entry as of 2026-08-14). A short fleet is not necessarily a fault; check
the sidecar before investigating.

---

## 4. Haul — last, and gated

**Do not launch haul on the same pass as the others.** Haulers route on the arbitrage pool,
and the pool is only as good as the market data underneath it. Coming up on a stale pool means
21 workers burning fuel chasing opportunities that no longer exist.

Wait for **both** gates:

1. **A marketbot capture cycle has landed.** `update_market` is scheduled `ten_minutely`, so
   this is a ≤10 minute wait once the fleet is healthy.

   ```bash
   sqlite3 data/market.db "select strftime('%H:%M',captured_at) m,
     count(*) rows, count(distinct station_id) st from market_orders
     where captured_at > strftime('%Y-%m-%dT%H:%M:%SZ','now','-40 minutes')
     group by m order by m;"
   ```

   Healthy (54-bot fleet, 2026-08-14): a ~40k-row burst across ~48 stations landing at
   every `:x0` bucket, with smaller trickle rows between bursts.

   **Compare `captured_at` against `strftime('%Y-%m-%dT%H:%M:%SZ',…)`, never `datetime(…)`.**
   The column is ISO-8601 (`2026-07-31T16:04:01Z`) while `datetime()` emits a space-separated
   form (`2026-07-31 16:04:01`), and `'T' > ' '` in a string compare — so a `datetime()` bound
   silently matches *every row of the day* and any window you ask for looks healthy. Times are
   stored in UTC; use `strftime('%H:%M', captured_at)` to read them, and add `'localtime'` only
   for display.

2. **The scanner has scanned against that data.**

   ```bash
   grep "^scan @" data/overmind/arbitrage-scanner.log | tail -3
   sqlite3 data/market.db "select count(*) from arbitrage_opportunities where status='available';"
   ```

   Healthy: ~320–400 available (2026-08-14 scale; it was ~98 when the mb fleet was 35).
   ~30 means the pool is starved and haul will idle.

Then:

```bash
setsid nohup ./bin/overmind --socket data/overmind/haul.sock \
  --fleet data/overmind/haul-fleet.yaml \
  --status-file data/overmind/haul-status.json --history-file data/overmind/haul-history.jsonl \
  --assets-db-path data/assets.db --secondment-ledger data/overmind/secondments.json \
  --stagger 10s \
  >> data/overmind/haul-overmind.log 2>&1 < /dev/null &
```

Since 2026-08-13 haul writes `haul-status.json` / `haul-history.jsonl` — it no longer uses the
default `fleet-status.json` / `fleet-history.jsonl`, so both flags must be passed.
`--secondment-ledger` is what lets a hauler that sells in nebula space nominate itself for the
pirate-unlock loan; without it the secondment pipeline silently gets no new nominations.

Expect fewer than 21 workers if any are seconded away (check `secondments.json` for
`phase=seconded`; those live in the unlock fleet until they graduate).

`--stagger 10s` is mandatory here regardless of pacing arithmetic: 21 workers is the fleet that
originally tripped the login limiter.

**Checkpoint:** within seconds of the first worker connecting you should see it claim work —
`haul: opp NNNNN <item>: buy N @<station> -> sell @<station>`. That single line proves the whole
chain (marketbots → market.db → scanner → pool → hauler) is live.

---

## 5. `market-prune` — after the fleets, and staged after downtime

> **This step caused the only real incident of the 2026-07-30 cold start. Read it before running it.**

The prune deletes `market_orders` rows older than `--retain`. In steady state that is a small
incremental slice every 30 minutes and it is harmless.

**After an outage longer than the retain window, it is not.** If the fleets were down for 6h and
the retain window is 4h, then *every row in the table* is older than the window, and the routine
restart becomes an unbatched `DELETE` of the entire table.

Observed on 2026-07-30 against a 20.4M-row table: ~10 minutes at 96% CPU, 64.5 GB written, WAL
inflated to 50 GB, and the write lock held throughout. Downstream, **all 35 marketbots failed
every `update_market`** — 144 × `database is locked (5) (SQLITE_BUSY)` across 45 scheduled fires,
with **zero rows landing** — and the scanner's first two scans died the same way.

**The diagnostic tell:** `market-prune` logs *nothing at all* while it runs — it only logs on
completion. So a silent prune process combined with `SQLITE_BUSY` everywhere else is the
signature. Confirm by watching its `wchar` climb into the tens of GB:

```bash
cat /proc/<prune-pid>/io | head -4
```

`kill -TERM` on the prune frees the lock immediately; SQLite rolls the transaction back cleanly
and captures resume within seconds.

**Staging the retain window does NOT work** — tried 2026-07-31 and it cannot produce a small
enough slice. `market_orders.bucket_utc` is **hour-granular**, so a sub-hour cutoff
(`--retain 16h30m`) deletes nothing at all, and the smallest slice the flag can express is one
whole hour bucket — 4.4-4.7M rows in normal traffic. That still does not finish inside two
minutes and still locks out the fleet while it runs.

**Use batched deletes instead**: many small transactions, so the marketbots' existing retries
absorb the contention rather than failing outright. Measured on 2026-07-31 — 50k rows per
transaction takes ~3.5s, clears ~12k rows/sec, and produced **zero** `SQLITE_BUSY` across a
26.6M-row cleanup with all 36 marketbots writing:

```bash
CUTOFF="2026-07-31T05:00:00Z"   # keep everything at/after this bucket
while :; do
  n=$(sqlite3 data/market.db "PRAGMA busy_timeout=15000;
      delete from market_orders
       where rowid in (select rowid from market_orders where bucket_utc < '$CUTOFF' limit 50000);
      select changes();" | tail -1)
  [ -z "$n" ] && { sleep 10; continue; }   # locked: back off, don't exit
  [ "$n" -eq 0 ] && break
  sleep 1.5                                 # duty cycle: leave the lock free between batches
done
```

Once the backlog is gone, the resident daemon's incremental 30-minute slices are small enough
that the flag-driven path is fine again.

Then start the resident daemon at the normal window:

```bash
setsid nohup ./bin/market-prune --db-path data/market.db --retain 4h --interval 30m \
  >> data/overmind/market-prune.log 2>&1 < /dev/null &
```

**Never use `--vacuum` to clear a backlog**, and never on a live fleet: VACUUM needs an exclusive
lock, meaning every overmind and all ~109 workers stopped. For a genuinely huge backlog the fast
technique is a fresh-DB rebuild-and-swap, not DELETE+VACUUM.

**Watch the WAL.** `data/market.db-wal` sat at 50 GB against a 19.9 GB database after the
incident. With a large fleet holding readers open continuously, a TRUNCATE checkpoint may never
get its exclusive moment, so the WAL does not shrink on its own.

```bash
ls -la data/market.db data/market.db-wal && df -h /home/robert | tail -1
```

---

## 6. The secondment daemon

`fleet-secondment` reconciles the haul↔unlock loans (nominate → drain from haul → run in
unlock → graduate → return). Like the scanner it is unsupervised — nothing restarts it, and
without it nominated haulers never move and graduated ones never come home. It performs no
game logins, so it can start any time after the haul and unlock overminds are up.

```bash
setsid nohup ./bin/fleet-secondment --watch 5m \
  >> data/overmind/secondment.log 2>&1 < /dev/null &
```

Defaults cover the rest (`--home haul --away unlock`, sockets, overrides sidecars,
`--ledger data/overmind/secondments.json`). `fleet-secondment --status` prints the ledger
read-only if you want to inspect it first.

---

## Checkpoints

Every overmind writes a status file with the same shape. This is the fastest whole-system read:

```bash
python3 -c "
import json
for f in ['haul','mb','assist','hunt','craft','unlock','mission-learn']:
    d=json.load(open(f'data/overmind/{f}-status.json')); ws=d['workers']
    print(f'{f:14} {len(ws):3d} workers  {sum(1 for w in ws if w.get(\"healthy\")):3d} healthy  '
          f'restarts={sum(w.get(\"restarts\",0) for w in ws)}  {d.get(\"overmind_commit\")}')
"
```

Healthy full system, 2026-08-14:

| fleet | workers | notes |
|---|---:|---|
| haul | 20–21 | `haul-status.json`; short by however many are seconded away |
| marketbots | 54 | |
| mission-learn | 40 | roster 41 − overrides sidecar |
| craft | 9 | plan runner banner required |
| assist | 5 | all should be home-docked with full tanks |
| hunt | 5 | |
| unlock | 25 | roster 46 − the 21 secondment-held haul agents |
| **total** | **~144** | **restarts=0, one shared `overmind_commit`** |

Data-layer health (2026-08-14 scale, 54-bot mb fleet):

| check | healthy value |
|---|---|
| stations captured, last 15 min (see the format warning above) | ~48 of 54 |
| rows per 10-minute capture burst | ~40,000 |
| `arbitrage_opportunities` available | ~320–400 (30 = starved) |
| `SQLITE_BUSY` in the mb log | 0 after the first minute |

---

## Traps specific to a cold start

**Status files survive the outage and lie until overwritten.** Every `*-status.json` still
holds the last pre-outage snapshot (full roster, all healthy), and a freshly launched overmind
takes a little while to overwrite it. Any script that waits on `len(workers)` to detect spawn
completion passes **instantly** against the stale file (observed 2026-08-05: a mission-learn
waiter saw "38 workers" from the Aug 2 file while the new fleet was at 1). Check
`overmind_commit` — or the file's mtime — matches the relaunch before trusting worker counts.

**Workers re-orient themselves — mostly.** On connect they backfill missed scheduled work
(`⏰ backfilling N missed scheduled task(s)`) and re-read live state, so stale cached positions
correct themselves: `dock` → `Already docked` → `refuel` is the normal, healthy sequence, not an
error. What does *not* self-correct is anything recorded in a queue file while the fleet was
down — most importantly a rescue record's POI, which is a snapshot and goes stale.

**Ships may have moved while you were down.** A drifting undocked ship gets auto-docked by the
Galactic Salvage Authority for a fee (`salvage.ship_recovered`), so an agent can come back up at
a station nobody routed it to. Trust `get_status` → `player.current_poi`, never a stored record.

**In-flight freight resumes only from disk.** There is no server-side listing of active
contracts; each agent's `data/agents/<id>/freight-held.json` is the only resume source. Do not
delete these during cleanup.

**`data/agents/*/schedule.json` churns constantly** — every worker rewrites `last_run`. This is
normal noise. Stage files explicitly when committing; never `git add -A` from the repo root.

**Do not infer the market cadence from the `⏰ [scheduled hourly]` log lines.** Those belong to
`kb_update`, `facilities`, and `capture_fuel`. `update_market` has its own `ten_minutely` entry
in the schedule, and it is the one that matters for haul. Read
`data/agents/<marketbot>/schedule.json` to confirm.
