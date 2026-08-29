---
name: reference_travel_priced_before_measured
description: "The server charges fuel for travel before checking distance, so a zero-distance move to the POI you already occupy is rejected as \"Insufficient fuel\" — an unsatisfiable precondition that mimics a strand"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T12:06:24.696Z
---

`travel <poi>` is priced BEFORE it is measured. Issuing it for the POI the ship
already occupies is not a no-op: at low fuel the server rejects it with
`Insufficient fuel for travel`, so a worker standing on its own destination can
be blocked by a fuel requirement for a move it does not need to make. At a
station whose desk is dry, that precondition can never be satisfied.

**This reads as a fuel strand and is not one.** The tell is a log pair:
`You are already at the target system.` immediately followed by
`Traveling to POI: <the poi already in the heartbeat>`. Compare the travel
target against `heartbeat poi=` before believing a fuel diagnosis.

Live 2026-08-13: explorer-1 was docked at `voss_redoubt` holding 420
liquid_hydrogen for opportunity 436396, whose sell station WAS voss_redoubt. 84
retries, ~40 minutes, against a sale it was docked on top of. Fixed by
`alreadyAtPOI` in `pkg/worker/autopilot.go` (commit `1df1b9d1`); the sale cleared
109,300 credits within seven seconds of the guard going live.

Related: [[reference_pin_arrival_check_four_directions]] is the same class of bug
one layer up — "am I already where I am being sent?" answered wrongly.
