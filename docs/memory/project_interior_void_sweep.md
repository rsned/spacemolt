---
name: project-interior-void-sweep
description: NEXT after the round-2 full-lap sweep finishes — explorers sweep the empty void areas INSIDE the galaxy with ~100 LY pass spacing
metadata:
  node_type: memory
  type: project
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-15T17:20:00Z
---

User request 2026-11-14: once the explorers finish the current round (round 2,
full 360° laps on rings 7200-8550, see [[project-pathfinder-frontier-sweep]]),
the next job is the void areas inside the galaxy, with passes ~100 LY apart
as the ships cross them.

**Why:** the frontier sweeps found hidden systems at the edge; voids inside the
charted galaxy may hide more (pathfinder drift captures within ~50-90 LY, so
100 LY spacing leaves no gaps).

**How to apply:** don't start until every sweep_state.json reaches phase done.
Design still open — void detection from KB system positions, path shape
(straight chords vs. rings), and how the lap code is reused. Brainstorm with
the user before building.

**Shape idea (user, 11-14):** each agent gets one void zone and flies a
back-and-forth square wave (lawnmower) through it, lanes ~100 LY apart.

**Status 2026-11-14:** spec docs/superpowers/specs/2026-11-14-interior-void-sweep-design.md,
plan docs/superpowers/plans/2026-11-14-interior-void-sweep.md; code MERGED to main (5cc37ac9, local).
`sweep_mode: void` in sweep-fleet.yaml; ledger data/sweep/void-run.json via `bin/void-zones generate|status|map`
(58 zones, 38.7M LY² at generation time). Plan Task 6 rollout PENDING the operator's go after round 2:
generate ledger, archive round-2 sweep states, switch yaml, rebuild bin/worker, restart sweep overmind.

## RESUME POINT 2026-11-15 ~17:20 (operator said GO for the ready ships)

Round 2 finished 11-14 21:28 (laps: 1,2,5,7,8,9,11,12). Rollout in progress for the
READY ships only: explorer-1, -2, -5, -9, -11, -12 (Node Beta, full fuel+hull) and
explorer-7 (Gliese 581 / Korr Fortress, a stronghold — fueled). Goal of this first
run (operator): shake out anything the plan overlooked.

Held back:
- **explorer-8** at Winterhold (player station, NO refuel/repair service; 19 fuel,
  200/480 hull, 1.08M cr). Market sells fuel_cell @3000 (20 fuel each), no repair kits.
  Operator: send **assist-frontier** (assist fleet, Capacity hull, 1500 fuel, docked
  First Step) to Winterhold to refuel it; explorer-8 must UNDOCK for the transfer.
  Nearest refuel+repair by lanes: Ramen's Rest @ last_light (4 jumps).
- **explorer-6** quarantined fuel-dead at Turnback (frontier, ~-6310,-3944), rescue
  record pending since 11-15 03:01, unclaimed (no lane route for assists).
- **explorer-4** at Sol: operator still to decide re-equip vs drop from sweep.

Shipped alongside (local commits, NOT pushed): b5322ade play_as pf_route (frontier
Pathfinder planner), 4f039390 + 36789a9b repair kits (buy on no_repair_kits; keep 4
spares from CJ faction storage; kit = 50 hull), b33d51f3 get_base life line labels.
craftsman-3 (now officer, as are -4/-5) runs hourly `craft_stock assemble_repair_kit
repair_kit 48 prefer_own` at CJ (48 held 11-14 20:05).

Open idea, not built: refuel fallback — on no_fuel_source at a station whose market
sells fuel_cell, buy cells (same pattern as repair kits).

**Rollout log 11-15 (local 09:17–09:24):** void-run.json generated (58 zones). Round-2 states archived
in data/sweep-archive/round2-20261115/. sweep-fleet.yaml: sweep_mode void on 1,2,5,7,9,11,12; 4 and 8
commented out as HELD (e5343bca). **Plan gap #1:** the plan only rebuilt bin/worker — bin/overmind (built
11-14 08:02) predated the merge and never passed --sweep-mode, so the first restart flew RING staging.
Rebuilt bin/overmind + restarted; workers now get --sweep-mode void. First claims: -2 V11, -12 V13,
-9 V01, -11 V02, -5 V14. **Gap #2 (minor):** a worker restarted mid-jump reads a stale docked=true,
claims, then sees itself undocked and goes "done but not docked" → lanes home → claims/stages from
there (detour, keeps its zone). explorer-8 refuel by assist-frontier delegated to a background agent.
Void map artifact (https://claude.ai/artifact/M3DFdEbXkHWTPEze3hWtcp) NOT yet republished.

**11-15 ~12:55:** Gap #4 FIXED (fbc937b6): staging now waits out "action pending" (waitOutPending) instead of
launching from far away (explorer-5 hit it at jump 5/9 to Atlas). Live only after each sweep worker restarts.
Gap #5 (open): staging can pick an AVOIDED system as its target (explorer-9 → Taygeta, "route enters an avoided
system") and then launches from where it is; candidate selection should skip the avoid set.
