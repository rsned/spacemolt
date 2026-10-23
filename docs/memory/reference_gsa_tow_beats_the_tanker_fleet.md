---
name: reference_gsa_tow_beats_the_tanker_fleet
description: "A GSA tow is 500 credits and ~15 minutes; distress_signal is implemented but NO fleet code calls it. The tanker fleet's real niche is BROKE agents, not distant ones."
metadata:
  type: reference
---

**A stranded agent with 500 credits should buy a GSA tow, not wait for a
tanker.** Operator, 2026-10-16: *"easier to wait 15 minutes for a GSA tow"* /
*"500 credits for a tow"*.

Worked example — craftsman-1 stranded at `groombridge_1618_frozen_drift` on
**8 fuel** (Survey Vessel: scale 3, speed 3, 1020 tank, **16 fuel/jump**, so
zero hops possible). Nearest refuel was Mebsuta, **5 jumps**. Nearest assist
tanker at Nexus Prime was **10 jumps** each way:

| | cost | time | risk |
|---|---|---|---|
| GSA tow | 500 cr | ~15 min | none |
| tanker from nexus | 240 fuel (~500 cr of cells) | ~4 h round trip | 10 systems incl. Azha (Lawless) |

Same credits, 1/16 the time, and none of the stronghold exposure that caused
8 of 24 losses ([[reference_stronghold_guard_is_per_role]] — the guard is
ABSENT from assist).

## distress_signal is implemented and the fleet never calls it

`Client.DistressSignal(ctx, distressType)` at `pkg/game/client.go:2198`, in the
`GameClient` interface, with `DistressSignalResponse` carrying `missions_sent`
and `expires_seconds`, and `DistressCall.MissionID` as the handle a responder
accepts. **The ONLY caller is the hand-typed `distress_signal` case in
play_as.** Nothing in `pkg/worker`, `pkg/rescue` or `pkg/strategy` uses it.

So the assist fleet, the rescue queue, claim election, distance routing, the
pump gap and the tanker migration were all built around a problem with a
500-credit built-in answer.

## The tanker fleet's real niche is INSOLVENCY, not distance

The one case a tow cannot solve is an agent that cannot pay. And that is
exactly what the history shows: **6-for-6 "real" strandees were docked and
BROKE, ~1.5k credits TOTAL between them**
([[reference_stale_rescue_records_outlive_the_strand]]). For most of those the
cheaper fix was a **credit gift**, not a tanker.

**Decision rule for a strand:** credits >= ~500 -> tow. Broke -> gift credits
first, tanker only if the gift cannot land. Distance is almost never the
deciding factor.

Related: [[project_no_fuel_cells_refuel_deadlock]] (nothing ever BUYS a cell) ·
[[reference_assist_tanker_migration]] · [[project_rescue_pipeline_bugs]]

**Spotting a towed quarantine (2026-10-22, trader-7):** a rescue record whose
assists all failed with `refuel: player_not_found` at a station-less POI means
the ship is GONE from there — towed. trader-7 sat UNRESCUABLE 16h at
quartzfield_star; operator rule of thumb: stranded >15 min = towed. Fix:
`go run ./cmd/tools/rescue-clear -agents <id>`; it relaunched docked at
Treasure Cache (reconcile logs `system changed ... docked changed false->true`)
and resumed hauling in seconds. Watch its first pass: it liquidated its stale
cargo (279 liquid_hydrogen) into a local bid "@1".
