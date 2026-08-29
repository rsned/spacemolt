---
name: reference_lawless_transit_vs_idle
description: Crossing Lawless space is routine at any pirate reputation; only sitting idle there is dangerous
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T21:19:13.160Z
---

**Travelling THROUGH Lawless space (no police protection) is not dangerous**, at any
pirate reputation including the default −30 hostile baseline. Every hauler, miner,
mission runner and taxi shuttle crosses lawless systems dozens to hundreds of times a
day without incident. User correction, 2026-08-13.

**The risk is spending long periods IDLE in lawless space** — parked in the open, at a
belt, at a star POI. Duration in the open is the variable, not reputation and not the
security rating of systems you pass through.

**Why this matters:** all 16 open player stations the probe found are in Lawless
systems (player stations get built in unclaimed territory). I concluded from that
that an agent needed the pirate unlock before it could be posted to one — *wrong*, and
it would have blocked eight ready marketbots behind an unnecessary unlock campaign.

**How to apply:** do not gate a posting on the pirate unlock just because the
destination or the route is Lawless. Ask instead whether the role leaves the agent
idle in the open there:

- `resident` — SAFE. Its idle script `resident_market` is `ensure_home; dock; refuel`:
  it stays docked and never undocks to a belt.
- `resident_gas` / `resident_ice` — would undock to a gas cloud / ice field and sit
  there, which IS the exposed case. (Both currently point at `resident_market`,
  paused since 2026-06-30, so they behave as `resident` today — check before
  assuming.)
- Mining and hunt roles work in the open by design; those are the ones where lawless
  space is a real consideration.

The pirate unlock still matters for what it actually buys: docking at a warlord
stronghold and not being attacked on sight while doing stronghold work. See
[[project_pirate_reputation_unlock_campaign]] and
[[reference_ironlight_combine_dev_faction]].
