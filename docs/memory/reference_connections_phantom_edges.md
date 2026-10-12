---
name: reference_connections_phantom_edges
description: "RESOLVED 2026-10-12: the phantom edges are GONE. KB connections matches the public v0.618 chart exactly (506 systems, 2132 edges, 0 phantom). Wormholes were never missing lanes -- they are hidden POIs"
metadata:
  type: reference
---

## ⭐🟢 RESOLVED 2026-10-12 — verified against the authoritative chart

v0.618 made the galaxy chart common knowledge, so `get_map` is now ground truth
for all 506 systems. Diffed it against the KB:

```
systems: KB 506 vs chart 506
edges:   KB 2132 vs chart 2132
phantom: 0        missing: 0
```

**There are no phantom edges.** All five systems this note accused
(`iron_reach`, `the_crucible`, `the_anvil`, `ironhearth`, `gold_run`) match the
chart exactly, zero extras. The producer fix `e55077fc` plus later cleanup did
remove the rows; the "38 rows, not yet cleaned" claim is **stale** — do not plan
routing work around it, and do not re-import the map expecting a win.

The only real gap was one system: `bnki`, a 5-POI dead end off `pipirima`,
present in `connections` but absent from `systems` (SQLite FKs are off, so the
orphan edge survived). Inserted the system row and the reverse edge 2026-10-12;
the graph is now an exact match.

**The wormhole puzzle dissolves too.** The chart is fully **symmetric** — 0
one-way lanes galaxy-wide. Wormholes were never missing *lanes*: they are hidden
**POIs** you travel to, charted once you reveal them with `survey_system`,
arrive there, or hold them in faction intel. That is why they never appeared in
`connections` and never should.

Re-run the check after any map import: diff `connections` against
`data/game-api/latest/get_map.json`.

Related: [[reference_v0618_visibility_overhaul]]

---
## HISTORICAL (superseded — kept for the failure shape)

The producer bug was real: a jump arrival carried the **departed** system's
neighbour list, so phantom lanes accumulated (19 by 2026-09-08, 38 by
2026-09-14 across five systems) and **shortened routes**, sending ships down
lanes that did not exist. If a future import regresses, that is the shape to
look for: asymmetric rows whose targets are a neighbouring system's neighbours.
