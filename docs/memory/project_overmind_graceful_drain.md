---
name: project_overmind_graceful_drain
description: "Future feature — overmind \"drain\" signal for clean fleet turndown (finish current task, no new work, report idle)"
metadata: 
  node_type: memory
  type: project
  originSessionId: 1e4c8a31-2e69-4e33-8b4b-1c2956839940
---

Feature request (user, 2026-06-28): add an overmind **drain** command/condition that signals agents to **finish their current task without an abort/shutdown, stop taking new work, and report when they have reached idle** — so we can do a *clean* turndown on demand.

**Why:** today stopping the overmind sends `control.TypeAbort` immediately (`cmd/overmind/main.go` shutdown path) → interrupts any in-flight haul mid-transit. Hauls resume via claim-recovery, but it is not clean (undock/flee mid-route, partial legs). A drain gives a safe quiescent state before stopping — and would have avoided the abrupt mid-haul aborts in the 2026-06-27/28 redeploys.

**Role-general (user, 2026-06-28): drain applies to EVERY standing role, each draining to its own natural safe/quiescent point — not just "stop after the current command."** Per-role drain-completion semantics:
- **Hauler:** finish the active haul — sell + complete the claim (or post the cost-order) — then idle. No new claims.
- **Miner:** keep mining until the cargo hold is full (or the current deposit is worked), **return to station, deposit cargo**, then idle. Don't strand a half-full hold in a belt.
- **Explorer / salvager / others:** finish the current survey/loot/loop to a safe point (docked / cargo settled), then idle.
- General rule: a draining worker completes its in-progress unit of work, **settles any in-progress goods (deposit/sell), returns to a safe docked state**, takes no new standing work, then reports idle. Each role implements its own "am I at a clean stopping point?" check.

**How to apply (fits the existing control plane, `pkg/overmind/control` + worker standing loop):**
- New `control.TypeDrain` (overmind → worker), distinct from `TypeAbort`. On receipt the worker sets a "draining" flag consumed by the standing loop: finish the active task per the role's drain semantics above, take **no new standing work**, then on reaching idle report it.
- The worker Status already carries `StandingBehavior` + `ActiveTaskID` (Phase-2b), so the overmind can detect idle (no active task + not mid-haul). Add an explicit "drained/idle" signal or let the overmind infer from Status.
- Overmind side: a `drain` command (all, or a group — ties to the broadcast/group-command request in [[project_current_status]]'s control-plane feature list). Track which workers have reported idle; when all are drained, log "fleet drained — safe to stop" (or auto-stop on a `--drain-then-stop` flag).
- Relates to [[project_fleet_pool_dynamic_membership]] (both are overmind lifecycle/control-plane enhancements) and [[project_overmind_fleet_manager]].

Brainstorm/spec this via the superpowers brainstorming → writing-plans flow when picked up.
