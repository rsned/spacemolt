---
name: reference_livelock_invisible_to_health_checks
description: "A busy-but-useless worker passes every health check we have; how to actually find one (grep 'held for next pass' at tick cadence)"
metadata:
  type: reference
---

**Every fleet health signal we run measures NOISE, not PROGRESS.** `fleet-watch` alerts on
log SILENCE, unmatched disconnects, and worker-count drops; the overmind's stall watchdog
fires on `SilenceTimeout` (90s). A worker spinning a futile loop is loud, healthy,
restarts=0, and 100% hull — it passes all of them indefinitely.

Measured cost: johnny_cab ran **14,244 iterations over 47 hours** (2026-08-19 22:01 →
2026-08-21 21:08) doing nothing, and was found only because the operator asked an
unrelated question. See [[project_pirate_reputation_unlock_campaign]].

**How to find one.** The tell is a per-agent line repeating at tick cadence. Cheap sweep:

```
grep -ao "held for next pass\|will retry next pass\|no travel needed" data/overmind/*-overmind.log \
  | sort | uniq -c | sort -rn | head
```

Then bound it per agent — `grep -ac "<agent>.*held for next pass" <log>` — and get the first
occurrence with `grep -am1` to date the onset. A count in the thousands with a first
occurrence days back is a livelock, not churn.

**Known generator:** a completed leg whose "already there / already docked" early return is
also the condition its completion check requires. Same shape as
[[reference_sell_leg_dock_gap]] and the pin-arrival bug in
[[reference_pin_arrival_check_four_directions]]. Three separate sites now — treat
"already X" as a state to RECONCILE, never as an error to retry.

**Unbuilt and worth building:** a progress signal per worker (missions retired, fares
delivered, credits delta) that fleet-watch can alert on when it flatlines while the log
stays busy.
