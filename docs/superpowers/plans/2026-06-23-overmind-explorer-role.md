# Overmind Explorer Role Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the overmind its first mobile worker role — an explorer that picks the nearest unexplored/stale frontier system, autopilots there, and captures it into the KB — plus the config to launch explorers in the swarm.

**Architecture:** The explorer is a standing role, not a new runtime. A new `pkg/worker/explore.go` adds frontier-first target selection (`NextExploreTarget`) over the KB jump graph and a one-step `Explore` engine that reuses the existing `Autopilot`. `WorkerDispatch` gains `explore` and `scan`. An `explore` catalog script + an `explorer` role + a fleet roster make it launchable. No `assign_task`, no new driver.

**Tech Stack:** Go 1.24, `pkg/navigation` (BFS jump graph), `pkg/knowledge` (KB), `pkg/worker` (dispatch + autopilot + capture), YAML config.

## Global Constraints

- Target Go 1.24+; use `game.Sleep*` constants for any durations.
- All new code must pass `golangci-lint` with no new findings (`golangci-lint run <pkg>` after each task). Match the package's existing `//nolint:errcheck` convention on `fmt.Fprintf`/`fmt.Fprintln` to an `io.Writer`.
- Run `go build ./...` and the relevant package tests before each commit.
- Tests exercise real behavior: pure-function tests for selection; the existing shared `fakeClient` (records method calls) plus a small `fakeKB` (embeds `knowledge.Base`, implements only `GetSystems`/`GetConnections`) for dispatch.
- This composes existing pieces unchanged — do not modify the autopilot engine, routing algorithms, or capture functions.
- Frontier-first selection: undiscovered connection endpoints rank above known-unvisited, which rank above stale; ties break by jump distance then system id.

Spec: `docs/superpowers/specs/2026-06-23-overmind-explorer-role-design.md`.

---

### Task 1: `NextExploreTarget` — frontier-first target selection

**Files:**
- Create: `pkg/worker/explore.go`
- Create: `pkg/worker/explore_test.go`

**Interfaces:**
- Produces:
  - `const DefaultExploreStaleTicks int64 = 8640`
  - `func NextExploreTarget(ctx context.Context, kb knowledge.Base, currentSystem string, staleTicks, nowTick int64) (target string, ok bool, err error)`
  - test helper `type fakeKB struct { knowledge.Base; systems []knowledge.System; conns []knowledge.Connection }` with `GetSystems`/`GetConnections` (reused by Task 2)
- Consumes: `pkg/navigation` (`JumpGraphFromConnections`, `BFSJumps`, `RouteInf`), `pkg/knowledge` (`Base`, `System`, `Connection`).

- [ ] **Step 1: Write the failing tests**

Create `pkg/worker/explore_test.go`:

```go
package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// fakeKB is a minimal knowledge.Base for selection tests: it embeds the
// interface (unimplemented methods panic) and serves seeded systems/connections.
type fakeKB struct {
	knowledge.Base
	systems []knowledge.System
	conns   []knowledge.Connection
}

func (f *fakeKB) GetSystems(context.Context) ([]knowledge.System, error) {
	return f.systems, nil
}
func (f *fakeKB) GetConnections(context.Context) ([]knowledge.Connection, error) {
	return f.conns, nil
}

func undirected(pairs ...[2]string) []knowledge.Connection {
	out := make([]knowledge.Connection, 0, len(pairs)*2)
	for _, p := range pairs {
		out = append(out,
			knowledge.Connection{FromSystem: p[0], ToSystem: p[1]},
			knowledge.Connection{FromSystem: p[1], ToSystem: p[0]},
		)
	}
	return out
}

func TestNextExploreTargetFrontierBeatsUnvisited(t *testing.T) {
	// a-b and a-c, both one jump away. c is a known unvisited system; b is a
	// frontier (a connection endpoint absent from GetSystems). Frontier wins.
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 10, LastUpdatedTick: 100},
			{ID: "c", LastVisitedTick: 0},
		},
		conns: undirected([2]string{"a", "b"}, [2]string{"a", "c"}),
	}
	target, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok || target != "b" {
		t.Fatalf("want frontier b, got target=%q ok=%v", target, ok)
	}
}

func TestNextExploreTargetNearestUnvisited(t *testing.T) {
	// No frontier (all endpoints known). b is 1 jump, c is 2. Both unvisited.
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 10, LastUpdatedTick: 100},
			{ID: "b", LastVisitedTick: 0},
			{ID: "c", LastVisitedTick: 0},
		},
		conns: undirected([2]string{"a", "b"}, [2]string{"b", "c"}),
	}
	target, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok || target != "b" {
		t.Fatalf("want nearest unvisited b, got target=%q ok=%v", target, ok)
	}
}

func TestNextExploreTargetStaleWhenNoUnvisited(t *testing.T) {
	// All known and visited; b is stale (last updated long ago), c is fresh.
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 5, LastUpdatedTick: 9_999},
			{ID: "b", LastVisitedTick: 5, LastUpdatedTick: 100},   // 9999-100 > 8640 -> stale
			{ID: "c", LastVisitedTick: 5, LastUpdatedTick: 9_900}, // fresh
		},
		conns: undirected([2]string{"a", "b"}, [2]string{"a", "c"}),
	}
	target, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 9_999)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok || target != "b" {
		t.Fatalf("want stale b, got target=%q ok=%v", target, ok)
	}
}

func TestNextExploreTargetNoneWhenAllFresh(t *testing.T) {
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 5, LastUpdatedTick: 9_990},
			{ID: "b", LastVisitedTick: 5, LastUpdatedTick: 9_990},
		},
		conns: undirected([2]string{"a", "b"}),
	}
	_, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 9_999)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Fatal("expected no target when everything reachable is fresh")
	}
}

func TestNextExploreTargetUnreachableExcluded(t *testing.T) {
	// d is a frontier but on a disconnected island (c-d), unreachable from a.
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "a", LastVisitedTick: 5, LastUpdatedTick: 100},
			{ID: "b", LastVisitedTick: 5, LastUpdatedTick: 100}, // fresh, reachable
			{ID: "c", LastVisitedTick: 5, LastUpdatedTick: 100},
		},
		conns: undirected([2]string{"a", "b"}, [2]string{"c", "d"}),
	}
	_, ok, err := NextExploreTarget(context.Background(), kb, "a", DefaultExploreStaleTicks, 200)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Fatal("unreachable frontier d must not be selected")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run TestNextExploreTarget 2>&1 | head`
Expected: build failure — `NextExploreTarget` / `DefaultExploreStaleTicks` undefined.

- [ ] **Step 3: Create `pkg/worker/explore.go` with the selection function**

Use exactly these imports for this task (Task 2 will widen the block to add `io` and `game` when it introduces `Explore`):

```go
package worker

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultExploreStaleTicks is the age, in game ticks, past which a visited
// system is worth re-capturing. ~1 day at 10s/tick (the KB system-freshness
// convention): 86400s / 10s = 8640 ticks.
const DefaultExploreStaleTicks int64 = 8640

// exploreClass ranks why a system is worth visiting; lower is higher priority.
type exploreClass int

const (
	classFrontier  exploreClass = iota // connection endpoint not yet a known system
	classUnvisited                     // known but never visited (LastVisitedTick == 0)
	classStale                         // known, visited, but stale
)

// NextExploreTarget picks the next system an explorer should visit from
// currentSystem, ranked by jump distance, preferring (1) undiscovered frontier
// systems — connection endpoints not yet in the KB's systems table — then
// (2) known-but-unvisited systems (LastVisitedTick == 0), then (3) stale known
// systems (nowTick-LastUpdatedTick > staleTicks). ok is false when nothing
// within reach is worth visiting, in which case target is "".
func NextExploreTarget(ctx context.Context, kb knowledge.Base, currentSystem string, staleTicks, nowTick int64) (string, bool, error) {
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return "", false, fmt.Errorf("explore: get connections: %w", err)
	}
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return "", false, fmt.Errorf("explore: get systems: %w", err)
	}

	graph := navigation.JumpGraphFromConnections(conns)

	// Node universe: current system, every graph endpoint, every known system.
	nodeSet := map[string]bool{currentSystem: true}
	for from, tos := range graph {
		nodeSet[from] = true
		for _, to := range tos {
			nodeSet[to] = true
		}
	}
	known := make(map[string]knowledge.System, len(systems))
	for _, s := range systems {
		known[s.ID] = s
		nodeSet[s.ID] = true
	}
	nodes := make([]string, 0, len(nodeSet))
	for id := range nodeSet {
		nodes = append(nodes, id)
	}

	dist := navigation.BFSJumps(graph, currentSystem, nodes)

	var bestID string
	var bestClass exploreClass
	bestDist := navigation.RouteInf
	have := false
	for _, id := range nodes {
		if id == currentSystem {
			continue
		}
		d, ok := dist[id]
		if !ok || d >= navigation.RouteInf {
			continue // unreachable
		}
		sys, isKnown := known[id]
		var class exploreClass
		switch {
		case !isKnown:
			class = classFrontier
		case sys.LastVisitedTick == 0:
			class = classUnvisited
		case nowTick-sys.LastUpdatedTick > staleTicks:
			class = classStale
		default:
			continue // known and fresh — skip
		}
		if !have || outranks(class, d, id, bestClass, bestDist, bestID) {
			bestID, bestClass, bestDist, have = id, class, d, true
		}
	}
	if !have {
		return "", false, nil
	}
	return bestID, true, nil
}

// outranks reports whether (class,d,id) beats the current best: lower class
// first, then smaller jump distance, then smaller system id (deterministic).
func outranks(class exploreClass, d int, id string, bClass exploreClass, bDist int, bID string) bool {
	if class != bClass {
		return class < bClass
	}
	if d != bDist {
		return d < bDist
	}
	return id < bID
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestNextExploreTarget -v`
Expected: PASS — all five tests.

- [ ] **Step 5: Build + lint**

Run: `go build ./... && golangci-lint run ./pkg/worker/`
Expected: build OK; 0 lint issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/explore.go pkg/worker/explore_test.go
git commit -m "feat(worker): frontier-first explore target selection"
```

---

### Task 2: `Explore` engine + `explore`/`scan` dispatch commands

**Files:**
- Modify: `pkg/worker/explore.go` (add `ExploreDeps` + `Explore`; extend imports with `io`, `game`)
- Modify: `pkg/worker/dispatch.go` (`supported` + `explore`/`scan` cases)
- Modify: `pkg/worker/dispatch_test.go` (add `fakeClient.Scan`; explore/scan tests)

**Interfaces:**
- Produces: `type ExploreDeps struct { Client game.GameClient; KB knowledge.Base; Out io.Writer; StaleTicks int64 }`; `func Explore(ctx context.Context, deps ExploreDeps) error`.
- Consumes: `NextExploreTarget`, `DefaultExploreStaleTicks` (Task 1); `Autopilot`/`AutopilotDeps`, `KBUpdateSystem`/`KBUpdatePOI` (existing); `game.GameClient.Scan`, `GetState`; the shared `fakeKB` from Task 1.

- [ ] **Step 1: Write the failing tests**

Add to `pkg/worker/dispatch_test.go` (the file already imports `context`, `io`, `slices`, `testing`, `game`):

```go
func (f *fakeClient) Scan(ctx context.Context) error {
	f.calls = append(f.calls, "scan")
	return nil
}

func TestDispatchScan(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	if !d.Supports("scan") {
		t.Fatal("scan should be supported")
	}
	if err := d.Run(context.Background(), []string{"scan"}); err != nil {
		t.Fatalf("Run scan: %v", err)
	}
	if !slices.Contains(f.calls, "scan") {
		t.Errorf("expected scan call, got %v", f.calls)
	}
}

func TestDispatchExploreAutopilotsToFrontier(t *testing.T) {
	// Current system "a" with a frontier neighbour "b". explore should pick b
	// and autopilot there (find_route + jump).
	f := &fakeClient{
		state: &game.State{CurrentSystem: "a", Fuel: 100, MaxFuel: 100},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}, {SystemID: "b", Name: "B"}},
	}
	kb := &fakeKB{
		systems: []knowledge.System{{ID: "a", LastVisitedTick: 5, LastUpdatedTick: 1}},
		conns:   undirected([2]string{"a", "b"}),
	}
	d := NewWorkerDispatch(f, kb, nil, io.Discard)
	if !d.Supports("explore") {
		t.Fatal("explore should be supported")
	}
	if err := d.Run(context.Background(), []string{"explore"}); err != nil {
		t.Fatalf("Run explore: %v", err)
	}
	if !slices.Contains(f.calls, "find_route:b") || !slices.Contains(f.calls, "jump:b") {
		t.Errorf("expected autopilot to frontier b, got %v", f.calls)
	}
}

func TestDispatchExploreNoTargetNoOp(t *testing.T) {
	// No connections -> nothing reachable -> explore no-ops without navigating.
	f := &fakeClient{state: &game.State{CurrentSystem: "a"}}
	kb := &fakeKB{systems: []knowledge.System{{ID: "a", LastVisitedTick: 5}}}
	d := NewWorkerDispatch(f, kb, nil, io.Discard)
	if err := d.Run(context.Background(), []string{"explore"}); err != nil {
		t.Fatalf("Run explore (no target): %v", err)
	}
	for _, c := range f.calls {
		if len(c) >= 11 && c[:11] == "find_route:" {
			t.Errorf("explore with no target must not navigate, got %v", f.calls)
		}
	}
}
```

The `dispatch_test.go` import block must include `"github.com/rsned/spacemolt/pkg/knowledge"` (for the `fakeKB` literals) — add it if not present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestDispatchScan|TestDispatchExplore' 2>&1 | head`
Expected: FAIL — `scan`/`explore` unsupported; `Explore` undefined.

- [ ] **Step 3: Add `Explore` to `pkg/worker/explore.go`**

Extend the import block to:

```go
import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)
```

Append:

```go
// ExploreDeps are the injected collaborators for one Explore step.
type ExploreDeps struct {
	Client     game.GameClient
	KB         knowledge.Base
	Out        io.Writer // progress; nil -> io.Discard
	StaleTicks int64     // 0 -> DefaultExploreStaleTicks
}

// Explore performs one exploration step: resolve the current system, choose the
// next target via NextExploreTarget, and autopilot to it (capturing each hop
// via the worker's plain KB capture). When there is no KB, no current system,
// or no reachable frontier, it logs and returns nil so the worker idles and
// retries on the next pass.
func Explore(ctx context.Context, deps ExploreDeps) error {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	if deps.KB == nil {
		fmt.Fprintln(out, "explore: no knowledge base; skipping") //nolint:errcheck
		return nil
	}
	stale := deps.StaleTicks
	if stale <= 0 {
		stale = DefaultExploreStaleTicks
	}
	state := deps.Client.GetState()
	if state == nil || state.CurrentSystem == "" {
		fmt.Fprintln(out, "explore: current system unknown; skipping") //nolint:errcheck
		return nil
	}
	target, ok, err := NextExploreTarget(ctx, deps.KB, state.CurrentSystem, stale, state.CurrentTick)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintf(out, "explore: no frontier reachable from %s; idling\n", state.CurrentSystem) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(out, "explore: heading to %s\n", target) //nolint:errcheck
	return Autopilot(ctx, AutopilotDeps{
		Client: deps.Client,
		Out:    out,
		OnWaypoint: func(ctx context.Context) error {
			if uerr := KBUpdateSystem(ctx, deps.Client, deps.KB, ""); uerr != nil {
				return uerr
			}
			return KBUpdatePOI(ctx, deps.Client, deps.KB, "")
		},
	}, target, "")
}
```

- [ ] **Step 4: Wire the dispatch commands in `pkg/worker/dispatch.go`**

Add `explore` and `scan` to the `supported` map:

```go
var supported = map[string]bool{
	"undock": true, "dock": true, "travel": true, "jump": true, "autopilot": true,
	"explore": true, "scan": true,
	"mine":   true,
	"refuel": true, "repair": true, "deposit_all": true, "sell_all": true,
	"view_market": true, "facilities": true, "kb_update": true,
	"update_market": true,
	"get_status":    true, "get_system": true, "get_cargo": true,
}
```

Add two cases to `Run`'s switch (place after the `autopilot` case):

```go
	case "explore":
		return Explore(ctx, ExploreDeps{Client: d.Client, KB: d.KB, Out: d.Out})
	case "scan":
		return d.Client.Scan(ctx)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestDispatch|TestNextExploreTarget' -v`
Expected: PASS — including the three new cases and Task 1's selection tests.

- [ ] **Step 6: Full package test + lint**

Run: `go test ./pkg/worker/ && golangci-lint run ./pkg/worker/`
Expected: all PASS; 0 issues.

- [ ] **Step 7: Commit**

```bash
git add pkg/worker/explore.go pkg/worker/dispatch.go pkg/worker/dispatch_test.go
git commit -m "feat(worker): explore + scan dispatch commands"
```

---

### Task 3: Explorer script, role, and fleet roster

**Files:**
- Create: `data/scripts/explore.smolt`
- Modify: `.gitignore` (allowlist the new script)
- Modify: `data/overmind/roles.yaml` (explorer role)
- Modify: `data/overmind/fleet.yaml` (explorer roster)

**Interfaces:**
- Consumes: dispatch commands `explore`, `scan`, `kb_update`, `update_market` (Task 2 + existing); `worker.LoadRoles`, `supervisor.LoadFleet`, `TestSeededCommandsAreDispatchable` (existing).

- [ ] **Step 1: Create the explorer idle script**

Create `data/scripts/explore.smolt`:

```
explore
scan
kb_update
update_market
```

- [ ] **Step 2: Allowlist the script in `.gitignore`**

The `data/scripts/*.smolt` glob is git-ignored with per-file negations. Add a line after the existing `!data/scripts/track_station.smolt`:

```
!data/scripts/explore.smolt
```

Verify it is now tracked:

Run: `git check-ignore data/scripts/explore.smolt && echo IGNORED || echo TRACKED`
Expected: `TRACKED`.

- [ ] **Step 3: Add the explorer role to `data/overmind/roles.yaml`**

Under `roles:` (a sibling of `resident:`), add:

```yaml
  explorer:
    schedule:
      - { every: hourly, command: "update_market" }
    idle: explore
```

- [ ] **Step 4: Add the explorer roster to `data/overmind/fleet.yaml`**

Append the twelve explorer accounts (they exist under `data/agents/explorer-*`). `station` is an optional home label; explorers disperse by where their accounts already sit, so leave it empty:

```yaml
  - { agent_id: explorer-1, role: explorer, station: "" }
  - { agent_id: explorer-2, role: explorer, station: "" }
  - { agent_id: explorer-3, role: explorer, station: "" }
  - { agent_id: explorer-4, role: explorer, station: "" }
  - { agent_id: explorer-5, role: explorer, station: "" }
  - { agent_id: explorer-6, role: explorer, station: "" }
  - { agent_id: explorer-7, role: explorer, station: "" }
  - { agent_id: explorer-8, role: explorer, station: "" }
  - { agent_id: explorer-9, role: explorer, station: "" }
  - { agent_id: explorer-10, role: explorer, station: "" }
  - { agent_id: explorer-11, role: explorer, station: "" }
  - { agent_id: explorer-12, role: explorer, station: "" }
```

- [ ] **Step 5: Verify config loads and every seeded command is dispatchable**

Run: `go test ./pkg/worker/ -run TestSeededCommandsAreDispatchable -v && go test ./pkg/overmind/supervisor/ -run TestLoadFleet -v 2>&1 | tail -5`
Expected: `TestSeededCommandsAreDispatchable` PASS (it loads `data/overmind/roles.yaml`, walks every `data/scripts/*.smolt` including `explore.smolt`, and asserts `explore`/`scan`/`kb_update`/`update_market` are all `Supports()`-ed). If there is no `TestLoadFleet`, instead run `go run ./cmd/overmind --help` is NOT needed; verify the roster parses with:

Run: `go test ./pkg/overmind/supervisor/ 2>&1 | tail -3`
Expected: PASS (the supervisor suite exercises `LoadFleet`).

- [ ] **Step 6: Sanity-check the full build + suite**

Run: `go build ./... && go test ./pkg/worker/ ./pkg/overmind/...`
Expected: build OK; all PASS.

- [ ] **Step 7: Commit**

```bash
git add data/scripts/explore.smolt .gitignore data/overmind/roles.yaml data/overmind/fleet.yaml
git commit -m "feat(overmind): explorer role, idle script, and fleet roster"
```

---

## Self-Review

**Spec coverage:**
- `NextExploreTarget` frontier-first selection (spec algorithm) → Task 1.
- `Explore` engine reusing `Autopilot` with plain capture (spec) → Task 2.
- `WorkerDispatch` `explore` + `scan` (spec) → Task 2.
- `data/scripts/explore.smolt` (explore→scan→kb_update→update_market) + `.gitignore` allowlist (spec) → Task 3.
- `explorer` role in roles.yaml + dispersed fleet roster (spec) → Task 3.
- Error handling: no KB / no current system / no target → no-op idle; autopilot errors propagate and are logged best-effort by `RunStanding` (spec) → Task 2 `Explore` + reused engine.
- Testing: selection unit tests (frontier/unvisited/stale/none/unreachable), explore+scan dispatch tests, seeded-command drift guard (spec) → Tasks 1–3.

**Placeholder scan:** None — every step has complete code/commands. Task 1 Step 3 deliberately states the final import block (`context`, `fmt`, `knowledge`, `navigation`); Task 2 Step 3 widens it to add `io`, `game`.

**Type consistency:** `NextExploreTarget(ctx, kb, currentSystem, staleTicks, nowTick) (string, bool, error)` and `DefaultExploreStaleTicks int64` defined in Task 1, consumed unchanged by `Explore` in Task 2. `ExploreDeps{Client, KB, Out, StaleTicks}` defined and consumed in Task 2's dispatch case. `fakeKB{Base, systems, conns}` + `undirected` defined in Task 1, reused in Task 2's dispatch tests. `Autopilot`/`AutopilotDeps`/`KBUpdateSystem`/`KBUpdatePOI` match the existing `pkg/worker` signatures. `game.State.CurrentSystem` (string) and `CurrentTick` (int64) match `pkg/game/types.go`.

**Non-goals honored:** no survey extraction, no `assign_task`, no new runtime, no changes to autopilot/routing/capture internals.
