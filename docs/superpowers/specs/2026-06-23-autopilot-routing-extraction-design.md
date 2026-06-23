# Autopilot & Routing Extraction — Design

**Date:** 2026-06-23
**Status:** Approved (brainstorming)
**Scope:** Overmind Phase 2 prerequisite (#2). Extract play_as's autopilot execution and routing algorithms into shared packages so overmind mobile workers (and a future tactical planner) can navigate. Mirrors the Plan B consolidation that already moved play_as automation into `pkg/worker`.

## Problem

Navigation logic lives only in the interactive `play_as` tool:

- `cmd/tools/play_as/autopilot.go` — multi-jump route execution (`FindRoute` → jump loop → fuel management → per-waypoint KB capture → final in-system `Travel`), plus fuel helpers. ~325 lines. Writes progress via `fmt.Printf`; depends on `globalKB`, `globalAgentID`, and play_as's `kbUpdateSystem`/`kbUpdatePOI` wrappers (which also write intel files).
- `cmd/tools/play_as/plan_route.go` — multi-waypoint route planning: BFS shortest-hops, Held-Karp TSP (≤15 waypoints) with a nearest-neighbor fallback, and a KB→graph adapter. ~468 lines. Pure algorithms wrapped in `fmt.Printf` display.

`pkg/worker.WorkerDispatch` (the headless worker command surface) has single-hop `travel` but **no** `jump` or `autopilot`, so a mobile worker cannot navigate to another system. The `autopilot` command uses the server's `FindRoute` for its route and does **not** use `plan_route`'s local BFS/TSP, so the two are cleanly separable.

## Design

Two new homes plus thin `play_as` wrappers. One execution engine, two callers (worker + play_as) differing only in injected output and capture hook.

### `pkg/navigation` (new — pure routing)

No game client, no worker runtime, no output. Pure functions returning data.

`route.go`:
- `const RouteInf = 1 << 30` — unreachable sentinel.
- `func BFSJumps(graph JumpGraph, src string, targets []string) map[string]int` — single-source shortest hop counts; unreachable targets map to `RouteInf`.
- `func OptimalOrder(start string, waypoints []string, dist map[string]map[string]int, returnToStart bool) (order []string, totalJumps int, ok bool)` — exact Held-Karp DP for `len(waypoints) <= 15`, nearest-neighbor heuristic above that. `ok=false` if any waypoint is unreachable.
- `func NearestNeighborOrder(start string, waypoints []string, dist map[string]map[string]int, returnToStart bool) (order []string, totalJumps int, ok bool)`.

`graph.go`:
- `type JumpGraph map[string][]string` — adjacency list.
- `func JumpGraphFromConnections(conns []knowledge.Connection) JumpGraph` — KB adapter. Imports `pkg/knowledge` only for the `Connection` type. (`pkg/knowledge` does not import `pkg/navigation`, so no cycle.)

These are the current `plan_route.go` functions (`bfsJumps`, `optimalOrder`, `nearestNeighborOrder`, `jumpGraphFromConnections`), made exported and moved verbatim in logic. `resolveSystemToken` stays in play_as (CLI token parsing, not routing).

### `pkg/worker/autopilot.go` (new — execution engine)

```go
// CaptureFunc records KB state at a waypoint; called once per arrival.
// Best-effort: a returned error is logged, not fatal.
type CaptureFunc func(ctx context.Context) error

type AutopilotDeps struct {
    Client     game.GameClient
    Out        io.Writer    // progress lines; nil -> io.Discard
    OnWaypoint CaptureFunc  // per-waypoint capture; nil -> no-op
}

// Autopilot executes a multi-jump route to target (a system id/name), then
// travels to poi within the destination system if poi != "". It calls
// FindRoute for the route, jumps each hop (attempting fuel-cell use / refuel
// when fuel is short), invokes OnWaypoint after each arrival, and performs the
// final in-system Travel. Returns on FindRoute failure, a jump that fails after
// fuel attempts, or ctx cancellation.
func Autopilot(ctx context.Context, deps AutopilotDeps, target, poi string) error
```

The fuel helpers move here as unexported functions of this file: `parseFuelEstimates(client) (fuelPerJump, estimatedFuel, fuelAvailable int)`, `autopilotUseFuelCells(ctx, client, out) bool`, `autopilotRefuelIfNeeded(ctx, client, out)`. `formatDuration` is currently a play_as helper used by both `autopilot.go` and `explore.go`; it moves to `pkg/worker` as exported `FormatDuration(seconds int) string`. The autopilot engine uses it internally, and play_as's `explore.go` is updated to call `worker.FormatDuration` — single definition, no duplication. The local `formatDuration` in play_as is removed.

### `WorkerDispatch` additions (`pkg/worker/dispatch.go`)

Add to `supported`: `"jump"`, `"autopilot"`. In `Run`:
- `case "jump"`: `d.Client.Jump(ctx, args[0])` (arg required).
- `case "autopilot"`: `Autopilot(ctx, AutopilotDeps{Client: d.Client, Out: d.Out, OnWaypoint: <plain capture>}, args[0], <optional poi arg>)` where the plain capture closure calls `KBUpdateSystem` then `KBUpdatePOI` (no intel files).

`supported` adding extra entries is safe: `roles_test.go` enforces seeded-commands ⊆ supported, not the reverse.

### play_as wrappers (parity preserved)

- `cmd/tools/play_as/autopilot.go`: `autopilot(client, ctx, parts, format)` becomes a thin wrapper that calls `worker.Autopilot(ctx, worker.AutopilotDeps{Client: client, Out: os.Stdout, OnWaypoint: <intel capture>}, system, poi)`. The intel capture closure calls play_as's existing `kbUpdateSystem`/`kbUpdatePOI` wrappers, **preserving the per-waypoint intel-file writes**. `autopilotTravelToPOI` is removed; the final-leg travel is handled inside the engine.
- `cmd/tools/play_as/plan_route.go`: keeps all display/formatting `fmt.Printf`; deletes the local `bfsJumps`/`optimalOrder`/`nearestNeighborOrder`/`jumpGraphFromConnections` and calls the `pkg/navigation` equivalents. `resolveSystemToken` and `currentJumpFuel` stay local.
- Dispatch cases in `main.go` (`"autopilot"`/`"ap"`, `"plan_route"`/`"plan-route"`) are unchanged in wiring; they call the now-thin wrappers.

## Data flow

```
worker (headless):  RunStanding / WorkerDispatch.Run("autopilot", [sys, poi?])
                      └─> worker.Autopilot(Out=worker stdout, OnWaypoint=plain KB capture)

play_as (interactive): autopilot cmd
                      └─> worker.Autopilot(Out=os.Stdout, OnWaypoint=intel-writing KB capture)

both:  Autopilot ─> Client.FindRoute ─> for each hop: [fuel check] Client.Jump ─> OnWaypoint
                 ─> Client.Travel(final poi)

plan_route (play_as): build []knowledge.Connection from KB
                      ─> navigation.JumpGraphFromConnections ─> navigation.BFSJumps / OptimalOrder
                      ─> play_as formats + prints the route
```

## Error handling

- `Autopilot` returns an error on: `FindRoute` failure, a hop's `Jump` failing after fuel attempts, or `ctx` cancellation. This mirrors the current play_as behavior.
- `OnWaypoint` capture errors are **best-effort**: logged to `Out`, never abort the route (matches today's autopilot, where KB capture is non-fatal).
- `pkg/navigation` functions do not error; unreachable routing is signaled by `RouteInf` / `ok=false`.

## Testing

- `pkg/navigation/route_test.go` — port the existing `plan_route_test.go` cases: BFS hop counts on a fixture graph, Held-Karp optimal order on a linear chain, return-to-start ordering, and an unreachable waypoint. Pure, deterministic, no mocks. (`TestResolveSystemToken` stays with `resolveSystemToken` in play_as.)
- `pkg/worker/autopilot_test.go` (new — no autopilot test exists today) — drive `Autopilot` with a fake `game.GameClient` whose `FindRoute` returns a canned multi-hop route:
  - asserts `Jump` is called once per hop in route order;
  - asserts `OnWaypoint` is invoked once per arrival (count == waypoints);
  - asserts a final `Travel` to the POI when `poi != ""`, and none when `poi == ""`;
  - a fuel-low fixture asserts a fuel-cell/refuel attempt occurs before the jump;
  - a capture hook that returns an error asserts the route still completes (best-effort).
- `pkg/worker/dispatch_test.go` — add cases asserting `Run(["jump", sysID])` calls `Client.Jump` and `Run(["autopilot", sys])` invokes the engine (fake client), and that `Supports("jump")`/`Supports("autopilot")` are true.

## Non-goals

- Multi-waypoint planning for the overmind tactical layer beyond exposing the `pkg/navigation` algorithms — no new planner is built here.
- `assign_task` / mobile-role wiring in roles.yaml — that is the next Phase 2 step, unblocked by this extraction.
- Changing the routing algorithms, fuel formulas, or `FindRoute` usage — this is a move + re-home, not a redesign.
- The pairwise jump-distance cache (`globalGraphCache`, currently unused) — left as-is.

## Files

- Create: `pkg/navigation/route.go`, `pkg/navigation/graph.go`, `pkg/navigation/route_test.go`
- Create: `pkg/worker/autopilot.go`, `pkg/worker/autopilot_test.go`
- Modify: `pkg/worker/dispatch.go`, `pkg/worker/dispatch_test.go`
- Modify: `cmd/tools/play_as/autopilot.go` (thin wrapper), `cmd/tools/play_as/plan_route.go` (call navigation), `cmd/tools/play_as/explore.go` (call `worker.FormatDuration`)
