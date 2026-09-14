---
name: reference_connections_phantom_edges
description: "The KB connections table GROWS phantom edges (38 by 2026-09-14, was 19) because a jump arrival carried the departed system's neighbour list; producer fixed e55077fc, rows not yet cleaned"
metadata:
  type: reference
---

`connections` is wrong in two opposite ways at once. It invents lanes that do
not exist, and it omits the only real one-way lanes in the game.

## ⭐🔴 2026-09-14: they GROW. 19 -> 38 in six days, and the cause is found

Counted again 2026-09-14: **38** one-way rows across **five** systems, not 19
across three. Two of the polluted systems are new.

| polluted | 09-08 | 09-14 | copied from |
|---|---:|---:|---|
| `iron_reach`   | 11 | **15** | ironhearth, sol, treasure_cache, **first_step** |
| `the_crucible` | – | **9**  | gold_run, treasure_cache |
| `the_anvil`    | 6  | **8**  | sol, treasure_cache, haven |
| `first_step`   | – | **4**  | saiph |
| `blood_forge`  | 2  | 2      | haven |

**Every one of the 38 passes the donor-attribution test** (same target, same
distance as some other system's real bidirectional edge), so none is a wormhole
and all 38 are safe to delete by this file's own rule.

### Root cause — FIXED `e55077fc`
A jump arrival carries only the new system's id and name. The client wrote them
over `state.System.ID/.Name` while leaving `.Connections` and `.POIs` holding
**the system just departed**. Any capture running before the next `get_system`
reply then wrote the old neighbours under the new id — and since the
`connections` upsert never deletes, every one is permanent. Hence monotonic
growth.

The chain proves it: `first_step` is polluted BY `saiph` and then itself donates
to `iron_reach`. A bad one-off import cannot do that; a per-arrival defect can.

`Client.enterSystem` now clears the per-system fields when the id actually
changes. Safe because every reader calls `GetSystem` first (`exploreSystem`,
`KBUpdateSystem`), and an empty list writes nothing rather than writing lies.

### ⭐🟢 DELETED 2026-09-14 — and validated against the canonical map
All 38 removed. Backup taken first with `VACUUM INTO`, audit CSV of every
deleted row + its donor kept. The DELETE embedded the donor-attribution rule
rather than deleting on one-wayness alone.

**`data/game-api/latest/get_map.json` is the canonical public connection map**
(505 systems, `connections` is a plain list of system ids). After the delete the
KB matched it EXACTLY: 2,130 edges, **0 in the KB but not canonical, 0 canonical
but missing**. Use this diff as the standing check — it is far stronger than
donor attribution:

```python
canon={(s['system_id'],c) for s in json.load(open('data/game-api/latest/get_map.json'))['systems']
       for c in (s.get('connections') or [])}
kb=set(db.execute("SELECT from_system,to_system FROM connections"))
```
`iron_reach` came out at exactly the 4 lanes the operator confirmed on 09-08.

### ⭐🔴 The OneWay heuristic was a PHANTOM detector, not a wormhole detector
`GetConnections` derived `OneWay` from a distance/geometry mismatch and called
those wormholes. The 38 geometry-mismatch rows were *precisely* the 38 phantoms
— a donor's distance under a different origin cannot match the geometry. After
the delete **zero rows are OneWay**. Comment corrected in `2f9d068d`; kept as a
phantom canary.

### ⭐🔴 Permanent wormholes DO exist (operator, 2026-09-14)
A few are permanent; **two are discovered through the smuggling chain**. They
are POIs (`wormhole_entrance` / `wormhole_exit`, shared id suffix), never
connection rows. **`UpsertSystemFromMap` DELETES any stored lane absent from the
public map**, so storing a wormhole as a connection row would destroy it on the
next import — give such rows a protected marker first. Pinned by
`TestUpsertSystemFromMap_PrunesLanesAbsentFromTheMap`.

### `last_updated_tick` is now a real freshness marker (`2f9d068d`)
Was a literal 0 on all 2,168 rows. Both write paths stamp it now, advancing with
MAX so it never regresses and a tickless capture cannot blank it. Re-importing
the whole map self-heals the table.

### Superseded: the old "still open" note
Not cleaned. They keep breaking routing — they wedged `auto-explore` into a
`horizon <-> first_step` oscillation for 30+ hops on 2026-09-14 (fixed
separately in `ee818b60` with a per-run frontier memory). Deleting them needs
the operator's call since `connections` is shared fleet-wide.
`last_updated_tick` is **0 on all 2,168 rows**, so the table carries no age
signal to triage with.

## 1. Phantom rows — the original 2026-09-08 finding (19 lanes)

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
