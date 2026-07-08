# show_system Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `show_system <id>` play_as REPL command that renders what the local knowledge base knows about a remote system, enriched beyond the live `get_system` output.

**Architecture:** A pure KB read (no server call). One new file `cmd/tools/play_as/show_system.go` holds three functions: `suggestSystems` (fuzzy not-found suggestions), `renderSystem` (pure string builder over KB structs), and `runShowSystem` (orchestration: KB reads → render). Dispatch is one case in `main.go`; the command name is added to the completer.

**Tech Stack:** Go 1.24, SQLite knowledge base (`pkg/knowledge`), existing play_as helpers (`globalKB`, `globalClock`, `formatAge`, `trimFloat`).

## Global Constraints

- Go 1.24+; use modern idioms (range-over-int where natural).
- Must pass `golangci-lint` with zero new findings.
- Reuse existing helpers; do not duplicate `formatAge` or `trimFloat`.
- The command is a local KB read — it never calls the game server and has no tick cost.
- No JSON output mode (matches `find_item`, which prints directly to stdout).

**Spec:** `docs/superpowers/specs/2026-07-07-show-system-command-design.md`

### Key existing types (verbatim, for reference)

```go
// pkg/knowledge/memory.go
type SystemConnection struct { SystemID string; Distance int }
type System struct {
	ID, Name, Description string
	Position        game.Position
	PoliceLevel     int
	SecurityStatus  string
	Empire          string
	IsStronghold    bool
	Connections     []SystemConnection
	POIs            []string
	LastUpdatedTick int64
	LastVisitedTick int64
}
func (s System) Visited() bool { return s.LastVisitedTick > 0 }
type POI struct {
	ID, SystemID, Name, Type, Class, Description string
	Position         game.Position
	Services         []string
	Resources        []game.POIResource
	Hidden           bool
	RevealDifficulty int
	ExpiresAt        string
	LastUpdatedTick  int64
	DetectedBy       string
}
// pkg/game/types.go
type POIResource struct { ResourceID string; Richness float64; Remaining float64 }
```

### KB interface methods used (verbatim)

```go
GetSystem(ctx context.Context, systemID string) (*System, error)   // nil,err when unknown
GetPOIs(ctx context.Context, systemID string) ([]POI, error)
GetSystems(ctx context.Context) ([]System, error)
```

### Existing play_as helpers used (verbatim)

```go
var globalKB knowledge.Base          // main.go:47 (may be nil if --db-path absent)
var globalClock *game.GameClock      // has .Tick() int64; may be nil
func formatAge(ticks int64) string   // nearest.go:139 ("N ticks ago" / "~N hours ago"; "unknown" if <0)
func trimFloat(f float64) string     // find_item.go:102
```

---

### Task 1: `suggestSystems` fuzzy matcher

**Files:**
- Create: `cmd/tools/play_as/show_system.go`
- Test: `cmd/tools/play_as/show_system_test.go`

**Interfaces:**
- Produces: `func suggestSystems(query string, systems []knowledge.System) []string` — returns up to 3 candidate system ids for an unknown `query`, ranked by: (1) case-insensitive substring match on id or name, then (2) Levenshtein distance ≤ 2 against id or name. Empty slice when nothing is close.
- Produces (internal): `func levenshtein(a, b string) int`.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestSuggestSystems(t *testing.T) {
	systems := []knowledge.System{
		{ID: "nexus_prime", Name: "Nexus Prime"},
		{ID: "nova_terra", Name: "Nova Terra"},
		{ID: "sol", Name: "Sol"},
	}
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"typo within distance 2", "nexis_prime", []string{"nexus_prime"}},
		{"substring on id", "nova", []string{"nova_terra"}},
		{"no match", "zzzzzz", nil},
		{"caps insensitive substring", "SOL", []string{"sol"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suggestSystems(tt.query, systems)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("suggestSystems(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestSuggestSystemsLimitsToThree(t *testing.T) {
	systems := []knowledge.System{
		{ID: "node_a"}, {ID: "node_b"}, {ID: "node_c"}, {ID: "node_d"},
	}
	if got := suggestSystems("node", systems); len(got) != 3 {
		t.Errorf("len(suggestSystems) = %d, want 3 (capped)", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestSuggestSystems -v`
Expected: FAIL — `undefined: suggestSystems`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/tools/play_as/show_system.go`:

```go
package main

import (
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// suggestSystems returns up to three system ids that plausibly match an unknown
// query, for a "did you mean" hint. Candidates are ranked by substring match
// first (id or name, case-insensitive), then by Levenshtein distance ≤ 2. It is
// pure — no I/O — so it is directly testable.
func suggestSystems(query string, systems []knowledge.System) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	type cand struct {
		id    string
		rank  int // 0 = substring, 1 = fuzzy
		dist  int
	}
	var cands []cand
	for _, s := range systems {
		id := strings.ToLower(s.ID)
		name := strings.ToLower(s.Name)
		switch {
		case strings.Contains(id, q) || (name != "" && strings.Contains(name, q)):
			cands = append(cands, cand{s.ID, 0, 0})
		default:
			d := levenshtein(q, id)
			if name != "" {
				if dn := levenshtein(q, name); dn < d {
					d = dn
				}
			}
			if d <= 2 {
				cands = append(cands, cand{s.ID, 1, d})
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].rank != cands[j].rank {
			return cands[i].rank < cands[j].rank
		}
		return cands[i].dist < cands[j].dist
	})
	var out []string
	for _, c := range cands {
		out = append(out, c.id)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// levenshtein returns the edit distance between two strings.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev = cur
	}
	return prev[len(rb)]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestSuggestSystems -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/show_system.go cmd/tools/play_as/show_system_test.go
git commit -m "feat(play_as): suggestSystems fuzzy matcher for show_system"
```

---

### Task 2: `renderSystem` pure renderer

**Files:**
- Modify: `cmd/tools/play_as/show_system.go`
- Test: `cmd/tools/play_as/show_system_test.go`

**Interfaces:**
- Consumes: `knowledge.System`, `knowledge.POI`, `game.POIResource`, `formatAge`, `trimFloat`.
- Produces: `func renderSystem(sys *knowledge.System, pois []knowledge.POI, nameByID map[string]string, nowTick int64) string` — the full text block. `nameByID` maps a connected system id to its display name (id used when absent). `nowTick` is the current game tick for the freshness line (pass 0 to omit the age suffix).

- [ ] **Step 1: Write the failing test**

First, replace the test file's import block so it reads exactly:

```go
import (
	"reflect"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)
```

Then append to `cmd/tools/play_as/show_system_test.go`:

```go
func TestRenderSystemVisited(t *testing.T) {
	sys := &knowledge.System{
		ID: "nexus_prime", Name: "Nexus Prime", Empire: "solarian",
		PoliceLevel: 3, SecurityStatus: "high_sec", Description: "A core hub.",
		LastVisitedTick: 1000,
		Connections: []knowledge.SystemConnection{
			{SystemID: "sol", Distance: 4},
			{SystemID: "procyon", Distance: 7},
		},
	}
	pois := []knowledge.POI{
		{ID: "nexus_stn", Name: "Nexus Station", Type: "station", Services: []string{"refuel", "market"}},
		{ID: "belt_a", Name: "Asteroid Belt", Type: "asteroid", Resources: []game.POIResource{{ResourceID: "iron", Richness: 0.8}}},
		{ID: "star_a", Name: "Alpha Star", Type: "star", Class: "G2 V"},
	}
	names := map[string]string{"sol": "Sol", "procyon": "Procyon"}

	got := renderSystem(sys, pois, names, 1360) // 360 ticks after visit

	for _, want := range []string{
		"Nexus Prime (nexus_prime)", "Solarian",
		"Security: 3 - high_sec", "Visited", "A core hub.",
		"Sol", "sol", "4 LY",
		"Nexus Station", "refuel, market",
		"iron(0.8)", "G2 V",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderSystem output missing %q\n---\n%s", want, got)
		}
	}
}

func TestRenderSystemUnexplored(t *testing.T) {
	sys := &knowledge.System{
		ID: "unknown_edge", Name: "Unknown Edge", PoliceLevel: 1,
		SecurityStatus: "low_sec", LastVisitedTick: 0,
	}
	got := renderSystem(sys, nil, nil, 500)
	if !strings.Contains(got, "Unexplored (map-import only)") {
		t.Errorf("expected unexplored marker, got:\n%s", got)
	}
	if !strings.Contains(got, "untrusted") {
		t.Errorf("expected untrusted security marker, got:\n%s", got)
	}
	if !strings.Contains(got, "POIs:\n  (none)") {
		t.Errorf("expected (none) POIs, got:\n%s", got)
	}
}

func TestRenderSystemHiddenPOIAndConnFallback(t *testing.T) {
	sys := &knowledge.System{
		ID: "s1", Name: "S1", LastVisitedTick: 10,
		Connections: []knowledge.SystemConnection{{SystemID: "ghost", Distance: 2}},
	}
	pois := []knowledge.POI{{ID: "wh1", Name: "Wormhole", Type: "wormhole", Hidden: true}}
	got := renderSystem(sys, pois, map[string]string{}, 20) // ghost not in map
	if !strings.Contains(got, "Wormhole (hidden)") {
		t.Errorf("expected hidden marker, got:\n%s", got)
	}
	if !strings.Contains(got, "ghost") { // falls back to id in name slot
		t.Errorf("expected connection id fallback, got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestRenderSystem -v`
Expected: FAIL — `undefined: renderSystem`.

- [ ] **Step 3: Write minimal implementation**

Append to `cmd/tools/play_as/show_system.go` (and add `"fmt"` to its imports):

```go
// renderSystem renders the knowledge base's view of a system in a layout that
// mirrors get_system, enriched with data get_system does not carry: per-POI
// resources/services, star/planet class, hidden flag, and a freshness line.
// It is pure: all inputs are passed in, so it is directly testable. nameByID
// resolves connection ids to display names (id shown when absent). nowTick is
// the current game tick for the age suffix (0 omits the age).
func renderSystem(sys *knowledge.System, pois []knowledge.POI, nameByID map[string]string, nowTick int64) string {
	var b strings.Builder

	// Header: Name (id) | Empire
	empire := sys.Empire
	if empire != "" {
		empire = strings.ToUpper(empire[:1]) + empire[1:]
	} else {
		empire = "Unknown"
	}
	fmt.Fprintf(&b, "%s (%s) | %s\n", sys.Name, sys.ID, empire)

	// Security + freshness line.
	sec := fmt.Sprintf("Security: %d - %s", sys.PoliceLevel, sys.SecurityStatus)
	if sys.Visited() {
		fresh := fmt.Sprintf("Visited (tick %d", sys.LastVisitedTick)
		if nowTick > 0 {
			fresh += ", " + formatAge(nowTick-sys.LastVisitedTick)
		}
		fresh += ")"
		fmt.Fprintf(&b, "%s   | %s\n", sec, fresh)
	} else {
		fmt.Fprintf(&b, "%s (untrusted)   | Unexplored (map-import only)\n", sec)
	}
	if sys.Description != "" {
		fmt.Fprintf(&b, "%s\n", sys.Description)
	}

	// Connections.
	b.WriteString("\nConnections:\n")
	if len(sys.Connections) == 0 {
		b.WriteString("  (none)\n")
	} else {
		nameW := 0
		labels := make([]string, len(sys.Connections))
		for i, c := range sys.Connections {
			label := c.SystemID
			if n := nameByID[c.SystemID]; n != "" {
				label = n
			}
			labels[i] = label
			nameW = max(nameW, len(label))
		}
		for i, c := range sys.Connections {
			fmt.Fprintf(&b, "  %-*s | %-12s | %d LY\n", nameW, labels[i], c.SystemID, c.Distance)
		}
	}

	// POIs.
	b.WriteString("\nPOIs:\n")
	if len(pois) == 0 {
		b.WriteString("  (none)\n")
		return b.String()
	}
	type row struct{ name, id, typ, class, detail string }
	rows := make([]row, 0, len(pois))
	nameW, idW, typeW, classW := len("Name"), len("ID"), len("Type"), len("Class")
	for _, p := range pois {
		name := p.Name
		if p.Hidden {
			name += " (hidden)"
		}
		detail := poiDetail(p)
		r := row{name, p.ID, p.Type, p.Class, detail}
		rows = append(rows, r)
		nameW = max(nameW, len(r.name))
		idW = max(idW, len(r.id))
		typeW = max(typeW, len(r.typ))
		classW = max(classW, len(r.class))
	}
	fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | Resources / Services\n",
		nameW, "Name", idW, "ID", typeW, "Type", classW, "Class")
	b.WriteString(strings.Repeat("-", nameW+idW+typeW+classW+30) + "\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | %s\n",
			nameW, r.name, idW, r.id, typeW, r.typ, classW, r.class, r.detail)
	}
	return b.String()
}

// poiDetail renders a POI's last column: resources (id(richness), comma-joined)
// when it has any, otherwise its services. Empty when it has neither.
func poiDetail(p knowledge.POI) string {
	if len(p.Resources) > 0 {
		parts := make([]string, 0, len(p.Resources))
		for _, r := range p.Resources {
			parts = append(parts, fmt.Sprintf("%s(%s)", r.ResourceID, trimFloat(r.Richness)))
		}
		return strings.Join(parts, ", ")
	}
	return strings.Join(p.Services, ", ")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestRenderSystem -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/show_system.go cmd/tools/play_as/show_system_test.go
git commit -m "feat(play_as): renderSystem KB renderer for show_system"
```

---

### Task 3: `runShowSystem` orchestration + dispatch + completer

**Files:**
- Modify: `cmd/tools/play_as/show_system.go`
- Modify: `cmd/tools/play_as/main.go` (dispatch case, near `case "find_item":`)
- Modify: `cmd/tools/play_as/completer.go:14-15` (metaCommands list)

**Interfaces:**
- Consumes: `globalKB` (`knowledge.Base`), `globalClock` (`*game.GameClock`), `suggestSystems`, `renderSystem`.
- Produces: `func runShowSystem(ctx context.Context, args []string) error`.

- [ ] **Step 1: Add the orchestration function**

Append to `cmd/tools/play_as/show_system.go` (add `"context"` and `"fmt"` to imports if not already present):

```go
// runShowSystem implements the show_system REPL command:
//
//	show_system <id>
//
// It prints what the knowledge base knows about a remote system, enriched
// beyond the live get_system output. Pure KB read: no server call, no tick cost.
func runShowSystem(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: show_system <id>")
	}
	if globalKB == nil {
		return fmt.Errorf("show_system: knowledge base not available (run with --db-path)")
	}
	id := args[0]

	sys, err := globalKB.GetSystem(ctx, id)
	if err != nil || sys == nil {
		fmt.Printf("System %q not found in knowledge base.\n", id)
		if systems, serr := globalKB.GetSystems(ctx); serr == nil {
			if s := suggestSystems(id, systems); len(s) > 0 {
				fmt.Printf("Did you mean: %s?\n", strings.Join(s, ", "))
			}
		}
		return nil
	}

	// id -> name map for connection labels (also powers not-found suggestions).
	nameByID := map[string]string{}
	if systems, serr := globalKB.GetSystems(ctx); serr == nil {
		for _, s := range systems {
			nameByID[s.ID] = s.Name
		}
	}

	pois, perr := globalKB.GetPOIs(ctx, id)

	var nowTick int64
	if globalClock != nil {
		nowTick = globalClock.Tick()
	}

	fmt.Print(renderSystem(sys, pois, nameByID, nowTick))
	if perr != nil {
		fmt.Printf("(POIs unavailable: %v)\n", perr)
	}
	return nil
}
```

- [ ] **Step 2: Wire the dispatch case**

In `cmd/tools/play_as/main.go`, immediately after the `case "find_item":` block (`return runFindItem(client, ctx, parts[1:])`), add:

```go
	case "show_system", "show-system":
		return runShowSystem(ctx, parts[1:])
```

- [ ] **Step 3: Add to the completer**

In `cmd/tools/play_as/completer.go`, extend the `metaCommands` slice (currently ends `"update_market", "find_item",`) to:

```go
	"update_market", "find_item", "show_system",
```

- [ ] **Step 4: Build and run the full package test + lint**

Run: `go build ./... && go test ./cmd/tools/play_as/ && golangci-lint run ./cmd/tools/play_as/`
Expected: build clean, tests PASS, `0 issues`.

- [ ] **Step 5: Manual smoke (optional, requires a populated knowledge.db)**

Run: `go run ./cmd/tools/play_as --db-path data/spacemolt-knowledge.db` then at the prompt: `show_system sol` (known) and `show_system nexis` (unknown → suggestions). Verify output shape.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/show_system.go cmd/tools/play_as/main.go cmd/tools/play_as/completer.go
git commit -m "feat(play_as): show_system <id> command — KB view of a remote system"
```

---

## Notes for the implementer

- `min`/`max` are Go 1.21+ builtins — no import needed.
- `formatAge` returns `"unknown"` for negative input, so an out-of-order tick (nowTick < LastVisitedTick) degrades gracefully.
- Do not add a JSON branch; `find_item` (the sibling this mirrors) prints straight to stdout and ignores the format flag.
- The dead `salvage_wreck`/other commands are unrelated; touch only the files listed.
