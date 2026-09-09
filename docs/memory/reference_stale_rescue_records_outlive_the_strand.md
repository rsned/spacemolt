---
name: reference_stale_rescue_records_outlive_the_strand
description: 2026-09-09 — all 6 quarantined agents were ALREADY FINE; the rescue records were stale by days. Check the live position before planning any rescue
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2d7bbf54-7b88-4de5-ba93-9477eb1cb891
  modified: 2026-09-09T06:00:53.225Z
---

**2026-09-09: cleared the whole strand list — 6 of 6 agents needed nothing but
their record deleted.** Fleet went 164/170 → **170/170 healthy, queue empty.**

The four `status:failed` records all carried the same tell: **the server's own
error names a DIFFERENT location than the record**.

| agent | record said | server said | reality |
|---|---|---|---|
| fighter-7 | ivorygate_belt, 0/90 | "they are at **The Veil Anchor**" | moved |
| fighter-10 | cargo_lanes_freight_depot, 5/130 | "**tank is already full (130/130)**" | fine |
| explorer-1 | sol/saturn, 0/100 | "they are at **Sol Central**" | at a fuel desk |
| engineer-1 | ironhearth/hearthfire, 0/380 | "they are at **Ironhearth Station**" | at a station |

The two `status:pending` assist records were worse: **empty `system_id`**
("operator must fill it"), so no rescuer could ever claim them — and both agents
were **fully functional** when inspected with `play_as`:

- **assist-sol**: docked Procyon Colonial, **50/100 fuel, 1,834,595 credits**
- **assist-krynn**: docked The Crucible Garrison, **134/140 fuel**, Siphon
  (a refueler, NOT a Tanker — fitted pump + 2 mining lasers), 1,834,597 credits

This **supersedes [[reference_assist_fleet_is_dry]]** for sol and krynn — both
were refunded/refuelled at some point and the memory never caught up. All five
assist tankers are now live; three completed rescues the same night
(trader-10, trader-5, trader-1).

**Rule: read the live position before planning a rescue.** A rescue record is a
snapshot that goes stale in hours, and nothing in the pipeline re-reads it. The
error text on a failed attempt is free live intel — mine it before dispatching
anyone. A quarantined agent's session is FREE, so `play_as <id>` with
`get_status` costs nothing and needs no fleet stop.

**Clear procedure** (verified twice now): take the flock on
`rescue-queue.json.lock`, append the records to `rescue-history.jsonl`
verbatim, atomically rewrite the queue. `pollRescues` logs
`no record for quarantined <id>; releasing` and the worker relaunches within
~60-90s. Script kept at
`scratchpad/clear_rescue_records.py <agent-id>...`; there is still no CLI tool.

**Still open: assist-sol flies a `theoria` Miner t0 (100 fuel), not a tanker** —
it lost its Capacity at algol on 08-15 and has never been re-hulled, so it is a
refueller that cannot refuel. It holds 1.83M credits and two tier-2 tankers were
listed on 09-09: **Capacity 91,120 at Nova Terra Central** and **Morningstar
114,477 at Crimson War Citadel**. Buying one is the
[[reference_assist_tanker_migration]] six-step procedure — and **never skip
step 5, `switch_ship`**.

See [[reference_rescue_queue_blocks_launch]] · [[reference_gsa_ship_recovery]] ·
[[project_rescue_pipeline_bugs]]
