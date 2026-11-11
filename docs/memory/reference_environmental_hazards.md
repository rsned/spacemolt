---
name: reference_environmental_hazards
description: "v0.687.0 POI environmental hazards (radiation/tidal_shear/charged_seam/debris/scar_dust/ion_storm, severity 1-4 = 1/3/8/25% hull per dose) and the worker hazard gate that leaves/refuses them"
metadata:
  node_type: memory
  type: reference
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-10T23:38:01.692Z
---

**Mechanic (v0.687.0, 2026-11-10):** a POI may carry `hazard {type, severity}`.
Severity 1-4 takes 1/3/8/25% of max hull per dose (shields first, damage-type
resistance reduces). radiation, tidal_shear, scar_dust dose every tick an
UNDOCKED ship stays; tidal_shear, charged_seam, debris dose on each mine
action; charged_seam/scar_dust/ion_storm lower scan power. Docked, offline and
in-battle pilots are not dosed. An emptied hull is a lost ship. Server pushes
`hazard_warning {poi_id, poi_name, hazard, hull, max_hull, dose_pct, message}`
per dose; travel/jump carry `poi_hazard`; get_system/get_poi/survey POIs carry
`hazard`; stats gain `deaths_by_hazard`. Operator: "only a few places in known
space ... fairly obvious if you know the mechanic exists, and it's not
instant." First galaxy victim: the black hole in Alzirr (likely "The Maw").

**Our handling (commit 89cc03b4):**
- Client logs every dose to the operational log as `☢️  HAZARD DOSE: ...`
  and keeps `State.LastHazardDose`.
- `pkg/worker/hazard_gate.go` runs before every standing/scheduled/task
  command (StandingDeps.dispatch) and inside mine_qty's mineLoop: a dose in
  the last 3 ticks at the current undocked POI with severity >= 2 or hull <
  70% -> dock / travel to nearest hazard-free station / any clean POI, then
  ErrHazardEscape aborts loops (fatal under -f). travel/mine at a POI KNOWN
  (from state.System.POIs) to be severity >= 3 -> ErrHazardRefused.
- KB migration 68: pois.hazard_type / hazard_severity / hazard_seen_tick; a
  read omitting the hazard never clears one.

**Not covered yet:** long composite commands (haul, deliver, autopilot legs,
hunt, missions) only see the gate between commands; cross-system route
planning does not avoid hazard systems (jump arrival lands at the star POI —
a black-hole star would dose every transiting ship).

**v0.689.0 hazard gear (11-10, struct fields commit d45278ac):** crafted
modules — radiation baffles / shear bracing / dust skirts cut radiation /
tidal_shear / scar_dust+debris damage 30/45/60% by tier (cost cargo, speed or
fuel); damped mining heads cut per-mine hazard damage 30/50/70% (less mining
power); signal scrubbers cancel 30/50/75% of the scan/survey penalty; hazard
sealants (consumable) +20/35/50% for 60/90/120 ticks. Resistance adds to
damage-type resistance, cap 90%. Best tiers need annealed seam glass. Module
JSON: `hazard_resistance{type:pct}`, `hazard_mining_reduction`,
`hazard_scan_filter`. The worker gate still judges by severity only — it does
not credit fitted gear.

**Explorers (commit 4a30497d):** explore now tours every POI of each arrived
system, docks/captures/refuels at stations, and below 30% fuel flies to the
nearest accessible station first.

Watch: grep overmind logs for `HAZARD DOSE|☢️`.
