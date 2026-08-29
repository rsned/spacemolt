---
name: reference_drone_bay_is_agent_wide
description: get_ship's drone_bay is an agent-wide ledger — deployed drones keep working in another system and survive a ship switch
metadata:
  type: reference
---

**Deployed drones belong to the AGENT, not the hull.** Confirmed 2026-08-28 by
the operator: craftsman-1 launched 25 mining drones in one system, then switched
hulls and flew a `survey_vessel` to Haven. `get_ship` still reported
`deployed_count: 25`, `bandwidth_used: 250`, `in_bay: []` — the drones stayed
mining in the other system the whole time.

So `drone_bay` in a `get_ship` reply is an agent-wide ledger, NOT a description
of what is physically aboard. Reading `deployed_count` as "drones here with me"
is wrong, and `in_bay: []` means "none stowed", not "none owned".

`bandwidth_used` is 10 per drone (25 drones -> 250).

**Reporting gap:** `bay_capacity` and `bandwidth_total` both came back **0** in
the same reply that reported 25 deployed and 250 used, so `get_ship` cannot tell
us remaining drone headroom. Matters for [[project_fleet_drone_refit]]
(175 bays / 800 drones).

See [[reference_ship_modules_never_captured]] — same shape of problem: the
capture exists but the totals do not.
