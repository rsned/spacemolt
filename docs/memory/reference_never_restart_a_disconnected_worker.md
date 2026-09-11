---
name: reference_never_restart_a_disconnected_worker
description: "FIXED a35f94c5 — DisconnectGrace used to fall through to a restart after 30min; with 70 workers behind one paced reconnect gate that grace ALWAYS expires, and the restart sends each worker to the back of the same queue. haul 16→0 in 7h"
metadata:
  node_type: memory
  type: reference
---

## The incident (2026-09-11, overnight)

A server-side event at **00:12:21-00:12:26** disconnected **70 workers within
five seconds** — `Disconnected: failed to get reader: use of closed network
connection`. Scope: haul 16, mission-learn 24, unlock 15, craft 9, assist 5,
shuttle 1. mining/hunt/mb were untouched.

They kept heartbeating, so the supervisor's `DisconnectGrace` branch correctly
left them to the fleet-wide reconnect gate. **For exactly 30 minutes.** At
00:42:30 the grace expired and the stall watchdog took the whole fleet:

```
stall watchdog: trader-5 frozen undocked in "Furud" for >15m0s
(last progress 2026-09-11T00:11:11); restarting
```

Result over the next seven hours: gate waits of 4+ minutes, workers SIGTERMed
before they ever authenticated, restart counters to 12, **haul 16 workers → 0**,
and 29 agents finally quarantined as `fuel-dead ... fuel 0/0` — state-read
failures, not dry tanks ([[reference_stale_rescue_records_outlive_the_strand]]).

## ⭐🔴 The rule

**A restart cannot reconnect a worker. Only the gate can.**

Restarting a disconnected-but-heartbeating worker is strictly negative: it
discards a live process, forces a FRESH login into the same contended gate, and
sends the worker to the **back of the queue** it was already waiting in. At
fleet scale that converts a recoverable blip into a self-sustaining outage.

And the arithmetic guarantees it fires: **70 workers cannot re-enter through a
paced host-wide gate inside 30 minutes**, so the grace always expires for the
tail of the queue. The timeout was not merely too short — any fixed timeout is
wrong, because the drain time scales with how many workers are waiting.

The code already contained the argument against itself: *"a restart here forces
a fresh login that cannot succeed during a block and deepens it."* It then did
it anyway once the timer ran out.

## The fix (`a35f94c5`)

`pkg/overmind/supervisor/supervisor.go`: the disconnect case has **no upper
bound**. A disconnected worker that keeps heartbeating is left alone
indefinitely.

**What still protects us:** the case a restart CAN fix — a wedged or dead
process — is a SILENT one, and the `SilenceTimeout` (90s) branch sits ABOVE
this one in the switch and fires regardless of connection state. Guarded by
`TestDisconnectedAndSilentWorkerIsStillRestarted`.

`DisconnectGrace` is kept but re-purposed: it now decides when this gets
**loud**, logging once per agent per incident —
`"<agent> disconnected for >30m and still waiting on the reconnect gate; NOT
restarting (a restart cannot reconnect it)"` — so a worker that never returns is
visible rather than silently parked. The flag clears when it goes healthy.

`TestDisconnectedWorkerRestartedAfterGrace` asserted the behaviour that caused
the outage and was replaced.

## ⭐ Diagnostic notes

- **The stall watchdog's "frozen undocked, last progress T" line is the
  tell-tale**, and `last progress` on EVERY worker being the same minute means a
  shared cause (a disconnect), not N independent wedges. Check the scope across
  fleets before treating it as a per-worker problem.
- **"no loop-iteration lines" separates a wedge from slow work.** A worker doing
  slow work prints `── [n/N]`; a wedged one prints nothing between its banner
  and the SIGTERM. Used this to clear the ExecuteLoop pacing fix of suspicion.
- The 2026-08-22 `CommandTimeout` fix (30min) can never fire before
  `StallTimeout` (15min) kills the worker — worth revisiting if a genuine
  single-command hang is ever suspected again.

Related: [[project_overmind_stall_kill_connect_loop]] (the still-open
connect-phase variant) · [[reference_standing_loop_wedge_after_reconnect]] ·
[[reference_login_rate_limits]]
