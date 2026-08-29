---
name: project_pairwise_jump_cache
description: Follow-up idea to precompute an all-pairs system jump-distance cache for routing
metadata: 
  node_type: memory
  type: project
  originSessionId: 1d0c7474-14f1-4241-bae7-4fc0ea2c1f85
---

Follow-up optimization (idea, not built): precompute an all-pairs **jump-distance** matrix between systems and cache it, instead of running BFS on demand.

**Context:** Added `plan_route` to play_as on 2026-06-17 (`cmd/tools/play_as/plan_route.go`) — optimal multi-system visiting order for fewest jumps via on-demand BFS over the KB `connections` table (edge = 1 jump), Held-Karp DP for the ordering. It only BFSes from start + waypoints, so it's already cheap for interactive use.

**Why:** A cached matrix makes routing O(1) lookups and would also benefit `autopilot` / `find_route`-style features that currently recompute paths.

**How to apply:**
- New KB table e.g. `connection_jumps(from_system, to_system, jumps)` populated by a builder tool (under `cmd/data/` or `cmd/tools/`).
- Rebuild/refresh when the `connections` graph changes (new edges discovered).
- Have `plan_route` (and friends) prefer the cache, falling back to live BFS when a pair is missing — keep the live-BFS path so it works without the cache.

Related: [[reference_spacemolt_kb_shared_db]] (KB schema is shared with sibling spacemolt-kb repo — coordinate any migration there).
