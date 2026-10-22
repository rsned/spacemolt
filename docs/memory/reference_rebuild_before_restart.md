---
name: reference_rebuild_before_restart
description: "A fleet restart deploys NOTHING unless bin/ is rebuilt first — binaries sat 18 days / 55 commits stale while every status file reported healthy. Plus: SIGUSR1 drains a fleet, and the 2026-10-22 nine-fleet rollout recipe."
metadata:
  node_type: memory
  type: reference
  originSessionId: 2d7bbf54-7b88-4de5-ba93-9477eb1cb891
  modified: 2026-10-22T08:08:17.953Z
---

## ⭐🔴 Restart ≠ deploy

On 2026-10-22 all **169 workers and 9 overminds** reported
`built=2026-10-03T09:17:26Z, commit=c6b24f8645dc` — **55 commits behind HEAD**,
including the whole facility-capture fix set. `bin/worker` and `bin/overmind`
on disk were *also* from 10-03.

Earlier that week marketbot_alhena had been removed and re-added, which *did*
relaunch a worker — **from the same stale binary**. Nothing warns you. The tell
is `built_at`/`commit` in any `*-status.json`.

**Always `go build -o bin/worker ./cmd/worker` (and overmind) BEFORE stopping
anything.** Back up first: `cp bin/worker bin/worker.<date>.bak`. Rebuilding
while workers run is safe (running processes hold their inode), but any
crash-restart after the rebuild silently picks up the new binary.

## Draining — `SIGUSR1`

`cmd/overmind/main.go:154` — **SIGUSR1 drains a fleet**: broadcasts drain, then
polls `DrainProgress()` for **60 × SleepShort ≈ 5 minutes**, logging
`drain: N/M idle — still busy: [...]`, then
`drain poll ended ... (force-stop with SIGTERM if needed)`. It **never
force-aborts** — stragglers are surfaced only. Workers stop taking *new* work
and stand down at their role's safe point.

Only **haul** really needs this (in-flight trips). On 2026-10-22 it took 21
workers from 2 full 1,900-unit holds to small residue in ~15 min with zero
aborted trips. There is also `--remove-drain-timeout 0` = wait indefinitely.

Draining costs **no logins**, so start haul's drain early and run it in
parallel with other fleets' restarts.

## Rollout recipe (worked 2026-10-22, 169 workers, zero losses)

Login pacing is the whole game: **~10 logins/min per IP**; `--stagger 10s` is
~6/min for ONE fleet, so **one fleet at a time**, ~60s between. Two staggering
at once is ~12/min and over the line; the failure is silent (workers sit at
`restarts=0` with no connect line).

1. Capture exact cmdlines first: `tr '\0' ' ' < /proc/<pid>/cmdline`. Per-fleet
   flags differ and are easy to drop — craft `--plan-queue`, mission-learn/mb
   `--market-db-path`, haul `--secondment-ledger`, **all** `--assets-db-path`
   (omitting it makes captures silent no-ops).
2. Order smallest-first to prove the binary: shuttle(1) → assist(5) → hunt(8) →
   unlock(19) → mission-learn(23) → mining(27) → craft(0) → **mb(64)** → haul.
3. `kill -TERM <overmind-pid>`; it logs `sending abort to connected workers` →
   `overmind shutdown complete` in ~2s.
4. **Confirm ZERO procs on the socket before relaunching** (`ps -eo args | grep
   '<fleet>.sock' | grep -v grep`). Relaunching early risks two workers for one
   agent fighting over the session (`session_replaced`).
5. `rm -f data/overmind/<fleet>.sock`, then `setsid nohup ./bin/overmind … &`.
6. Verify with `commit` in the status file, not process count.

**Never `pgrep -f`** — the pattern matches the watching command itself, so an
`until ! pgrep -af 'agent X'` loop never exits.

## Schedules ARE editable

`data/agents/<id>/schedule.json` is plain JSON; removing an entry works. The
"[[reference_schedule_seeding_is_additive_only]]" rule is about **roles.yaml →
agent** seeding only. On 2026-10-22 `get_system_agents` was pruned from **65**
agents (retired by `d9873d8f`; it aliased to GetNearby, which every role already
scheduled at the same ten-minutely cadence = ~9,360 redundant calls/day). Edit
the file, then restart the fleet to pick it up.
