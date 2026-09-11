---
name: reference_stale_rescue_records_outlive_the_strand
description: "A rescue record with max_fuel=0 is a STATE-READ FAILURE, not a dead ship — 54 of 55 records after the 2026-09-10 block were false. Verify live before rescuing; the pattern is now 12-for-13"
metadata:
  node_type: memory
  type: reference
---

## ⭐🔴 `max_fuel=0` means "state never read", NOT "empty tank"

**This one field settles it.** A live ship ALWAYS reports `max_fuel > 0`. A
rescue record reading `fuel 0/0` with an empty `system` and `poi` means the
worker never got any ship state at all — it could not reach the server. The
watchdog then reports the silence as `fuel-dead: stalled >15m undocked, fuel
0/0`, which reads exactly like a real strand and is not one.

**2026-09-10, after the IP block: 54 of 55 records were this artifact.** All 55
were stamped inside the block/freeze window (05:19–08:52Z). Live checks on a
sample were 4-for-4 false:

| agent | record said | actually was |
|---|---|---|
| salvager-5 | fuel-dead, undocked, 0/0 | **docked** ramens_rest, fuel **71/95** |
| trader-4 | fuel-dead, undocked, 0/0 | **docked**, fuel **95/100** |
| explorer-2 | fuel-dead, undocked, 0/0 | **docked**, fuel **130/150** |
| marketbot_algol | fuel-dead, undocked, 0/0 | **docked** dross_citadel, **120/120** |

Historic pattern now **12-for-13**: every batch of stale records checked has
been already-fine. Verify live, then clear in bulk — do not rescue.

## The cost of not checking

The one REAL record (miner-3) shows what the noise buys you. It sat at
**Alpheratz, fuel 4/150 — parked AT a station** ("Starlight Cartographer") but
undocked. See [[reference_sell_leg_dock_gap]]: standing at a POI is not being
docked. Five assist tankers were dispatched across the galaxy, all five failed
(with the IP-block error, not for anything to do with miner-3), and it was
marked UNRESCUABLE.

**The actual repair, in three commands: `dock` (already docked) → `refuel` →
`no_fuel_source` → gift credits → refuel succeeds. Total cost: 146 CREDITS.**
It had 0 credits; the station pump was right there. See
[[project_no_fuel_cells_refuel_deadlock]] — nothing ever BUYS a cell, and
"no fuel source" here meant "no money", not "no pump".

⭐ **Check for a station AT the strandee's own POI before dispatching anything.**

## Runbook

1. `python3 -c "import json;..."` over `data/overmind/rescue-queue.json`; split
   on `max_fuel==0 and not system` — those are artifacts.
2. Sample-verify 3-5 with `./bin/server-cmd --agent=<id> --cmd=get_status`
   (one login each). Look at `ship.fuel/max_fuel` and `player.docked_at_base`.
3. `python3 scripts/clear_rescue_records.py <ids...>` — flock-guarded, archives
   to `rescue-history.jsonl`. Back the queue up first.
4. **`kill -HUP` the overmind to release** — `restoreQuarantine` runs before the
   supervisor, so the record must be gone AND the overmind must re-read.
   Verified: `assist-haven` went healthy within 15s of the HUP.
5. Result 2026-09-10: **170 workers, 171/171 healthy, rescue queue EMPTY.**

## ⭐ Gifts address by USERNAME, not agent_id

`send_gift --payload recipient=miner-3` fails `player_not_found`. It needs the
in-game name — `Derrick 'DeepCore' Drill` — which the rescue record helpfully
carries as `target_username`.

## ⭐🔴 2026-09-11: it RECURRED with no block at all — the wedge manufactures them

29 records reappeared overnight (24 artifacts + 5 real) with **zero IP blocks
and zero rate_limited precursors in the whole window**. So the block was never
the cause of false records; it was only an unusually large trigger.

**The real generator is the reconnect wedge.** Workers connect, print the agent
banner, and then emit NOTHING — no loop-iteration lines, no commands — until
the stall watchdog SIGTERMs them at 15 minutes. Restart, wedge, repeat:
restart counters reached 2-12 across the haul fleet, and the watchdog
eventually writes a `fuel-dead ... fuel 0/0` record, which quarantines them.
haul went 16 -> 0 workers over ~7 hours this way.

**The diagnostic that separates wedge from slow-loop:** grep the worker for
`── [` iteration lines in its final minutes. A worker doing slow work prints
them; a wedged one prints nothing at all between its banner and the SIGTERM.
Used this on 2026-09-11 to clear the ExecuteLoop pacing fix of suspicion — a
paced `loop -f 100` takes 100x10s = 16m and would ALSO cross the 15m stall
threshold, so this was a real possibility and had to be checked, not assumed.

See [[reference_standing_loop_wedge_after_reconnect]] and
[[project_overmind_stall_kill_connect_loop]] — this is those two bugs
compounding, and it will keep eating the fleet until one of them is fixed.

## ⭐ The "real" strandees were ALSO just broke, 5 for 5

All five non-artifact records were **docked at a station with 0-2 credits** —
four at BD+20 2457, one at the_anvil_arsenal. Not one needed a tanker. Their
rescue attempts failed because the RESCUER was "not connected" (the assist
fleet was wedged), which is logged as if the strandee were unreachable.

Total repair cost for all five: **1,566 credits** (176 + 196 + 278 + 720 + 196).
Fix is gift-credits then `refuel`. Combined with miner-3 the day before, that
is **6 for 6 where the answer was money, not fuel delivery**.

⭐🔴 **Stop dispatching tankers before checking `docked_at_base` and `credits`.**

## ⭐ Apostrophes in usernames: never pass a name through `xargs`

`send_gift` needs the exact in-game name, and many carry apostrophes —
`Fortress 'Fight' Fisher`, `Scott 'Scrapper' Jr.`, `Derrick 'DeepCore' Drill`.
Piping through `xargs` STRIPS them and the gift fails `player_not_found`, which
reads like a wrong recipient rather than a quoting bug. Read the name from the
record and pass it as a list arg to subprocess, or quote it in the shell:
`--payload "recipient=Fortress 'Fight' Fisher"`.

Related: [[reference_rescue_queue_blocks_launch]] ·
[[reference_docked_zero_fuel_invisible_to_watchdog]] ·
[[project_execute_loop_has_no_pacing_floor]] (the block that manufactured these)
