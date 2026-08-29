---
name: reference-facility-rent-cycle
description: Facility rent cycle = 100 ticks ≈ 17 minutes; 86.4 cycles per real-time day
metadata: 
  node_type: memory
  type: reference
  originSessionId: 73e356b1-e0c3-4d21-8b72-cfe61b9cf707
---

A facility **rent cycle is 100 game ticks**. With `pkg/game/constants.go` `SleepTick = 10 * time.Second`, that's 1000 seconds ≈ **16.67 minutes per cycle** (which the in-game UI rounds to "~17 minutes").

For real-time conversions:

```
seconds_per_cycle = 100 ticks × 10s/tick = 1000s
cycles_per_day    = 86,400s ÷ 1000s    = 86.4
daily_rent        = rent_per_cycle × 86.4
hourly_rent       = rent_per_cycle × 3.6
```

Integer-safe formulation (no float drift): `perCycle * 86400 / 1000`.

Used by `cmd/tools/play_as/main.go` `formatFacilityList` to show a "Daily rent" column alongside per-cycle in the Personal facilities table (commit forthcoming). The `rent_paid_until_tick` field in `facility list` responses is in game ticks, so multiply by 10s to get real wall-clock time to next rent.

Related: [[reference_facility_list_field_omissions]] — server currently omits `rent_per_cycle` from faction facility list entries, so daily-rent display falls back to 0 unless we consult the catalog.
