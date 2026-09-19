---
name: reference-ship-commissioning-modes-and-costs
description: commission_ship has TWO modes — credits-only (expensive) or provide-materials (cheap labour + your components); buying a listed hull often beats both
metadata:
  type: reference
---

`commission_ship` quotes **two options**. Observed 2026-09-18 for a
Congregation at Starfall Salvage Station (shipyard tier here=1, required=0,
build 320 ticks ~53 min):

```
credits-only:       38,603 cr
provide-materials:   5,939 cr labour  (+ materials worth ~16,332 cr)
```

Compare buying the same hull listed at First Step Memorial Station:
**13,279 cr** (a previous unit went for 13,406 + 268 sales tax = 13,674; the
station manager RESTOCKS — a replacement appeared within ~35 min).

**Why the gap:** credits-only means the shipyard buys the components on the
market itself, and it charges you **2.0x their market value** to do it:
38,603 - 5,939 labour = 32,664 for materials worth ~16,332. Supplying your
own avoids that 32,664/hull premium entirely. Our components come from the
engineers' ore->component conversion
([[project_ore_to_component_conversion]]), so our marginal cost on them is
near zero — over 15 hulls the markup avoided is ~490,000 cr. It also explains
why a LISTED hull (~13,300) undercuts credits-only so badly: no bespoke
panic-buy of components is priced in.

**So the ranking by cash outlay is:**
1. `provide-materials` — 5,939, if you already hold the components
2. buy a listed hull — ~13,300, instant, but stock is thin and unreliable
3. `credits-only` — 38,603, nearly 3x the purchase price. Rarely correct.

The bill of materials is in `ship_build_materials` (Congregation: 50
aluminum_sheet, 12 cargo_container, 14 flex_polymer, 5 fuel_tank, 3
hull_plating = 84 items, 133 volume). For the provide-materials rate those
components must be AT that yard.

🔴 **Two mistakes to avoid, both made 2026-09-18:**
1. Do NOT assume the recipe means you must always supply components — the
   credits-only mode exists.
2. Do NOT assume the shipyard always supplies them either. I concluded that
   from finding zero materials in `agent_storage_items` at Starfall AFTER
   four hulls were built there. That table keeps only the NEWEST capture
   ([[project_fleet_asset_snapshots]]), so absence now is not absence then.
   The commission quote is the authority, not a storage snapshot.

**Constraint that does bite:** faction-locked hulls. `congregation` has
`faction = outerrim`, so it can only be commissioned at an Outer Rim
shipyard. All seven have one — deep_range_outpost,
first_step_memorial_station, frontier_station (= the MOBILE capital, poi
`mobile_capital`; the KB's location for it goes stale), ramens_rest,
starfall_salvage_station, unknown_edge_waystation, void_gate_outpost.

**A finished commission goes to STORAGE at that shipyard — it does NOT
auto-switch.** `switch_ship <id>` is required and the agent must be docked
there. This differs from `buy_listed_ship`, which DOES auto-switch and stores
the old hull immediately (proven with trader-3 at First Step, same day).
Builds serialise PER YARD, so spreading across the seven yards parallelises.

See [[reference_send_gift_ship_transfer]]: gifting a built hull is free but
it stays parked where it was built.
