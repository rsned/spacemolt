---
name: reference_duplicate_haul_overmind_corrupts_status
description: "TWO haul overminds ran for 7 days on the same socket+fleet file; the one owning ZERO workers kept writing haul-status.json with a frozen roster, so the status file and the dashboard roster both lie"
metadata:
  type: reference
---

**Found 2026-10-10.** Two `./bin/overmind --socket data/overmind/haul.sock
--fleet data/overmind/haul-fleet.yaml` processes, started 6 minutes apart on
2026-10-03, both still running 7 days later. Both held a LISTEN on the same
unix socket path (the later bind replaced the inode).

**All 18 live haul workers had `PPid=352512`. PID 350742 owned ZERO workers**
— an orphan supervisor, spawning nothing, but still writing the shared
`haul-status.json` and printing fleet tables into the shared
`haul-overmind.log`.

## The symptom is a status file that alternates between two realities

Consecutive reads of `haul-status.json` gave `workers: 16` then `workers: 22`;
the `removed` set reported agents as removed that were provably running; the
same agent appeared twice in the log 5 seconds apart with different systems
and different restart counts (one frozen at `restarts: 100`, the
MaxRestarts cap — see [[reference_crash_loop_cap_parks_agents_forever]]).

**This cost me a false diagnosis.** I read `fleet: None` for six haulers off
the dashboard roster (which reads the status file) and reported them as
"orphaned, in no fleet, idle" — then issued readds. They were running the
whole time: `ps -o lstart=` on their worker PIDs showed starts hours earlier.
Their genuinely low trip counts came from having been seconded to the unlock
fleet during the measurement window.

## How to tell truth from the fossil

- **Authoritative:** scan `/proc/*/cmdline` for `worker --agent` and read
  `PPid` from `/proc/<pid>/status`. Worker start time: `ps -o lstart= -p <pid>`.
- **Not authoritative:** `haul-status.json`, the `removed` array, the
  dashboard `/api/overmind/roster` `fleet` field — all downstream of the file
  the orphan clobbers.
- Related fossil modes: [[reference_fleet_status_fossil]] (a status file 17
  days dead) and torn reads (the file rewrites every ~2.5s).

**Do not kill a supervisor before mapping PPid ownership** — the one that
looks stale by socket inode is not necessarily the one without workers.
Killing 350742 is safe precisely because it owns none.

Also beware: `grep 'removed from fleet'` over this log matches SEPTEMBER
lines. Always anchor the date — the same bad-grep trap as
[[reference_fleet_status_fossil]].
