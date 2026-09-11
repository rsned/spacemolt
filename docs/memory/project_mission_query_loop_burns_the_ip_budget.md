---
name: project_mission_query_loop_burns_the_ip_budget
description: "MEASURED 2026-09-09 — find_route + get_active_missions + get_missions are 63% of ALL fleet traffic; three workers emitted byte-identical tallies with zero accept_mission. This is the structural waste behind the IP blocks, not fleet size"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2d7bbf54-7b88-4de5-ba93-9477eb1cb891
  modified: 2026-09-10T03:05:31.292Z
---

**The first measurement we have ever had of what the fleet actually sends.**
`send_tally` (`fc6f5d41`) went live on the pool fleets at 2026-09-09 ~20:03 and
answered the question within four minutes.

## The numbers (10 worker-windows, 5 min each, unlock + mining roles)

| command | share |
|---|---:|
| `find_route` | **26.2%** |
| `get_active_missions` | **21.2%** |
| `get_missions` | **15.7%** |
| `refuel` | 9.2% |
| get_status | 6.0% |
| everything else | ~22% |

**Three mission queries = 63% of all traffic.**

**Rate, corrected.** The first pass measured 9.3 commands/min/worker, but that
sampled mostly FIRST windows, which are inflated by login + scheduled-task
backfill. Steady state (second windows onward):

| worker kind | rate | example |
|---|---|---|
| caught in the loop | **~7/min, 90-100% waste** | `salvager-3` 36 cmds, ALL of them 12/12/12 |
| healthy, doing work | **~3.6/min** | `explorer-12` 18 cmds: jump=6, accept_mission=1 |
| healthy miner | ~6/min, productive | `miner-1` 32 cmds of which **mine=26** |

**Always compare second windows.** A first window cannot distinguish a loop from
a busy startup — `pirate-11` read 54 then 21, while `pirate-12` read 49 then 42
with the same signature. The second number is the one that identifies a loop.

**This is the answer to "why do we get blocked when others run 1000 agents on
one IP".** An agent issuing ~1.5 useful commands/min lets 1000 agents coexist.
We issue 9.3, of which ~6 are re-asking questions nobody acted on. **Fleet size
was never the cause** — see the correction in
[[reference_ip_block_20260909_freeze_and_staged_thaw]].

## Bug 1 — the mission-query loop (the big one)

**Three workers emitted BYTE-IDENTICAL tallies:**

```
pirate-12 / pirate-15 / pirate-3:
  find_route=16 get_active_missions=12 get_missions=12 refuel=4
```

Independent agents converging on identical counts is a **deterministic loop**,
not opportunistic work. And **pirate-12 and pirate-15 show `accept_mission=0`**:
they fetch the board, fetch active missions, compute routes, accept nothing, and
repeat — ~40 wasted calls per worker per 5 minutes.

**A second, even cleaner signature landed minutes later** — `salvager-3`,
`salvager-8` and `trader-2`, all three:

```
find_route=13 get_active_missions=13 get_missions=13   (totals 45/45/44)
```

**All three commands at the SAME count.** That means one loop iteration issues
exactly one of each: 13 iterations in 300s = one every ~23s, which is the
`idle_ticks: 2` idle period (20s) plus the work. So **the mission pass runs on
EVERY idle pass and spends three queries before deciding it has nothing to do** —
there is no board cache, and no short-circuit for "nothing actionable here".

That also quantifies the cadence change: at the old one-tick period this was
~26 iterations = ~78 queries per worker per 5 min. `idle_ticks: 2` genuinely
halved it — while leaving each iteration just as wasteful.

Suspects to check first (all already documented):
[[reference_settled_mission_livelock]] (server lists a mission ACTIVE it refuses
to complete) · the dry-board backoff in the missionrunner roster notes, which is
supposed to park a worker that finds nothing acceptable ·
[[reference_missions_vacuous_test_trap]] (the existing tests may not cover the
loop at all).

## Bug 1b — `find_route` has a SECOND source: freight

Do not assume fixing the mission loop fixes `find_route`. `engineer-1` logged
**`find_route=34` in five minutes — one every 9 seconds — with `get_missions=0`**
and `shipping=16`. That is the freight/shipping route planner, not the mission
board. `shipping` itself runs 10-25 per window on the engineer/explorer workers
(`engineer-4` and `explorer-8` both at `shipping=25`).

So `find_route` (26% of all traffic) is fed by at least two independent callers,
and the freight one is the heavier of the two per worker.

## Bug 2 — the refuel loop

`pirate-2` and `pirate-4`, both: **`refuel=11` in 5 minutes** — one every 27s —
with `find_route=11` and `get_active_missions=11` at the *same* count. Matching
counts mean refuel is being retried inside the same failing pass rather than
succeeding and moving on. Suspect
[[project_no_fuel_cells_refuel_deadlock]] (`no_fuel_cells` ≠ `no_fuel_source`;
nothing ever BUYS a cell).

## How to re-measure

`scratchpad/tally_agg.sh`, or by hand:

```
grep -ho 'send_tally .*' data/overmind/*-overmind.log | tr ' ' '\n' \
  | grep '=' | grep -v '^total=' \
  | awk -F= '{a[$1]+=$2; t+=$2} END {for (k in a) printf "%8d %5.1f%% %s\n", a[k], 100*a[k]/t, k}' \
  | sort -rn | head -20
```

**Watch for identical tallies across workers** — that is the loop signature, and
it is far easier to spot than a raw rate.

## ⭐🟢 FIXED 2026-09-11 — `c1e4c169` (hunt) + `250634cd` (missions). NOT YET DEPLOYED.

### The budget model, corrected by the operator

`game_query` is **300/min per bot** (keyed on the character over WebSocket, on
the session over HTTP/MCP) — we were never remotely close, peak 23.2/min. But
**the counter that decides an IP block is shared across everything from the
address.** So a loop that looks fine for one agent adds up across the fleet and
blocks the whole lot. That, not the per-bot cap, is why the poll must be slow.
My earlier "different bucket, so not dangerous" was wrong.

Measured aggregate: **~630-765 requests/min fleet-wide**, ~44% of it these
three queries.

### Why skipping is safe (server team, 2026-09-11)

Boards hold two kinds of offer, and **neither rewards fast polling**:
- **Computed on request** from your own state — ship, skills, standing, what
  you already finished. Asking again immediately returns the same answer. Only
  a repeatable coming off cooldown or another player posting/cancelling moves
  it.
- **Postings that turn over** on a timer the SERVER owns. Your query does not
  trigger a refresh.

Prescribed shape: **slow poll while parked · keep last-seen mission ids and
diff, react to new ids · re-check after something changes on YOUR side**
(docked somewhere new, job finished or dropped, standing/skills moved).

### What shipped

Two gates, same reasoning, one constant
(`game.SleepMissionBoardPoll = 30 * SleepTick`, 5 min, operator-chosen).

| role | file | keyed on |
|---|---|---|
| hunt (35%) | `hunt_board_gate.go`, new `huntBoardGate` on WorkerDispatch | its own dry flag |
| unlock + missionrunner (63%) | `mission.go` `missionBoardGated` | the EXISTING `State.dry` |

⭐ **Reused `State.dry` rather than inventing a second idleness notion** — the
reposition/park logic already owns that concept and a parallel one would drift.

**Two bypasses, both load-bearing:**
- **Different station** → read at once; a new dock is a different board.
- **Last pass found work** → never gate. A worker holding an active mission is
  not dry and needs `get_active_missions` to find it; gating it would strand
  the job. Regression-tested in both roles.

⭐ **Gating the board read is NOT skipping the pass.** `missionReadBoard`
returning empty already falls through to freight ("an empty board is a prime
freight opportunity"), and freight is evaluated BEFORE the board read. This
matters because engineer-4/5 and explorer-8 run `shipping=24-36` per window
while showing the same wasteful query signature — they are earning, and the
gate leaves that untouched.

### Expected effect

A parked dry agent goes from ~12 board reads per 5 min to **1 per 5 min**.
Verify after deploy with the usual second-window rule, and by watching for the
new log line `board at <station> unchanged and last pass was dry`.

**Deploy still needed** — the fleet is running binaries from 2026-09-10 01:32.

## Status (superseded — see FIXED above)

NOT FIXED. Measured only. `idle_ticks: 2` on the pool roles (`094edfe0`) halves
how often these loops *start*, which buys headroom but leaves each loop just as
wasteful. Fixing the loops is the real win and is worth far more than the
cadence change.
