---
name: reference_connections_graph_is_directed
description: "The KB connections table is DIRECTED; an undirected BFS invents shortcuts and underestimates jump distance by up to 5x"
metadata:
  type: reference
---

`connections(from_system, to_system)` in the KB is a **directed** graph. Reading
it undirected — adding `g[b].add(a)` for every row — invents lanes that do not
exist and produces routes the server will not fly.

**Measured 2026-09-08**, posting marketbot_010..013 from Haven:

| target   | undirected BFS | directed BFS | server's own route |
|----------|---------------:|-------------:|-------------------:|
| alzirr   |  5 | 26 | **26** |
| diphda   |  9 | 22 | **22** |
| tiaki    | 10 | 23 | **23** |
| gsc_0034 | 10 | 23 | **23** |

Directed BFS matched the server **exactly, 4 of 4**. Undirected was wrong by up
to 5x, and wrong in the dangerous direction — it makes a far posting look near.

**Why so few one-way rows cause so much error.** Only **17 of 2,147** rows lack a
reverse, so the two graphs look almost identical by row count. But all 17 fan out
of just two hub systems, `iron_reach` (11) and `the_anvil` (6), reaching
alpha_centauri, sirius, pollux, ross_128, factory_belt, ashford and others.
Mirroring those 17 hands the BFS a set of long-range shortcuts straight into the
middle of the map, which nearly every route then "uses". A handful of one-way
hub lanes is enough to collapse the whole diameter.

**Rule:** build the adjacency directed, or ask the server. Never mirror the rows.

Distance drives posting decisions — the 2026-08-13 Crimson batch was pinned "by
measured margin" — so this error class silently sends a starter hull at a station
it cannot reach. It did not bite here only because these hulls burn 1 fuel/jump
against a 130 tank. Related: [[reference_ship_jump_time_and_fuel_formulas]],
[[reference_station_id_aliases]].
