---
name: reference_client_cargo_used_drifts_upward
description: "FIXED 2026-08-22 (uncommitted, not deployed): deposit_items never cleared the client's cargo list, so Ship.CargoUsed climbs forever and CargoCapacity is set to FREE space — cargoFree goes negative fleet-wide"
metadata:
  type: reference
---

**Found 2026-08-22 from a dashboard screenshot showing `cargo 232/125`.** The agents
are not over capacity; the client's cargo accounting is wrong. Two bugs, both in
one three-line handler at `pkg/game/client.go:3959`:

```go
case "deposit_items":
    if cargoSpace, ok := result["cargo_space"].(float64); ok {
        c.state.Ship.CargoCapacity = cargoSpace     // <-- FREE space, not total
    }
    // <-- and it NEVER removes the deposited items from c.state.Ship.Cargo
```

**Bug 1 — CargoUsed only ever grows.** `Ship.CargoUsed` is recomputed CLIENT-SIDE by
summing `c.state.Ship.Cargo[].Quantity` (client.go:4071 and :4154). Depositing does
not remove the items from that slice, so every mined unit stays counted forever.

**Bug 2 — capacity is the wrong field.** Per `server_docs/openapi.json`,
`DepositItemsResponse` carries BOTH `cargo_remaining` and `cargo_space`, and
`WithdrawItemsResponse` carries `cargo_space` + `cargo_total`. `cargo_space` is the
FREE space. Capacity is `used + free`. **`cargo_space` at client.go:3960 is the ONLY
one of the three the client reads anywhere — `cargo_remaining` and `cargo_total` are
never used, and there is no withdraw handler at all.**

**Proof (server vs client, same agents, 2026-08-22):** `agent_hulls.cargo_used`
comes from the server's ship payload; the status file comes from client state.

| agent | server says | client says | drift |
|---|---|---|---|
| miner-10 | 0 | 182 / 100 | climbing (148 → 182 in ~1h) |
| prophet-2 | 0 | 168 / 125 | climbing (125 → 168) |
| overmind | 0 | 150 / 75 | climbing (115 → 150) |
| miner-9 | 0 | 98 / 50 | |

**This is NOT cosmetic.** `cargoFree := Ship.CargoCapacity - Ship.CargoUsed` goes
NEGATIVE at `pkg/worker/mission.go:776`, `pkg/worker/haul.go:1118` and `:856`, and
`pkg/skills/expr.go:203` computes a bogus cargoPct. A worker whose client believes
it has negative free space will decline to load. **Check whether this contributes to
[[project_haul_revenue_halved_v0547]] before assuming allocation is the whole story.**
Mining is unaffected only because the mine loop stops on the SERVER's `cargo_full`
(a GoalReachedError), not on this local check.

**FIXED 2026-08-22 in `pkg/game/client.go` (uncommitted, NOT yet deployed — running workers still carry the old binary).** in the `deposit_items` case take both fields —
`CargoUsed = cargo_remaining`, `CargoCapacity = cargo_remaining + cargo_space` — AND
reconcile `c.state.Ship.Cargo` by decrementing the deposited `item_id`/`quantity`,
because :4071/:4154 recompute CargoUsed from that slice and would clobber the fix
on the next mine. Shared client, all 161 workers: needs tests and a fleet redeploy.


## What the fix does

`case "deposit_items"` now takes `CargoUsed = cargo_remaining`,
`CargoCapacity = cargo_remaining + cargo_space`, and calls a new
`(*Client).removeShipCargo(itemID, qty)` to keep `Ship.Cargo` truthful — without
that last part, the recompute sites regrow the bogus figure on the next mine.
Guarded on `source` being empty or "cargo": a `source: storage|faction` deposit is
a cross-storage transfer that never touches the hold.

**Wire-type trap:** `cargo_space`/`cargo_remaining`/`quantity` are INTEGERS on the
wire (operator, and the OpenAPI schema agrees), but they reach this handler through
`encoding/json` into `map[string]any`, which decodes every JSON number as float64.
The `.(float64)` assertions are correct — "fixing" them to `.(int)` makes them
silently never match. There is no `UseNumber` anywhere in the client.
`serverapi.DepositItemsResponse` already exists with proper int fields
(`CargoRemaining`, `CargoSpace`); this handler predates it and still uses the map.

Tests: `pkg/game/deposit_cargo_test.go` (3 cases, red before the fix — the partial
case failed with `CargoCapacity = 120`, proving the free-space bug directly).
