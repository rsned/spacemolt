---
name: reference_crash_loop_cap_parks_agents_forever
description: An agent that hits MaxRestarts=100 without going healthy is parked FOREVER; dashboard remove/readd does NOT free it (fixed 2114df0e)
metadata:
  node_type: memory
  type: reference
---

**`s.restarts` is cleared in exactly ONE place** — the `case seen && w.Healthy`
branch of the supervisor reap loop. So an agent that reaches `MaxRestarts`
(default **100**) without ever coming up healthy is stuck permanently:
`tryRestart` returns early, so it never launches, never reports healthy, and
never clears its own counter. **Only restarting the whole overmind frees it** —
for mb that is 54 logins to recover one agent.

**The documented remedy did NOT work.** Live 2026-08-23, after a ~30-minute
network outage, `marketbot_000`, `marketbot_003` and `miner-1` sat at exactly
`restarts=100`, `seen=false`, `last_seen=0001-01-01`, no process. Dashboard
`remove` + `readd` returned `{"status":"accepted"}` for every one and changed
nothing: the **overrides sidecar** and the fleet's **WorkerInfo** are not where
the crash-loop counter lives.

**The refusal was completely silent** — no log line said why a worker with no
process was not being relaunched. That is what made it expensive to find. Look
for the signature instead: `restarts` exactly at 100 (or the cap) **with
`seen=false` and an empty role/system/POI** in `<fleet>-status.json`. An agent
with live position/credit data that is merely unhealthy is a DIFFERENT problem.

**FIXED `2114df0e`** — `memberAdd` now clears both the counter and the
log-once flag, so readd is a real escape hatch, and `tryRestart` logs the
refusal once per agent:
`worker "X" parked at the crash-loop cap (100 restarts); it will NOT relaunch
until readd or an overmind restart`.
**The fix only takes effect once each overmind is restarted onto it** — until
then the old remove/readd is still a no-op and a fleet restart is the only cure.

**How to apply:** after any long outage, check for agents pinned at the cap
before assuming the fleet self-healed — `healthy=N/M` hides them, and they
never recover on their own. Space remove/readd calls ~30s apart so the
relaunches do not become a login burst [[reference_login_rate_limits]].

Related: [[reference_sigstop_preserves_game_sessions]] ·
[[project_overmind_stall_kill_connect_loop]]
