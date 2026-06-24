# Overmind Phase 2a — Mobile Explorer Role + Swarm Roster — Design

**Date:** 2026-06-23
**Status:** Approved (brainstorming)
**Scope:** Overmind Phase 2, sub-project 2a. Give the overmind its first **mobile** worker role — an explorer that patrols systems and fills the knowledge base — and a fleet roster that lets us launch a real heterogeneous swarm. Unblocked by the autopilot/routing extraction (#2, merged 2026-06-23). Dynamic task assignment (`assign_task`) is the separate sub-project 2b and is explicitly out of scope here.

## Problem

Every overmind worker today is a **resident**: it tracks market/facility data and does station-local idle mining via `RunStanding` (scheduled commands + an idle script, all serialized on one game connection). Nothing moves between systems under overmind control. The pieces for a mobile worker now exist — `pkg/worker.WorkerDispatch` has `jump` and `autopilot` (from #2), and `pkg/worker/capture.go` already captures systems/POIs/markets — but there is no role that uses them and no logic for **where a mobile worker should go next**. The existing `pkg/strategy/explorer.go` is a stub (scans the current system and sleeps; never navigates), and `cmd/tools/play_as/explore.go` is a rich *single-system* explorer with no inter-system target selection.

## Design

The explorer is a **standing role, not a new runtime.** It reuses `RunStanding` (idle-script loop, `ExecMu` serialization, pause/abort via the existing control reader) — the same machinery residents use. Its idle script repeatedly runs one new `explore` dispatch command that navigates to the next frontier system; existing capture commands (`scan`, `kb_update`, `update_market`) record what it finds. No new driver, no `assign_task`.

The only genuinely new logic is **inter-system target selection**, which the knowledge base fully supports: `System` carries `LastVisitedTick` (0 = never visited) and `LastUpdatedTick`, and the KB exposes `GetConnections` / `GetSystems`.

### `pkg/worker/explore.go` (new)

```go
// NextExploreTarget picks the next system an explorer should visit from
// currentSystem, ranked by jump distance, preferring (1) undiscovered frontier
// systems — connection endpoints not yet in the KB's systems table — then
// (2) known-but-unvisited systems (LastVisitedTick == 0), then (3) stale known
// systems (nowTick-LastUpdatedTick > staleTicks). ok is false when nothing
// within reach is worth visiting (no frontier, everything known is fresh), in
// which case target is "".
func NextExploreTarget(ctx context.Context, kb knowledge.Base, currentSystem string, staleTicks, nowTick int64) (target string, ok bool, err error)

// ExploreDeps are the injected collaborators for Explore.
type ExploreDeps struct {
    Client     game.GameClient
    KB         knowledge.Base
    Out        io.Writer     // progress; nil -> io.Discard
    StaleTicks int64         // 0 -> DefaultExploreStaleTicks
}

// Explore performs one exploration step: resolve the current system, choose the
// next target via NextExploreTarget, and autopilot to it (capturing each hop
// via the worker's plain KB capture). When no target is found it logs and
// returns nil (the worker idles and retries on the next pass).
func Explore(ctx context.Context, deps ExploreDeps) error
```

Algorithm for `NextExploreTarget`:
1. Load `kb.GetConnections(ctx)` → `graph := navigation.JumpGraphFromConnections`. The graph's node set includes **connection endpoints that are not yet known systems** — these are the frontier.
2. BFS the graph from `currentSystem` for jump distances to every reachable node (`navigation.BFSJumps` over the union of all graph node ids).
3. Build the known-system index from `kb.GetSystems(ctx)` (id → `{LastVisitedTick, LastUpdatedTick}`). Classify each reachable node (excluding `currentSystem`, `dist < navigation.RouteInf`):
   - **frontier** — node id not in the known-system index (referenced only by a connection); highest priority.
   - **unvisited** — known but `LastVisitedTick == 0`.
   - **stale** — known, visited, and `nowTick-LastUpdatedTick > staleTicks`.
   - otherwise (known and fresh) → not a candidate.
4. Rank by class (frontier → unvisited → stale), then smallest jump distance, then system id (deterministic tie-break).
5. Return the best target, or `ok=false` when no class has a candidate.

`DefaultExploreStaleTicks` ≈ one day of ticks (system freshness threshold; ~8640 ticks at 10s/tick), matching the existing KB freshness convention.

`Explore` reuses the autopilot engine from #2: it calls `Autopilot(ctx, AutopilotDeps{Client, Out, OnWaypoint: <plain KBUpdateSystem+KBUpdatePOI>}, target, "")`, the same headless capture hook WorkerDispatch already uses for `autopilot`. The arrived system is then captured more fully by the script's `kb_update`.

### `WorkerDispatch` additions (`pkg/worker/dispatch.go`)

Add to `supported` and `Run`:
- `explore` → `Explore(ctx, ExploreDeps{Client: d.Client, KB: d.KB, Out: d.Out})`. Requires a KB (no-ops with a logged note when `d.KB == nil`).
- `scan` → `d.Client.Scan(ctx)` (passthrough; reveals current-system POIs without docking, for richer in-system capture).

`supported` additions are safe: `roles_test.go` enforces seeded-commands ⊆ supported, not the reverse.

### Catalog script (`data/scripts/explore.smolt`, new)

```
explore
scan
kb_update
update_market
```

One explore step (navigate to the next frontier system, capturing each hop) followed by in-system capture: `scan` reveals POIs, `kb_update` persists system + POIs, `update_market` snapshots the market when docked at a station (no-ops otherwise). Add `!data/scripts/explore.smolt` to the `.gitignore` script allowlist (alongside `idle_mine.smolt`, `track_station.smolt`).

### Roles (`data/overmind/roles.yaml`)

```yaml
  explorer:
    schedule:
      - { every: hourly, command: "update_market" }
    idle: explore
```

`RunStanding` runs the `explore` idle script once per idle pass (default `SleepShort`), serialized on `ExecMu`; a multi-jump autopilot completes within the pass before the next one begins.

### Roster (`data/overmind/fleet.yaml`)

Add explorer entries mapping the existing explorer accounts to **dispersed home systems — one per empire band** — so explorers fan out across the map rather than converging. Each entry is `{ agent_id, role: explorer, station: <home POI in that band> }`. The empire-band numbering convention (trailing digit encodes the band; see `project_agent_empire_bands`) drives the home assignment. Exact agent↔home mapping is filled in during implementation from the live system roster; the structure mirrors the existing resident entries.

## Data flow

```
RunStanding idle pass (explorer role, holding ExecMu):
  explore        -> NextExploreTarget(KB) -> Autopilot(target)  [captures each hop via OnWaypoint]
  scan           -> reveal current-system POIs
  kb_update      -> persist arrived system + POIs to KB
  update_market  -> market snapshot if docked at a station
  (sleep IdleInterval, repeat)

Heartbeat (every SleepTick): Status{StandingBehavior: "explorer", ...} -> overmind
```

## Error handling / safety

- **Fuel:** `Autopilot` already uses fuel cells / refuels when low; no new fuel logic here.
- **Interrupted / failed leg:** `Autopilot` returns an error (combat-cancel, no fuel after attempts, ctx cancel); `Explore` propagates it; `RunStanding.runLine` logs it best-effort and the next idle pass re-selects a target. No crash.
- **No reachable frontier:** `Explore` logs and returns nil; the worker idles and retries cheaply (the KB grows as peers report).
- **Pause / Abort:** handled by the existing control reader. Pause is honored **between** explore steps, not mid-flight (a long autopilot finishes the current step first); `Abort` exits immediately via the reader's existing path. This matches resident-worker pause semantics.
- **Combat / danger guardrails:** hard auto-flee is **Plan C** and out of scope; the explorer relies on autopilot's jump-cancel-on-combat plus the overmind's `Abort`.

## Testing

- `pkg/worker/explore_test.go` — `NextExploreTarget` over a fake `knowledge.Base` with a fixture graph: frontier (connection endpoint absent from `GetSystems`) chosen over a nearer known-unvisited system; nearest unvisited chosen when no frontier; stale-but-visited chosen when no unvisited; fresh systems ignored; unreachable systems excluded; empty candidate set → `ok=false`. Deterministic tie-break by id.
- `pkg/worker/dispatch_test.go` — extend the existing `fakeClient` (already has `FindRoute`/`Jump` from #2) with a `Scan` recorder and a fake KB; assert `Run(["explore"])` autopilots to the selected target and `Run(["scan"])` calls `Client.Scan`; assert `explore` no-ops cleanly when the KB yields no target.
- `pkg/worker/roles_test.go` / fleet load — the `explorer` role parses and validates (`explore`/`update_market` are dispatchable per the seeded-commands drift guard); `fleet.yaml` parses the explorer entries.

## Non-goals

- Survey-scanner / hidden-POI / faint-signature / anomaly extraction from `play_as/explore.go` — a later cycle.
- Dynamic task assignment (`assign_task`, task store, tactical planner) — Phase 2b.
- Salvager and hauler roles — later cycles (hauler awaits arbitrage data).
- Combat / danger auto-flee guardrails — Plan C.
- Fuel-budget round-trip planning and arbitrage-driven targeting.
- Changing the autopilot engine, routing algorithms, or capture functions — this composes them unchanged.

## Files

- Create: `pkg/worker/explore.go`, `pkg/worker/explore_test.go`
- Modify: `pkg/worker/dispatch.go` (`explore`, `scan`), `pkg/worker/dispatch_test.go`
- Create: `data/scripts/explore.smolt`; modify `.gitignore` (allowlist it)
- Modify: `data/overmind/roles.yaml` (explorer role), `data/overmind/fleet.yaml` (explorer roster)
