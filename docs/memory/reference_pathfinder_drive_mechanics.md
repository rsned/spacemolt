---
name: reference-pathfinder-drive-mechanics
description: "Pathfinder drive (measured 11-11/12 on craftsman-1's Survey Vessel): flat 5x-jump fuel charge per launch/replot, NO per-tick drain, 10 units/tick, bearing CCW from +x; module 18 CPU / 30 power utility"
metadata:
  node_type: memory
  type: reference
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-12T20:25:51.540Z
---

Measured by the operator on craftsman-1 (Survey Vessel, 1,120 fuel with expanded_fuel_tank), 2026-11-11/12; data in data/stations/cloverfield/events.jsonl (get_ship / get_location around `jump <bearing>`).

- **Fuel = one flat charge per launch, paid up front.** Haven→Market Prime (bearing 51.0): 1,120 → 1,075 by drift tick 3, still 1,075 at arrival (41 ticks). Each mid-drift replot charges the same again (server text: "5x jump's fuel"). Operator confirmed 2026-11-12: get_ship stays FLAT through long drifts — no per-tick burn.
- So range is limited by the NUMBER of launches/replots, not drift length. Survey Vessel: 45/hop → ~24 hops per tank. Pick hulls by cheap per-jump fuel, not just a big tank.
- Speed 10 map units/tick, straight line; bearing = degrees counter-clockwise from +x in KB map coords. Deep Range test: 226.5° out ~151 ticks, replot 46.5° (~180°) brought it home.
- Module: pathfinder_drive, utility slot, size 10, 18 CPU, 30 power. All 100 T3 hulls have the capacity; 57 fit it beside their default modules. Top fuel T3: Reserve 4,000 (tanker; assist-haven owns one), Survey Vessel 1,020 (+25% fuel eff, anomaly detection), Odyssey 1,000 (+50% fuel eff).
- Bot rule (proposed): keep fuel for ≥2 more launches (one correction + one home); turn back at that reserve. Running dry out there = self-destruct.

Client support: play_as `jump <bearing>` (pathfinder_jump / pathfinder_redirect / pathfinder_arrival actions); JumpResult.Drifting; KB save skipped while drifting. User plans a pathfinder player guide and frontier-exploring explorer bots.
