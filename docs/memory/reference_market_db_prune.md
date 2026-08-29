---
name: reference_market_db_prune
description: "market.db pruning daemon (retain 4h); daemons die silently on reboot; for a big backlog use fresh-DB rebuild-swap, NOT DELETE+VACUUM"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 1e4c8a31-2e69-4e33-8b4b-1c2956839940
  modified: 2026-07-31T02:48:15.802Z
---

## ⭐🔴 2026-07-30 — DOWNTIME ITSELF ARMS THE PRUNE TRAP. Never restart the daemon at `--retain 4h` after an outage longer than the retain window.
Every fleet was down ~6h (13:12→18:56). Relaunching `market-prune --retain 4h --interval 30m` with everything else meant **the whole 20.4M-row `market_orders` table was older than the retain window**, so the routine restart became an unbatched whole-table DELETE: ~10 min at 96% CPU, **64.5 GB written**, WAL at **50 GB**, write lock held the entire time. All 35 marketbots failed their `update_market` (**144 × `database is locked (5) (SQLITE_BUSY)`** across 45 scheduled fires, **zero rows landed**) and the arbitrage-scanner's first two scans died the same way. `kill -TERM` on the prune fixed it **instantly** — 2,149 rows across 7 stations landed within seconds, zero lock errors after.
- **The tell:** prune logs NOTHING while running (it only logs on completion), so a silent prune process + `SQLITE_BUSY` everywhere else = this. Confirm via `/proc/<pid>/io` `wchar` climbing into the tens of GB and ~96% CPU.
- **Safe restart after an outage = staged retain**, each pass deleting a survivable slice: `--retain 24h` once → `12h` → `8h` → then the resident `4h --interval 30m`. Same reasoning as the 07-14 "backlog delete is not safe" note, but the trigger here was *downtime*, not a retention change — which is why it ambushes an otherwise-correct relaunch.
- **Bring prune up LAST**, after the fleets are healthy and fresh captures exist — never in the same breath as the scanner and marketbots.
- Related: WAL sat at 50 GB against a 19.9 GB db afterwards (disk 86% full). Same shape as the 07-21 rebuild (62 GB db + 54 GB WAL); a 35-worker fleet holding readers open means a TRUNCATE checkpoint may never get its exclusive moment.

## 2026-07-14 incident — daemons died, DB ballooned to 47.5 GB, rebuilt to 2 GB
- **Prune daemon AND arbitrage-scanner die silently on host reboot/restart — nothing auto-restarts them.** Both died 2026-07-12; by 07-14 the unpruned firehose was 47.5 GB (~2.4 days). Always check they're alive: `pgrep -af 'market-prune|arbitrage-scanner'`. Relaunch lines: `nohup bin/market-prune --retain 4h --interval 30m >> data/overmind/market-prune.log 2>&1 &` and `nohup bin/arbitrage-scanner watch --interval 10m --offset 3m >> data/overmind/arbitrage-scanner.log 2>&1 &` (**cadence is 10m/+3m as of 2026-07-30**, not the 15m this note and [[reference_overmind_launch_commands]] used to claim — read the scanner log's own `watch:` banner before assuming).
- **For a LARGE backlog (>1 day), do NOT use `market-prune --vacuum`.** Its `PruneOrders` is a single-transaction `DELETE FROM market_orders WHERE bucket_utc < ?` of 100M+ rows — even WITH the `idx_orders_bucket` index it runs unbounded (>50 min, WAL ballooned past 25 GB, never committed in a test). An in-place `DROP`+VACUUM inside a txn also journals all freed pages (WAL → tens of GB).
- **FAST technique = fresh-DB rebuild-and-swap** (all writers must be down): build a brand-new file, copy only the keep-set + all other tables into it, verify, swap. Writes only the ~1% kept, no mass mutation, no VACUUM needed (born compact). **47.5 GB → 2.04 GB in ~45 s.** Steps: dump `CREATE TABLE`/`CREATE INDEX` from `sqlite_master`; `sqlite3 new.db` create tables → `ATTACH old` → `INSERT … SELECT * FROM old.t` (market_orders gets `WHERE bucket_utc >= strftime('%Y-%m-%dT%H:%M:%SZ','now','-4 hours')`, all others full) → create indexes → `PRAGMA integrity_check` + `foreign_key_check` → `mv old aside; mv new into place`. Old 44 GB kept as `data/market.db.prevac-bak` until fleet verified.
- Other-table sizes (2026-07-14, for cost estimation): market_orders ~128M rows (~44 GB, 99% of file); market_ohlcv 3.68M; arbitrage_opportunities 769k; fleet_timeseries 34k; haul_results 6k; items 684; stations 40; analyses 0.
- **VACUUM/rebuild needs ALL writers down = every overmind + all ~75 workers, not just manual play_as.** Fleet workers hold market.db open read-write for their whole lifetime (`lsof data/market.db`). Stop via `kill -TERM` on the 5 overmind PIDs (each cancels ctx → aborts its workers). Relaunch lines in [[reference_overmind_launch_commands]] (+ assist/craft: see that note's update).

---
### (prior notes — retention mechanics)

`data/market.db` is the single volatile-market store (~13 GB as of 2026-06-27). Pruning **IS wired up** — but as a **running daemon, not cron/code**, so grep the process list, not just cron/systemd/scripts:

```
./bin/market-prune --db-path data/market.db --retain 12h --interval 30m   (pid varied)
```

**Table roles (key to retention):**
- `market_orders` — raw capture firehose: ~592 bytes/row, **~0.85–2.4M rows/hour** across the ~40 marketbots. The ENTIRE 13 GB is just ~11–12h of this. The 12h daemon keeps it at a **stable ~13 GB steady-state — NOT runaway growth**.
- `market_ohlcv` — durable rolled-up candles: ~504k rows spanning **7 days**. **Never pruned** (`PruneOrders` only deletes `market_orders`), so pruning raw orders loses zero historical price data. This is the long-term substrate; live tooling (arbitrage scanner re-scans /30min, hauler gate recaptures live) only needs the last capture or two.

**To shrink the file (e.g. user chose 4h retention → ~3–4 GB):**
- A live 12h→4h switch is **NOT safe**: the one-shot ~14M-row backlog DELETE locks the `market_orders` write table for **minutes** (an 8h-step test timed out >2 min), stalling marketbot captures. Incremental per-cycle deletes (one aged bucket) are fine — the fleet tolerates them — but a backlog delete is not.
- `DELETE` alone never shrinks the file; only `VACUUM` reclaims, and **VACUUM needs an exclusive lock = ALL writers stopped**: the haul overmind (haul.sock) AND the marketbot overmind (mb.sock) AND pause the prune daemon.
- **Do it in the next operator-gated restart downtime** (the same one deploying empty-book + display-name + drain + re-added salvager-9/trader-9 — see [[project_current_status]]):
  1. stop haul overmind + marketbot overmind; `kill` the 12h prune daemon.
  2. `./bin/market-prune --retain 4h --db-path data/market.db --vacuum` (slow delete + shrink, but no writers to block). 13 GB → ~3–4 GB.
  3. relaunch both overminds; start the new daemon: `nohup ./bin/market-prune --db-path data/market.db --retain 4h --interval 30m >> data/overmind/logs/market-prune.log 2>&1 &` (start it at ~:15/:45 so its incremental delete lands mid-gap between the :00/:30 marketbot captures).
- Durable scheduling (survives reboot) is a future nicety — overmind-integrated prune ticker or a systemd unit; today it's a nohup daemon.

## ⭐🔴 2026-08-27 — ROOT CAUSE: the retry policy, not the database

`market: write failed after 5 attempts: database is locked (5) (SQLITE_BUSY)`
does NOT mean the DB is wedged. `pkg/market/collector.go` had
**`maxRetryAttempts = 5`** against a **5s `BusyTimeout`**. All ~153 workers hold
`market.db` open **read-write** (`lsof` shows fd `13ur` on every one, not just
the marketbots), and against that much contention 5 tries gave up while the lock
was merely contended.

Proof the DB was healthy: a plain write with `busy_timeout=20000` returns
`WRITE OK`, and `pragma wal_checkpoint(PASSIVE)` flushed **45,837 of 45,845**
WAL pages. No wedged writer, no stuck transaction.

**The marketbots were hitting it too, chronically** — a steady **4-6 lock errors
per minute** in `mb-overmind.log`, every minute, silently losing captures. That
predates the 08-26 outage.

FIXED in **`a474d01d`** (committed, UNDEPLOYED — needs a worker rebuild + fleet
restart): `maxRetryAttempts` 5 → **24**, `DefaultConfig().BusyTimeout` 5s →
**15s**, and a new `retryDelay()` capping the exponential backoff at **2s**
(uncapped, attempt 24 would wait over an hour).

## ⭐🟢 2026-08-27 — BATCHED DRAIN beats the rebuild-swap when workers are live

The 07-14 fresh-DB rebuild-swap below is still the fastest way to shed a huge
backlog, but **its cost is a full fleet stop**: every worker holds the file
read-write, so an `mv` swap leaves all 153 writing into an orphaned inode. That
is 153 fresh logins to recover.

The 07-30 trap was specifically an **unbatched** whole-table DELETE holding the
write lock ~10 min. Batched, the same delete is safe with the fleet fully live:

```
DELETE FROM market_orders WHERE rowid IN (
  SELECT rowid FROM market_orders WHERE bucket_utc < <cutoff> LIMIT 50000);
```
in a loop, `busy_timeout=60000`, 2s between batches. Script kept at
`scratchpad/batch_prune.sh '<-4 hours>' <batch>`.

**Measured 08-27: lock errors ran 4-6/min BEFORE the drain and 2-6/min DURING —
the drain added essentially nothing.** Cleared 5,967,168 rows in 34 min on a
contended file; a second pass over contiguous fresh rows ran ~500k/min.

Two gotchas:
- `sqlite3 "pragma busy_timeout=N; ...; select changes();"` prints the pragma
  value as its own output line. Parse `tail -1`, or the loop reads the timeout
  as the row count and exits immediately.
- Capture is ~1.5M rows/hour at 52 marketbots, so **a backlog re-forms while you
  drain**. Re-check `older_than_4h` after finishing and drain again before
  starting the daemon, or its first pass meets a multi-million-row single
  transaction — the 07-30 trap all over again.

This reclaims rows, not disk: the file stays at its high-water mark with free
pages that later inserts reuse. Shrinking still needs VACUUM with all writers
down — worth a real downtime, not a manufactured one.

