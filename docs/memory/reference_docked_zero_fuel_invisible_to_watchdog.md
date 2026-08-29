---
name: reference_docked_zero_fuel_invisible_to_watchdog
description: "A docked worker at 0 fuel is invisible to the stall watchdog, so a depleted station fuel desk strands agents that nothing in the fleet can see or rescue."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 00cd813a-f76a-48cf-bb7a-0c47c76e1566
  modified: 2026-08-26T18:07:42.844Z
---

`supervisor.Stalled()` opens with an early return:

```go
if info.LastStatus.Docked || info.LastStatus.Drained || info.LastStatus.Quiesced {
    return false
}
```

**Docked is treated as inherently safe**, because a docked ship can normally
refuel at the station desk. `Stranded()` gates on `Stalled()`, so a docked
worker is never quarantined and never gets a rescue-queue record — and the
assist role is queue-driven, so no record means no rescuer is ever dispatched.

That assumption breaks when a station's fuel desk runs dry. Live 2026-08-26:
Haven's `grand_exchange` was attacked and its `fuel_reserve` fell to **0 of
500,000 capacity** while repairs were incomplete. Seven agents (miner-6,
pirate-1, pirate-5, pirate-13, salvager-9, salvager-10, and assist-sol
itself) sat docked there at 0 fuel. Every health check read normal.
pirate-1 alone burned **7,193** `get_missions` retries against a board that
was also offline. assist-haven was docked at the same station the whole time
holding 3,988/4,000 fuel with a working refuel rig, idle, because its queue
was empty.

Server error codes that identify this state:
- `station_fuel_empty: This station's fuel reserves are depleted. Buy fuel
  cells from the market and use them directly.`
- `no_fuel_cells: No fuel cells found in cargo.` (the cargo fallback path)

The worker has **no buy-fuel-cells fallback** — it calls station refuel, gets
`station_fuel_empty`, and gives up. Buying locally is also a stranded-premium
trap: grand_exchange listed `fuel_cell` at 2,200-3,000 against 575 at
the_core.

**Recovery that works:** hand-write a `pending` rescue record into
`data/overmind/rescue-queue.json`. Adding a record does NOT quarantine the
worker — quarantine is driven by the supervisor's own `Stranded()` check, not
by a record existing. The nearest assister claims it (BFS distance 0 wins
immediately) and pumps a full tank. See
[[reference_ship_to_ship_refuel_works_while_docked]].

Two code fixes this argues for, neither done yet:
1. `Stalled()` should not treat docked-at-zero-fuel as safe when the
   station's desk is empty.
2. The refuel path should honor the server's own instruction and buy fuel
   cells when `station_fuel_empty` comes back.

Related: [[reference_worker_quiesce_park]] ·
[[reference_livelock_invisible_to_health_checks]] ·
[[reference_station_fuel_price_spread]] · [[reference_gsa_ship_recovery]]
