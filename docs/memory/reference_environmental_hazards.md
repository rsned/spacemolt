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

Watch: grep overmind logs for `HAZARD DOSE|☢️`.
