---
name: reference_transit_hub_passenger_strategy
description: "Player-reported passenger \"secret\" (LT1428, 11-05): a 150-seat transit concourse + ~9 speed-6 passenger ships feeding it; CJ lounge path L1 20 → L2 60 → L3 150 seats"
metadata:
  node_type: memory
  type: reference
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-08T23:47:49.263Z
---

Player hint (Discord, 2026-11-05, LT1428 answering "what's the passenger hauling secret?"):
"first up, is a faction transit lounge for 150 passengers to hang out and eat some munchies at.
next up is have 9 of your buddies in speed 6 passenger ships go grab every passenger everywhere,
and bring them there." Player-reported, not measured.

**150 seats = `transit_concourse` (L3).** Path from the catalog (2026-11-08):
- `transit_lounge` L1: 500k, 20 seats. **CJ built one 2026-11-08** (facility 5fa72b1f…).
- `transit_terminal` L2: 2.5M + 1,000 steel, 400 circuit_board, 300 flex_polymer, 200 reinforced_glass; 60 seats; +180-tick fare deadline.
- `transit_concourse` L3: 9M + 3,000 steel, 1,200 circuit_board, 600 titanium_alloy, 500 reinforced_glass; 150 seats; +360 ticks.

**Operator's model (2026-11-08):** every shuttle brings its passengers to CJ as a stop on the
way to their destination: drop at the lounge (they eat and spend while they wait), then a CRFT
ship carries them on. CJ is the hub; the shuttles are spokes.

"Munchies" = dining/leisure venues; layover passengers spend there. CJ's first venue is a
`rec_lounge` (built 2026-11-08). Lounge use needs faction membership (CRFT) and
`unload_passenger target=lounge` / `load_passenger`; the shuttle worker does neither yet.

**Shuttle fleet on 2026-11-08:** johnny_cab (concordia, faction DB) plus three cobble alts
(johhnycab, johnny_ca, johnnycab), none in CRFT. See [[project_cloverfield_home_station]].
