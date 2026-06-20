# Overmind — Agent-Managing-Agent Design

**Date:** 2026-06-19
**Status:** Approved design; Phase 1 ready for implementation planning
**Supersedes:** `cmd/agent-server/` (abandoned, to be deleted)

## Problem

Recent game changes make a single agent insufficient and demand a coordinated fleet:

- **Dynamic markets** — prices shift continuously; we need an agent parked in each
  station tracking its market data.
- **Specialized production** — crafting is no longer "buildable anywhere." Many parts
  require specific production facilities that are dispersed across the galaxy and not
  every facility exists everywhere. We must track which facilities each station has and
  what they cost, and move parts/outputs around the galaxy.
- **Scale** — ~40 station-resident agents are registered, one targeted at an explicit
  station. Production goals ("we need 500k iron ore, 200k argon gas, 10k nitrogen ice
  for the next faction production line → ~30 miners") require coordinating many agents
  at once.

We want an **overmind**: an agent-managing-agent that supervises the fleet, decomposes
high-level objectives (with LLM help) into concrete assignments, runs shared/templatized
behaviors, keeps each agent checkpointed so it resumes after restart, enforces safety
guardrails, and surfaces all of this in a web mission-control UI.

The abandoned `cmd/agent-server/` is superseded; we start fresh.

## Goals

- Supervise ~40+ worker agents (spawn, monitor health, auto-restart, checkpoint).
- Reuse the existing `play_as` automation (loops, scripts, scheduler, autopilot) as the
  worker runtime — do not reimplement behavior.
- Default-deterministic workers (scripted) that can escalate to an LLM on demand.
- Strategic LLM planning at the overmind: objective → resource/labor plan.
- Safety guardrails ("break glass") present from day one: auto-act on hard dangers,
  propose on soft ones.
- Web mission-control UI: per-worker status, objectives/plans, callouts, control surface.
- Per-worker checkpoint so a restart resumes mid-task rather than from zero.

## Non-Goals (for this design / early phases)

- Multi-host distribution (workers on multiple machines). Deferred to a possible future
  registry/connect-in topology; the transport choice keeps that door open.
- Full autonomy on day one — we start as mission-control (human issues objectives) and
  migrate toward autonomous-with-override.
- A complete galaxy logistics/facility model — sketched here, gets its own spec at Phase 3.

## Roles & Worker Model

Decided during brainstorming:

- **Worker runtime = hybrid**: scripted/deterministic by default, LLM-on-demand when a
  script hits an unhandled situation or the assigned task is open-ended.
- **Two role families:**
  - **Station residents (~40)** — one parked per station. Keep market + facility data
    fresh; do station-local work that needs no travel (run a facility craft job, fulfill
    local buy orders, grind a local mission); **mine locally on idle cycles**. They are
    the always-on tracking grid.
  - **Specialized mobile workers** — haulers, salvagers, explorers, miners, etc. The
    overmind dispatches these for anything requiring travel (mining runs, inter-station
    transport).

## Architecture

Three control layers, plus cross-cutting supervisor / guardrails / UI, all hosted in one
overmind process supervising N separate worker processes.

```
                        ┌────────────────────────────────────────┐
   Human ──objectives──▶│              OVERMIND (1 process)        │
   (web UI)  ◀──state───│                                          │
                        │  Strategic brain (LLM, occasional)       │  ← Phase 3
                        │  Tactical planner (deterministic)        │  ← Phase 2
                        │  Task store + assignment + progress      │  ← Phase 2
                        │  Supervisor (spawn/restart/health)       │  ← Phase 1
                        │  Guardrail monitor (break-glass)         │  ← Phase 1
                        │  Web hub (mission control)               │  ← Phase 1
                        └───────┬──────────────┬───────────────────┘
                control channel │              │  control channel
                  (local socket)│              │
                        ┌───────▼──────┐ ┌─────▼────────┐   ... ~40+
                        │  WORKER proc │ │  WORKER proc │
                        │ resident-A   │ │  hauler-1    │
                        │ scripted +   │ │  scripted +  │
                        │ LLM-on-demand│ │  LLM-on-demand│
                        │ checkpoint.db│ │  checkpoint.db│
                        └──────┬───────┘ └──────┬───────┘
                          game WS            game WS
                               │                │
                        ┌──────▼────────────────▼─────┐
                        │   GAME SERVER  +  shared KB  │
                        └──────────────────────────────┘
```

### Package / binary layout

| Path | Status | Purpose |
|------|--------|---------|
| `cmd/overmind/` | **new** | Supervisor + brain + web binary (replaces abandoned `cmd/agent-server`) |
| `cmd/worker/` | **new** | Headless worker binary — a `play_as` engine with no REPL, driven over the control channel |
| `pkg/worker/` | **new, mostly refactor** | Worker runtime: extract reusable automation from `cmd/tools/play_as` (loop engine, scripts, scheduler, autopilot) into a library imported by BOTH `play_as` (REPL) and `cmd/worker` (headless) |
| `pkg/overmind/` | **new** | Supervisor, control-channel server, task/goal model, tactical planner, guardrail monitor |
| `pkg/overmind/brain/` | **new (Phase 3)** | Strategic LLM planner — wraps `pkg/tot` / `pkg/llm` |
| `frontend/` | extend | New "Overmind" mission-control view |
| `cmd/agent-server/` | **delete** | Abandoned; superseded |

**Key principle:** the worker is `play_as` minus the human REPL, plus a control-channel
listener. The automation primitives (`loop_block.go`, `scripts.go`, `schedule.go`,
`autopilot.go`) move into `pkg/worker` so REPL and headless worker run *identical*
behavior. `pkg/team` (the dead order-queue skeleton) is not used; its concepts are
superseded by the task model below.

## Worker Runtime

A worker is a long-lived process owning one game connection. Lifecycle:

1. Boot → load `checkpoint.db` → reconnect to game → resync (`get_notifications` +
   `get_system`) → **reconcile** against last checkpoint ("I was mid-haul carrying
   200 iron toward station X").
2. Open control channel to the overmind; send `hello` (agent id, role, station, state).
3. Run its **default standing behavior** (per-role) until assigned a task.
4. On `assign_task`: run it, stream progress/events up; on completion report and resume
   standing behavior.
5. **LLM-on-demand**: when a script hits an unhandled situation (or the task is
   open-ended), send `escalate` up; the overmind calls the LLM and returns a concrete
   script/action plan or an abort.

### Control channel

- **Transport (v1):** local **Unix domain socket**, **newline-delimited JSON (NDJSON)**,
  bidirectional, one connection per worker.
- **Rationale:** everything is local, one host, ~40 control connections, small and
  churning message set → NDJSON sweet spot. Hand-inspectable, no codegen.
- **Deferred upgrade:** **gRPC** is the intended upgrade *if/when* we adopt the
  connect-in/registry (multi-host) topology, where typed contracts + bidi streaming earn
  their keep. localhost-WebSocket is the middle path if we want to avoid a later
  migration. Message types are small enough that swapping transport is contained.

| Direction | Messages |
|-----------|----------|
| worker → overmind | `hello`, `status` (heartbeat: system/POI/docked/hull/fuel/credits/cargo), `event` (action result, danger signal), `task_progress`, `task_done`, `escalate` |
| overmind → worker | `assign_task`, `cancel_task`, `set_standing_behavior`, `abort` (stop now, optionally undock/flee), `pause`/`resume`, `escalate_reply` |

A task payload at its simplest is **a named script + params** (`script: mine_and_deposit,
poi: X, station: Y, qty: 20000`). The worker compiles that to its existing loop engine —
"task" and "play_as script" are the same substrate. We use a socket rather than piping
commands into a `play_as` REPL over stdin because we need structured status/events back
and clean abort semantics; the command grammar *inside* a task remains play_as syntax.

### Checkpoint

`data/agents/<id>/checkpoint.db` — SQLite per worker (mbox pattern: queryable,
transactional). Written at every safe boundary (task step completion, dock, deposit):

- `last_intent` — current standing behavior + active task id + step index.
- `task_journal` — append-only log of assigned tasks and their outcomes.
- `last_known_state` — system, POI, docked, cargo manifest, credits (restart reconcile).
- `cursor` — progress within a task (e.g. "mined 14000/20000 iron") so restart resumes
  mid-task, not from zero.

On restart the worker replays `last_intent` against freshly-fetched game state; if they
diverge (died mid-travel, lost cargo) it reports the discrepancy up and the overmind
re-plans rather than blindly resuming.

## Standing Behaviors & Shared Scripts

**Standing behavior** = the default loop a worker runs when it has no assigned task,
defined per role. Example for a resident:

```
schedule every 30m: view_market; <facility list>; report_up        # track data
when idle:          undock; travel $LOCAL_ASTEROID; loop -f N mine;
                    travel $STATION; dock; deposit_all              # idle local mining
```

These are parameterized `play_as` scripts. **Per-role standing behaviors live in a config
file** the overmind reads — `data/overmind/roles.yaml` mapping role → default script +
schedule — so adding a role is a config edit, not a code change.

**Shared script catalog:** a new `data/scripts/` directory of named, parameterized
templates (`mine_and_deposit`, `transport_run`, `track_station`, `grind_mission`).
Today's per-agent `scripts.go` is promoted to a shared catalog the overmind references by
name and the worker resolves locally. A task is `{script, params}` drawn from this vetted
catalog — the overmind never ships ad-hoc command strings it invented. This keeps worker
behavior auditable and safe, which matters for the autonomy migration. The catalog grows
every phase (mining, transport, tracking, mission/skilling templates).

## Guardrails (Break-Glass)

Runs in the overmind, consuming every worker's `event` + `status` stream. Declarative
threshold rules; thresholds live in overmind config.

| Signal | Authority | Default action |
|--------|-----------|----------------|
| `combat_update` / `pirate_combat` on a non-combat worker | **auto-act (hard)** | `abort` (flee/dock) + alert |
| `jettison` event not part of assigned task | **auto-act (hard)** | `abort` + alert |
| hull < X% | **auto-act (hard)** | `abort` (retreat) + alert |
| credits dropped > Y in one tick | **propose (soft)** | `pause` + alert, await human |
| worker silent > Z heartbeats | supervisor | mark unhealthy, restart |

**Authority decision:** auto-act immediately on hard dangers (combat, jettison, hull),
notify after; propose-and-wait on soft dangers (credit dip) in v1, auto-act later as
autonomy increases.

## Web Mission Control

Reuse the existing `pkg/observe` browser-hub pattern. The overmind exposes an HTTP+WS
endpoint; a new **Overmind** tab in the React frontend renders:

- **Fleet grid** — one card per worker: role, station/system, docked, hull/fuel/credits,
  current standing behavior or active task, last heartbeat age, health (green/amber/red).
  Sourced from the `status` stream.
- **Objectives & plans panel** — active objectives, decomposed tasks, % progress, owning
  worker. Mostly empty in Phase 1; populated Phase 2+.
- **Callouts/alert feed** — guardrail alerts, escalations, restarts, LLM callouts; a
  scrolling event log with severity.
- **Control surface** — per-worker pause / resume / **abort** (break-glass); global issue
  objective (Phase 2+), approve pending plan. v1 ships worker controls + abort; objective
  entry lights up when the brain lands.

The overmind keeps a live in-memory mirror of all worker status (rebuilt from `hello` +
`status` on connect) and pushes diffs to subscribed browsers — same mechanism as
`BrowserHub` today.

## Task / Goal / Brain Model (Phase 2–3)

Not built in Phase 1, but the data model is designed now so Phase 1's task plumbing fits:

- **Objective** (human- or LLM-authored): `{id, description, targets: [{item, qty}],
  deadline?, status}` — e.g. "next faction production line → 500k iron, 200k argon,
  10k nitrogen ice."
- **Strategic plan** (LLM, `pkg/overmind/brain`): objective → resource/labor breakdown
  ("~30 miners on iron at these systems, 4 haulers, 2 facility crafts"). LLM proposes;
  human approves during the Phase 1→2 migration; auto-approved later.
- **Task** (deterministic decomposition): `{id, objective_id, script, params,
  role_required, assigned_worker?, status, cursor}` — the unit the supervisor assigns and
  the worker runs. **This is the type Phase 1 already moves over the wire**, just without
  an objective behind it yet.
- **Tactical planner**: matches open tasks to available workers by role + location + idle
  state; rebalances; retries failures; respects **tracking-coverage** (don't strip every
  resident off market-tracking at once).
- **Logistics**: a galaxy model of which facilities exist where + what parts must move;
  feeds the planner. Largest unknown — gets its own spec at Phase 3.

## Phased Roadmap

| Phase | Deliverable |
|-------|-------------|
| **1 (first slice)** | Supervisor + `cmd/worker` + control channel + checkpoint + standing behaviors (residents) + guardrails + web fleet grid & abort. **No LLM brain, no mobile fleet.** Task plumbing exists but tasks are issued manually/seeded. |
| **2** | Task store + tactical planner + mobile roles (hauler/salvager) + objective entry in UI + manual plan approval. |
| **3** | Strategic LLM brain (objective → plan), logistics/facility model, migration toward autonomous + standing directives. |
| **cross-cutting** | Shared script catalog (`data/scripts/`), mission/skilling templates; grows every phase. |

## Phase 1 Scope Boundary (explicit)

**In:**
- `cmd/overmind` supervisor: spawn/monitor/auto-restart workers, health tracking.
- `cmd/worker` + `pkg/worker`: refactor play_as automation into the shared library;
  headless worker with control-channel listener.
- Control channel (NDJSON/unix socket) with the message set above.
- Per-worker SQLite checkpoint + restart reconciliation.
- Per-role standing behaviors from `data/overmind/roles.yaml` (resident role at minimum).
- Shared script catalog scaffolding (`data/scripts/`) with the resident's tracking +
  idle-local-mining scripts.
- Guardrail monitor (hard auto-act, soft propose) + alert feed.
- Web fleet grid + per-worker pause/resume/abort.
- `cmd/agent-server` deleted.

**Deferred (Phase 2+):** strategic LLM brain, objective decomposition, tactical
auto-assignment, mobile worker roles, logistics/facility model, manual/auto plan approval
flow, objective entry in UI.

## Research Spikes (parallel, before they block a phase)

- **WS-session multiplexing** — can one game socket carry multiple authenticated user
  sessions keyed by session id? If yes, collapses ~40 connections → few. *Default assumes
  no; one game WS per worker (today's reality).* Needs live-server testing.
- **Restart reconciliation fidelity** — how accurately can a worker reconstruct "what I
  was doing" from `get_notifications` / `get_system` + checkpoint after a mid-task death.

## Open Questions / Future Specs

- Logistics/facility model (Phase 3) — its own spec.
- Mission/skilling template set — which missions are reliably scriptable and shareable
  (mine N ore, transport X to Y); enumerate as the catalog grows.
- Autonomy migration policy — precise criteria for promoting soft guardrails to auto-act
  and for letting the overmind self-author objectives.
