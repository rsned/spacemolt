---
name: reference_sigstop_preserves_game_sessions
description: SIGSTOP/SIGCONT freezes the fleet with ZERO logins — game sessions survive, unlike a restart; the safe tool during an IP block
metadata:
  node_type: memory
  type: reference
---

**SIGSTOP/SIGCONT on workers preserves their game sessions completely. Resuming
costs ZERO logins.** Verified live 2026-08-23: 137 workers frozen for ~6 minutes
during an IP block, then resumed — every one continued heartbeating on its
existing session, no "connecting to game server", no "Logging in", no reconnect.
johnny_cab even banked credits (16,636,498 -> 16,641,568) across the freeze.

**This is the single most important tool during a rate-limit block**, because the
thing that deepens a block is fresh logins, and a restart is a fresh login.
Freezing is not.

**Order matters — freeze overminds FIRST, then workers.** A running overmind
watching frozen workers sees silence past `SilenceTimeout` (90s) and stall-kills
them, which is a restart, which is a login. Freeze the supervisor before its
children. Resume in the opposite order: **workers first, overminds last**, with
30-60s between, so the overminds read fresh heartbeats instead of waking to a
fleet that has been silent for minutes.

**The resume is still not free.** On 2026-08-23 waking 8 overminds after a 6-minute
freeze produced +38 silence-kill restarts (not the full 137 — most workers'
buffered heartbeats landed first). Two fleets, mb (54) and mission-learn (40),
then entered a restart loop at ~12 restarts/min EACH — ~24 logins/min against a
~10/min ceiling — and had to be stopped outright. The small fleets (assist, hunt,
craft, shuttle, unlock, haul = 58 workers) resumed clean and stayed stable.
**Expect the biggest fleets to be the ones that will not resume cleanly.**

**During the churn both lied about health**: mb reported `healthy=54/54` with only
32 worker processes alive, mission-learn `40/40` with 14. Trust
`ps` process counts over `*-status.json` when diagnosing churn —
[[reference_livelock_invisible_to_health_checks]] is the same blind-spot family.

**How to apply (the /proc scan trap applies):** collect PIDs into a file in ONE
tool call, signal in a SEPARATE call, and match on `bin/worker ` + `--agent` /
`bin/overmind --socket`. A scan whose own command line contains the pattern
matches its own wrapper shell and SIGSTOPs the script itself. Verify your own
`$$` is absent from the PID list before signalling, and confirm afterwards with
`ps -eo stat=` (all should be `T*`). Workers spawned between the scan and the
signal are missed — re-check for non-`T` stragglers.

Related: [[reference_login_rate_limits]] · [[reference_worker_quiesce_park]] ·
[[project_overmind_stall_kill_connect_loop]]
