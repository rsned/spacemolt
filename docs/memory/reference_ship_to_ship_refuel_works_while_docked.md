---
name: reference_ship_to_ship_refuel_works_while_docked
description: "Ship-to-ship refuel works dock-to-dock at the same POI, and the client's cached fuel stays 0 afterwards even though the server shows a full tank."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 00cd813a-f76a-48cf-bb7a-0c47c76e1566
  modified: 2026-08-26T18:07:57.496Z
---

`RefuelShip(ctx, targetUsername, qty)` (wire type `refuel` with a `target`
payload, needs a `refuel_rig` fitted) **works while both ships are docked at
the same POI**. Neither ship has to undock.

Verified live 2026-08-26: assist-haven, docked at `grand_exchange`, rescued
miner-6, also docked there, in **2 seconds** — `assist: rescued miner-6 (+130
fuel to Barnaby 'Bedrock' Burke)`. Every prior successful rescue had been to
an undocked target at a belt, which made dock-to-dock look untested and
probably-broken. It is neither.

Note `rescue_fuel` in the queue record is a floor, not a cap: a record asking
for 30 got a full 130-unit tank.

## The client's fuel reading goes stale after an external refuel

After the transfer, **every client-side view still read `fuel 0/130`** — the
overmind status file, the worker's heartbeat, and its own route planner
(`WARNING: Fuel low (0%) and no fuel cells in cargo!`). The server disagreed:
miner-6's very next refuel attempt returned

```
tank_full: Fuel tank is already full (130/130). No refueling needed.
```

and it then jumped Haven → Trader's Rest → onward without trouble. So an
externally-applied fuel transfer never invalidates the client's cached ship
state.

**Do not conclude a rescue failed because the status file still shows 0.**
Confirm against the server: a `tank_full` reply, or successful jumps.

This likely also explains stale terminal rescue failures of the form
`travel: insufficient fuel for route: route to X needs ~N fuel, 0 available`
sitting at `attempts: 5` in the queue (engineer-1, assist-krynn, assist-nexus,
trader-10 as of 2026-08-26) — those ships may actually have had fuel while
the client believed they had none. Worth re-checking before treating them as
operator-only cases.

Related: [[reference_docked_zero_fuel_invisible_to_watchdog]] ·
[[reference_client_cargo_used_drifts_upward]] (same shape of bug: client
state drifting away from server truth) · [[reference_gsa_ship_recovery]]
