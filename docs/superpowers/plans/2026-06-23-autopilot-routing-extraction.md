# Autopilot & Routing Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract play_as's autopilot execution and routing algorithms into shared packages (`pkg/navigation`, `pkg/worker`) so overmind mobile workers can navigate, leaving play_as as thin wrappers.

**Architecture:** `pkg/navigation` holds the pure routing algorithms (BFS, Held-Karp TSP, KB→graph adapter) with no game-client or output dependency. `pkg/worker/autopilot.go` holds the autopilot execution engine, writing progress to an injected `io.Writer` and calling an injected per-waypoint `CaptureFunc` (so play_as keeps its intel-file writes while cmd/worker uses plain KB capture). `WorkerDispatch` gains `jump` and `autopilot` commands. play_as `autopilot`/`plan_route` become thin wrappers over the new packages.

**Tech Stack:** Go 1.24, standard library. Tests use the existing `pkg/worker` `fakeClient` (embeds `game.GameClient`) and pure-function unit tests.

## Global Constraints

- Target Go 1.24+; use `game.Sleep*` constants for durations (the fuel helpers use `game.SleepQuick`).
- All new code must pass `golangci-lint` with no new findings; run `golangci-lint run <pkg>` after each task.
- Run `go build ./...` and the relevant package tests before each commit.
- Tests must exercise real behavior: pure functions for `pkg/navigation`; the shared `fakeClient` (real method-call recording, no behavior mocking of the unit under test) for the autopilot engine.
- This is a MOVE + re-home, not a redesign: do not change routing algorithms, fuel formulas, or `FindRoute` usage.
- DECISION (from design): play_as autopilot's `formatRaw` per-jump raw-JSON dump is DROPPED — the engine writes human-readable progress to `Out` only. The styled-vs-raw branching disappears.
- DECISION: per-waypoint capture is an injected hook — play_as passes its intel-writing wrappers, cmd/worker passes plain `worker.KBUpdate*`.

Design spec: `docs/superpowers/specs/2026-06-23-autopilot-routing-extraction-design.md`.

---

### Task 1: `pkg/navigation` — pure routing algorithms

Move the pure routing functions out of `cmd/tools/play_as/plan_route.go` into a new dependency-light package, exported, with ported tests.

**Files:**
- Create: `pkg/navigation/route.go`
- Create: `pkg/navigation/graph.go`
- Create: `pkg/navigation/route_test.go`

**Interfaces:**
- Produces:
  - `const RouteInf = 1 << 30`
  - `type JumpGraph map[string][]string`
  - `func JumpGraphFromConnections(conns []knowledge.Connection) JumpGraph`
  - `func BFSJumps(graph JumpGraph, src string, targets []string) map[string]int`
  - `func OptimalOrder(start string, waypoints []string, dist map[string]map[string]int, returnToStart bool) ([]string, int, bool)`
  - `func NearestNeighborOrder(start string, waypoints []string, dist map[string]map[string]int, returnToStart bool) ([]string, int, bool)`

- [ ] **Step 1: Write the failing tests**

Create `pkg/navigation/route_test.go`:

```go
package navigation

import (
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// buildTestGraph builds an undirected graph from edge pairs.
func buildTestGraph(edges [][2]string) JumpGraph {
	g := make(JumpGraph)
	for _, e := range edges {
		g[e[0]] = append(g[e[0]], e[1])
		g[e[1]] = append(g[e[1]], e[0])
	}
	return g
}

// distMatrix computes all-pairs jump distances for use in OptimalOrder tests.
func distMatrix(g JumpGraph, nodes []string) map[string]map[string]int {
	d := make(map[string]map[string]int)
	for _, n := range nodes {
		d[n] = BFSJumps(g, n, nodes)
	}
	return d
}

func TestBFSJumps(t *testing.T) {
	// a - b - c - d   and   a - e - d
	g := buildTestGraph([][2]string{
		{"a", "b"}, {"b", "c"}, {"c", "d"}, {"a", "e"}, {"e", "d"},
	})
	targets := []string{"a", "b", "c", "d", "e", "x"}
	got := BFSJumps(g, "a", targets)

	want := map[string]int{"a": 0, "b": 1, "c": 2, "d": 2, "e": 1, "x": RouteInf}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("BFSJumps a->%s = %d, want %d", k, got[k], v)
		}
	}
}

func TestOptimalOrder(t *testing.T) {
	g := buildTestGraph([][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}})
	nodes := []string{"a", "b", "c", "d"}
	dist := distMatrix(g, nodes)

	order, total, ok := OptimalOrder("a", []string{"d", "b", "c"}, dist, false)
	if !ok {
		t.Fatal("OptimalOrder returned not ok")
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if !slices.Equal(order, []string{"b", "c", "d"}) {
		t.Errorf("order = %v, want [b c d]", order)
	}
}

func TestOptimalOrderReturn(t *testing.T) {
	g := buildTestGraph([][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}, {"d", "a"}})
	nodes := []string{"a", "b", "c", "d"}
	dist := distMatrix(g, nodes)

	_, total, ok := OptimalOrder("a", []string{"b", "c", "d"}, dist, true)
	if !ok {
		t.Fatal("OptimalOrder returned not ok")
	}
	if total != 4 {
		t.Errorf("total = %d, want 4 (full loop)", total)
	}
}

func TestOptimalOrderUnreachable(t *testing.T) {
	g := buildTestGraph([][2]string{{"a", "b"}})
	nodes := []string{"a", "b", "z"}
	dist := distMatrix(g, nodes)

	if _, _, ok := OptimalOrder("a", []string{"b", "z"}, dist, false); ok {
		t.Error("expected not ok for unreachable waypoint, got ok")
	}
}

func TestJumpGraphFromConnections(t *testing.T) {
	conns := []knowledge.Connection{
		{FromSystem: "a", ToSystem: "b"},
		{FromSystem: "b", ToSystem: "c"},
		{FromSystem: "", ToSystem: "x"}, // skipped: empty endpoint
	}
	g := JumpGraphFromConnections(conns)
	if !slices.Contains(g["a"], "b") || !slices.Contains(g["b"], "a") {
		t.Errorf("expected undirected a<->b, got %v", g)
	}
	if !slices.Contains(g["b"], "c") || !slices.Contains(g["c"], "b") {
		t.Errorf("expected undirected b<->c, got %v", g)
	}
	if len(g["x"]) != 0 {
		t.Errorf("empty-endpoint connection should be skipped, got %v", g["x"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/navigation/ 2>&1 | head`
Expected: build failure — package `pkg/navigation` and its functions do not exist yet.

- [ ] **Step 3: Create `pkg/navigation/graph.go`**

```go
// Package navigation holds pure routing algorithms (BFS shortest-hops and
// Held-Karp / nearest-neighbor waypoint ordering) over a jump graph. It has no
// game-client or output dependency so it can be reused by worker autopilot,
// play_as plan_route, and the overmind tactical planner.
package navigation

import (
	"slices"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// JumpGraph is an undirected adjacency list where each edge is one jump.
type JumpGraph map[string][]string

// JumpGraphFromConnections builds an undirected jump graph from KB connections.
// Each edge counts as a single jump; connections with an empty endpoint are
// skipped, and duplicate edges are collapsed.
func JumpGraphFromConnections(conns []knowledge.Connection) JumpGraph {
	graph := make(JumpGraph)
	add := func(a, b string) {
		if slices.Contains(graph[a], b) {
			return
		}
		graph[a] = append(graph[a], b)
	}
	for _, c := range conns {
		if c.FromSystem == "" || c.ToSystem == "" {
			continue
		}
		add(c.FromSystem, c.ToSystem)
		add(c.ToSystem, c.FromSystem)
	}
	return graph
}
```

- [ ] **Step 4: Create `pkg/navigation/route.go`**

Port the algorithm bodies verbatim from `cmd/tools/play_as/plan_route.go` (current lines: `routeInf` 18, `bfsJumps` 301-342, `optimalOrder` 347-430, `nearestNeighborOrder` 433-468), with these renames: `routeInf`→`RouteInf`, `bfsJumps`→`BFSJumps` (param `graph map[string][]string`→`graph JumpGraph`), `optimalOrder`→`OptimalOrder`, `nearestNeighborOrder`→`NearestNeighborOrder`. The full file:

```go
package navigation

import "sort"

// RouteInf is the "unreachable" sentinel for jump distances. It is large enough
// that summing a handful of them never overflows an int.
const RouteInf = 1 << 30

// BFSJumps returns the jump distance from src to each system in targets.
// Unreachable targets map to RouteInf.
func BFSJumps(graph JumpGraph, src string, targets []string) map[string]int {
	want := make(map[string]bool, len(targets))
	for _, t := range targets {
		want[t] = true
	}

	out := make(map[string]int, len(targets))
	for _, t := range targets {
		out[t] = RouteInf
	}
	out[src] = 0

	visited := map[string]bool{src: true}
	queue := []string{src}
	found := 0
	if want[src] {
		found++
	}
	for len(queue) > 0 && found < len(want) {
		cur := queue[0]
		queue = queue[1:]
		d := out[cur]
		neighbors := graph[cur]
		// Stable iteration for deterministic tie-breaking.
		sorted := append([]string(nil), neighbors...)
		sort.Strings(sorted)
		for _, nb := range sorted {
			if visited[nb] {
				continue
			}
			visited[nb] = true
			if _, ok := out[nb]; !ok || out[nb] > d+1 {
				out[nb] = d + 1
			}
			if want[nb] {
				found++
			}
			queue = append(queue, nb)
		}
	}
	return out
}

// OptimalOrder returns the waypoint visiting order that minimizes total jumps
// from start, optionally returning to start. It uses an exact Held-Karp DP for
// small inputs and a nearest-neighbor heuristic beyond that.
func OptimalOrder(start string, waypoints []string, dist map[string]map[string]int, returnToStart bool) ([]string, int, bool) {
	m := len(waypoints)
	if m == 1 {
		total := dist[start][waypoints[0]]
		if returnToStart {
			total += dist[waypoints[0]][start]
		}
		if total >= RouteInf {
			return nil, 0, false
		}
		return waypoints, total, true
	}

	// Beyond this size Held-Karp's 2^m table is too large; fall back to a
	// nearest-neighbor heuristic (no longer guaranteed optimal).
	const heldKarpMax = 15
	if m > heldKarpMax {
		return NearestNeighborOrder(start, waypoints, dist, returnToStart)
	}

	full := (1 << m) - 1
	// dp[mask][i] = min jumps to start at `start`, visit exactly `mask`, end at i.
	dp := make([][]int, 1<<m)
	parent := make([][]int, 1<<m)
	for mask := range dp {
		dp[mask] = make([]int, m)
		parent[mask] = make([]int, m)
		for i := range dp[mask] {
			dp[mask][i] = RouteInf
			parent[mask][i] = -1
		}
	}
	for i, w := range waypoints {
		dp[1<<i][i] = dist[start][w]
	}

	for mask := 1; mask <= full; mask++ {
		for i := range waypoints {
			if mask&(1<<i) == 0 || dp[mask][i] >= RouteInf {
				continue
			}
			base := dp[mask][i]
			for j := range waypoints {
				if mask&(1<<j) != 0 {
					continue
				}
				cost := base + dist[waypoints[i]][waypoints[j]]
				next := mask | (1 << j)
				if cost < dp[next][j] {
					dp[next][j] = cost
					parent[next][j] = i
				}
			}
		}
	}

	best, bestEnd := RouteInf, -1
	for i, w := range waypoints {
		total := dp[full][i]
		if returnToStart {
			total += dist[w][start]
		}
		if total < best {
			best, bestEnd = total, i
		}
	}
	if bestEnd < 0 || best >= RouteInf {
		return nil, 0, false
	}

	// Reconstruct the order by walking parent pointers backward.
	order := make([]string, 0, m)
	mask, cur := full, bestEnd
	for cur != -1 {
		order = append(order, waypoints[cur])
		prev := parent[mask][cur]
		mask ^= 1 << cur
		cur = prev
	}
	for l, r := 0, len(order)-1; l < r; l, r = l+1, r-1 {
		order[l], order[r] = order[r], order[l]
	}
	return order, best, true
}

// NearestNeighborOrder is the heuristic fallback for large waypoint sets.
func NearestNeighborOrder(start string, waypoints []string, dist map[string]map[string]int, returnToStart bool) ([]string, int, bool) {
	remaining := make(map[string]bool, len(waypoints))
	for _, w := range waypoints {
		remaining[w] = true
	}
	order := make([]string, 0, len(waypoints))
	cur, total := start, 0
	for len(remaining) > 0 {
		next, nd := "", RouteInf
		// Deterministic: break ties by system id.
		keys := make([]string, 0, len(remaining))
		for w := range remaining {
			keys = append(keys, w)
		}
		sort.Strings(keys)
		for _, w := range keys {
			if dist[cur][w] < nd {
				next, nd = w, dist[cur][w]
			}
		}
		if next == "" || nd >= RouteInf {
			return nil, 0, false
		}
		order = append(order, next)
		total += nd
		delete(remaining, next)
		cur = next
	}
	if returnToStart {
		if dist[cur][start] >= RouteInf {
			return nil, 0, false
		}
		total += dist[cur][start]
	}
	return order, total, true
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/navigation/ -v`
Expected: PASS — all five tests.

- [ ] **Step 6: Build + lint**

Run: `go build ./... && golangci-lint run ./pkg/navigation/`
Expected: build OK (play_as still has its own copies — untouched until Task 4); 0 lint issues.

- [ ] **Step 7: Commit**

```bash
git add pkg/navigation/
git commit -m "feat(navigation): pure routing algorithms (BFS, Held-Karp, KB graph)"
```

---

### Task 2: `pkg/worker/autopilot.go` — execution engine

**Files:**
- Create: `pkg/worker/autopilot.go`
- Create: `pkg/worker/autopilot_test.go`
- Modify: `pkg/worker/dispatch_test.go` (extend the shared `fakeClient` with `FindRoute`, `Jump`, `GetRawJSON`)

**Interfaces:**
- Produces:
  - `type CaptureFunc func(ctx context.Context) error`
  - `type AutopilotDeps struct { Client game.GameClient; Out io.Writer; OnWaypoint CaptureFunc }`
  - `func Autopilot(ctx context.Context, deps AutopilotDeps, targetSystem, targetPOI string) error`
  - `func FormatDuration(seconds int) string`
- Consumes: `game.GameClient` (`FindRoute(ctx, sys) ([]game.RouteStep, error)`, `Jump(ctx, sys) (*game.JumpResult, error)`, `Travel(ctx, poi) (*game.TravelResult, error)`, `GetStatus`, `GetState`, `GetRawJSON`, `RawCommand`), `game.SleepQuick`.

- [ ] **Step 1: Extend the shared `fakeClient`**

In `pkg/worker/dispatch_test.go`, add fields and methods to the existing `fakeClient` (it embeds `game.GameClient`, so unimplemented methods panic). Add `"encoding/json"` to that file's imports.

Add fields to the struct:

```go
type fakeClient struct {
	game.GameClient // embedded; unimplemented methods panic if called
	calls           []string
	state           *game.State
	route           []game.RouteStep // returned by FindRoute
	jumpCanceled    bool             // Jump returns Canceled=true when set
}
```

Add methods:

```go
func (f *fakeClient) FindRoute(ctx context.Context, target string) ([]game.RouteStep, error) {
	f.calls = append(f.calls, "find_route:"+target)
	return f.route, nil
}
func (f *fakeClient) Jump(ctx context.Context, sys string) (*game.JumpResult, error) {
	f.calls = append(f.calls, "jump:"+sys)
	return &game.JumpResult{Canceled: f.jumpCanceled}, nil
}
func (f *fakeClient) GetRawJSON(key string) json.RawMessage { return nil }
```

- [ ] **Step 2: Write the failing test**

Create `pkg/worker/autopilot_test.go`:

```go
package worker

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func autopilotFake() *fakeClient {
	return &fakeClient{
		// Fuel full so autopilotRefuelIfNeeded no-ops; empty cargo.
		state: &game.State{Fuel: 100, MaxFuel: 100},
		route: []game.RouteStep{
			{SystemID: "sys_a", Name: "Alpha"}, // current system, skipped
			{SystemID: "sys_b", Name: "Bravo"},
			{SystemID: "sys_c", Name: "Charlie"},
		},
	}
}

func TestAutopilotJumpsEachHopAndCaptures(t *testing.T) {
	f := autopilotFake()
	var captures int
	err := Autopilot(context.Background(), AutopilotDeps{
		Client:     f,
		Out:        io.Discard,
		OnWaypoint: func(ctx context.Context) error { captures++; return nil },
	}, "sys_c", "")
	if err != nil {
		t.Fatalf("Autopilot: %v", err)
	}
	// First route entry (current system) is skipped; two jumps follow.
	if !slices.Contains(f.calls, "jump:sys_b") || !slices.Contains(f.calls, "jump:sys_c") {
		t.Fatalf("expected jumps to sys_b and sys_c, got %v", f.calls)
	}
	if captures != 2 {
		t.Errorf("OnWaypoint called %d times, want 2 (one per arrival)", captures)
	}
	// No POI -> no final travel.
	for _, c := range f.calls {
		if len(c) >= 7 && c[:7] == "travel:" {
			t.Errorf("unexpected travel call %q with empty POI", c)
		}
	}
}

func TestAutopilotTravelsToPOIAfterJumps(t *testing.T) {
	f := autopilotFake()
	err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: io.Discard}, "sys_c", "trade_hub")
	if err != nil {
		t.Fatalf("Autopilot: %v", err)
	}
	if !slices.Contains(f.calls, "travel:trade_hub") {
		t.Errorf("expected final travel:trade_hub, got %v", f.calls)
	}
}

func TestAutopilotCaptureErrorIsNonFatal(t *testing.T) {
	f := autopilotFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client:     f,
		Out:        io.Discard,
		OnWaypoint: func(ctx context.Context) error { return errors.New("kb down") },
	}, "sys_c", "")
	if err != nil {
		t.Fatalf("capture errors must be non-fatal, got %v", err)
	}
	if !slices.Contains(f.calls, "jump:sys_c") {
		t.Errorf("route should still complete despite capture errors, got %v", f.calls)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int]string{5: "5s", 60: "1m", 90: "1m 30s", 125: "2m 5s"}
	for secs, want := range cases {
		if got := FormatDuration(secs); got != want {
			t.Errorf("FormatDuration(%d) = %q, want %q", secs, got, want)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestAutopilot|TestFormatDuration' 2>&1 | head`
Expected: build failure — `Autopilot`, `AutopilotDeps`, `FormatDuration` undefined.

- [ ] **Step 4: Create `pkg/worker/autopilot.go`**

```go
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// CaptureFunc records knowledge-base state at a waypoint. Autopilot calls it
// once after each jump arrival; it is best-effort — a returned error is logged
// to the autopilot's writer and never aborts the route. nil means no capture.
type CaptureFunc func(ctx context.Context) error

// AutopilotDeps are the injected dependencies for Autopilot.
type AutopilotDeps struct {
	Client     game.GameClient
	Out        io.Writer   // progress lines; nil -> io.Discard
	OnWaypoint CaptureFunc // per-arrival capture; nil -> no-op
}

// Autopilot executes a multi-jump route to targetSystem, then travels to
// targetPOI within the destination system when targetPOI != "". It uses
// FindRoute for the route, jumps each hop (attempting fuel-cell use / refuel
// when fuel runs short), invokes OnWaypoint after each arrival, and performs
// the final in-system Travel. Returns on FindRoute failure, a jump that fails
// after fuel attempts, a jump interruption, or ctx cancellation.
func Autopilot(ctx context.Context, deps AutopilotDeps, targetSystem, targetPOI string) error {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	client := deps.Client

	fmt.Fprintf(out, "Finding route to %s...\n", targetSystem)
	route, err := client.FindRoute(ctx, targetSystem)
	if err != nil {
		return fmt.Errorf("find_route failed: %w", err)
	}
	// The first entry is the current system; a route of <=1 means we are
	// already in the target system.
	if len(route) <= 1 {
		fmt.Fprintln(out, "Already in target system (or no route found).")
		if targetPOI != "" {
			return autopilotTravelToPOI(ctx, client, out, targetPOI)
		}
		return nil
	}
	route = route[1:]

	fuelPerJump, estimatedFuel, fuelAvailable := parseFuelEstimates(client)
	totalJumps := len(route)
	fmt.Fprintf(out, "\n Route: %d jump(s) to %s\n", totalJumps, targetSystem)
	for i, step := range route {
		fmt.Fprintf(out, "   %d. %s\n", i+1, step.Name)
	}
	if estimatedFuel > 0 {
		fmt.Fprintf(out, "   Fuel: %d per jump, ~%d total, %d available\n", fuelPerJump, estimatedFuel, fuelAvailable)
		if estimatedFuel > fuelAvailable {
			fmt.Fprintf(out, "   WARNING: Not enough fuel! Need %d more.\n", estimatedFuel-fuelAvailable)
		}
	}
	// Each jump ~2 ticks travel + ~1 tick update overhead.
	estTotalTicks := totalJumps * 3
	fmt.Fprintf(out, "   Est. time: ~%d ticks (~%s)\n\n", estTotalTicks, FormatDuration(estTotalTicks*10))

	startTime := time.Now()
	for i, step := range route {
		// Check fuel before each jump — use fuel cells if below 10%.
		autopilotRefuelIfNeeded(ctx, client, out)

		remaining := ""
		if i > 0 {
			perJump := time.Since(startTime) / time.Duration(i)
			left := perJump * time.Duration(totalJumps-i)
			remaining = fmt.Sprintf(" | ETA %s", FormatDuration(int(left.Seconds())))
		}
		fmt.Fprintf(out, "[Jump %d/%d] Jumping to %s...%s\n", i+1, totalJumps, step.Name, remaining)

		result, err := client.Jump(ctx, step.SystemID)
		if err != nil {
			// Insufficient fuel: try fuel cells and retry once.
			if strings.Contains(err.Error(), "no_fuel") || strings.Contains(err.Error(), "nsufficient fuel") {
				if autopilotUseFuelCells(ctx, client, out) {
					fmt.Fprintf(out, "  Retrying jump to %s...\n", step.Name)
					result, err = client.Jump(ctx, step.SystemID)
				}
			}
			if err != nil {
				return fmt.Errorf("jump %d/%d to %s failed: %w", i+1, totalJumps, step.Name, err)
			}
		}
		if result.Canceled {
			name := targetSystem
			if state := client.GetState(); state != nil {
				name = state.System.Name
			}
			fmt.Fprintf(out, "  Jump interrupted! Stopped in %s.\n", name)
			return fmt.Errorf("autopilot interrupted at jump %d/%d (combat?)", i+1, totalJumps)
		}
		fmt.Fprintf(out, "  Arrived in %s\n", step.Name)

		if deps.OnWaypoint != nil {
			if err := deps.OnWaypoint(ctx); err != nil {
				fmt.Fprintf(out, "  (waypoint capture failed: %v)\n", err)
			}
		}
	}

	// Refresh full state so the caller's statusline shows the correct location.
	_ = client.GetStatus(ctx)
	fmt.Fprintf(out, "\n Arrived at %s in %s (%d jumps)\n",
		targetSystem, FormatDuration(int(time.Since(startTime).Seconds())), totalJumps)

	if targetPOI != "" {
		return autopilotTravelToPOI(ctx, client, out, targetPOI)
	}
	return nil
}

// autopilotTravelToPOI travels to a named POI in the current system.
func autopilotTravelToPOI(ctx context.Context, client game.GameClient, out io.Writer, targetPOI string) error {
	fmt.Fprintf(out, "Traveling to POI: %s...\n", targetPOI)
	result, err := client.Travel(ctx, targetPOI)
	if err != nil {
		return fmt.Errorf("travel to %s failed: %w", targetPOI, err)
	}
	if result.Canceled {
		return fmt.Errorf("travel to %s was interrupted", targetPOI)
	}
	fmt.Fprintf(out, "  Arrived at %s\n", result.POI)
	return nil
}

// parseFuelEstimates extracts fuel info from the cached find_route response.
func parseFuelEstimates(client game.GameClient) (fuelPerJump, estimatedFuel, fuelAvailable int) {
	raw := client.GetRawJSON("_last")
	if raw == nil {
		return 0, 0, 0
	}
	var resp struct {
		FuelPerJump   int `json:"fuel_per_jump"`
		EstimatedFuel int `json:"estimated_fuel"`
		FuelAvailable int `json:"fuel_available"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, 0, 0
	}
	return resp.FuelPerJump, resp.EstimatedFuel, resp.FuelAvailable
}

// autopilotUseFuelCells uses all fuel_cell items in cargo. Returns true if any
// were used.
func autopilotUseFuelCells(ctx context.Context, client game.GameClient, out io.Writer) bool {
	state := client.GetState()
	if state == nil {
		return false
	}
	used := false
	for _, item := range state.Ship.Cargo {
		if !strings.Contains(strings.ToLower(item.ItemID), "fuel_cell") || item.Quantity < 1 {
			continue
		}
		qty := int(item.Quantity)
		fmt.Fprintf(out, "  Fuel low — using %d %s from cargo...\n", qty, item.ItemID)
		if err := client.RawCommand(ctx, "use_item", map[string]any{
			"item_id":  item.ItemID,
			"quantity": qty,
		}); err != nil {
			fmt.Fprintf(out, "  Warning: use_item %s failed: %v\n", item.ItemID, err)
			continue
		}
		time.Sleep(game.SleepQuick)
		used = true
	}
	if used {
		// Refresh state — RawCommand doesn't update internal fuel/cargo state.
		_ = client.GetStatus(ctx)
		time.Sleep(game.SleepQuick)
		if state = client.GetState(); state != nil && state.MaxFuel > 0 {
			fmt.Fprintf(out, "  Fuel now: %.0f/%.0f (%.0f%%)\n", state.Fuel, state.MaxFuel, (state.Fuel/state.MaxFuel)*100)
		}
	}
	return used
}

// autopilotRefuelIfNeeded uses fuel_cell items from cargo when fuel is below 10%.
func autopilotRefuelIfNeeded(ctx context.Context, client game.GameClient, out io.Writer) {
	state := client.GetState()
	if state == nil || state.MaxFuel == 0 {
		return
	}
	fuelPct := (state.Fuel / state.MaxFuel) * 100
	if fuelPct >= 10 {
		return
	}
	for _, item := range state.Ship.Cargo {
		if !strings.Contains(strings.ToLower(item.ItemID), "fuel_cell") || item.Quantity < 1 {
			continue
		}
		fmt.Fprintf(out, "  Fuel low (%.0f%%) — using %s from cargo...\n", fuelPct, item.ItemID)
		if err := client.RawCommand(ctx, "use_item", map[string]any{"item_id": item.ItemID}); err != nil {
			fmt.Fprintf(out, "  Warning: use_item %s failed: %v\n", item.ItemID, err)
			return
		}
		time.Sleep(game.SleepQuick)
		_ = client.GetStatus(ctx)
		time.Sleep(game.SleepQuick)
		if state = client.GetState(); state != nil && state.MaxFuel > 0 {
			fmt.Fprintf(out, "  Fuel now: %.0f/%.0f (%.0f%%)\n", state.Fuel, state.MaxFuel, (state.Fuel/state.MaxFuel)*100)
		}
		return
	}
	fmt.Fprintf(out, "  WARNING: Fuel low (%.0f%%) and no fuel cells in cargo!\n", fuelPct)
}

// FormatDuration formats seconds as "Xm Ys", "Xm", or "Xs".
func FormatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	m := seconds / 60
	s := seconds % 60
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestAutopilot|TestFormatDuration' -v`
Expected: PASS — all four tests.

- [ ] **Step 6: Full package test + lint**

Run: `go build ./... && go test ./pkg/worker/ && golangci-lint run ./pkg/worker/`
Expected: build OK; all worker tests PASS; 0 lint issues.

- [ ] **Step 7: Commit**

```bash
git add pkg/worker/autopilot.go pkg/worker/autopilot_test.go pkg/worker/dispatch_test.go
git commit -m "feat(worker): autopilot execution engine with injected capture hook"
```

---

### Task 3: WorkerDispatch `jump` + `autopilot` commands

**Files:**
- Modify: `pkg/worker/dispatch.go`
- Modify: `pkg/worker/dispatch_test.go`

**Interfaces:**
- Consumes: `Autopilot`, `AutopilotDeps`, `CaptureFunc` (Task 2); `KBUpdateSystem(ctx, client, kb, detectedBy)`, `KBUpdatePOI(ctx, client, kb, detectedBy)` (existing in `pkg/worker/capture.go`); `game.GameClient.Jump`.

- [ ] **Step 1: Write the failing tests**

Add to `pkg/worker/dispatch_test.go`:

```go
func TestDispatchJump(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	if !d.Supports("jump") {
		t.Fatal("jump should be supported")
	}
	if err := d.Run(context.Background(), []string{"jump", "sys_z"}); err != nil {
		t.Fatalf("Run jump: %v", err)
	}
	if !slices.Contains(f.calls, "jump:sys_z") {
		t.Errorf("expected jump:sys_z, got %v", f.calls)
	}
}

func TestDispatchAutopilot(t *testing.T) {
	f := autopilotFake()
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	if !d.Supports("autopilot") {
		t.Fatal("autopilot should be supported")
	}
	if err := d.Run(context.Background(), []string{"autopilot", "sys_c"}); err != nil {
		t.Fatalf("Run autopilot: %v", err)
	}
	if !slices.Contains(f.calls, "find_route:sys_c") || !slices.Contains(f.calls, "jump:sys_c") {
		t.Errorf("expected find_route + jumps, got %v", f.calls)
	}
}
```

Add `"slices"` to the `dispatch_test.go` imports if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestDispatchJump|TestDispatchAutopilot' -v`
Expected: FAIL — `jump`/`autopilot` not supported (Run returns "unsupported command").

- [ ] **Step 3: Add the commands**

In `pkg/worker/dispatch.go`, add `"jump"` and `"autopilot"` to the `supported` map:

```go
var supported = map[string]bool{
	"undock": true, "dock": true, "travel": true, "jump": true, "autopilot": true,
	"mine": true, "refuel": true, "repair": true, "deposit_all": true, "sell_all": true,
	"view_market": true, "facilities": true, "kb_update": true,
	"update_market": true,
	"get_status":    true, "get_system": true, "get_cargo": true,
}
```

In `Run`'s switch, add these cases (place after the `travel` case):

```go
	case "jump":
		if len(args) < 1 {
			return fmt.Errorf("jump: missing target system")
		}
		_, err := d.Client.Jump(ctx, args[0])
		return err
	case "autopilot":
		if len(args) < 1 {
			return fmt.Errorf("autopilot: missing target system")
		}
		poi := ""
		if len(args) >= 2 {
			poi = args[1]
		}
		return Autopilot(ctx, AutopilotDeps{
			Client: d.Client,
			Out:    d.Out,
			OnWaypoint: func(ctx context.Context) error {
				if d.KB == nil {
					return nil
				}
				if err := KBUpdateSystem(ctx, d.Client, d.KB, ""); err != nil {
					return err
				}
				return KBUpdatePOI(ctx, d.Client, d.KB, "")
			},
		}, args[0], poi)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestDispatch' -v`
Expected: PASS — including the existing `TestDispatchRunsKnownCommands` and the two new cases.

- [ ] **Step 5: Full package test + lint**

Run: `go test ./pkg/worker/ && golangci-lint run ./pkg/worker/`
Expected: all PASS; 0 issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/dispatch.go pkg/worker/dispatch_test.go
git commit -m "feat(worker): add jump and autopilot to WorkerDispatch"
```

---

### Task 4: play_as thin wrappers

Repoint play_as at the shared packages: `autopilot` calls the worker engine with its intel-writing capture hook; `plan_route` calls `pkg/navigation`; `explore.go` uses `worker.FormatDuration`. Delete the now-moved code.

**Files:**
- Modify: `cmd/tools/play_as/autopilot.go`
- Modify: `cmd/tools/play_as/plan_route.go`
- Modify: `cmd/tools/play_as/explore.go`
- Modify: `cmd/tools/play_as/plan_route_test.go`

**Interfaces:**
- Consumes: `worker.Autopilot`/`worker.AutopilotDeps`/`worker.FormatDuration` (Tasks 2), `navigation.JumpGraph`/`JumpGraphFromConnections`/`BFSJumps`/`OptimalOrder`/`RouteInf` (Task 1). Uses existing play_as `kbUpdateSystem`/`kbUpdatePOI` (`kb_update.go`), `globalKB`, `resolveSystemToken`, `currentJumpFuel`, `displayName`, `plural`, `isReturnFlag`.

- [ ] **Step 1: Replace `cmd/tools/play_as/autopilot.go` with a thin wrapper**

Replace the ENTIRE file contents with:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/worker"
)

// autopilot executes a multi-jump route to a target system via the shared
// worker engine, updating the KB (and writing intel files) at each waypoint.
// Usage: autopilot <system> [poi]
func autopilot(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	if len(parts) < 2 {
		return fmt.Errorf("usage: autopilot <system-name> [poi-name]")
	}
	targetSystem := parts[1]
	targetPOI := ""
	if len(parts) >= 3 {
		targetPOI = strings.Join(parts[2:], " ")
	}

	// Styled mode prints progress; raw mode suppresses it (the legacy per-jump
	// JSON dump is intentionally dropped — workers never used it).
	out := io.Writer(os.Stdout)
	if format != formatStyled {
		out = io.Discard
	}

	return worker.Autopilot(ctx, worker.AutopilotDeps{
		Client: client,
		Out:    out,
		// play_as preserves its per-waypoint intel-file writes via these wrappers.
		OnWaypoint: func(ctx context.Context) error {
			if globalKB == nil {
				return nil
			}
			if err := kbUpdateSystem(client, ctx); err != nil {
				return err
			}
			return kbUpdatePOI(client, ctx)
		},
	}, targetSystem, targetPOI)
}
```

This removes `autopilotTravelToPOI`, `parseFuelEstimates`, `autopilotUseFuelCells`, `autopilotRefuelIfNeeded`, and `formatDuration` from play_as (all now live in `pkg/worker`).

- [ ] **Step 2: Repoint `plan_route.go` at `pkg/navigation`**

In `cmd/tools/play_as/plan_route.go`:

a. Delete the local `routeInf` const (line 18), and the functions `buildJumpGraph` (269-275), `jumpGraphFromConnections` (281-297), `bfsJumps` (301-342), `optimalOrder` (347-430), `nearestNeighborOrder` (433-468). KEEP `isReturnFlag`, `currentJumpFuel`, `plural`, `displayName`, `resolveSystemToken`.

b. Add `"github.com/rsned/spacemolt/pkg/navigation"` to imports. Remove `"slices"`/`"sort"`/`"knowledge"` imports ONLY if no longer used after the deletions (verify with the compiler — `resolveSystemToken` uses none of them; `slices.ContainsFunc` is still used at line 37, so keep `slices`; `knowledge` is no longer referenced once `jumpGraphFromConnections` is gone, so remove it; `sort` is no longer referenced, remove it).

c. Replace the graph-building block. The current lines 110-121:

```go
	// Build the connection graph (undirected, one jump per edge).
	graph, err := buildJumpGraph(ctx)
	if err != nil {
		return err
	}

	// Compute pairwise jump distances among the start and all waypoints.
	nodes := append([]string{startID}, waypoints...)
	dist := make(map[string]map[string]int, len(nodes))
	for _, n := range nodes {
		dist[n] = bfsJumps(graph, n, nodes)
	}
```

become:

```go
	// Build the connection graph (undirected, one jump per edge).
	conns, err := globalKB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("load connections: %w", err)
	}
	graph := navigation.JumpGraphFromConnections(conns)

	// Compute pairwise jump distances among the start and all waypoints.
	nodes := append([]string{startID}, waypoints...)
	dist := make(map[string]map[string]int, len(nodes))
	for _, n := range nodes {
		dist[n] = navigation.BFSJumps(graph, n, nodes)
	}
```

d. Replace the reachability sentinel check (line 125) `if dist[startID][w] >= routeInf {` with `if dist[startID][w] >= navigation.RouteInf {`.

e. Replace the solver call (line 132) `order, total, ok := optimalOrder(startID, waypoints, dist, returnToStart)` with `order, total, ok := navigation.OptimalOrder(startID, waypoints, dist, returnToStart)`.

- [ ] **Step 3: Repoint `explore.go` at `worker.FormatDuration`**

In `cmd/tools/play_as/explore.go`, the call at line 88 uses `formatDuration(totalTicks*10)`. Replace `formatDuration(` with `worker.FormatDuration(` and add `"github.com/rsned/spacemolt/pkg/worker"` to its imports (if not already imported).

- [ ] **Step 4: Trim `plan_route_test.go`**

In `cmd/tools/play_as/plan_route_test.go`, delete `buildTestGraph`, `distMatrix`, `TestBFSJumps`, `TestOptimalOrder`, `TestOptimalOrderReturn`, `TestOptimalOrderUnreachable` (now covered in `pkg/navigation/route_test.go`). KEEP `TestResolveSystemToken`. Remove the now-unused `"slices"` import. The file should retain only the `package main`, `"testing"` import, and `TestResolveSystemToken`.

- [ ] **Step 5: Build, test, lint play_as**

Run: `go build ./... && go test ./cmd/tools/play_as/ && golangci-lint run ./cmd/tools/play_as/`
Expected: build OK (no undefined references, no unused imports); play_as tests PASS (`TestResolveSystemToken` plus the rest of the package); 0 lint issues.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/autopilot.go cmd/tools/play_as/plan_route.go cmd/tools/play_as/explore.go cmd/tools/play_as/plan_route_test.go
git commit -m "refactor(play_as): autopilot/plan_route thin wrappers over shared packages"
```

---

### Task 5: Full-suite verification

**Files:** none (verification only).

- [ ] **Step 1: Build, full suite, lint the touched packages**

Run:
```bash
go build ./... && \
go test ./pkg/navigation/... ./pkg/worker/... ./cmd/tools/play_as/... && \
go test ./... && \
golangci-lint run ./pkg/navigation/ ./pkg/worker/ ./cmd/tools/play_as/
```
Expected: build OK; targeted packages PASS; full suite PASS; 0 lint issues.

- [ ] **Step 2: Confirm no orphaned references**

Run: `grep -rn "formatDuration\|bfsJumps\|optimalOrder\|jumpGraphFromConnections\|routeInf" cmd/tools/play_as/`
Expected: no matches (all moved/renamed). `resolveSystemToken`, `currentJumpFuel`, `displayName`, `plural`, `isReturnFlag` may still appear and are fine.

- [ ] **Step 3: Confirm the binaries build to bin/**

Run: `go build -o bin/worker ./cmd/worker/ && go build -o bin/play_as ./cmd/tools/play_as/`
Expected: both build.

---

## Self-Review

**Spec coverage:**
- `pkg/navigation` pure routing (spec) → Task 1.
- `pkg/worker/autopilot.go` engine + `AutopilotDeps`/`CaptureFunc` + fuel helpers + `FormatDuration` (spec) → Task 2.
- `WorkerDispatch` `jump` + `autopilot` with plain capture (spec) → Task 3.
- play_as thin wrappers: `autopilot` intel-hook parity, `plan_route` → navigation, `explore.go` → `worker.FormatDuration`, `resolveSystemToken` stays (spec) → Task 4.
- Drop autopilot raw-JSON dump (spec DECISION) → Task 4 Step 1 (raw mode → `io.Discard`).
- Testing: ported routing tests (Task 1), new engine tests incl. capture-non-fatal + POI + per-hop (Task 2), dispatch jump/autopilot (Task 3), kept `TestResolveSystemToken` (Task 4). Error handling (FindRoute/jump/cancel; capture best-effort) covered by engine code + `TestAutopilotCaptureErrorIsNonFatal`.

**Placeholder scan:** None — every step has complete code and exact commands.

**Type consistency:** `JumpGraph`/`BFSJumps`/`OptimalOrder`/`NearestNeighborOrder`/`RouteInf`/`JumpGraphFromConnections` defined in Task 1 and used unchanged in Task 4. `Autopilot`/`AutopilotDeps`/`CaptureFunc`/`FormatDuration` defined in Task 2, consumed in Tasks 3-4. `fakeClient` extended in Task 2 (`FindRoute`/`Jump`/`GetRawJSON` + `route`/`jumpCanceled` fields), reused in Tasks 2-3 (`autopilotFake`). `KBUpdateSystem`/`KBUpdatePOI` signatures `(ctx, client, kb, detectedBy)` match `pkg/worker/capture.go`.
