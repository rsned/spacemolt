---
name: reference_connections_phantom_edges
description: "The KB connections table carries 19 phantom edges (copied neighbour lists) and omits wormhole lanes entirely, so it errs in BOTH directions"
metadata:
  type: reference
---

`connections` is wrong in two opposite ways at once. It invents lanes that do
not exist, and it omits the only real one-way lanes in the game.

## 1. Phantom rows — 19 lanes that do not exist

Each is a verbatim copy of another system's neighbour list written under the
wrong `from_system`, betrayed by naming the same target at the same `distance`
as the donor's own real edge. `alpha_centauri` at 279 and `sirius` at 715 sit
under both `iron_reach` and `the_anvil` when both belong to `sol` — two origins
cannot be equidistant from the same star.

| polluted | phantoms | copied from |
|---|---:|---|
| `iron_reach`  | 11 | `ironhearth`, `sol`, `treasure_cache` |
| `the_anvil`   |  6 | `sol`, `treasure_cache` |
| `blood_forge` |  2 | `haven` |

Strip them and `iron_reach` has **4** connections (blood_forge, krynn,
the_anvil, the_crucible), operator-confirmed against the in-game map 2026-09-08;
`blood_forge` has 5. The KB gives `the_anvil` 5; the operator counts 3 plus a
wormhole end, so `frostfeld`/`hd_20794` there are unverified.

They are long-range and land on hubs (`sol`, `market_prime`, `sirius`), so a
handful collapses the map's apparent diameter: a BFS predicted 5-10 jumps for
the 2026-09-08 marketbot postings that the server actually routed at **22-26**.

## 2. ⭐ Wormholes — real one-way lanes, absent from the table

**The only genuine one-way lanes are uncollapsed active wormholes** (operator,
2026-09-08). They are NOT in `connections` — verified, zero rows for any active
pair. They live only as POIs: `wormhole_entrance` paired to `wormhole_exit` by a
shared id suffix (`the_frying_pan` hadar -> `the_frying_pan_exit` gsc_0036;
`leap_of_faith` alzirr -> `leap_of_faith_exit` praecipua; `wh_entrance_826d1efd`
cervantes -> `wh_exit_826d1efd` zavijava). 50 more are `wormhole_collapsed`.

**So a route model built from `connections` alone silently misses every wormhole
shortcut** and overestimates those distances — the opposite error to the
phantoms.

## Deleting phantoms safely

Every one of the 19 one-way rows is a phantom *today*, but do NOT delete rows
merely for being one-way: that rule is only sound while wormhole lanes stay out
of the table, and it would silently purge them if a future capture adds them.
**Require donor attribution** — same target, same distance, as some other
system's real bidirectional edge — before deleting anything.

Distance drives posting decisions, so prefer the server's own `Route found: N
jump(s)` over any local computation. Related:
[[reference_ship_jump_time_and_fuel_formulas]], [[reference_capture_loss_taxonomy]].
