---
name: reference_idle_loop_ran_3x_per_tick
description: "The worker idle loop defaulted to SleepTick/3 (3.33s), running 3 passes per game tick and ~43/sec fleet-wide — the steady-state floor behind the recurring IP rate-blocks."
metadata:
  node_type: memory
  type: reference
---

`RunStanding` defaulted `IdleInterval` to `game.SleepShort`, and **SleepShort is
`SleepTick/3` = 3.33 seconds**, not the 5s CLAUDE.md claims
([[reference_sleep_constants_actual]]). So every worker ran an idle pass **three
times per game tick** — about **43 passes per second across 144 workers**.

Nothing useful can happen at that rate: the game advances once per 10s, and
**every mutation is capped at 1 per tick per agent** (105 of 216 commands in the
v0.566 spec carry `Rate limited: This is a mutation command (1 per tick /
10 seconds)`). Two of every three passes could only emit redundant calls.

Most passes DO short-circuit locally in `WorkerDispatch.redundant` — the
telltale log is a repeating pair every 3-4s:

```
22:51:23 dock: already docked; skipped   refuel: tank full; skipped
22:51:26 dock: already docked; skipped   refuel: tank full; skipped
```

That guard **fails open by design**, so anything it cannot positively confirm
redundant gets sent — which is how a sub-tick loop still reaches the server.

FIXED: `IdleInterval` now defaults to `game.SleepTick`, and the defaults moved
into a testable `applyStandingDefaults`. One tick is the floor because it
matches the mutation limit, and the blocking dispatch adds each command's own
response time on top (the operator's framing: 10s tick + ~10s response).

## Why it was hard to see

**Log volume went DOWN across the window the blocks began.** On 2026-08-27 the
IP was blocked from 08:30 to 13:09 — escalating 120s -> 1,759s — yet every
fleet's hourly log line count fell from 03:00 to 09:00. There was no burst to
find. It was the steady-state floor, so looking for a spike finds nothing.

Two other false leads, both recorded so they are not re-chased:
- Blamed stranded miners spamming `find_route`. Wrong — mining made **13**
  find_route calls all day, and the top "offender" was only heartbeating.
  Per-agent block counts measure who was TALKING during an IP-wide block, not
  who caused it.
- Blamed `ensureHome`'s `FindRoute`. Wrong — it early-returns when parked at
  home, and `ensure_home` appears **zero** times in any fleet log.

Related: [[reference_login_rate_limits]] · [[reference_login_vs_reconnect_gating]]
