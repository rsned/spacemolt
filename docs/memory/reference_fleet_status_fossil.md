---
name: reference_fleet_status_fossil
description: A status file no overmind writes still parses, so tools read a 17-day-old snapshot as live — this is where "legacy binary workers" came from
metadata:
  type: reference
---

`data/overmind/fleet-status.json` was last written **2026-08-12 19:59** and no
`fleet.sock` overmind has run since. A workstation reboot does NOT clear it: it
is a file on disk, not process state.

**Symptoms it produced, all misleading:**

- **"legacy binary workers"** — 19 entries frozen at the version they reported
  on 08-12, all with the identical `last_seen 02:59:23`. Nothing was actually
  running an old binary. **A fleet whose 19 workers share one heartbeat second
  is a fossil, not a fleet.**
- `salvager-3`/`salvager-9` appeared never-started while a live salvager-9 ran
  in haul. Inflated the off-map count.
- Empty `version` renders as "legacy" on the dashboard — those are quarantined
  agents that never started, so never reported one.

**FIXED `3743e471`:** file deleted; `fleet.yaml` **emptied but kept**, because it
is the DEFAULT `--fleet` value and a bare `bin/overmind` would re-launch all 27
entries alongside the fleets already running them (two logins as one agent →
the server kicks one). `fleet-history.jsonl` KEPT — history, not a snapshot.
`fleet-report` / `haul-distances` / `haul-dashboard` defaulted `--status-file`
to the deleted file and now default to `haul-status.json`.

Five of its entries were never real: `hauler-1..3`, `resident-nebula-1..2` — no
`data/agents/` dir, no assets row, no game account.

**Generalise:** the dashboard has no staleness gate. Any `*-status.json` whose
newest heartbeat is old still renders as current — `mining-status.json` is in
the same state now (fleet deliberately stopped).
Part of [[reference_capture_loss_taxonomy]] mechanism 6.

## 2026-09-18: the fossil can also fake a DEAD fleet

`data/overmind/mission-status.json` (mtime **2026-07-20**, contents from
07-17) made health sweeps report a `mission` fleet at 0/1 with engineer-1
"64 days stale". Nothing was wrong: the `mission` fleet was superseded by
`mission-learn` in July, its overmind was stopped, and the status file was
simply left behind. engineer-1 has been running fine under `mission-learn`
the whole time (23 restarts, active, 1.26M cr). Restarting fleets for 64
days changed nothing because **there was no process to restart** — nothing
reads or writes a retired fleet's status file.

So the fossil cuts BOTH ways: it can make a dead fleet look alive (the
original `fleet-status.json` case) or a healthy one look dead (this).

**Deleted the file 2026-09-18** (untracked; backed up first). True fleet
state was then 168/169, the only stale worker being hauler-0.

🔴 **Guard for any health sweep that globs `*-status.json`:** check the
file's MTIME first and skip anything older than ~1 hour as a retired fleet,
instead of counting its workers as dead. Nothing cleans these up when a
fleet is retired, so there will be more.

**2026-10-23 — `activity` in `<fleet>-status.json` is STICKY, not live.** It holds the last task string a worker
was assigned (e.g. `Opportunity #1687396 · buying up to 1350 …`) and is never cleared when the worker goes idle:
14 drained haul workers showed byte-identical activity strings for 45 minutes while their logs were nothing but
5-minute `send_tally` idle reconciles. The status file also carries no `drained` field. To know whether a hauler is
really busy, read its recent non-heartbeat log lines (`grep '\[worker:<id>\]' haul-overmind.log | grep -v heartbeat
| tail`) — `send_tally` with only faction_info/get_skills/get_status/list_ships/shipping = idle. The overmind's
SIGUSR1 drain poll gives up after ~3 min (`drain poll ended`), so it cannot confirm completion either.
