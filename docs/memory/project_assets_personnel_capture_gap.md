---
name: project_assets_personnel_capture_gap
description: LATER TASK (parked 2026-08-30) — capture_profile/capture_faction drop v0.572.0 crew, marine, and boarding stats; spec for the pkg/assets columns to add
metadata:
  type: project
---

**Parked 2026-08-30 by the operator ("save that as a later task").**

**Gap:** none of the worker `capture_*` commands persist the v0.572.0 personnel
data. `capture_profile` writes `agent_hulls` from `list_ships`, whose
`OwnedShipInfo` has NO personnel fields; the `get_status` call in the same pass
does carry `ship.personnel` + `crew_capacity`/`marine_capacity` for the ACTIVE
hull but `CaptureProfile` only reads `Player` from it. `agent_stats` keeps 8
counters and none of the 26 new boarding/personnel/prize stats.
`capture_faction` ignores `faction_info.personnel` (the faction reserve).
Knowledge-side capture (`ship_captures`, `seen_prize_events`, `ships` catalog
crew cols) DOES exist — see [[reference_v0572_boarding_personnel]].

**Spec (bounded, `pkg/assets` migration pattern, tests first):**
1. `agent_hulls` + `Hull`: `crew_capacity`, `marine_capacity`, `fit_crew`,
   `fit_marines`, `injured_crew`, `injured_marines` as **NULLABLE** ints —
   filled for the active hull from `State.Ship`, NULL for spares (list_ships
   cannot say). NULL not 0, so "unknown" never reads as "no crew".
2. `agent_stats` + `Stats`: `ships_captured`, `ships_lost_to_capture`,
   `crew_deaths`, `marine_deaths`, `crew_injuries`, `marine_injuries`,
   `boarding_attempts`, `boarding_victories`, `prizes_claimed`,
   `prizes_delivered`, `prize_value_recovered`.
3. `faction_profile`: reserve `fit_crew`/`fit_marines`/`injured_*` from
   `FactionInfoResponse.personnel` (`FactionPersonnelEmployment`).

**Why it matters:** casualties are invisible until then, and nothing can
treat them until Layer B (`treat_personnel`) lands. Do this BEFORE B so the
first treatment run has a baseline.
