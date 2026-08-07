# Mission + Freight Scan-Rate Backoff — Design

Phase 1 of dialling down board polling: gate the *search for new work* in the
mission/freight path behind a per-agent interval. Phase 2 (deferred, sketched at
the end) lifts the same mechanism into the shared idle loop for every role.

## Problem

Every worker role runs a shared idle loop (`pkg/worker/standing.go:105`) that
re-runs its idle script every `IdleInterval`, defaulting to `game.SleepShort`
(`SleepTick/3` ≈ 3.3s). Measured against a live worker on 2026-08-06, the real
board re-read cadence is **~6 seconds** — 14,400 reads per worker per day, and
roughly 547k/day across the 38-worker mission fleet alone, with a freight poll
riding the same pass.

Boards do not change anywhere near that fast. The result is pure waste:

| Source | Lines logged 2026-08-06 |
|---|---:|
| `missions: no acceptable missions on this board` | 132,280 |
| `freight: no candidate (...)` | 123,941 |
| `missions: no board entries here` | 43,387 |

The overmind logs now total **12.5 GB** (mb 4.5G, haul 3.4G, mission-learn 3.0G).
The cost is server calls, worker CPU, and a log volume that makes real events hard
to find.

**The governing constraint, from the operator:** an action costs a tick to execute
plus a tick to hear back, so `2 × SleepTick` = **20s is the hard floor** below which
nothing can have changed — polling faster is definitionally wasted calls. For
boards specifically the useful interval is far slower still: **60s at the fastest,
3–5 minutes at rest.**

## What is gated — and what is not

This is the crux of the design, and the reason it is safe.

A `Missions()` pass does far more than read a board. It reconciles held freight,
builds a fuel model, evaluates candidates, repositions, docks, and delivers. The
code says so explicitly: *"reconcile must precede EVERY early return"*
(`pkg/worker/mission.go:437-439`). Throttling the whole `missions` command would
strand in-flight freight and stall transits.

So the gate covers **only the search for new work**:

- the mission board read (`get_missions`) and its candidate evaluation
- the freight board read (`shipping list`) and its candidate evaluation

and explicitly **not**:

- freight reconcile of already-held contracts
- delivery, transit, docking, refuelling
- debt payment
- anything driven by the scheduler rather than the idle script

When the gate is closed, the pass performs no server call for new work and emits
no "no candidate" line. That is where both the call volume and the log lines go.

## Cadence

Two states, one timestamp per agent, no ladder to reason about:

| Last board read | Next read |
|---|---|
| produced work (mission accepted, freight taken) | **now + 60s** |
| came back dry | **now + 4 min** |

60s is the operator's floor for when things are happening; 4 minutes is the middle
of the stated 3–5 minute resting range. At rest this is ~40× fewer polls than the
measured 6s cadence.

The self-managing property matters: there is no explicit "fast mode window" to
expire, because the *next* dry read relaxes the interval on its own.

## The dry-pass interaction

`missionDryPassLimit = 3` (`pkg/worker/mission.go:25`) counts consecutive dry
passes to decide when to reposition to another station.

**A gated pass must not count as a dry pass.** A skipped read is not evidence the
station is empty. Without this, the dry counter would still tick at ~6s while real
reads happened every 4 minutes, so repositioning would fire roughly 40× sooner in
wall-clock terms and workers would hop constantly — burning fuel and tick budget to
chase boards they never actually looked at.

This is the one place the change can cause real harm, so it gets an explicit test.

## Where it lives

A small self-contained gate in `pkg/worker`:

```go
// pollGate throttles the search for new work. Zero value polls immediately.
type pollGate struct{ nextAt time.Time }

func (g *pollGate) shouldPoll(now time.Time) bool
func (g *pollGate) noteResult(foundWork bool, now time.Time)
```

Held on the existing per-agent `missionRunState`, which is already the home for
cross-pass memory like `parkedUntil` (`pkg/worker/mission.go:69-76`). It is
in-memory, so a worker restart simply polls once immediately — the correct
behaviour, and no persistence to get wrong.

Keeping it a named type rather than two inline fields is what makes phase 2 a lift
rather than a rewrite.

## Testing

Table-driven against an injected clock (the mission path already injects `NowFn`):

- gate closed ⇒ the board read is not issued at all, and no "no candidate" line is
  emitted
- a dry read sets the next read to +4min
- a productive read sets the next read to +60s
- **a gated pass does not increment the dry counter** (the repositioning guard)
- in-flight freight reconcile still runs on a gated pass — the safety property the
  whole gating design rests on

## Rollout

The change is inert for any worker until its binary is replaced, and behaviour
change is confined to how often new-work searches fire. Roll it to one
mission-fleet worker first and confirm from its log that board reads land ~4
minutes apart at rest and ~60s apart after an accept, then fleet-wide.

Expected signal: mission-fleet log growth drops by roughly an order of magnitude,
and `missions: no acceptable missions on this board` becomes rare rather than
dominant.

## Deferred: phase 2, the generic idle-loop backoff

The same `pollGate` lifted into `RunStanding` to drive `IdleInterval` adaptively
for **every** role, which is where the haul (3.4 GB) and marketbot (4.5 GB) log
volume lives.

Deliberately not done in phase 1: haul's arbitrage pool genuinely refreshes every
10 minutes and polling it too slowly costs real credits, so it should not change
behaviour on the same pass that fixes the mission fleet. Phase 2's floor is the
principled `2 × SleepTick` = 20s rather than the 60s used for boards — different
loops, different useful minimums.
