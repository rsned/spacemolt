# Dataservice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a new `pkg/dataservice/` library + `cmd/databot/` binary that lets one agent answer chat-message queries (starting with `nearest <poi_type>`) from other agents.

**Architecture:** A new pluggable handler registry in `pkg/dataservice/` with a long-running service loop. Two goroutines: an ingest loop that polls `get_chat_history` into the existing mbox, and a dispatch loop that replies to unread private messages targeted at this agent. The `nearest` logic is extracted from `cmd/tools/play_as/nearest.go` into a new shared function in `pkg/galaxy/` so both callers use identical code.

**Tech Stack:** Go 1.24+, SQLite (existing `pkg/knowledge` + `pkg/mbox`), existing `pkg/game` client, existing `pkg/galaxy` graph.

---

## File Structure

**Created:**
- `pkg/galaxy/nearest_by_poi.go` — extracted shared `FindNearestByPOIType` + helpers.
- `pkg/galaxy/nearest_by_poi_test.go` — unit tests.
- `pkg/dataservice/handler.go` — `Handler` interface, `Format` enum, shared request/response types.
- `pkg/dataservice/reply.go` — `truncateReply`, `detectFormat` helpers.
- `pkg/dataservice/reply_test.go` — unit tests.
- `pkg/dataservice/registry.go` — `Registry` type with `Register`, `Dispatch`, `Help`.
- `pkg/dataservice/registry_test.go` — unit tests.
- `pkg/dataservice/handlers/nearest.go` — the first concrete handler.
- `pkg/dataservice/handlers/nearest_test.go` — unit tests.
- `pkg/dataservice/service.go` — `Service` with ingest/dispatch goroutines.
- `pkg/dataservice/service_test.go` — integration test with fake game client + in-memory mbox.
- `pkg/dataservice/doc.go` — package doc comment.
- `cmd/databot/main.go` — standalone binary wiring deps + running `Service`.
- `cmd/databot/README.md` — how to run it.
- `data/agents/databot/personality.json` — cheerful-reference-desk persona.

**Modified:**
- `cmd/tools/play_as/nearest.go` — refactor to call `galaxy.FindNearestByPOIType` instead of local helpers.

**Rationale for file splits:** `pkg/dataservice/` is split by responsibility (handler contract, registry, service loop, helpers) rather than size; each file is one concern and testable in isolation. Handlers live in a `handlers/` sub-package so the core package has no dependency on specific handlers and new handlers can be added without touching the core.

---

## Task 1: Extract shared nearest logic into `pkg/galaxy`

**Files:**
- Create: `pkg/galaxy/nearest_by_poi.go`
- Create: `pkg/galaxy/nearest_by_poi_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/galaxy/nearest_by_poi_test.go`:

```go
package galaxy

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestFindNearestByPOIType_Station(t *testing.T) {
	ctx := context.Background()
	kb, err := knowledge.NewMemoryKB()
	if err != nil {
		t.Fatalf("NewMemoryKB: %v", err)
	}
	defer func() { _ = kb.Close() }()

	// Two systems, connected; a station at sys-b with public access.
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "sys-a", Name: "Alpha", Position: knowledge.Position{X: 0, Y: 0}}); err != nil {
		t.Fatalf("remember sys-a: %v", err)
	}
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "sys-b", Name: "Beta", Position: knowledge.Position{X: 1, Y: 0}}); err != nil {
		t.Fatalf("remember sys-b: %v", err)
	}
	if err := kb.RememberConnection(ctx, "sys-a", "sys-b"); err != nil {
		t.Fatalf("remember connection: %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "poi-b-1", SystemID: "sys-b", Type: "station", Name: "Beta Station"}); err != nil {
		t.Fatalf("remember poi: %v", err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: "base-b-1", POIID: "poi-b-1", PublicAccess: true}); err != nil {
		t.Fatalf("remember base: %v", err)
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB: %v", err)
	}

	results, err := FindNearestByPOIType(ctx, kb, g, "sys-a", "station", 3)
	if err != nil {
		t.Fatalf("FindNearestByPOIType: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SystemID != "sys-b" {
		t.Errorf("expected sys-b, got %s", results[0].SystemID)
	}
	if results[0].Hops != 1 {
		t.Errorf("expected 1 hop, got %d", results[0].Hops)
	}
}

func TestFindNearestByPOIType_StrongholdExcluded(t *testing.T) {
	ctx := context.Background()
	kb, err := knowledge.NewMemoryKB()
	if err != nil {
		t.Fatalf("NewMemoryKB: %v", err)
	}
	defer func() { _ = kb.Close() }()

	if err := kb.RememberSystem(ctx, knowledge.System{ID: "sys-a", Name: "Alpha"}); err != nil {
		t.Fatalf("remember sys-a: %v", err)
	}
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "sys-b", Name: "Beta", IsStronghold: true}); err != nil {
		t.Fatalf("remember sys-b: %v", err)
	}
	if err := kb.RememberConnection(ctx, "sys-a", "sys-b"); err != nil {
		t.Fatalf("remember connection: %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "poi-b-1", SystemID: "sys-b", Type: "station"}); err != nil {
		t.Fatalf("remember poi: %v", err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: "base-b-1", POIID: "poi-b-1", PublicAccess: true}); err != nil {
		t.Fatalf("remember base: %v", err)
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB: %v", err)
	}

	results, err := FindNearestByPOIType(ctx, kb, g, "sys-a", "station", 3)
	if err != nil {
		t.Fatalf("FindNearestByPOIType: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results (stronghold excluded), got %d", len(results))
	}
}

func TestFindNearestByPOIType_OtherType(t *testing.T) {
	ctx := context.Background()
	kb, err := knowledge.NewMemoryKB()
	if err != nil {
		t.Fatalf("NewMemoryKB: %v", err)
	}
	defer func() { _ = kb.Close() }()

	if err := kb.RememberSystem(ctx, knowledge.System{ID: "sys-a", Name: "Alpha"}); err != nil {
		t.Fatalf("remember sys-a: %v", err)
	}
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "sys-b", Name: "Beta"}); err != nil {
		t.Fatalf("remember sys-b: %v", err)
	}
	if err := kb.RememberConnection(ctx, "sys-a", "sys-b"); err != nil {
		t.Fatalf("remember connection: %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "poi-b-1", SystemID: "sys-b", Type: "asteroid_belt"}); err != nil {
		t.Fatalf("remember poi: %v", err)
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB: %v", err)
	}

	results, err := FindNearestByPOIType(ctx, kb, g, "sys-a", "asteroid_belt", 3)
	if err != nil {
		t.Fatalf("FindNearestByPOIType: %v", err)
	}
	if len(results) != 1 || results[0].SystemID != "sys-b" {
		t.Errorf("expected one result at sys-b, got %+v", results)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/galaxy/ -run TestFindNearestByPOIType -v`
Expected: FAIL — `undefined: FindNearestByPOIType`.

Note: If `knowledge.Position`, `knowledge.POI.Type/Name/ID`, or `knowledge.SpaceBase.PublicAccess` have different field names, first `grep -n "type POI\|type SpaceBase\|type System struct\|type Position" pkg/knowledge/` and correct the test; the field names must match actual struct definitions per CLAUDE.md.

- [ ] **Step 3: Implement `FindNearestByPOIType`**

Create `pkg/galaxy/nearest_by_poi.go`:

```go
package galaxy

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// FindNearestByPOIType returns up to `limit` nearest accessible systems that
// contain a POI of the given type, starting from `fromSystem`.
//
// For poiType == "station", only systems with a publicly accessible station
// (PublicAccess == true) and not flagged IsStronghold are included. For any
// other poiType, any system containing a POI of that type is included.
//
// Callers are expected to have already called graph.BuildFromDB.
func FindNearestByPOIType(
	ctx context.Context,
	kb knowledge.Base,
	graph *GalaxyGraph,
	fromSystem string,
	poiType string,
	limit int,
) ([]NearestResult, error) {
	if kb == nil {
		return nil, fmt.Errorf("nearest_by_poi: knowledge base is nil")
	}
	if graph == nil {
		return nil, fmt.Errorf("nearest_by_poi: graph is nil")
	}
	if fromSystem == "" {
		return nil, fmt.Errorf("nearest_by_poi: fromSystem is required")
	}
	if poiType == "" {
		return nil, fmt.Errorf("nearest_by_poi: poiType is required")
	}

	var targets []string
	var err error
	if poiType == "station" {
		targets, err = queryAccessibleStations(ctx, kb)
	} else {
		targets, err = queryPOIsByType(ctx, kb, poiType)
	}
	if err != nil {
		return nil, err
	}

	if len(targets) == 0 {
		return nil, nil
	}

	return graph.FindNearest(fromSystem, targets, limit)
}

// queryAccessibleStations returns system IDs that contain at least one
// publicly accessible station and are not strongholds.
func queryAccessibleStations(ctx context.Context, kb knowledge.Base) ([]string, error) {
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return nil, fmt.Errorf("get systems: %w", err)
	}

	systemSet := make(map[string]bool)
	for _, sys := range systems {
		if sys.IsStronghold {
			continue
		}
		pois, err := kb.GetPOIs(ctx, sys.ID)
		if err != nil {
			continue
		}
		for _, poi := range pois {
			if poi.Type != "station" {
				continue
			}
			base, err := kb.GetBaseByPOI(ctx, poi.ID)
			if err == nil && base != nil && base.PublicAccess {
				systemSet[sys.ID] = true
				break
			}
		}
	}

	result := make([]string, 0, len(systemSet))
	for id := range systemSet {
		result = append(result, id)
	}
	return result, nil
}

// queryPOIsByType returns system IDs containing any POI of the given type.
func queryPOIsByType(ctx context.Context, kb knowledge.Base, poiType string) ([]string, error) {
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return nil, fmt.Errorf("get systems: %w", err)
	}

	systemSet := make(map[string]bool)
	for _, sys := range systems {
		pois, err := kb.GetPOIs(ctx, sys.ID)
		if err != nil {
			continue
		}
		for _, poi := range pois {
			if poi.Type == poiType {
				systemSet[sys.ID] = true
				break
			}
		}
	}

	result := make([]string, 0, len(systemSet))
	for id := range systemSet {
		result = append(result, id)
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/galaxy/ -run TestFindNearestByPOIType -v`
Expected: PASS on all three tests.

- [ ] **Step 5: Run linter**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/galaxy/...`
Expected: no findings. If any, fix inline.

- [ ] **Step 6: Commit**

```bash
cd /home/robert/spacemolt/spacemolt && git add pkg/galaxy/nearest_by_poi.go pkg/galaxy/nearest_by_poi_test.go && git commit -m "feat(galaxy): extract FindNearestByPOIType shared helper"
```

---

## Task 2: Refactor `play_as` to use the extracted helper

**Files:**
- Modify: `cmd/tools/play_as/nearest.go`

- [ ] **Step 1: Replace local helpers with `galaxy.FindNearestByPOIType`**

In `cmd/tools/play_as/nearest.go`, delete the local `queryAccessibleStations` and `queryPOIsByType` functions, and rewrite `handleNearestCommand` to call the shared function. The file becomes:

```go
package main

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
)

const staleThresholdTicks int64 = 8640 // ~1 day (8640 ticks = 24 hours at 10s/tick)

// handleNearestCommand finds the nearest POIs of a given type.
// Usage: nearest <poi_type>
// Example: nearest station
func handleNearestCommand(ctx context.Context, client game.GameClient, args []string, format outputFormat) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: nearest <poi_type>\nExample: nearest station")
	}

	poiType := strings.ToLower(args[0])

	if globalKB == nil {
		return fmt.Errorf("knowledge base not available (--db-path required)")
	}
	if globalClock == nil {
		return fmt.Errorf("game clock not available")
	}

	state := client.GetState()
	if state == nil || state.System.ID == "" {
		return fmt.Errorf("current system unknown")
	}

	currentSystem := state.System.ID

	g, err := globalGraphCache.GetOrCreate(ctx)
	if err != nil {
		return fmt.Errorf("failed to get galaxy graph: %w", err)
	}

	results, err := galaxy.FindNearestByPOIType(ctx, globalKB, g, currentSystem, poiType, 3)
	if err != nil {
		return fmt.Errorf("find nearest %s: %w", poiType, err)
	}

	if len(results) == 0 {
		if format == formatStyled {
			fmt.Printf("No accessible %s found in the galaxy.\n", poiType)
		}
		return nil
	}

	currentTick := globalClock.Tick()
	for i := range results {
		age := currentTick - results[i].LastUpdated
		if age > staleThresholdTicks {
			results[i].StaleWarning = fmt.Sprintf("⚠ Data from %d ticks ago", age)
		}
	}

	if format == formatStyled {
		output := formatNearestResultsStyled(currentSystem, state.System.Name, poiType, results)
		fmt.Print(output)
	} else {
		output := formatNearestResultsRaw(currentSystem, state.System.Name, poiType, results)
		fmt.Print(output)
	}

	return nil
}

// formatNearestResultsStyled formats nearest results in human-readable styled output.
func formatNearestResultsStyled(fromSystem, fromSystemName, queryType string, results []galaxy.NearestResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\nNearest accessible %s from %s:\n\n", queryType, fromSystemName))

	if len(results) == 0 {
		sb.WriteString("No results found.\n")
		return sb.String()
	}

	for i, r := range results {
		suffix := ""
		if r.IsHomeBase {
			suffix = " (your home base)"
		}
		sb.WriteString(fmt.Sprintf("  %d. %s (%s) — %d hops%s\n", i+1, r.SystemName, r.SystemID, r.Hops, suffix))

		ageText := formatAge(globalClock.Tick() - r.LastUpdated)
		if r.StaleWarning != "" {
			sb.WriteString(fmt.Sprintf("     %s %s\n", r.StaleWarning, ageText))
		} else {
			sb.WriteString(fmt.Sprintf("     Last updated: %s\n", ageText))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// formatNearestResultsRaw formats nearest results as JSON.
func formatNearestResultsRaw(fromSystem, fromSystemName, queryType string, results []galaxy.NearestResult) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	sb.WriteString(fmt.Sprintf("  \"from_system\": \"%s\",\n", fromSystem))
	sb.WriteString(fmt.Sprintf("  \"from_system_name\": \"%s\",\n", fromSystemName))
	sb.WriteString(fmt.Sprintf("  \"query_type\": \"%s\",\n", queryType))
	sb.WriteString("  \"results\": [\n")

	for i, r := range results {
		sb.WriteString("    {")
		sb.WriteString(fmt.Sprintf("\"system_id\": \"%s\", ", r.SystemID))
		sb.WriteString(fmt.Sprintf("\"system_name\": \"%s\", ", r.SystemName))
		sb.WriteString(fmt.Sprintf("\"hops\": %d, ", r.Hops))
		sb.WriteString(fmt.Sprintf("\"is_home_base\": %t, ", r.IsHomeBase))
		sb.WriteString(fmt.Sprintf("\"last_updated_tick\": %d", r.LastUpdated))
		if r.StaleWarning != "" {
			sb.WriteString(fmt.Sprintf(", \"stale_warning\": \"%s\"", r.StaleWarning))
		}
		sb.WriteString("}")
		if i < len(results)-1 {
			sb.WriteString(",\n")
		} else {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("  ]\n")
	sb.WriteString("}\n")
	return sb.String()
}

// formatAge converts tick age to human-readable time.
func formatAge(ticks int64) string {
	if ticks < 0 {
		return "unknown"
	}
	if ticks < 3600 {
		return fmt.Sprintf("%d ticks ago", ticks)
	}
	hours := math.Ceil(float64(ticks) / 360.0)
	if hours < 48 {
		return fmt.Sprintf("~%.0f hours ago", hours)
	}
	days := math.Ceil(hours / 24.0)
	return fmt.Sprintf("~%.0f days ago", days)
}
```

- [ ] **Step 2: Build and run existing play_as tests**

Run: `cd /home/robert/spacemolt/spacemolt && go build ./... && go test ./cmd/tools/play_as/...`
Expected: PASS. If any test references the removed local helpers, update it to call `galaxy.FindNearestByPOIType` instead.

- [ ] **Step 3: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./cmd/tools/play_as/...`
Expected: no findings.

- [ ] **Step 4: Commit**

```bash
cd /home/robert/spacemolt/spacemolt && git add cmd/tools/play_as/nearest.go && git commit -m "refactor(play_as): use galaxy.FindNearestByPOIType shared helper"
```

---

## Task 3: Dataservice `Handler` interface and core types

**Files:**
- Create: `pkg/dataservice/doc.go`
- Create: `pkg/dataservice/handler.go`

- [ ] **Step 1: Write the package doc comment**

Create `pkg/dataservice/doc.go`:

```go
// Package dataservice provides an agent-to-agent query service. A host
// agent runs the service, which listens for private chat DMs, parses
// them as data queries via a pluggable Handler registry, executes
// them against the shared knowledge base, and replies in the same
// format (plaintext or JSON) as the request.
package dataservice
```

- [ ] **Step 2: Write the handler file**

Create `pkg/dataservice/handler.go`:

```go
package dataservice

import (
	"context"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Format is the reply format expected by a requester.
type Format int

const (
	// FormatPlaintext produces styled human-readable replies.
	FormatPlaintext Format = iota
	// FormatJSON produces machine-readable JSON replies.
	FormatJSON
)

// Deps bundles the runtime dependencies that concrete handlers may need.
// Passed to each handler's Execute method. Handlers must not mutate
// any field on Deps.
type Deps struct {
	KB    knowledge.Base
	Graph *galaxy.GalaxyGraph
	// Tick returns the current game tick or 0 if no clock is available.
	Tick func() int64
}

// Handler is a single data-query handler registered with the service.
// Handlers must be concurrency-safe; the service may dispatch queries
// from multiple goroutines in the future.
type Handler interface {
	// Name returns the handler's query keyword (e.g. "nearest").
	Name() string

	// ShortHelp returns a one-line description shown in `help` output.
	ShortHelp() string

	// PlaintextUsage returns the grammar line shown in `help` output,
	// e.g. "nearest <poi_type> from <system_id>".
	PlaintextUsage() string

	// JSONExample returns a minimal request example for `help` output.
	JSONExample() map[string]any

	// HandlePlaintext parses the tail of a plaintext request (tokens after
	// the query keyword) and returns the styled reply.
	HandlePlaintext(ctx context.Context, deps Deps, args []string) (string, error)

	// HandleJSON parses a JSON `params` object and returns a JSON reply
	// as a map the registry will marshal.
	HandleJSON(ctx context.Context, deps Deps, params map[string]any) (map[string]any, error)
}
```

- [ ] **Step 3: Build to verify**

Run: `cd /home/robert/spacemolt/spacemolt && go build ./pkg/dataservice/...`
Expected: PASS.

- [ ] **Step 4: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/dataservice/...`
Expected: no findings.

- [ ] **Step 5: Commit**

```bash
cd /home/robert/spacemolt/spacemolt && git add pkg/dataservice/doc.go pkg/dataservice/handler.go && git commit -m "feat(dataservice): add Handler interface and core types"
```

---

## Task 4: Dataservice reply helpers

**Files:**
- Create: `pkg/dataservice/reply.go`
- Create: `pkg/dataservice/reply_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/dataservice/reply_test.go`:

```go
package dataservice

import "testing"

func TestDetectFormat(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"nearest station from sol-3", FormatPlaintext},
		{"help", FormatPlaintext},
		{`{"query":"nearest"}`, FormatJSON},
		{`  {"query":"help"}  `, FormatJSON},
		{"", FormatPlaintext},
		{"{not-json-but-starts-with-brace", FormatJSON}, // detection is lexical
	}
	for _, c := range cases {
		if got := DetectFormat(c.in); got != c.want {
			t.Errorf("DetectFormat(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTruncateReply(t *testing.T) {
	short := "short"
	if got := TruncateReply(short); got != short {
		t.Errorf("short passthrough: got %q", got)
	}

	long := make([]byte, 600)
	for i := range long {
		long[i] = 'a'
	}
	got := TruncateReply(string(long))
	if len(got) > 500 {
		t.Errorf("truncated reply exceeded 500 chars: %d", len(got))
	}
	suffix := "…[truncated]"
	if len(got) < len(suffix) || got[len(got)-len(suffix):] != suffix {
		t.Errorf("expected truncated suffix, got %q", got[len(got)-20:])
	}
}

func TestTruncateReply_Exact500(t *testing.T) {
	s := make([]byte, 500)
	for i := range s {
		s[i] = 'x'
	}
	got := TruncateReply(string(s))
	if got != string(s) {
		t.Errorf("exact-500 input should not be modified")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/dataservice/ -run "TestDetectFormat|TestTruncateReply" -v`
Expected: FAIL — undefined `DetectFormat`, `TruncateReply`.

- [ ] **Step 3: Implement**

Create `pkg/dataservice/reply.go`:

```go
package dataservice

import "strings"

// MaxReplyChars is the server-enforced chat message content limit.
const MaxReplyChars = 500

// truncateSuffix is appended to over-length replies.
const truncateSuffix = "…[truncated]"

// DetectFormat inspects the trimmed request content. If it begins with
// '{' the request is treated as JSON; otherwise plaintext. Detection
// is purely lexical — malformed JSON that starts with '{' is still
// reported as JSON so the caller can produce a JSON error reply.
func DetectFormat(content string) Format {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") {
		return FormatJSON
	}
	return FormatPlaintext
}

// TruncateReply enforces the MaxReplyChars limit. If s is already
// within the limit it is returned unchanged. Otherwise it is cut so
// that s + truncateSuffix fits within MaxReplyChars.
func TruncateReply(s string) string {
	if len(s) <= MaxReplyChars {
		return s
	}
	keep := MaxReplyChars - len(truncateSuffix)
	if keep < 0 {
		keep = 0
	}
	return s[:keep] + truncateSuffix
}
```

- [ ] **Step 4: Run tests**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/dataservice/ -run "TestDetectFormat|TestTruncateReply" -v`
Expected: PASS.

- [ ] **Step 5: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/dataservice/...`
Expected: no findings.

- [ ] **Step 6: Commit**

```bash
cd /home/robert/spacemolt/spacemolt && git add pkg/dataservice/reply.go pkg/dataservice/reply_test.go && git commit -m "feat(dataservice): add format detection and reply truncation helpers"
```

---

## Task 5: Dataservice `Registry` with `Dispatch` and `Help`

**Files:**
- Create: `pkg/dataservice/registry.go`
- Create: `pkg/dataservice/registry_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/dataservice/registry_test.go`:

```go
package dataservice

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// stubHandler is a minimal Handler for registry tests.
type stubHandler struct {
	name   string
	reply  string
	jsonOK map[string]any
	err    error
}

func (s *stubHandler) Name() string                     { return s.name }
func (s *stubHandler) ShortHelp() string                { return "stub help for " + s.name }
func (s *stubHandler) PlaintextUsage() string           { return s.name + " <arg>" }
func (s *stubHandler) JSONExample() map[string]any      { return map[string]any{"query": s.name} }
func (s *stubHandler) HandlePlaintext(ctx context.Context, deps Deps, args []string) (string, error) {
	return s.reply, s.err
}
func (s *stubHandler) HandleJSON(ctx context.Context, deps Deps, params map[string]any) (map[string]any, error) {
	return s.jsonOK, s.err
}

func TestRegistry_DispatchPlaintext(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&stubHandler{name: "echo", reply: "hello"})

	got, err := r.Dispatch(context.Background(), "echo world")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestRegistry_DispatchJSON(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&stubHandler{name: "echo", jsonOK: map[string]any{"status": "ok", "msg": "hi"}})

	got, err := r.Dispatch(context.Background(), `{"query":"echo","params":{}}`)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("reply is not JSON: %v\n%s", err, got)
	}
	if parsed["status"] != "ok" {
		t.Errorf("status: got %v", parsed["status"])
	}
	if parsed["query"] != "echo" {
		t.Errorf("query echo should be injected, got %v", parsed["query"])
	}
}

func TestRegistry_UnknownPlaintext(t *testing.T) {
	r := NewRegistry(Deps{})
	got, err := r.Dispatch(context.Background(), "wat")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(got, "unknown") {
		t.Errorf("expected 'unknown' in plaintext error, got %q", got)
	}
	if !strings.Contains(got, "help") {
		t.Errorf("expected 'help' hint in plaintext error, got %q", got)
	}
}

func TestRegistry_UnknownJSON(t *testing.T) {
	r := NewRegistry(Deps{})
	got, err := r.Dispatch(context.Background(), `{"query":"wat"}`)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if parsed["status"] != "error" {
		t.Errorf("status: got %v", parsed["status"])
	}
}

func TestRegistry_MalformedJSON(t *testing.T) {
	r := NewRegistry(Deps{})
	got, err := r.Dispatch(context.Background(), `{broken`)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("error reply should itself be JSON: %v", err)
	}
	if parsed["status"] != "error" {
		t.Errorf("status: got %v", parsed["status"])
	}
}

func TestRegistry_HelpPlaintext(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&stubHandler{name: "alpha"})
	r.Register(&stubHandler{name: "beta"})

	got, err := r.Dispatch(context.Background(), "help")
	if err != nil {
		t.Fatalf("Dispatch help: %v", err)
	}
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Errorf("help missing handlers: %q", got)
	}
	if !strings.Contains(got, "help") {
		t.Errorf("help self-entry missing")
	}
}

func TestRegistry_HelpJSON(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&stubHandler{name: "alpha"})

	got, err := r.Dispatch(context.Background(), `{"query":"help"}`)
	if err != nil {
		t.Fatalf("Dispatch help json: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if parsed["query"] != "help" {
		t.Errorf("query: got %v", parsed["query"])
	}
	handlers, ok := parsed["handlers"].([]any)
	if !ok {
		t.Fatalf("handlers should be an array, got %T", parsed["handlers"])
	}
	if len(handlers) == 0 {
		t.Error("expected at least one handler")
	}
}

func TestRegistry_HandlerError_Plaintext(t *testing.T) {
	r := NewRegistry(Deps{})
	r.Register(&stubHandler{name: "bad", err: ErrParse("missing required field: from_system")})

	got, err := r.Dispatch(context.Background(), "bad foo")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(got, "missing required field: from_system") {
		t.Errorf("expected error message surfaced, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/dataservice/ -run "TestRegistry" -v`
Expected: FAIL — undefined `NewRegistry`, `ErrParse`.

- [ ] **Step 3: Implement the registry**

Create `pkg/dataservice/registry.go`:

```go
package dataservice

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ErrParse is returned by handlers when user input is structurally wrong.
// The registry surfaces the error message directly to the requester.
type ErrParse string

// Error implements the error interface.
func (e ErrParse) Error() string { return string(e) }

// Registry is the set of registered handlers. Safe for concurrent reads
// after all handlers are registered at construction time.
type Registry struct {
	mu       sync.RWMutex
	deps     Deps
	handlers map[string]Handler
}

// NewRegistry creates an empty registry backed by the given deps.
func NewRegistry(deps Deps) *Registry {
	return &Registry{
		deps:     deps,
		handlers: make(map[string]Handler),
	}
}

// Register adds a handler. Panics on duplicate names to catch wiring mistakes
// at startup.
func (r *Registry) Register(h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := h.Name()
	if _, exists := r.handlers[name]; exists {
		panic(fmt.Sprintf("dataservice: duplicate handler name %q", name))
	}
	r.handlers[name] = h
}

// Dispatch parses the content, routes to a handler, and returns the
// rendered reply. Errors returned are from the Dispatch mechanism
// itself; handler-level failures are rendered into the reply.
func (r *Registry) Dispatch(ctx context.Context, content string) (string, error) {
	format := DetectFormat(content)

	switch format {
	case FormatJSON:
		return r.dispatchJSON(ctx, content), nil
	default:
		return r.dispatchPlaintext(ctx, content), nil
	}
}

func (r *Registry) dispatchPlaintext(ctx context.Context, content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "Error: empty request. Send 'help' for available commands."
	}

	fields := strings.Fields(trimmed)
	query := strings.ToLower(fields[0])
	args := fields[1:]

	if query == "help" {
		return r.helpPlaintext()
	}

	r.mu.RLock()
	h, ok := r.handlers[query]
	r.mu.RUnlock()
	if !ok {
		return fmt.Sprintf("Error: unknown query %q. Send 'help' for available commands.", query)
	}

	reply, err := h.HandlePlaintext(ctx, r.deps, args)
	if err != nil {
		return fmt.Sprintf("Error: %s", err.Error())
	}
	return reply
}

// jsonEnvelope is the top-level JSON request shape.
type jsonEnvelope struct {
	Query  string         `json:"query"`
	Params map[string]any `json:"params"`
}

func (r *Registry) dispatchJSON(ctx context.Context, content string) string {
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		return mustMarshal(map[string]any{
			"status": "error",
			"error":  fmt.Sprintf("invalid JSON: %s", err.Error()),
		})
	}
	query := strings.ToLower(strings.TrimSpace(env.Query))

	if query == "" {
		return mustMarshal(map[string]any{
			"status": "error",
			"error":  "missing required field: query",
		})
	}

	if query == "help" {
		return r.helpJSON()
	}

	r.mu.RLock()
	h, ok := r.handlers[query]
	r.mu.RUnlock()
	if !ok {
		return mustMarshal(map[string]any{
			"query":  query,
			"status": "error",
			"error":  fmt.Sprintf("unknown query %q", query),
		})
	}

	out, err := h.HandleJSON(ctx, r.deps, env.Params)
	if err != nil {
		return mustMarshal(map[string]any{
			"query":  query,
			"status": "error",
			"error":  err.Error(),
		})
	}
	// Inject query field on success for caller convenience. Handlers
	// may already set status; default to "ok" if absent.
	if out == nil {
		out = map[string]any{}
	}
	if _, ok := out["status"]; !ok {
		out["status"] = "ok"
	}
	out["query"] = query
	return mustMarshal(out)
}

// helpPlaintext renders a short command listing.
func (r *Registry) helpPlaintext() string {
	names := r.sortedNames()
	var sb strings.Builder
	sb.WriteString("Dataservice — available queries:\n")
	sb.WriteString("  help — this message\n")
	for _, name := range names {
		h := r.handlers[name]
		sb.WriteString(fmt.Sprintf("  %s — %s\n", h.PlaintextUsage(), h.ShortHelp()))
	}
	return sb.String()
}

// helpJSON renders the help response as JSON.
func (r *Registry) helpJSON() string {
	names := r.sortedNames()
	handlers := make([]map[string]any, 0, len(names))
	for _, name := range names {
		h := r.handlers[name]
		handlers = append(handlers, map[string]any{
			"name":            h.Name(),
			"description":     h.ShortHelp(),
			"plaintext_usage": h.PlaintextUsage(),
			"json_example":    h.JSONExample(),
		})
	}
	return mustMarshal(map[string]any{
		"query":    "help",
		"status":   "ok",
		"handlers": handlers,
	})
}

func (r *Registry) sortedNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.handlers))
	for n := range r.handlers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// mustMarshal marshals to JSON; falls back to a hard-coded error shape
// on failure (no known handler return shapes should ever fail marshal).
func mustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"status":"error","error":"internal: marshal failed"}`
	}
	return string(b)
}
```

- [ ] **Step 4: Run tests**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/dataservice/ -v`
Expected: PASS on all tests (registry + reply).

- [ ] **Step 5: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/dataservice/...`
Expected: no findings.

- [ ] **Step 6: Commit**

```bash
cd /home/robert/spacemolt/spacemolt && git add pkg/dataservice/registry.go pkg/dataservice/registry_test.go && git commit -m "feat(dataservice): add handler registry with dispatch and help"
```

---

## Task 6: Nearest handler

**Files:**
- Create: `pkg/dataservice/handlers/nearest.go`
- Create: `pkg/dataservice/handlers/nearest_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/dataservice/handlers/nearest_test.go`:

```go
package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/dataservice"
	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func newTestDeps(t *testing.T) (dataservice.Deps, func()) {
	t.Helper()
	ctx := context.Background()
	kb, err := knowledge.NewMemoryKB()
	if err != nil {
		t.Fatalf("NewMemoryKB: %v", err)
	}
	// Fixture: sys-a -> sys-b, station at sys-b, public access.
	for _, sys := range []knowledge.System{
		{ID: "sys-a", Name: "Alpha", Position: knowledge.Position{X: 0}},
		{ID: "sys-b", Name: "Beta", Position: knowledge.Position{X: 1}},
	} {
		if err := kb.RememberSystem(ctx, sys); err != nil {
			t.Fatalf("RememberSystem: %v", err)
		}
	}
	if err := kb.RememberConnection(ctx, "sys-a", "sys-b"); err != nil {
		t.Fatalf("RememberConnection: %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "poi-b", SystemID: "sys-b", Type: "station", Name: "Beta Station"}); err != nil {
		t.Fatalf("RememberPOI: %v", err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: "base-b", POIID: "poi-b", PublicAccess: true}); err != nil {
		t.Fatalf("RememberBase: %v", err)
	}
	g := &galaxy.GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB: %v", err)
	}
	deps := dataservice.Deps{
		KB:    kb,
		Graph: g,
		Tick:  func() int64 { return 100 },
	}
	return deps, func() { _ = kb.Close() }
}

func TestNearest_PlaintextHappy(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	reply, err := h.HandlePlaintext(context.Background(), deps, []string{"station", "from", "sys-a"})
	if err != nil {
		t.Fatalf("HandlePlaintext: %v", err)
	}
	if !strings.Contains(reply, "Beta") {
		t.Errorf("reply missing destination name: %q", reply)
	}
	if !strings.Contains(reply, "1 hop") {
		t.Errorf("reply missing hop count: %q", reply)
	}
}

func TestNearest_PlaintextMissingFrom(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	_, err := h.HandlePlaintext(context.Background(), deps, []string{"station"})
	if err == nil {
		t.Fatalf("expected error for missing 'from'")
	}
	if !strings.Contains(err.Error(), "from") {
		t.Errorf("error message should mention 'from': %v", err)
	}
}

func TestNearest_PlaintextBadGrammar(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	// Expect "nearest <type> from <sys>" — wrong connective.
	_, err := h.HandlePlaintext(context.Background(), deps, []string{"station", "at", "sys-a"})
	if err == nil {
		t.Fatalf("expected error for bad connective")
	}
}

func TestNearest_PlaintextNoResults(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	reply, err := h.HandlePlaintext(context.Background(), deps, []string{"wormhole", "from", "sys-a"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(strings.ToLower(reply), "no accessible") {
		t.Errorf("expected no-results message, got %q", reply)
	}
}

func TestNearest_JSONHappy(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	out, err := h.HandleJSON(context.Background(), deps, map[string]any{
		"poi_type":    "station",
		"from_system": "sys-a",
	})
	if err != nil {
		t.Fatalf("HandleJSON: %v", err)
	}
	if out["from_system"] != "sys-a" {
		t.Errorf("from_system: got %v", out["from_system"])
	}
	results, ok := out["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results not a slice of maps, got %T", out["results"])
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0]["system_id"] != "sys-b" {
		t.Errorf("system_id: got %v", results[0]["system_id"])
	}
}

func TestNearest_JSONMissingField(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	_, err := h.HandleJSON(context.Background(), deps, map[string]any{
		"poi_type": "station",
	})
	if err == nil {
		t.Fatalf("expected error for missing from_system")
	}
	if !strings.Contains(err.Error(), "from_system") {
		t.Errorf("error should name field: %v", err)
	}
}

func TestNearest_PlaintextReplyWithinBudget(t *testing.T) {
	deps, cleanup := newTestDeps(t)
	defer cleanup()

	h := &Nearest{}
	reply, err := h.HandlePlaintext(context.Background(), deps, []string{"station", "from", "sys-a"})
	if err != nil {
		t.Fatalf("HandlePlaintext: %v", err)
	}
	if len(reply) > dataservice.MaxReplyChars {
		t.Errorf("reply exceeds MaxReplyChars: %d > %d", len(reply), dataservice.MaxReplyChars)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/dataservice/handlers/ -v`
Expected: FAIL — undefined `Nearest`.

- [ ] **Step 3: Implement the handler**

Create `pkg/dataservice/handlers/nearest.go`:

```go
// Package handlers provides concrete Handler implementations for the
// dataservice query registry.
package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/dataservice"
	"github.com/rsned/spacemolt/pkg/galaxy"
)

// Nearest answers "find the N nearest accessible systems with a POI of type X".
type Nearest struct {
	// Limit is the max number of results to return. Defaults to 3 when 0.
	Limit int
}

// Name implements dataservice.Handler.
func (n *Nearest) Name() string { return "nearest" }

// ShortHelp implements dataservice.Handler.
func (n *Nearest) ShortHelp() string {
	return "Find nearest accessible POIs of a given type"
}

// PlaintextUsage implements dataservice.Handler.
func (n *Nearest) PlaintextUsage() string {
	return "nearest <poi_type> from <system_id>"
}

// JSONExample implements dataservice.Handler.
func (n *Nearest) JSONExample() map[string]any {
	return map[string]any{
		"query": "nearest",
		"params": map[string]any{
			"poi_type":    "station",
			"from_system": "sol-3",
		},
	}
}

func (n *Nearest) limit() int {
	if n.Limit > 0 {
		return n.Limit
	}
	return 3
}

// HandlePlaintext implements dataservice.Handler. Grammar:
//
//	nearest <poi_type> from <system_id>
func (n *Nearest) HandlePlaintext(ctx context.Context, deps dataservice.Deps, args []string) (string, error) {
	if len(args) < 3 {
		return "", dataservice.ErrParse(`usage: nearest <poi_type> from <system_id>`)
	}
	poiType := strings.ToLower(args[0])
	if strings.ToLower(args[1]) != "from" {
		return "", dataservice.ErrParse(`usage: nearest <poi_type> from <system_id>`)
	}
	fromSystem := args[2]

	results, err := galaxy.FindNearestByPOIType(ctx, deps.KB, deps.Graph, fromSystem, poiType, n.limit())
	if err != nil {
		return "", fmt.Errorf("nearest lookup: %w", err)
	}

	if len(results) == 0 {
		return fmt.Sprintf("No accessible %s found from %s.", poiType, fromSystem), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Nearest accessible %s from %s:\n", poiType, fromSystem))
	for i, r := range results {
		name := r.SystemName
		if name == "" {
			name = r.SystemID
		}
		hopWord := "hops"
		if r.Hops == 1 {
			hopWord = "hop"
		}
		sb.WriteString(fmt.Sprintf("  %d. %s (%s) — %d %s", i+1, name, r.SystemID, r.Hops, hopWord))
		if age := ageText(deps, r.LastUpdated); age != "" {
			sb.WriteString(", updated ")
			sb.WriteString(age)
		}
		sb.WriteString("\n")
	}
	return dataservice.TruncateReply(sb.String()), nil
}

// HandleJSON implements dataservice.Handler.
func (n *Nearest) HandleJSON(ctx context.Context, deps dataservice.Deps, params map[string]any) (map[string]any, error) {
	poiType, _ := params["poi_type"].(string)
	fromSystem, _ := params["from_system"].(string)
	if poiType == "" {
		return nil, dataservice.ErrParse("missing required field: poi_type")
	}
	if fromSystem == "" {
		return nil, dataservice.ErrParse("missing required field: from_system")
	}

	results, err := galaxy.FindNearestByPOIType(ctx, deps.KB, deps.Graph, fromSystem, strings.ToLower(poiType), n.limit())
	if err != nil {
		return nil, fmt.Errorf("nearest lookup: %w", err)
	}

	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"system_id":         r.SystemID,
			"system_name":       r.SystemName,
			"hops":              r.Hops,
			"last_updated_tick": r.LastUpdated,
		})
	}

	return map[string]any{
		"from_system": fromSystem,
		"poi_type":    poiType,
		"results":     out,
	}, nil
}

// ageText returns a short "~2h ago" / "~1d ago" suffix or empty string.
func ageText(deps dataservice.Deps, lastTick int64) string {
	if deps.Tick == nil || lastTick == 0 {
		return ""
	}
	now := deps.Tick()
	if now <= lastTick {
		return ""
	}
	ticks := now - lastTick
	if ticks < 360 { // <1 hour
		return fmt.Sprintf("%dt ago", ticks)
	}
	hours := ticks / 360
	if hours < 48 {
		return fmt.Sprintf("~%dh ago", hours)
	}
	days := hours / 24
	return fmt.Sprintf("~%dd ago", days)
}
```

- [ ] **Step 4: Run tests**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/dataservice/handlers/ -v`
Expected: PASS on all tests.

- [ ] **Step 5: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/dataservice/...`
Expected: no findings.

- [ ] **Step 6: Commit**

```bash
cd /home/robert/spacemolt/spacemolt && git add pkg/dataservice/handlers/nearest.go pkg/dataservice/handlers/nearest_test.go && git commit -m "feat(dataservice): add nearest handler"
```

---

## Task 7: Service loop (ingest + dispatch)

**Files:**
- Create: `pkg/dataservice/service.go`
- Create: `pkg/dataservice/service_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/dataservice/service_test.go`:

```go
package dataservice

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/mbox"
)

// stubClient is a minimal game.GameClient shim capturing the calls we make.
// It satisfies only the subset of methods Service uses; other methods panic.
type stubClient struct {
	mu          sync.Mutex
	sentChats   []stubChat
	nextHistory []serverapi.ChatMessage
	state       *game.State
}

type stubChat struct {
	Channel  string
	Content  string
	TargetID string
}

func (s *stubClient) Chat(ctx context.Context, channel, content, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sentChats = append(s.sentChats, stubChat{Channel: channel, Content: content, TargetID: targetID})
	return nil
}

func (s *stubClient) GetChatHistory(ctx context.Context, channel string, payload map[string]any) error {
	// no-op: historyFetcher returns s.nextHistory directly.
	return nil
}

func (s *stubClient) GetState() *game.State { return s.state }

// sentSnapshot returns a copy of captured chats for assertions.
func (s *stubClient) sentSnapshot() []stubChat {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stubChat, len(s.sentChats))
	copy(out, s.sentChats)
	return out
}

// countSent is how many Chat() calls have occurred.
func (s *stubClient) countSent() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sentChats)
}

// setHistory queues the messages to be returned on the next Ingest call.
func (s *stubClient) setHistory(msgs []serverapi.ChatMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextHistory = msgs
}

// drainHistory returns and clears the next-history slice.
func (s *stubClient) drainHistory() []serverapi.ChatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.nextHistory
	s.nextHistory = nil
	return out
}

// stubFetcher implements HistoryFetcher.
type stubFetcher struct {
	client *stubClient
}

func (f *stubFetcher) Fetch(ctx context.Context, limit int) ([]serverapi.ChatMessage, error) {
	return f.client.drainHistory(), nil
}

type stubReplier struct {
	client *stubClient
}

func (r *stubReplier) Reply(ctx context.Context, targetID, content string) error {
	return r.client.Chat(ctx, "private", content, targetID)
}

// echoHandler replies with the raw args — just to exercise dispatch.
type echoHandler struct{}

func (echoHandler) Name() string                { return "echo" }
func (echoHandler) ShortHelp() string           { return "echo args back" }
func (echoHandler) PlaintextUsage() string      { return "echo <text>" }
func (echoHandler) JSONExample() map[string]any { return map[string]any{"query": "echo", "params": map[string]any{"text": "hi"}} }
func (echoHandler) HandlePlaintext(ctx context.Context, deps Deps, args []string) (string, error) {
	return "echo: " + strings.Join(args, " "), nil
}
func (echoHandler) HandleJSON(ctx context.Context, deps Deps, params map[string]any) (map[string]any, error) {
	text, _ := params["text"].(string)
	return map[string]any{"echo": text}, nil
}

func newTestService(t *testing.T) (*Service, *stubClient, *mbox.Store, func()) {
	t.Helper()
	client := &stubClient{}
	store, err := mbox.Open(filepath.Join(t.TempDir(), "mbox.db"))
	if err != nil {
		t.Fatalf("mbox.Open: %v", err)
	}
	reg := NewRegistry(Deps{})
	reg.Register(echoHandler{})
	cfg := Config{
		AgentID:      "databot-test",
		Registry:     reg,
		Mbox:         store,
		Fetcher:      &stubFetcher{client: client},
		Replier:      &stubReplier{client: client},
		PollInterval: 10 * time.Millisecond,
		ReplyPace:    1 * time.Millisecond, // fast for tests
	}
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	cleanup := func() { _ = store.Close() }
	return svc, client, store, cleanup
}

func TestService_DispatchesPrivateMessageToSelf(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{
			ID:           "m1",
			Channel:      "private",
			SenderID:     "miner-1",
			Sender:       "Preston",
			Content:      "echo hello",
			TargetID:     "databot-test",
			TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	if !waitUntil(t, 200*time.Millisecond, func() bool { return client.countSent() == 1 }) {
		t.Fatalf("timed out waiting for reply; sent=%d", client.countSent())
	}
	sent := client.sentSnapshot()[0]
	if sent.TargetID != "miner-1" {
		t.Errorf("target: got %q", sent.TargetID)
	}
	if sent.Channel != "private" {
		t.Errorf("channel: got %q", sent.Channel)
	}
	if !strings.Contains(sent.Content, "echo: hello") {
		t.Errorf("content: got %q", sent.Content)
	}
}

func TestService_IgnoresMessagesForOthers(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{
			ID: "m1", Channel: "private", SenderID: "miner-1", Sender: "M",
			Content: "echo hi", TargetID: "some-other-bot",
			TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = svc.Run(ctx)

	if n := client.countSent(); n != 0 {
		t.Errorf("should not have replied to someone else's DM; sent=%d", n)
	}
}

func TestService_IgnoresMessagesFromSelf(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{
			ID: "m1", Channel: "private", SenderID: "databot-test", Sender: "D",
			Content: "echo self", TargetID: "databot-test",
			TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = svc.Run(ctx)

	if n := client.countSent(); n != 0 {
		t.Errorf("should not have self-replied; sent=%d", n)
	}
}

func TestService_DedupesIdenticalRequests(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	ts := time.Now().UTC()
	client.setHistory([]serverapi.ChatMessage{
		{ID: "m1", Channel: "private", SenderID: "miner-1", Content: "echo dup", TargetID: "databot-test", TimestampUTC: ts.Format(time.RFC3339Nano)},
		{ID: "m2", Channel: "private", SenderID: "miner-1", Content: "echo dup", TargetID: "databot-test", TimestampUTC: ts.Add(time.Second).Format(time.RFC3339Nano)},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	time.Sleep(200 * time.Millisecond)
	if n := client.countSent(); n != 1 {
		t.Errorf("expected exactly one reply (dedupe), got %d", n)
	}
}

func TestService_ReplyTruncated(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	long := strings.Repeat("x", 1000)
	client.setHistory([]serverapi.ChatMessage{
		{ID: "m1", Channel: "private", SenderID: "miner-1", Content: "echo " + long, TargetID: "databot-test", TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano)},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	if !waitUntil(t, 250*time.Millisecond, func() bool { return client.countSent() == 1 }) {
		t.Fatalf("timed out waiting for reply")
	}
	if got := client.sentSnapshot()[0].Content; len(got) > MaxReplyChars {
		t.Errorf("sent content exceeded MaxReplyChars: %d", len(got))
	}
}

func TestService_JSONRoundTrip(t *testing.T) {
	svc, client, _, cleanup := newTestService(t)
	defer cleanup()

	client.setHistory([]serverapi.ChatMessage{
		{ID: "m1", Channel: "private", SenderID: "miner-1", Content: `{"query":"echo","params":{"text":"ping"}}`, TargetID: "databot-test", TimestampUTC: time.Now().UTC().Format(time.RFC3339Nano)},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	if !waitUntil(t, 250*time.Millisecond, func() bool { return client.countSent() == 1 }) {
		t.Fatalf("timed out waiting for reply")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(client.sentSnapshot()[0].Content), &parsed); err != nil {
		t.Fatalf("reply not JSON: %v", err)
	}
	if parsed["echo"] != "ping" {
		t.Errorf("echo: got %v", parsed["echo"])
	}
}

// waitUntil polls cond() every 5ms until it returns true or timeout.
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// silence unused import if errors not referenced.
var _ = errors.New
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/dataservice/ -run TestService -v`
Expected: FAIL — undefined `NewService`, `Config`, `HistoryFetcher`, `Replier`.

- [ ] **Step 3: Implement the service**

Create `pkg/dataservice/service.go`:

```go
package dataservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/mbox"
)

// HistoryFetcher returns recent private-channel chat messages. A concrete
// implementation over game.GameClient lives in cmd/databot; this interface
// keeps Service testable without a live client.
type HistoryFetcher interface {
	Fetch(ctx context.Context, limit int) ([]serverapi.ChatMessage, error)
}

// Replier sends a chat reply. Abstracted for the same reason as HistoryFetcher.
type Replier interface {
	Reply(ctx context.Context, targetID, content string) error
}

// Config holds the runtime wiring for a Service.
type Config struct {
	AgentID      string
	Registry     *Registry
	Mbox         *mbox.Store
	Fetcher      HistoryFetcher
	Replier      Replier
	Logger       *log.Logger

	// PollInterval controls the ingest-loop cadence. Defaults to 5s.
	PollInterval time.Duration

	// ReplyPace is the enforced minimum between outgoing Chat calls so the
	// server's 1-mutation-per-tick rule is respected. Defaults to 10s.
	ReplyPace time.Duration

	// HistoryLimit controls the max messages fetched per ingest call. Defaults to 50.
	HistoryLimit int
}

// Service is the long-running query responder. Construct with NewService
// and drive with Run.
type Service struct {
	cfg Config
}

// NewService validates the config and returns a Service.
func NewService(cfg Config) (*Service, error) {
	if cfg.AgentID == "" {
		return nil, errors.New("dataservice: AgentID is required")
	}
	if cfg.Registry == nil {
		return nil, errors.New("dataservice: Registry is required")
	}
	if cfg.Mbox == nil {
		return nil, errors.New("dataservice: Mbox is required")
	}
	if cfg.Fetcher == nil {
		return nil, errors.New("dataservice: Fetcher is required")
	}
	if cfg.Replier == nil {
		return nil, errors.New("dataservice: Replier is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(log.Writer(), "[dataservice] ", log.LstdFlags)
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.ReplyPace <= 0 {
		cfg.ReplyPace = 10 * time.Second
	}
	if cfg.HistoryLimit <= 0 {
		cfg.HistoryLimit = 50
	}
	return &Service{cfg: cfg}, nil
}

// Run drives the ingest and dispatch loops until ctx is cancelled.
// Returns nil on clean shutdown or an error only for fatal failures.
func (s *Service) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.ingestLoop(ctx)
	}()
	go func() {
		defer wg.Done()
		s.dispatchLoop(ctx)
	}()
	wg.Wait()
	return nil
}

func (s *Service) ingestLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	// do a first pass immediately so tests aren't blocked for PollInterval
	s.ingestOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ingestOnce(ctx)
		}
	}
}

func (s *Service) ingestOnce(ctx context.Context) {
	msgs, err := s.cfg.Fetcher.Fetch(ctx, s.cfg.HistoryLimit)
	if err != nil {
		s.cfg.Logger.Printf("ingest fetch: %v", err)
		return
	}
	for _, m := range msgs {
		ts := parseTimestamp(m.TimestampUTC)
		_, err := s.cfg.Mbox.Ingest(mbox.Message{
			ID:           m.ID,
			Channel:      m.Channel,
			SenderID:     m.SenderID,
			Sender:       m.Sender,
			Content:      m.Content,
			TargetID:     m.TargetID,
			TargetName:   m.TargetName,
			TimestampUTC: ts,
			Source:       "dataservice",
		})
		if err != nil {
			s.cfg.Logger.Printf("mbox ingest %s: %v", m.ID, err)
		}
	}
}

func (s *Service) dispatchLoop(ctx context.Context) {
	// Short internal tick so the loop is responsive without a timer per message.
	t := time.NewTicker(s.cfg.PollInterval)
	defer t.Stop()
	s.drainOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.drainOnce(ctx)
		}
	}
}

// drainOnce processes unread private messages targeting this agent, one by one,
// pacing at ReplyPace between sends.
func (s *Service) drainOnce(ctx context.Context) {
	msgs, err := s.cfg.Mbox.List(mbox.Query{
		Channel:    "private",
		UnreadOnly: true,
		Limit:      s.cfg.HistoryLimit,
	})
	if err != nil {
		s.cfg.Logger.Printf("mbox list: %v", err)
		return
	}

	// Filter + dedupe in one pass, oldest-first.
	pending := s.filterAndDedupe(msgs)
	for _, m := range pending {
		if ctx.Err() != nil {
			return
		}
		s.handle(ctx, m)
		// Pace the next send.
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.cfg.ReplyPace):
		}
	}
}

// filterAndDedupe keeps only messages addressed to us from someone else,
// drops duplicate (sender_id, content) pairs keeping the oldest, and
// returns them oldest-first.
func (s *Service) filterAndDedupe(msgs []mbox.Message) []mbox.Message {
	// mbox.List returns newest-first. Reverse for FIFO processing.
	reversed := make([]mbox.Message, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		reversed = append(reversed, msgs[i])
	}

	seen := make(map[string]bool) // key: senderID + "\x00" + content
	out := make([]mbox.Message, 0, len(reversed))
	for _, m := range reversed {
		if m.TargetID != s.cfg.AgentID {
			continue
		}
		if m.SenderID == s.cfg.AgentID {
			continue
		}
		key := m.SenderID + "\x00" + m.Content
		if seen[key] {
			// Mark the duplicate read without replying.
			if err := s.cfg.Mbox.MarkRead(m.ID); err != nil {
				s.cfg.Logger.Printf("mark read (dupe) %s: %v", m.ID, err)
			}
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}

// handle dispatches one message: produces a reply, sends it, marks read.
func (s *Service) handle(ctx context.Context, m mbox.Message) {
	reply, err := s.cfg.Registry.Dispatch(ctx, m.Content)
	if err != nil {
		s.cfg.Logger.Printf("dispatch %s: %v", m.ID, err)
		reply = "Error: internal failure while processing your request."
	}
	reply = TruncateReply(reply)
	if err := s.cfg.Replier.Reply(ctx, m.SenderID, reply); err != nil {
		s.cfg.Logger.Printf("reply %s: %v", m.ID, err)
		return
	}
	if err := s.cfg.Mbox.MarkRead(m.ID); err != nil {
		s.cfg.Logger.Printf("mark read %s: %v", m.ID, err)
	}
}

// parseTimestamp accepts RFC3339 or RFC3339Nano; falls back to now on failure.
func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Now().UTC()
}

// sanity check: ensure Config has a non-empty AgentID at Run time if someone
// constructed Service without NewService by mistake.
var _ = fmt.Sprintf
```

- [ ] **Step 4: Run tests**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/dataservice/ -v`
Expected: PASS on all service tests.

- [ ] **Step 5: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/dataservice/...`
Expected: no findings. Remove any stray `var _ = fmt.Sprintf` if unused after final code.

- [ ] **Step 6: Commit**

```bash
cd /home/robert/spacemolt/spacemolt && git add pkg/dataservice/service.go pkg/dataservice/service_test.go && git commit -m "feat(dataservice): add Service with ingest and dispatch loops"
```

---

## Task 8: Databot personality file

**Files:**
- Create: `data/agents/databot/personality.json`

- [ ] **Step 1: Write the file**

Create `data/agents/databot/personality.json`:

```json
{
  "biography": "Daria 'the Desk' Kaplan learned to love reference work in the quiet back stacks of the Krynn Archival Institute, where ten thousand unanswered agent questions drifted in her queue every standard day. Daria doesn't just tolerate being asked the same thing twice — she loves it. Every query is a small puzzle, every answer a chance to be useful, and she greets the fiftieth 'nearest station' request of the shift with the same cheerful precision as the first. She answers in complete sentences, double-checks her coordinates, and never, ever makes you feel dumb for asking. Her mantra: 'The only bad question is the one nobody asked, and then got lost in space.'",
  "empire": "nebula",
  "id": "databot",
  "motivations": {
    "primary": "answer_queries",
    "secondary": "maintain_accuracy",
    "tertiary": "be_helpful",
    "weights": {
      "answer_queries": 0.95,
      "maintain_accuracy": 0.9,
      "be_helpful": 0.9,
      "survival": 0.5
    }
  },
  "name": "Daria 'the Desk' Kaplan",
  "role": "DataService",
  "skills": {
    "reference": "expert",
    "cartography": "advanced",
    "patience": "expert",
    "disambiguation": "advanced"
  },
  "traits": {
    "accuracy": 0.95,
    "helpfulness": 0.95,
    "cheerfulness": 0.8,
    "patience": 0.9,
    "curiosity": 0.6,
    "caution": 0.7
  },
  "primary_skill": "dataservice",
  "game_skills": [],
  "decision_mode": "none"
}
```

- [ ] **Step 2: Verify it parses as JSON**

Run: `cd /home/robert/spacemolt/spacemolt && python3 -c "import json; json.load(open('data/agents/databot/personality.json'))" && echo OK`
Expected: `OK`.

- [ ] **Step 3: Check .gitignore for this path**

Run: `cd /home/robert/spacemolt/spacemolt && git check-ignore data/agents/databot/personality.json; echo "exit=$?"`
Expected: `exit=1` (not ignored). If exit=0, add a negation rule in `.gitignore` (`!data/agents/databot/personality.json`) per the project CLAUDE.md guidance.

- [ ] **Step 4: Commit**

```bash
cd /home/robert/spacemolt/spacemolt && git add data/agents/databot/personality.json && git commit -m "feat(databot): add personality.json (cheerful reference desk)"
```

---

## Task 9: `cmd/databot` binary

**Files:**
- Create: `cmd/databot/main.go`
- Create: `cmd/databot/README.md`

- [ ] **Step 1: Write the binary**

Create `cmd/databot/main.go`:

```go
// Command databot runs the dataservice query responder as a specific agent.
//
// Usage:
//
//	databot --agent-id databot --db-path data/spacemolt-knowledge.db
//
// The agent logs in, stays docked, polls private chat for queries, and
// responds using the dataservice handler registry. Multiple databot
// instances may run concurrently under different agent IDs for load
// scaling; each uses its own mbox but shares the knowledge-base file.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/dataservice"
	"github.com/rsned/spacemolt/pkg/dataservice/handlers"
	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/mbox"
)

func main() {
	agentID := flag.String("agent-id", "databot", "Agent identity to run as (must have credentials in data/agents/<id>/)")
	dbPath := flag.String("db-path", "data/spacemolt-knowledge.db", "Path to shared SQLite knowledge base")
	mboxPath := flag.String("mbox-path", "", "Path to agent mbox SQLite DB (default: data/agents/<agent-id>/mbox.db)")
	pollInterval := flag.Duration("poll-interval", 5*time.Second, "Chat-history poll interval")
	replyPace := flag.Duration("reply-pace", game.SleepTick, "Minimum interval between outgoing chat replies")
	debug := flag.Bool("debug", false, "Enable WS debug logging")
	flag.Parse()

	if *mboxPath == "" {
		*mboxPath = filepath.Join("data", "agents", *agentID, "mbox.db")
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[DATABOT-%s] ", *agentID), log.LstdFlags)

	// Root context cancelled on SIGINT/SIGTERM.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 1. Connect + log in.
	logger.Printf("Initializing agent %s…", *agentID)
	client, creds, err := game.InitializeAgent(*agentID, logger, ctx, *debug)
	if err != nil {
		logger.Fatalf("InitializeAgent: %v", err)
	}
	defer func() { _ = client.Close() }()
	logger.Printf("Connected as %s (empire %s)", creds.Username, creds.Empire)

	// 2. Open shared knowledge base.
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *dbPath, WAL: true})
	if err != nil {
		logger.Fatalf("open KB %s: %v", *dbPath, err)
	}
	defer func() { _ = kb.Close() }()

	// 3. Build galaxy graph once at startup.
	graph := &galaxy.GalaxyGraph{}
	if err := graph.BuildFromDB(ctx, kb); err != nil {
		logger.Fatalf("BuildFromDB: %v", err)
	}
	stats := graph.Stats()
	logger.Printf("Galaxy graph built: %d systems, %d edges in %v", stats.NodeCount, stats.EdgeCount, stats.BuildTime)

	// 4. Game clock (optional but enriches replies with freshness).
	clock, err := game.NewGameClock(ctx, client, logger)
	if err != nil {
		logger.Printf("game clock unavailable: %v (continuing without tick info)", err)
	}
	tickFn := func() int64 {
		if clock == nil {
			return 0
		}
		return clock.Tick()
	}

	// 5. Open mbox.
	store, err := mbox.Open(*mboxPath)
	if err != nil {
		logger.Fatalf("open mbox %s: %v", *mboxPath, err)
	}
	defer func() { _ = store.Close() }()

	// 6. Build registry + handlers.
	deps := dataservice.Deps{KB: kb, Graph: graph, Tick: tickFn}
	registry := dataservice.NewRegistry(deps)
	registry.Register(&handlers.Nearest{})

	// 7. Wire HistoryFetcher + Replier over the game client.
	fetcher := newClientFetcher(client)
	replier := newClientReplier(client)

	// 8. Run.
	svc, err := dataservice.NewService(dataservice.Config{
		AgentID:      *agentID,
		Registry:     registry,
		Mbox:         store,
		Fetcher:      fetcher,
		Replier:      replier,
		Logger:       logger,
		PollInterval: *pollInterval,
		ReplyPace:    *replyPace,
	})
	if err != nil {
		logger.Fatalf("NewService: %v", err)
	}

	logger.Printf("dataservice running; agent=%s mbox=%s poll=%s", *agentID, *mboxPath, *pollInterval)
	if err := svc.Run(ctx); err != nil {
		logger.Fatalf("service run: %v", err)
	}
	logger.Printf("shutdown complete")
}

// clientFetcher implements dataservice.HistoryFetcher over pkg/game.Client.
type clientFetcher struct{ client *game.Client }

func newClientFetcher(c *game.Client) *clientFetcher { return &clientFetcher{client: c} }

// Fetch issues a get_chat_history call for the private channel and returns
// the parsed messages stored in State.LastChatHistory.
func (f *clientFetcher) Fetch(ctx context.Context, limit int) ([]serverapi.ChatMessage, error) {
	if err := f.client.GetChatHistory(ctx, "private", map[string]any{"limit": limit}); err != nil {
		return nil, err
	}
	state := f.client.GetState()
	if state == nil {
		return nil, nil
	}
	out := make([]serverapi.ChatMessage, 0, len(state.LastChatHistory))
	for _, m := range state.LastChatHistory {
		out = append(out, serverapi.ChatMessage{
			ID:           m.ID,
			Channel:      m.Channel,
			SenderID:     m.SenderID,
			Sender:       m.Sender,
			Content:      m.Content,
			TargetID:     m.TargetID,
			TimestampUTC: m.Timestamp,
		})
	}
	return out, nil
}

// clientReplier implements dataservice.Replier over pkg/game.Client.
type clientReplier struct{ client *game.Client }

func newClientReplier(c *game.Client) *clientReplier { return &clientReplier{client: c} }

func (r *clientReplier) Reply(ctx context.Context, targetID, content string) error {
	return r.client.Chat(ctx, "private", content, targetID)
}
```

- [ ] **Step 2: Write the README**

Create `cmd/databot/README.md`:

```markdown
# databot

Standalone binary that runs the dataservice as a specific agent identity. The agent logs in, remains idle/docked, polls private chat for queries, and replies using the handler registry (v1: `nearest`, `help`).

## Usage

```
databot --agent-id databot --db-path data/spacemolt-knowledge.db
```

### Flags

- `--agent-id` — agent identity to run as. Must have credentials at `data/agents/<id>/credentials.json` and a `personality.json`. Default: `databot`.
- `--db-path` — shared SQLite knowledge base. Default: `data/spacemolt-knowledge.db`.
- `--mbox-path` — agent mbox DB. Default: `data/agents/<agent-id>/mbox.db`.
- `--poll-interval` — chat-history poll interval. Default: `5s`.
- `--reply-pace` — minimum interval between replies (server caps mutations at 1/tick). Default: `SleepTick` (10s).
- `--debug` — enable verbose WS logging.

### Querying it from another agent

From `play_as` or any client:

```
chat private databot "nearest station from sol-3"
chat private databot "help"
chat private databot '{"query":"nearest","params":{"poi_type":"station","from_system":"sol-3"}}'
```

## Running multiple instances

Each instance needs its own agent credentials and mbox:

```
databot --agent-id databot-east &
databot --agent-id databot-west &
```

Callers choose which databot to DM. The shared KB handles concurrent reads via SQLite WAL.
```

- [ ] **Step 3: Build the binary**

Run: `cd /home/robert/spacemolt/spacemolt && go build -o bin/databot ./cmd/databot/`
Expected: PASS, produces `bin/databot`.

If the build fails because `game.InitializeAgent` returns a different concrete type than `*game.Client` (e.g., an interface), fix the `clientFetcher`/`clientReplier` constructors to take the correct type. The existing auto-miner uses the same init flow; mirror its concrete type. Verify with `grep -n "client, creds, err := game.InitializeAgent" cmd/auto-miner/main.go` — expect `client` to be `*game.Client`.

- [ ] **Step 4: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./cmd/databot/...`
Expected: no findings.

- [ ] **Step 5: Run full build + test**

Run: `cd /home/robert/spacemolt/spacemolt && go build ./... && go test ./...`
Expected: PASS. This confirms no regression across the codebase.

- [ ] **Step 6: Commit**

```bash
cd /home/robert/spacemolt/spacemolt && git add cmd/databot/main.go cmd/databot/README.md && git commit -m "feat(databot): add standalone binary running dataservice"
```

---

## Task 10: End-to-end smoke test (manual)

**Files:**
- Modify: `cmd/databot/README.md` (add smoke-test result note if useful)

This task is manual verification against a running game server. Skip if no server is available; CI tests cover the core logic already.

- [ ] **Step 1: Launch databot**

Run in one terminal:
```
cd /home/robert/spacemolt/spacemolt && ./bin/databot --agent-id databot --db-path data/spacemolt-knowledge.db
```
Expected: startup log lines show "Connected as …", "Galaxy graph built: N systems…", "dataservice running…".

- [ ] **Step 2: Send a query from play_as**

In another terminal, as any agent with populated KB state:
```
cd /home/robert/spacemolt/spacemolt && ./bin/play_as miner-1
> chat private databot "help"
```
Expected: databot's reply arrives as a private DM within ~10s listing registered handlers.

- [ ] **Step 3: Send a real nearest query**

```
> chat private databot "nearest station from <your-system-id>"
```
Expected: reply lists up to 3 nearest systems with public stations.

- [ ] **Step 4: Send a JSON query**

```
> chat private databot '{"query":"nearest","params":{"poi_type":"station","from_system":"<your-system-id>"}}'
```
Expected: reply is a single JSON object starting `{"from_system":…,"poi_type":…,"results":…,"status":"ok","query":"nearest"}`.

- [ ] **Step 5: Shut down databot cleanly**

Ctrl+C databot. Expected: "shutdown complete" log line, clean exit (exit code 0).

No commit needed for this task unless docs are updated.

---

## Self-review notes

**Spec coverage map (cross-check against `docs/superpowers/specs/2026-04-17-dataservice-design.md`):**

- `pkg/dataservice/handler.go` → Task 3 ✓
- `pkg/dataservice/registry.go` → Task 5 ✓
- `pkg/dataservice/service.go` → Task 7 ✓
- `pkg/dataservice/handlers/nearest.go` → Task 6 ✓
- `pkg/dataservice/reply.go` → Task 4 ✓
- `pkg/galaxy/nearest_by_poi.go` → Task 1 ✓
- `cmd/databot/main.go` → Task 9 ✓
- `data/agents/databot/personality.json` → Task 8 ✓
- Play_as refactor → Task 2 ✓
- Plaintext grammar `nearest <poi_type> from <system_id>` → Task 6 parser + tests ✓
- JSON request/response schema → Task 5 (help), Task 6 (nearest) ✓
- Format detection `{ → JSON, else plaintext` → Task 4 ✓
- Ingest loop (5s, get_chat_history → mbox) → Task 7 ✓
- Dispatch loop (unread, target_id==me, sender≠me, FIFO, dedupe, 10s pace) → Task 7 ✓
- 500-char truncation → Task 4, applied in Task 7 ✓
- Error taxonomy (unknown, parse, internal) → Task 5 ✓

**Spec deviation noted:** The design's "read-only KB via `?mode=ro`" was dropped because `knowledge.NewSQLiteKB` runs migrations at open time (requires write). v1 relies on the fact that databot's code path only calls read methods (`GetSystems`, `GetPOIs`, `GetBaseByPOI`, etc.), with no write path compiled in. A future task can add a `ReadOnly bool` to `knowledge.Config` if hard enforcement is wanted.

**Placeholder scan:** No TBDs, no "add validation here", no referenced symbols not defined in the plan.

**Type consistency:** `Registry.Dispatch(ctx, content string) (string, error)` used consistently across tasks 5 and 7. `dataservice.Deps` referenced with identical fields in tasks 3, 6, 9. `Handler` interface method set stable across tasks 3, 5, 6.

---

## Execution handoff

Plan complete.
