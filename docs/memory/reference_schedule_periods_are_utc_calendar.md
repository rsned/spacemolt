---
name: reference_schedule_periods_are_utc_calendar
description: "Worker schedule frequencies are UTC calendar periods, not intervals: a weekly one-off run Saturday fires AGAIN at Sunday 00:00 UTC (16:00 PT); remove one-offs right after they land"
metadata:
  type: reference
---

Seen live on 2026-11-21. Weekly purified_water delivers ran Saturday morning, then all fired again at 16:00–17:48 PT, which is Sunday 00:00 UTC.
- craftsman-4 re-ran Dross, Mera, Thane and Korr.
- craftsman-5 re-ran Crix and Voss, plus its "one-off" Korr cargo task.
- Every stronghold marketbot got a second 1,000 water.

`weekly` means "not yet run this UTC calendar week", not "7 days since last_run". `daily` is the same per UTC day.

What this means:
- A "weekly one-off" is only safe until the next Sunday 00:00 UTC. Remove it as soon as it lands.
- A one-off added on a Saturday PT can fire twice within hours.
- Pair this with [[reference_schedule_due_batch_not_cancellable]].
