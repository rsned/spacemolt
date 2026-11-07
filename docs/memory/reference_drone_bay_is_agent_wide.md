---
name: reference_drone_bay_is_agent_wide
description: get_ship's drone_bay is an agent-wide ledger, but drones go DORMANT when the controlling ship or agent leaves the system
metadata:
  type: reference
---

**Deployed drones go DORMANT as soon as the controlling ship or agent leaves the
system (operator, 2026-11-06).** This corrects the earlier reading of this note,
which said they keep mining elsewhere. Drones cannot jump, so a bot must stay in
the system it mines. Launch-and-leave does not work, a hull switch that leaves the
system stops them, and a station-less system leaves them only the ship's cargo
hold to deposit into.

**Why:** the 2026-08-28 reply below only showed the ledger still listing them.
**How to apply:** a marketbot's station must share a system with a resource POI;
fit at home and stay. Trips away pause mining until the bot returns.

So `drone_bay` in a `get_ship` reply is an agent-wide ledger, NOT a description of
what is physically aboard or working. craftsman-1 launched 25 drones, then flew to
Haven in another hull; `get_ship` still reported `deployed_count: 25`,
`bandwidth_used: 250`, `in_bay: []`. `in_bay: []` means "none stowed", not "none owned".

`bandwidth_used` is 10 per drone (25 drones -> 250).

**Reporting gap:** `bay_capacity` and `bandwidth_total` both came back **0** in
the same reply that reported 25 deployed and 250 used, so `get_ship` cannot tell
us remaining drone headroom. Matters for [[project_fleet_drone_refit]]
(175 bays / 800 drones).

See [[reference_ship_modules_never_captured]] — same shape of problem: the
capture exists but the totals do not.

## ⭐🔴 A drone bot fits a drone bay OR other modules — never both

Operator, 2026-09-12. The bay consumes the fitting budget, so there is no room
for a second functional module alongside it. And:

**Drones go DORMANT if the bay is removed, or if the agent switches ship.**

### What this rules out

The `compact_steel_refinery` and `compact_copper_wiring_plant` are bolt-on ship
modules that passively convert ore *in the hold* — no station, no facility, no
craft command. That looked like the clean answer to the `no_facility` wall the
2026-09-12 ore conversions hit (44 no_facility in one hour; marketbot_010, the
68,396-iron bot, among them).

**It is not available to the drone marketbots.** Fitting one means pulling the
bay, which stops the mining that produces the ore in the first place. Do not
propose it for them again, and do not "just try it on one" — the cost is that
bot's drones going dormant.

It remains theoretically open for non-drone hulls, but those modules are
described as drawing "far more CPU and power than a refinery ship", and
`ship_modules` has never captured a row so current fittings are invisible —
price it against a real hull before building any.

### What this leaves

Ore must travel to a mill. The conversion set lives on the `craftsman` role at
hub stations, so the flow is: drone bots and miners produce ore in place ->
haulers move ore INWARD to a hub -> craftsmen convert -> components move out.
See [[project_ore_to_component_conversion]].
