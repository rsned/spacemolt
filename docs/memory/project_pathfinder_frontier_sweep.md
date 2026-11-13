---
name: project-pathfinder-frontier-sweep
description: "Pathfinder clockwise frontier sweep for 10 explorers — spec+plan approved 11-12, code MERGED to main 11-13 (local, unpushed) (worker verb pathfinder_sweep, pkg/sweep, migration 75 APPLIED to live KB 11-13); ops (buy/fit hulls, 7 drives in CJ faction storage) pending"
metadata:
  type: project
---

Status 2026-11-12 (evening): spec docs/superpowers/specs/2026-11-12-pathfinder-frontier-sweep-design.md and plan docs/superpowers/plans/2026-11-12-pathfinder-frontier-sweep.md approved; operator chose NATIVE execution. Code Tasks 1-8 done on branch `pathfinder-sweep` (worktree .worktrees/pathfinder-sweep; ledger .superpowers/sdd/2026-11-12-pathfinder-frontier-sweep/progress.md): pkg/sweep (geometry/fuel/state), worker verb `pathfinder_sweep` (state in data/agents/<id>/sweep_state.json), KB migration 75 systems.position_source (19 frontier ids = 'estimate'), role pathfinder_sweep + data/overmind/sweep-fleet.yaml (agent lines still commented). Task 9 (ops) pending: operator brings 7 pathfinder_drives to CJ FACTION storage (faction_withdraw → cargo, Officer rank); buy 3 more drives; hulls 3 Reserve + 6 Compendium + 1 Survey Vessel. Migration 75 must be applied to the live KB (bin/kb-migrate) before launch.

Status 2026-11-13: branch merged to main (fast-forward to 050b6710, local, not pushed); worktree + branch deleted; migration 75 applied to live KB via bin/kb-migrate (api 506, estimate 19). Next: Task 9 ops (hulls, drives, first-launch check, sweep-fleet.yaml, launch).

Decided by the operator:
- Any 10 explorer agents; T3 hull with pathfinder_drive (see [[reference-pathfinder-drive-mechanics]]).
- Start from an already-explored frontier system; sweep CLOCKWISE around the galaxy's outer reaches.
- Ships 50 LY apart radially: each traces a larger concentric ring.
- Re-aim by 5° every time a ship has swept 5° of arc since start / last adjustment.
- At ≤75 fuel: final jump toward the nearest empire station system (end somewhere safe).
- Stock holds with fuel_cell (20) / premium_fuel_cell (50) to refuel mid-drift.
- **Hazards: leave quickly on experiencing one** (operator 2026-11-12). Frontier systems carry ~30x the hazard rate of the original galaxy (6/19 systems vs 3/~505).

Constraints learned:
- **Frontier systems have NO coordinates** in the API (operator confirmed 2026-11-12). The operator then wrote ESTIMATED positions for all 19 into the KB systems table (was 0,0). Two clusters: NW (~140°, r 5,055–7,901: Long Night, Still Point, Cold Lens, Far Wick, Bristle, Ash Pearl…) and SW (~235°, r 7,779–8,817: Whitecap, Quiet Floor, Brushfield, Sallow, Narrows, Gloaming). Galaxy reach is now ~8,800 LY, not 7,500. Mid-drift redirect replies carry origin_x/origin_y, so ships can measure real positions.
- All 6 hazardous frontier systems are in the SW cluster (Gloaming radiation 2–3 + the Monolith kill); the NW cluster has none recorded.
- Frontier wildlife/hostiles kill: a Repeater Monolith group destroyed craftsman-1's Survey Vessel at Gloaming (11-12), energy weapons, ~305 per volley. Hostile data is in the hostile_* KB tables.

Open: what a ship does at a newly found system mid-sweep; operator review of the playback.
