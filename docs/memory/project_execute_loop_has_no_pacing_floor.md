---
name: project_execute_loop_has_no_pacing_floor
description: "ROOT CAUSE of the 2026-09-09/10 IP block — a mine timeout desyncs with the server's pending action, then an unpaced `loop -f 100 mine` retries into instant 'already pending' refusals at 51/min, crossing the 30/min per-session game_mutation cap. FIXED"
metadata:
  node_type: memory
  type: project
---

## The server's actual limit (first time we have ever seen it)

Captured by the `fc6f5d41` eventLogger, 950 times in the mining fleet:

```
rate_limited bucket=game_mutation limit_per_min=30 current=30
msg="Rate limit reached: game actions are capped at 30/min for this session
     (30 so far this window). Actions resolve on a 10-second tick, so there is
     no benefit to sending them faster than that. Retry in 55 seconds."
```

⭐🔴 **30 mutations/min, PER SESSION.** One agent can trip it alone. That is the
whole answer to "how do others run 1000 agents on one IP" — fleet size was never
the variable. And **the tick is 10s, so ~6/min is the only USEFUL rate.**

## ⭐🔴 The causal chain (corrected — see the dead end below)

A healthy `mine` is tick-bound: send, wait a tick, get the yield. That is ~3-6
per minute and **cannot** reach 30. The burst needs a trigger:

1. `mine` is sent. The server acks **"OK: Mine action pending. Will execute on
   next tick."** — 12,480 of these in two hours, it is the normal reply.
2. Our terminator (`client.go` `Mine`) accepts only `MiningYield` /
   `ActionResult` / an error. **The pending-ack is none of those**, so the
   client keeps waiting and can hit `SleepActionStartTimeout` (3 ticks / 30s).
   94 such timeouts in two hours.
3. The loop treats the timeout as an ordinary error. `-f` tolerates it and
   fires the **next iteration immediately**.
4. But the server still holds that mine pending, so the retry is **refused
   INSTANTLY: "Another action is already pending (mine). Wait for it to
   complete."** — 447 of these. A real round-trip, **costing no tick**.
5. `ExecuteLoop` had no pacing floor, so the loop spun at wire speed.
   **fighter-7 issued 51 mine attempts in the 21:44 minute** (its neighbouring
   minutes: 6, 6, 3). That crosses 30/min.
6. `rate_limited` also returns instantly → sustains the spin → violations pile
   up → IP block, escalating toward the 30-minute cap.

**Only waiting a tick can clear step 4.** Retrying is guaranteed useless.

### Dead end: the 99-iterations-per-second bursts sent NOTHING

At 20:07:11 eleven workers each spun up to 99 iterations in one second. It looks
damning and it is NOT the cause: 878 of those iterations errored `not connected`
(post-reconnect-storm) and **put zero bytes on the wire**. Same defect, no
server impact. Do not re-derive the block from those numbers — check the error
TEXT before attributing a burst to traffic.

## The fix — SHIPPED

`pkg/worker/loop.go` + `pkg/worker/loop_rate_limit.go`, tests in
`loop_pacing_test.go`:

1. **`ErrRateLimited` aborts a loop even under `-f`.** `-f` tolerates
   *failures*; a rate limit is the server saying stop. Joins `*TokenError` and
   `context.Canceled` as fatal-under-force. Classified on `*game.ServerError`
   code (`rate_limited`, `ip_timed_out`) **and on message text**, because the
   MCP transport delivers a message and nothing else.
2. **An iteration that errored and was tolerated by `-f` sleeps
   `game.SleepTick` before the next one.** A SUCCESSFUL command already cost a
   tick and is deliberately left unpaced, so healthy loops lose no throughput —
   only the pathological fast-fail spin is capped.

This bounds every fast-failing body, not just this one: `already pending`,
`not connected`, and the 2026-08-26 "Nothing to mine here" case (documented in
`data/agents/random-3/scripts/idle_mine.smolt`, where a failed jump left the
agent docked and the loop ran 100 mine attempts per pass — filed then as a
routing bug, really the first sighting of this defect).

**NOT changed, deliberately:** the `mine` terminator. Accepting the pending-ack
as terminal would make `Mine()` return immediately on every success and let the
loop spin at full speed — strictly worse. The timeout is now harmless because
the retry is paced.

**Also unaffected:** the Go-native `mineLoop` in `mine_qty.go` already slept
`SleepTick` between calls. Only the SCRIPTED path was unpaced.

Context: `loop -f 100 mine` in `data/scripts/idle_mine.smolt`; the `-f` count was
raised 25 -> 100 on 2026-08-21, which quadrupled the burst ceiling.

## ⭐🟢 VERIFIED IN PRODUCTION 2026-09-10 02:26

Staged thaw of all 9 fleets onto the paced binaries (`scratchpad/thaw_fleet.sh`,
one fleet at a time, `--stagger 10s`, abort if block lines climbed 15 over
baseline). **126 workers up, ZERO blocks through the entire thaw.**

| metric | before | after |
|---|---:|---:|
| peak `mine` iters / worker / **second** | **99** | **1** |
| peak `mine` / worker / **minute** (fighter-7) | **51** | **6** |
| `mine` per 5-min window (random-npc) | 72 | 15 |
| three mission queries, share of all traffic | **63%** | **29.3%** |
| `find_route` share | 26.2% | **4.1%** |

Several workers settled at exactly `mine=30` per 5-min window = **6.0/min, the
tick ceiling** — the loop now does as much work as the game can absorb and no
more.

⭐ **`rl_aborts=0`** — the rate-limit abort path never fired. Pacing alone held
us under the cap. The abort is the backstop, not the mechanism; if it ever
starts firing, something else regressed.

**Thaw order that worked:** shuttle → assist → hunt → **mining** (the specimen,
early and small so the fix is observed with a small blast radius) → craft →
haul → mission-learn → unlock → **mb last** (51 workers ≈ 11 min of continuous
logins, the only real `session_auth` risk in the run).

**Rebuild ALL worker binaries, not just the one you are testing:** hunt was
running a stale `bin/worker-gate` from 09-09 and would have kept the bug.

Related: [[reference_rate_limit_buckets_and_escalation]] ·
[[project_mission_query_loop_burns_the_ip_budget]] (the QUERY-side waste, a
separate and much milder problem) · [[reference_sigstop_preserves_game_sessions]]
