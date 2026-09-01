---
name: reference_rescue_queue_blocks_launch
description: An "off-map" agent is a QUARANTINED one — restoreQuarantine runs before the supervisor, so a queued agent is never launched and shows restarts 0 with a zero last_seen
metadata:
  type: reference
---

**"Off-map" on the dashboard == a record in `data/overmind/rescue-queue.json`.**

`cmd/overmind/main.go` Step 3a calls `restoreQuarantine(...)` explicitly BEFORE
`sup.Run`, "so restored quarantines take effect before the supervisor launches
anyone". A quarantined agent is therefore **never forked** — which is why the
symptoms are `restarts: 0` and `last_seen: 0001-01-01T00:00:00Z` (Go zero time).
A dashboard rendering "seen 17756551h" is just *now minus zero*.

**This is the mechanism working, not a bug.** Diagnose the QUEUE, not the
launcher.

2026-08-29, 14 queued. Two classes:

- **Genuinely stranded (out of fuel):** assist-nexus 0/1500, assist-krynn 4/140,
  engineer-1 1/380, fighter-3 1/160, random-1/npc/clark + fighter-9 at 0.
- **Healthy but still held:** **trader-1 at 240/240 — a FULL tank docked at
  Haven grand_exchange** — plus trader-10 210/720, trader-6 72/150. A
  quarantined agent cannot release itself; **delete its rescue-queue record**
  ([[reference_gsa_ship_recovery]]). trader-1 also holds a nominated
  pirate-unlock slot while idle.

**Two POIs are fuel traps**, each holding agents from different fleets:
`treasure_cache_gas_plume` (assist-krynn, engineer-1) and Nexus Prime's
`null_matter_anomaly` (assist-nexus, fighter-3).

Related: [[reference_assist_fleet_is_dry]] — the rescuers are stranded too.

## Operator clear procedure (VERIFIED 2026-09-01)
Deleting a record releases the agent at the NEXT STATUS TICK — no overmind
restart needed: `pollRescues` treats a quarantined worker with no record as
"operator resolved" and calls ReleaseQuarantine. Procedure: under
`flock rescue-queue.json.lock`, move `status:failed` records into
rescue-history.jsonl and rewrite the queue. 14 agents freed this way in one
pass; every overmind logged "no record for quarantined X; releasing" within
seconds. Genuinely-still-stranded agents simply re-stall and re-file FRESH
records with live positions — that is the self-healing path, use it.
Docked-at-0-fuel agents never re-file (watchdog blind); hand-write a
`status:pending` record for those (fields: copy a history row; target_username
from data/agents/<id>/credentials.json).
