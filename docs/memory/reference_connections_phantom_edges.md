---
name: reference_connections_phantom_edges
description: "The KB connections table carries 19 PHANTOM edges — copies of other systems' neighbour lists filed under the wrong origin; every one-way row is one"
metadata:
  type: reference
---

`connections` contains **19 rows for lanes that do not exist**. Each is a verbatim
copy of some *other* system's neighbour list written under the wrong
`from_system`, and each is detectable because it names the same target at the
same `distance` as the donor's own real edge.

| polluted system | phantom rows | copied from |
|---|---:|---|
| `iron_reach`  | 11 | `ironhearth`, `sol`, `treasure_cache` |
| `the_anvil`   |  6 | `sol`, `treasure_cache` |
| `blood_forge` |  2 | `haven` |

**⭐ The detection rule is exact: a one-way row IS a phantom.** All 19 one-way
rows are phantoms and all 19 attribute to a donor; zero legitimate one-way lanes
exist in the table. The real graph is fully symmetric, so **filter to
bidirectional edges** (or delete these rows) and the corruption is gone.

Ground truth, operator-confirmed against the in-game map 2026-09-08:
`iron_reach` has **4** connections (blood_forge, krynn, the_anvil, the_crucible)
— exactly its bidirectional set. `blood_forge` has 5 (alzirr, iron_reach, krynn,
stillwater, valor). The KB gives `the_anvil` 5 (frostfeld, hd_20794, iron_reach,
ironhearth, krynn); the operator counts **3 plus a wormhole end**, so treat
frostfeld/hd_20794 there as unverified.

**Why it bites hard.** The phantoms are long-range and land on hubs — `sol`,
`market_prime`, `sirius`, `alpha_centauri`. A handful of them collapses the
apparent diameter of the whole map. Reading the table naively (mirroring every
row) predicted 5-10 jumps for the 2026-09-08 marketbot postings; the server
routed **22-26**.

**Do not conclude from that episode that the table is "directed" — that was my
wrong read, corrected by the operator.** Dropping the one-way rows happened to
reproduce the server exactly 4 of 4 that day, but only because those particular
routes never traversed a phantom. It is not a routing model, just the same
filter arriving at the right answer for the wrong reason.

**Distance drives posting decisions**, so trust the server's own `Route found: N
jump(s)` over any local computation. Related:
[[reference_ship_jump_time_and_fuel_formulas]], [[reference_capture_loss_taxonomy]].
