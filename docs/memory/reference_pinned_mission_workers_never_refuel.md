---
name: reference_pinned_mission_workers_never_refuel
description: "A pinned worker on a mission role runs no idle script, so it never refuels once it stops travelling — unlike a resident, which tops up every pass"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T04:29:51.467Z
---

Refuelling is not a property of being docked — it is a property of the ROLE:

- **resident** roles run `data/scripts/resident_market.smolt` on every idle pass,
  which is literally `ensure_home / dock / refuel`. They top up forever, for free.
- **mission roles (e.g. `unlock`) run no idle script at all.** Their only refuel is
  the one autopilot performs while travelling. A pinned mission worker that stops
  travelling therefore **never refuels again**.

That is how marketbot_alhena and marketbot_sheratan reached 0/130 fuel while docked
at voss_redoubt and thane_keep holding 242,722 and 197,230 credits, beside fuel desks
publishing a working all-in price of 20. Nothing in their loop was ever going to ask.
The pin-alias drain ([[reference_station_id_aliases]]) emptied the tanks; the absent
refuel step is why they stayed empty.

**Fixed** (`topUpAtPin`, pkg/worker/mission.go): a pinned worker refuels as it parks —
docked, idle, and about to sit for `missionParkWindow`, which rate-limits it to one
attempt per window. Broke or desk-empty is logged and it parks anyway.

**Diagnostic that made this visible:** grep the fleet log for the agent and `refuel`.
Nothing at all — not even a failure — means the role never calls it. A depleted desk
would have logged an error; silence means nobody asked.

**Watch for:** any new role built on the mission loop rather than a resident script
inherits this. `roles.yaml` already carries a 2026-06-30 note that idle_mine was
paused for stranding bots at 0 fuel — same class of bug, different trigger.

Related: [[reference_station_fuel_price_spread]] · [[project_refuel_timing_endpoint_choice]]
