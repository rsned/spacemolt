---
name: reference-ship-commissioning-is-credits-only
description: commission_ship charges credits only — the shipyard supplies the ship_build_materials; do NOT haul components to a shipyard
metadata:
  type: reference
---

`commission_ship` at a shipyard charges **credits only**. The shipyard
supplies the components. The `ship_build_materials` table lists a hull's bill
of materials, but that is the RECIPE, not a list of things the commissioner
must deliver.

Proven 2026-09-18 at **Starfall Salvage Station**: four Congregations
commissioned at **5,723 cr each**, with **zero** aluminum_sheet /
cargo_container / flex_polymer / fuel_tank / hull_plating ever stored at that
base by any agent, and the commissioning ship's cargo far too small to carry
the 336 components four hulls would need.

**Costs vs buying:** a listed Congregation at First Step Memorial Station cost
**13,674** (13,406 + 268 sales tax, 2%) on the same day. Commissioning is
**58% cheaper**, at the price of 320 ticks (~53 min) of build time. Buy when
you need the hull NOW; commission otherwise.

**Constraint that DOES bite:** faction-locked hulls. `congregation` has
`faction = outerrim`, so it can only be commissioned at an Outer Rim shipyard.
All seven Outer Rim stations have one — deep_range_outpost,
first_step_memorial_station, frontier_station (= the MOBILE capital, poi
`mobile_capital`, currently in void_gate, NOT frontier), ramens_rest,
starfall_salvage_station, unknown_edge_waystation, void_gate_outpost.

🔴 **The mistake this replaces:** on 2026-09-18 I read `ship_build_materials`,
assumed the commissioner supplies the components, and produced a whole plan to
consolidate 5,866 units of volume (2,399 aluminum_sheet, 505 cargo_container,
599 flex_polymer, 250 fuel_tank, 49 hull_plating) into Outer Rim space —
including a fictitious "bootstrap trap" about needing freighters to move the
parts that build the freighters, and alarm that the Outer Rim held zero fuel
tanks. All of it was wrong. Verify what a command actually consumes before
planning logistics around a recipe table.

See [[reference_prayer_class_freight_hulls]] for ranking freight hulls, and
[[project_haul_fleet_hull_attrition]] for why the fleet needed re-hulling.
