---
name: project_haul_rolling_drain_on_completion
description: "FUTURE enhancement (user idea 2026-07-23): flag haulers to drain-on-next-mission-completion for zero-abort rolling binary upgrades — needs a per-worker long/disabled force-stop mode on top of shipped membership remove"
metadata:
  node_type: memory
  type: project
  originSessionId: 86df3835-f10f-4c25-928d-7948d122ecd7
  modified: 2026-07-23T18:02:35.045Z
---

**User idea (2026-07-23, during the dynamic-fleet-membership rollout): to roll the HAUL fleet onto a new binary without aborting any in-flight haul, flag each hauler to drain as it COMPLETES its current mission/haul, then cycle it (remove→readd respawns on new binary).**

Context: haul is the hard fleet to update — ~21 workers, many mid-haul (in transit / trading). A synchronized drain or the shipped `remove`'s fixed 4-min force-stop would abort long hauls and strand cargo. Marketbots (35, all idle) and shuttle are easy (full down+restart+stagger, or instant on-idle cycle); haul goes LAST and careful. See [[project_fleet_pool_dynamic_membership]].

**What shipped already supports (manual, today):** watch a hauler complete + dock, then POST the dashboard remove AT THAT MOMENT — idle worker drains instantly (no 4-min wait), stops, readd respawns on the new binary. Roll the fleet by doing this per-worker as each completes = zero aborted hauls.

**The gap to automate it:** shipped membership `remove` uses a fixed `RemoveDrainTimeout` (4m0s) force-stop. A hauler flagged while mid-long-haul (>4min remaining) gets force-stopped, not allowed to finish. A true "flag now, drain whenever it next reaches idle, never force-stop" needs a per-worker **drain-on-next-idle mode with a long or disabled force-stop timeout** — a small enhancement on the membership engine (pkg/overmind/supervisor/membership.go: leavingState.deadline). Possibly expose as a "cycle on completion" flag on the dashboard remove button, or a batch "roll fleet on-completion" op that flags all workers and lets each drain at its natural boundary.

Fast-follow candidate; not built. Relates to [[reference_haul_fleet_capacity_ceiling]] [[reference_overmind_launch_commands]].
