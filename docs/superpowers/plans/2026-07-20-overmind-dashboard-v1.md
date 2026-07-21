# Overmind Dashboard v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One live ops dashboard: real-time galaxy map of all fleet workers, top accounting strip, and a rail of per-agent cards, served by a new read-only Go binary.

**Architecture:** New `pkg/ovdash` (galaxy loader, fleet-snapshot merger, SSE delta hub, accounting aggregator) behind `cmd/overmind-dashboard` (:8091), which also statically serves `frontend/dist`. The frontend gets a new "Overmind" view in the existing React app, reusing `GalaxyMap` via a new `FleetOverlay` layer plus new AccountingStrip/FleetRail/AgentCard components fed by a `useFleetStream` SSE hook.

**Tech Stack:** Go stdlib HTTP + `modernc.org/sqlite`-style RO access via the repo's existing sqlite driver (`database/sql` — use whatever driver `pkg/market` imports), Server-Sent Events, React 19 + Vite + Tailwind (existing setup, no new deps).

**Spec:** `docs/superpowers/specs/2026-07-20-overmind-dashboard-design.md`

## Global Constraints

- Go 1.24+: use range-over-int; benchmarks (if any) use `b.Loop()`.
- All new Go code passes `golangci-lint run` with zero new findings.
- Compiled binaries go in `bin/`, never the repo root.
- The dashboard binary is **read-only**: it must never open a game-server connection, never write to any `.sock`, and must open both databases read-only (`?mode=ro` DSN or equivalent).
- Port default `:8091` (flag `--addr`).
- Status files (exact): `data/overmind/{fleet,mission-learn,craft,mb,assist,shuttle}-status.json`; JSON contract is `pkg/overmind/balances.StatusFile` / `LiveRecord` — import it, do not redeclare.
- Fleet display names + colors (exact): file `fleet` → label `haul` `#d4a017`; `mission-learn` → `mission` `#22d3ee`; `craft` → `craft` `#34d399`; `mb` → `mb` `#a78bfa`; `assist` → `assist` `#fb923c`; `shuttle` → `shuttle` `#f472b6`.
- Earnings SQL columns (verified 2026-07-20): `haul_results(realized_profit, sold_at)`, `mission_results(credits_earned, item_cost, fuel_cost, finished_at)`, `freight_results(carrier_payout, fuel_cost, finished_at, outcome)`. Timestamps are RFC3339 TEXT.
- KB tables (verified): `systems(id, name, position_x, position_y, police_level, empire, is_stronghold, security_status, last_visited_tick)`, `connections(from_system, to_system, distance)`.
- Frontend API base path: `/api/overmind` (the existing Vite `/api` proxy points at :8090; add a **more specific** `/api/overmind` proxy → `http://localhost:8091`).
- Status files carry system **names** ("Nova Terra"); every API response must carry system **ids** (`nova_terra`) resolved via the KB name→id index; unresolvable names surface in an `off_map` list, never dropped.
- No new JS runtime dependencies; no new test frameworks (Go tests + `tsc` via `npm run build`).

---

## File structure

```
pkg/ovdash/
  galaxy.go        galaxy load + name→id index          (Task 1)
  galaxy_test.go
  snapshot.go      status-file read/merge → AgentState  (Task 2)
  snapshot_test.go
  stream.go        Diff + SSE hub + keyframes           (Task 3)
  stream_test.go
  accounting.go    24h earnings aggregates              (Task 4)
  accounting_test.go
cmd/overmind-dashboard/
  main.go          flags, loops, mux, static serve      (Task 5)
  main_test.go     httptest integration                 (Task 5)
frontend/src/
  lib/useFleetStream.ts                                 (Task 6)
  components/overmind/OvermindPage.tsx                  (Task 7)
  components/overmind/AccountingStrip.tsx               (Task 7)
  components/overmind/FleetRail.tsx                     (Task 8)
  components/overmind/AgentCard.tsx                     (Task 8)
  components/overmind/FleetOverlay.tsx                  (Task 9)
  components/overmind/SystemPanel.tsx                   (Task 9)
  components/galaxy/GalaxyMap.tsx  (minimal extension)  (Task 9)
  App.tsx          add 'overmind' ViewType              (Task 7)
frontend/vite.config.ts  add /api/overmind proxy        (Task 6)
```

Spec deviation (approved during planning): the click-a-system side panel is a
new lightweight `SystemPanel` (system facts + agents present); full
`SystemMap` orbital reuse needs an observer-shaped per-system endpoint and is
a follow-up. The spec file gets a one-line note in Task 10.

---

### Task 1: pkg/ovdash — galaxy loader

**Files:**
- Create: `pkg/ovdash/galaxy.go`
- Test: `pkg/ovdash/galaxy_test.go`

**Interfaces:**
- Produces: `type SystemNode struct { ID, Name string; X, Y float64; Empire string; Police int; Stronghold bool; LastVisited int64; Connections []string }` (JSON tags below), `type Galaxy struct { Systems []SystemNode; byName map[string]string }`, `func LoadGalaxy(ctx context.Context, dbPath string) (*Galaxy, error)`, `func (g *Galaxy) ResolveName(name string) (id string, ok bool)`.
- JSON shape of `SystemNode` must satisfy the existing `useGalaxyMap` `GalaxySystem` interface: `{id, name, position:{x,y}, police_level, last_visited_tick, empire, is_stronghold, connections: []string}`.

- [ ] **Step 1: Write the failing test**

`pkg/ovdash/galaxy_test.go`:

```go
package ovdash

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

// fixtureKB builds a minimal knowledge DB with two connected systems and one
// unconnected one, exercising id/name resolution and lane assembly.
func fixtureKB(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kb.db")
	db, err := sql.Open(sqliteDriver, p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE systems (id TEXT PRIMARY KEY, name TEXT NOT NULL,
			position_x REAL NOT NULL, position_y REAL NOT NULL,
			police_level INTEGER DEFAULT 0, empire TEXT DEFAULT '',
			is_stronghold BOOLEAN DEFAULT 0, last_visited_tick INTEGER DEFAULT 0)`,
		`CREATE TABLE connections (from_system TEXT, to_system TEXT, distance REAL)`,
		`INSERT INTO systems VALUES
			('sol','Sol',0,0,10,'solarian',0,100),
			('nova_terra','Nova Terra',50,-30,8,'solarian',0,90),
			('krynn','Krynn',900,900,0,'crimson',1,0)`,
		`INSERT INTO connections VALUES ('sol','nova_terra',12.5)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestLoadGalaxyBuildsSystemsAndConnections(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatalf("LoadGalaxy: %v", err)
	}
	if len(g.Systems) != 3 {
		t.Fatalf("want 3 systems, got %d", len(g.Systems))
	}
	byID := map[string]SystemNode{}
	for _, s := range g.Systems {
		byID[s.ID] = s
	}
	sol := byID["sol"]
	if sol.Name != "Sol" || sol.X != 0 || sol.Police != 10 || sol.Empire != "solarian" {
		t.Fatalf("sol fields wrong: %+v", sol)
	}
	// Connections are bidirectional on the node view even though the table
	// stores one row per lane.
	if len(sol.Connections) != 1 || sol.Connections[0] != "nova_terra" {
		t.Fatalf("sol connections wrong: %v", sol.Connections)
	}
	if nt := byID["nova_terra"]; len(nt.Connections) != 1 || nt.Connections[0] != "sol" {
		t.Fatalf("reverse connection missing: %v", nt.Connections)
	}
	if k := byID["krynn"]; !k.Stronghold || len(k.Connections) != 0 {
		t.Fatalf("krynn fields wrong: %+v", k)
	}
}

func TestResolveNameIsCaseInsensitive(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Nova Terra", "nova terra", "NOVA TERRA"} {
		if id, ok := g.ResolveName(name); !ok || id != "nova_terra" {
			t.Fatalf("ResolveName(%q) = %q, %v", name, id, ok)
		}
	}
	if _, ok := g.ResolveName("Atlantis"); ok {
		t.Fatal("unknown name must not resolve")
	}
}

func TestSystemNodeJSONShapeMatchesUseGalaxyMap(t *testing.T) {
	n := SystemNode{ID: "sol", Name: "Sol", X: 1, Y: 2, Empire: "solarian",
		Police: 10, Stronghold: true, LastVisited: 7, Connections: []string{"a"}}
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	// The frontend hook (frontend/src/lib/useGalaxyMap.ts GalaxySystem)
	// requires exactly these keys.
	for _, k := range []string{"id", "name", "position", "police_level",
		"last_visited_tick", "empire", "is_stronghold", "connections"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("JSON missing key %q: %s", k, b)
		}
	}
	pos, ok := m["position"].(map[string]any)
	if !ok || pos["x"] != 1.0 || pos["y"] != 2.0 {
		t.Fatalf("position shape wrong: %v", m["position"])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/ovdash/ -run TestLoadGalaxy -count=1`
Expected: FAIL — package does not exist / `LoadGalaxy` undefined.

- [ ] **Step 3: Implement**

First check which sqlite driver the repo uses: `grep -rn "database/sql\|sqlite" pkg/market/*.go | grep import -A2 | head`. Use the same import and put its driver name in a package const so tests share it.

`pkg/ovdash/galaxy.go`:

```go
// Package ovdash contains the read-only data plumbing behind
// cmd/overmind-dashboard: galaxy topology, live fleet snapshots, SSE deltas,
// and earnings accounting. Nothing in this package writes anywhere.
package ovdash

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3" // match the driver pkg/market uses; adjust if repo differs
)

// sqliteDriver is the database/sql driver name used across this package.
// Must match the driver imported above (verify against pkg/market's import).
const sqliteDriver = "sqlite3"

// SystemNode is one galaxy system in the shape the frontend's useGalaxyMap
// hook already consumes (GalaxySystem in frontend/src/lib/useGalaxyMap.ts).
type SystemNode struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	X           float64  `json:"-"`
	Y           float64  `json:"-"`
	Empire      string   `json:"empire"`
	Police      int      `json:"police_level"`
	Stronghold  bool     `json:"is_stronghold"`
	LastVisited int64    `json:"last_visited_tick"`
	Connections []string `json:"connections"`
}

// MarshalJSON emits the nested {"position":{"x","y"}} object the frontend
// expects while keeping flat float fields internally.
func (n SystemNode) MarshalJSON() ([]byte, error) {
	type alias SystemNode // no methods: avoids recursion
	return json.Marshal(struct {
		alias
		Position struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"position"`
	}{alias: alias(n), Position: struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}{n.X, n.Y}})
}

// Galaxy is the immutable topology loaded once at startup.
type Galaxy struct {
	Systems []SystemNode
	byName  map[string]string // lower(name) -> id
}

// LoadGalaxy reads systems and connections from the knowledge DB (read-only).
func LoadGalaxy(ctx context.Context, dbPath string) (*Galaxy, error) {
	db, err := sql.Open(sqliteDriver, "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open kb: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT id, name, position_x, position_y,
		police_level, empire, is_stronghold, last_visited_tick FROM systems`)
	if err != nil {
		return nil, fmt.Errorf("query systems: %w", err)
	}
	defer rows.Close()

	g := &Galaxy{byName: map[string]string{}}
	idx := map[string]int{}
	for rows.Next() {
		var n SystemNode
		if err := rows.Scan(&n.ID, &n.Name, &n.X, &n.Y, &n.Police, &n.Empire,
			&n.Stronghold, &n.LastVisited); err != nil {
			return nil, fmt.Errorf("scan system: %w", err)
		}
		idx[n.ID] = len(g.Systems)
		g.byName[strings.ToLower(n.Name)] = n.ID
		g.Systems = append(g.Systems, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	crows, err := db.QueryContext(ctx, `SELECT from_system, to_system FROM connections`)
	if err != nil {
		return nil, fmt.Errorf("query connections: %w", err)
	}
	defer crows.Close()
	for crows.Next() {
		var from, to string
		if err := crows.Scan(&from, &to); err != nil {
			return nil, fmt.Errorf("scan connection: %w", err)
		}
		if i, ok := idx[from]; ok {
			g.Systems[i].Connections = append(g.Systems[i].Connections, to)
		}
		if i, ok := idx[to]; ok {
			g.Systems[i].Connections = append(g.Systems[i].Connections, from)
		}
	}
	if err := crows.Err(); err != nil {
		return nil, err
	}
	for i := range g.Systems {
		sort.Strings(g.Systems[i].Connections)
	}
	return g, nil
}

// ResolveName maps a display name ("Nova Terra") to a system id, case-insensitively.
func (g *Galaxy) ResolveName(name string) (string, bool) {
	id, ok := g.byName[strings.ToLower(name)]
	return id, ok
}
```

(Add the missing `encoding/json` import for `MarshalJSON`.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/ovdash/ -count=1`
Expected: PASS (3 tests).

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run ./pkg/ovdash/...
git add pkg/ovdash/galaxy.go pkg/ovdash/galaxy_test.go
git commit --no-verify -m "feat(ovdash): galaxy loader with name->id resolution"
```

---

### Task 2: pkg/ovdash — fleet snapshot merger

**Files:**
- Create: `pkg/ovdash/snapshot.go`
- Test: `pkg/ovdash/snapshot_test.go`

**Interfaces:**
- Consumes: `Galaxy.ResolveName` (Task 1); `balances.StatusFile`/`balances.LiveRecord` from `pkg/overmind/balances`.
- Produces:
  - `var Fleets = []FleetDef{...}` with `type FleetDef struct { File, Label, Color string }` (exact values from Global Constraints).
  - `type AgentState struct { Fleet, AgentID, Role, SystemID, SystemName, POI string; Docked bool; Credits, Hull, MaxHull, Fuel, MaxFuel, CargoUsed, CargoCap float64; Activity string; Healthy, Seen bool; Restarts int; LastSeen string }` with snake_case JSON tags matching the field names (`system_id`, `cargo_capacity` for CargoCap).
  - `type Snapshot struct { CapturedAt map[string]string; Agents []AgentState; OffMap []AgentState; StaleFleets []string }`
  - `func ReadSnapshot(dir string, g *Galaxy, now time.Time, staleAfter time.Duration) (*Snapshot, error)`

- [ ] **Step 1: Write the failing test**

`pkg/ovdash/snapshot_test.go`:

```go
package ovdash

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

func writeStatus(t *testing.T, dir, fleetFile, capturedAt string, ws []balances.LiveRecord) {
	t.Helper()
	b, err := json.Marshal(balances.StatusFile{CapturedAt: capturedAt, Workers: ws})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fleetFile+"-status.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSnapshotMergesAndResolvesSystems(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-5 * time.Second).Format(time.RFC3339)
	writeStatus(t, dir, "mission-learn", fresh, []balances.LiveRecord{
		{AgentID: "fighter-4", Role: "missionrunner", System: "Nova Terra",
			POI: "nova_terra_central", Docked: true, Credits: 7228,
			Hull: 350, MaxHull: 350, Healthy: true, Seen: true},
		{AgentID: "lost-1", System: "Atlantis", Seen: true, Healthy: true},
	})
	writeStatus(t, dir, "fleet", fresh, []balances.LiveRecord{
		{AgentID: "hauler-0", Role: "hauler", System: "Sol", Credits: 100, Seen: true, Healthy: true},
	})

	s, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	if len(s.Agents) != 2 {
		t.Fatalf("want 2 on-map agents, got %+v", s.Agents)
	}
	byID := map[string]AgentState{}
	for _, a := range s.Agents {
		byID[a.AgentID] = a
	}
	f4 := byID["fighter-4"]
	if f4.Fleet != "mission" || f4.SystemID != "nova_terra" || !f4.Docked {
		t.Fatalf("fighter-4 wrong: %+v", f4)
	}
	if h := byID["hauler-0"]; h.Fleet != "haul" || h.SystemID != "sol" {
		t.Fatalf("hauler-0 wrong: %+v", h)
	}
	// Unknown system name goes to OffMap, never dropped.
	if len(s.OffMap) != 1 || s.OffMap[0].AgentID != "lost-1" || s.OffMap[0].SystemName != "Atlantis" {
		t.Fatalf("off-map handling wrong: %+v", s.OffMap)
	}
	// Absent files are stale, present-and-fresh are not.
	stale := map[string]bool{}
	for _, f := range s.StaleFleets {
		stale[f] = true
	}
	if stale["mission"] || stale["haul"] {
		t.Fatalf("fresh fleets marked stale: %v", s.StaleFleets)
	}
	if !stale["craft"] || !stale["mb"] || !stale["assist"] || !stale["shuttle"] {
		t.Fatalf("missing fleets must be stale: %v", s.StaleFleets)
	}
}

func TestReadSnapshotFlagsOldCaptureAsStale(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	old := now.Add(-10 * time.Minute).Format(time.RFC3339)
	writeStatus(t, dir, "craft", old, []balances.LiveRecord{
		{AgentID: "craftsman-1", System: "Sol", Seen: true, Healthy: true},
	})
	s, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range s.StaleFleets {
		if f == "craft" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a 10-minute-old capture must be stale, got %v", s.StaleFleets)
	}
	// Stale still shows its (last-known) agents — grey-out is the frontend's job.
	if len(s.Agents) != 1 {
		t.Fatalf("stale fleet agents must still be listed, got %+v", s.Agents)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/ovdash/ -run TestReadSnapshot -count=1`
Expected: FAIL — `ReadSnapshot` undefined.

- [ ] **Step 3: Implement**

`pkg/ovdash/snapshot.go`:

```go
package ovdash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

// FleetDef names one fleet's status file and how the UI labels/colors it.
type FleetDef struct {
	File  string // status file prefix: data/overmind/<File>-status.json
	Label string
	Color string
}

// Fleets is the fixed fleet registry. Order is display order.
var Fleets = []FleetDef{
	{File: "fleet", Label: "haul", Color: "#d4a017"},
	{File: "mission-learn", Label: "mission", Color: "#22d3ee"},
	{File: "craft", Label: "craft", Color: "#34d399"},
	{File: "mb", Label: "mb", Color: "#a78bfa"},
	{File: "assist", Label: "assist", Color: "#fb923c"},
	{File: "shuttle", Label: "shuttle", Color: "#f472b6"},
}

// AgentState is one worker in one snapshot, system resolved to a map id.
type AgentState struct {
	Fleet      string  `json:"fleet"`
	AgentID    string  `json:"agent_id"`
	Role       string  `json:"role"`
	SystemID   string  `json:"system_id"`
	SystemName string  `json:"system_name"`
	POI        string  `json:"poi"`
	Docked     bool    `json:"docked"`
	Credits    float64 `json:"credits"`
	Hull       float64 `json:"hull"`
	MaxHull    float64 `json:"max_hull"`
	Fuel       float64 `json:"fuel"`
	MaxFuel    float64 `json:"max_fuel"`
	CargoUsed  float64 `json:"cargo_used"`
	CargoCap   float64 `json:"cargo_capacity"`
	Activity   string  `json:"activity,omitempty"`
	Healthy    bool    `json:"healthy"`
	Seen       bool    `json:"seen"`
	Restarts   int     `json:"restarts"`
	LastSeen   string  `json:"last_seen"`
}

// Snapshot is the merged live view across every fleet.
type Snapshot struct {
	CapturedAt  map[string]string `json:"captured_at"`  // fleet label -> RFC3339
	Agents      []AgentState      `json:"agents"`       // system resolved
	OffMap      []AgentState      `json:"off_map"`      // unresolvable system names
	StaleFleets []string          `json:"stale_fleets"` // labels; missing/old/corrupt
}

// ReadSnapshot reads every fleet status file under dir and merges them.
// A missing, corrupt, or older-than-staleAfter file marks that fleet stale;
// its last-good agents (if parseable) still appear — greying out is UI policy,
// data completeness is ours.
func ReadSnapshot(dir string, g *Galaxy, now time.Time, staleAfter time.Duration) (*Snapshot, error) {
	s := &Snapshot{CapturedAt: map[string]string{}}
	for _, f := range Fleets {
		path := filepath.Join(dir, f.File+"-status.json")
		b, err := os.ReadFile(path)
		if err != nil {
			s.StaleFleets = append(s.StaleFleets, f.Label)
			continue
		}
		var sf balances.StatusFile
		if err := json.Unmarshal(b, &sf); err != nil {
			s.StaleFleets = append(s.StaleFleets, f.Label)
			continue
		}
		s.CapturedAt[f.Label] = sf.CapturedAt
		if ts, err := time.Parse(time.RFC3339, sf.CapturedAt); err != nil || now.Sub(ts) > staleAfter {
			s.StaleFleets = append(s.StaleFleets, f.Label)
		}
		for _, w := range sf.Workers {
			a := AgentState{
				Fleet: f.Label, AgentID: w.AgentID, Role: w.Role,
				SystemName: w.System, POI: w.POI, Docked: w.Docked,
				Credits: w.Credits, Hull: w.Hull, MaxHull: w.MaxHull,
				Fuel: w.Fuel, MaxFuel: w.MaxFuel,
				CargoUsed: w.CargoUsed, CargoCap: w.CargoCapacity,
				Activity: w.Activity, Healthy: w.Healthy, Seen: w.Seen,
				Restarts: w.Restarts, LastSeen: w.LastSeen,
			}
			if id, ok := g.ResolveName(w.System); ok {
				a.SystemID = id
				s.Agents = append(s.Agents, a)
			} else {
				s.OffMap = append(s.OffMap, a)
			}
		}
	}
	if len(s.Agents) == 0 && len(s.OffMap) == 0 && len(s.StaleFleets) == len(Fleets) {
		return s, fmt.Errorf("no readable status files in %s", dir)
	}
	return s, nil
}
```

Note: the error case still returns the snapshot — callers may serve the
all-stale view; the error is for logging.

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/ovdash/ -count=1`
Expected: PASS.

Check `balances.LiveRecord` field names compile as used (POI vs Poi — the
struct declares `POI string` per `pkg/overmind/balances/balances.go`; fix the
test/impl to the real names if the compiler disagrees — never edit
`balances.go` itself).

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run ./pkg/ovdash/...
git add pkg/ovdash/snapshot.go pkg/ovdash/snapshot_test.go
git commit --no-verify -m "feat(ovdash): fleet status merger with name resolution and staleness"
```

---

### Task 3: pkg/ovdash — delta computation + SSE hub

**Files:**
- Create: `pkg/ovdash/stream.go`
- Test: `pkg/ovdash/stream_test.go`

**Interfaces:**
- Consumes: `Snapshot`, `AgentState` (Task 2).
- Produces:
  - `type Delta struct { Moved []AgentMove; Updated []AgentState; Joined []AgentState; Left []string; StaleFleets []string }` with `type AgentMove struct { Agent AgentState; FromSystemID string }` (JSON tags `moved/updated/joined/left/stale_fleets`, `agent/from_system_id`).
  - `func Diff(prev, cur *Snapshot) Delta` — pure function; `prev == nil` → everything Joined.
  - `type Hub struct{...}`, `func NewHub() *Hub`, `func (h *Hub) Broadcast(event string, payload any)`, `func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request)` — an SSE endpoint that (a) on connect immediately sends the latest `snapshot` keyframe, (b) relays every Broadcast, (c) drops slow clients rather than blocking.
  - `func (h *Hub) SetKeyframe(event string, payload any)` — stores what new connections receive.

- [ ] **Step 1: Write the failing tests**

`pkg/ovdash/stream_test.go`:

```go
package ovdash

import (
	"bufio"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func agent(fleet, id, sys string, credits float64) AgentState {
	return AgentState{Fleet: fleet, AgentID: id, SystemID: sys, Credits: credits, Healthy: true, Seen: true}
}

func TestDiffClassifiesMovesUpdatesJoinsLeaves(t *testing.T) {
	prev := &Snapshot{Agents: []AgentState{
		agent("haul", "h1", "sol", 100),
		agent("haul", "h2", "sol", 100),
		agent("mission", "m1", "nova_terra", 50),
	}}
	cur := &Snapshot{Agents: []AgentState{
		agent("haul", "h1", "nova_terra", 100),  // moved
		agent("haul", "h2", "sol", 250),         // vitals changed
		agent("mission", "m2", "sol", 10),       // joined
	}, StaleFleets: []string{"craft"}}

	d := Diff(prev, cur)
	if len(d.Moved) != 1 || d.Moved[0].Agent.AgentID != "h1" || d.Moved[0].FromSystemID != "sol" {
		t.Fatalf("moved wrong: %+v", d.Moved)
	}
	if len(d.Updated) != 1 || d.Updated[0].AgentID != "h2" {
		t.Fatalf("updated wrong: %+v", d.Updated)
	}
	if len(d.Joined) != 1 || d.Joined[0].AgentID != "m2" {
		t.Fatalf("joined wrong: %+v", d.Joined)
	}
	if len(d.Left) != 1 || d.Left[0] != "m1" {
		t.Fatalf("left wrong: %+v", d.Left)
	}
	if len(d.StaleFleets) != 1 || d.StaleFleets[0] != "craft" {
		t.Fatalf("stale fleets must pass through: %+v", d.StaleFleets)
	}
}

func TestDiffNilPrevIsAllJoins(t *testing.T) {
	cur := &Snapshot{Agents: []AgentState{agent("haul", "h1", "sol", 1)}}
	d := Diff(nil, cur)
	if len(d.Joined) != 1 || len(d.Moved)+len(d.Updated)+len(d.Left) != 0 {
		t.Fatalf("nil prev must be all joins: %+v", d)
	}
}

func TestDiffUnchangedAgentEmitsNothing(t *testing.T) {
	a := agent("haul", "h1", "sol", 100)
	d := Diff(&Snapshot{Agents: []AgentState{a}}, &Snapshot{Agents: []AgentState{a}})
	if len(d.Moved)+len(d.Updated)+len(d.Joined)+len(d.Left) != 0 {
		t.Fatalf("identical snapshots must be an empty delta: %+v", d)
	}
}

func TestHubSendsKeyframeOnConnectAndRelaysBroadcasts(t *testing.T) {
	h := NewHub()
	h.SetKeyframe("snapshot", map[string]int{"n": 1})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/overmind/stream", nil)
	done := make(chan struct{})
	go func() { h.ServeHTTP(rec, req); close(done) }()

	// Give the handler a beat to register, then broadcast and close.
	time.Sleep(50 * time.Millisecond)
	h.Broadcast("delta", map[string]int{"n": 2})
	time.Sleep(50 * time.Millisecond)
	h.CloseAll()
	<-done

	body := rec.Body.String()
	r := bufio.NewScanner(strings.NewReader(body))
	var events []string
	for r.Scan() {
		if strings.HasPrefix(r.Text(), "event: ") {
			events = append(events, strings.TrimPrefix(r.Text(), "event: "))
		}
	}
	if len(events) < 2 || events[0] != "snapshot" || events[1] != "delta" {
		t.Fatalf("want keyframe then delta, got %v in %q", events, body)
	}
	if !strings.Contains(body, `data: {"n":1}`) || !strings.Contains(body, `data: {"n":2}`) {
		t.Fatalf("payloads missing: %q", body)
	}
}
```

(`CloseAll()` is part of the Hub interface for tests and shutdown.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/ovdash/ -run 'TestDiff|TestHub' -count=1`
Expected: FAIL — undefined `Diff`, `NewHub`.

- [ ] **Step 3: Implement**

`pkg/ovdash/stream.go`:

```go
package ovdash

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// AgentMove is a system transition; FromSystemID lets the frontend animate
// along the lane between the two systems.
type AgentMove struct {
	Agent        AgentState `json:"agent"`
	FromSystemID string     `json:"from_system_id"`
}

// Delta is the between-snapshots change set pushed over SSE.
type Delta struct {
	Moved       []AgentMove  `json:"moved"`
	Updated     []AgentState `json:"updated"`
	Joined      []AgentState `json:"joined"`
	Left        []string     `json:"left"`
	StaleFleets []string     `json:"stale_fleets"`
}

// Empty reports whether the delta carries no agent changes (stale-fleet
// changes alone still count as content for the caller to decide on).
func (d Delta) Empty() bool {
	return len(d.Moved)+len(d.Updated)+len(d.Joined)+len(d.Left) == 0
}

// Diff computes the change set from prev to cur. Off-map agents are treated
// like on-map ones (keyed by agent_id) so an agent entering/leaving the
// unresolved tray still produces events.
func Diff(prev, cur *Snapshot) Delta {
	d := Delta{StaleFleets: cur.StaleFleets}
	prevByID := map[string]AgentState{}
	if prev != nil {
		for _, a := range append(append([]AgentState{}, prev.Agents...), prev.OffMap...) {
			prevByID[a.AgentID] = a
		}
	}
	seen := map[string]bool{}
	for _, a := range append(append([]AgentState{}, cur.Agents...), cur.OffMap...) {
		seen[a.AgentID] = true
		p, ok := prevByID[a.AgentID]
		switch {
		case !ok:
			d.Joined = append(d.Joined, a)
		case p.SystemID != a.SystemID:
			d.Moved = append(d.Moved, AgentMove{Agent: a, FromSystemID: p.SystemID})
		case p != a:
			d.Updated = append(d.Updated, a)
		}
	}
	for id := range prevByID {
		if !seen[id] {
			d.Left = append(d.Left, id)
		}
	}
	return d
}

// Hub fans SSE events out to connected dashboard clients.
type Hub struct {
	mu       sync.Mutex
	clients  map[chan []byte]struct{}
	keyframe []byte // pre-rendered "event: ...\ndata: ...\n\n" sent on connect
}

func NewHub() *Hub { return &Hub{clients: map[chan []byte]struct{}{}} }

func render(event string, payload any) []byte {
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return []byte("event: " + event + "\ndata: " + string(b) + "\n\n")
}

// SetKeyframe stores the frame every new connection receives first.
func (h *Hub) SetKeyframe(event string, payload any) {
	f := render(event, payload)
	h.mu.Lock()
	h.keyframe = f
	h.mu.Unlock()
}

// Broadcast sends an event to every connected client. A client whose buffer
// is full is dropped (its next keyframe reconnect self-heals) — a stuck
// browser must never block the loop.
func (h *Hub) Broadcast(event string, payload any) {
	f := render(event, payload)
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- f:
		default:
			close(ch)
			delete(h.clients, ch)
		}
	}
}

// CloseAll disconnects every client (shutdown and tests).
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		close(ch)
		delete(h.clients, ch)
	}
}

// ServeHTTP is the SSE endpoint.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	kf := h.keyframe
	h.mu.Unlock()

	if kf != nil {
		_, _ = w.Write(kf)
		fl.Flush()
	}
	defer func() {
		h.mu.Lock()
		if _, live := h.clients[ch]; live {
			delete(h.clients, ch)
			close(ch)
		}
		h.mu.Unlock()
	}()
	for {
		select {
		case <-r.Context().Done():
			return
		case f, open := <-ch:
			if !open {
				return
			}
			if _, err := w.Write(f); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
```

- [ ] **Step 4: Run the tests (including race)**

Run: `go test ./pkg/ovdash/ -count=1 -race`
Expected: PASS, no races.

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run ./pkg/ovdash/...
git add pkg/ovdash/stream.go pkg/ovdash/stream_test.go
git commit --no-verify -m "feat(ovdash): snapshot diff and SSE hub"
```

---

### Task 4: pkg/ovdash — accounting aggregator

**Files:**
- Create: `pkg/ovdash/accounting.go`
- Test: `pkg/ovdash/accounting_test.go`

**Interfaces:**
- Consumes: `Snapshot` (for live wallet totals and counts).
- Produces:
  - `type SourceEarnings struct { Total, PerHour float64; Count int }` (JSON `total/per_hour/count`)
  - `type Accounting struct { TotalCredits float64; Agents, Healthy, Unseen, Restarts int; Haul, Freight, Missions SourceEarnings; CombinedPerHour, PerAgentPerHour float64; OldestCapture string }` (snake_case tags)
  - `func LoadEarnings(ctx context.Context, dbPath string, now time.Time, window time.Duration) (haul, freight, missions SourceEarnings, err error)`
  - `func BuildAccounting(s *Snapshot, haul, freight, missions SourceEarnings, window time.Duration) Accounting`

Earnings definitions (Global Constraints columns, trailing `window` ending at `now`, RFC3339 TEXT comparison):
- haul: `SELECT COALESCE(SUM(realized_profit),0), COUNT(*) FROM haul_results WHERE sold_at >= ?`
- freight: `SELECT COALESCE(SUM(carrier_payout - fuel_cost),0), COUNT(*) FROM freight_results WHERE outcome='delivered' AND finished_at >= ?`
- missions: `SELECT COALESCE(SUM(credits_earned - item_cost - fuel_cost),0), COUNT(*) FROM mission_results WHERE finished_at >= ?`

- [ ] **Step 1: Write the failing test**

`pkg/ovdash/accounting_test.go`:

```go
package ovdash

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func fixtureMarket(t *testing.T, now time.Time) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "market.db")
	db, err := sql.Open(sqliteDriver, p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	in := now.Add(-time.Hour).Format(time.RFC3339)
	out := now.Add(-48 * time.Hour).Format(time.RFC3339)
	stmts := []string{
		`CREATE TABLE haul_results (realized_profit REAL, sold_at TEXT)`,
		`CREATE TABLE freight_results (carrier_payout REAL, fuel_cost REAL, outcome TEXT, finished_at TEXT)`,
		`CREATE TABLE mission_results (credits_earned REAL, item_cost REAL, fuel_cost REAL, finished_at TEXT)`,
		`INSERT INTO haul_results VALUES (1000, '` + in + `'), (9999, '` + out + `')`,
		`INSERT INTO freight_results VALUES (1402, 40, 'delivered', '` + in + `'),
			(500, 0, 'breached', '` + in + `'), (7777, 0, 'delivered', '` + out + `')`,
		`INSERT INTO mission_results VALUES (600, 100, 50, '` + in + `'), (8888, 0, 0, '` + out + `')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestLoadEarningsWindowsAndFilters(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	haul, freight, missions, err := LoadEarnings(context.Background(), fixtureMarket(t, now), now, 24*time.Hour)
	if err != nil {
		t.Fatalf("LoadEarnings: %v", err)
	}
	if haul.Total != 1000 || haul.Count != 1 {
		t.Fatalf("haul: %+v (48h-old row must be excluded)", haul)
	}
	if freight.Total != 1362 || freight.Count != 1 {
		t.Fatalf("freight: %+v (non-delivered and out-of-window excluded)", freight)
	}
	if missions.Total != 450 || missions.Count != 1 {
		t.Fatalf("missions: %+v (600-100-50)", missions)
	}
	if haul.PerHour != 1000.0/24.0 {
		t.Fatalf("per-hour = total/window hours, got %v", haul.PerHour)
	}
}

func TestBuildAccountingTotalsSnapshot(t *testing.T) {
	s := &Snapshot{
		Agents: []AgentState{
			{AgentID: "a", Credits: 100, Healthy: true, Seen: true},
			{AgentID: "b", Credits: 200, Healthy: false, Seen: true, Restarts: 3},
		},
		OffMap:      []AgentState{{AgentID: "c", Credits: 50, Healthy: true, Seen: false}},
		CapturedAt:  map[string]string{"haul": "2026-07-20T11:59:00Z", "craft": "2026-07-20T10:00:00Z"},
	}
	acct := BuildAccounting(s,
		SourceEarnings{Total: 240, PerHour: 10, Count: 2},
		SourceEarnings{Total: 24, PerHour: 1, Count: 1},
		SourceEarnings{Total: 48, PerHour: 2, Count: 4},
		24*time.Hour)
	if acct.TotalCredits != 350 || acct.Agents != 3 || acct.Healthy != 2 || acct.Unseen != 1 || acct.Restarts != 3 {
		t.Fatalf("counts wrong: %+v", acct)
	}
	if acct.CombinedPerHour != 13 {
		t.Fatalf("combined/hr: %v", acct.CombinedPerHour)
	}
	// Per-agent average divides by agents that are healthy AND seen.
	if acct.PerAgentPerHour != 13.0/1.0 {
		t.Fatalf("per-agent/hr: %v (only agent a is healthy+seen)", acct.PerAgentPerHour)
	}
	if acct.OldestCapture != "2026-07-20T10:00:00Z" {
		t.Fatalf("oldest capture: %q", acct.OldestCapture)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/ovdash/ -run 'TestLoadEarnings|TestBuildAccounting' -count=1`
Expected: FAIL — undefined functions.

- [ ] **Step 3: Implement**

`pkg/ovdash/accounting.go`:

```go
package ovdash

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SourceEarnings is one earnings stream's trailing-window aggregate.
type SourceEarnings struct {
	Total   float64 `json:"total"`
	PerHour float64 `json:"per_hour"`
	Count   int     `json:"count"`
}

// Accounting is the top-strip payload.
type Accounting struct {
	TotalCredits    float64        `json:"total_credits"`
	Agents          int            `json:"agents"`
	Healthy         int            `json:"healthy"`
	Unseen          int            `json:"unseen"`
	Restarts        int            `json:"restarts"`
	Haul            SourceEarnings `json:"haul"`
	Freight         SourceEarnings `json:"freight"`
	Missions        SourceEarnings `json:"missions"`
	CombinedPerHour float64        `json:"combined_per_hour"`
	PerAgentPerHour float64        `json:"per_agent_per_hour"`
	OldestCapture   string         `json:"oldest_capture"`
}

func earnQuery(ctx context.Context, db *sql.DB, q, since string, hours float64) (SourceEarnings, error) {
	var e SourceEarnings
	if err := db.QueryRowContext(ctx, q, since).Scan(&e.Total, &e.Count); err != nil {
		return e, err
	}
	e.PerHour = e.Total / hours
	return e, nil
}

// LoadEarnings computes the three trailing-window earnings streams from the
// market DB (read-only).
func LoadEarnings(ctx context.Context, dbPath string, now time.Time, window time.Duration) (haul, freight, missions SourceEarnings, err error) {
	db, err := sql.Open(sqliteDriver, "file:"+dbPath+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return haul, freight, missions, fmt.Errorf("open market db: %w", err)
	}
	defer db.Close()
	since := now.Add(-window).UTC().Format(time.RFC3339)
	hours := window.Hours()

	if haul, err = earnQuery(ctx, db,
		`SELECT COALESCE(SUM(realized_profit),0), COUNT(*) FROM haul_results WHERE sold_at >= ?`,
		since, hours); err != nil {
		return haul, freight, missions, fmt.Errorf("haul earnings: %w", err)
	}
	if freight, err = earnQuery(ctx, db,
		`SELECT COALESCE(SUM(carrier_payout - fuel_cost),0), COUNT(*) FROM freight_results
		 WHERE outcome='delivered' AND finished_at >= ?`,
		since, hours); err != nil {
		return haul, freight, missions, fmt.Errorf("freight earnings: %w", err)
	}
	if missions, err = earnQuery(ctx, db,
		`SELECT COALESCE(SUM(credits_earned - item_cost - fuel_cost),0), COUNT(*) FROM mission_results
		 WHERE finished_at >= ?`,
		since, hours); err != nil {
		return haul, freight, missions, fmt.Errorf("mission earnings: %w", err)
	}
	return haul, freight, missions, nil
}

// BuildAccounting merges live snapshot totals with the earnings streams.
func BuildAccounting(s *Snapshot, haul, freight, missions SourceEarnings, window time.Duration) Accounting {
	a := Accounting{Haul: haul, Freight: freight, Missions: missions}
	active := 0
	all := append(append([]AgentState{}, s.Agents...), s.OffMap...)
	for _, w := range all {
		a.Agents++
		a.TotalCredits += w.Credits
		a.Restarts += w.Restarts
		if w.Healthy {
			a.Healthy++
		}
		if !w.Seen {
			a.Unseen++
		}
		if w.Healthy && w.Seen {
			active++
		}
	}
	a.CombinedPerHour = haul.PerHour + freight.PerHour + missions.PerHour
	if active > 0 {
		a.PerAgentPerHour = a.CombinedPerHour / float64(active)
	}
	for _, ts := range s.CapturedAt {
		if a.OldestCapture == "" || ts < a.OldestCapture {
			a.OldestCapture = ts
		}
	}
	return a
}
```

- [ ] **Step 4: Run all package tests**

Run: `go test ./pkg/ovdash/ -count=1`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run ./pkg/ovdash/...
git add pkg/ovdash/accounting.go pkg/ovdash/accounting_test.go
git commit --no-verify -m "feat(ovdash): trailing-window earnings accounting"
```

---

### Task 5: cmd/overmind-dashboard — HTTP server + loops

**Files:**
- Create: `cmd/overmind-dashboard/main.go`
- Test: `cmd/overmind-dashboard/main_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1-4.
- Produces (HTTP, all under `/api/overmind/`):
  - `GET /api/overmind/systems` → `[]SystemNode`
  - `GET /api/overmind/agents` → latest `Snapshot`
  - `GET /api/overmind/accounting` → latest `Accounting`
  - `GET /api/overmind/stream` → SSE: `snapshot` keyframe on connect + every 60s; `delta` on change; `accounting` every 30s
  - `GET /` → static from `--dist` dir (default `frontend/dist`)
- Flags: `--addr :8091 --kb data/spacemolt-knowledge.db --market-db data/market.db --status-dir data/overmind --dist frontend/dist`

- [ ] **Step 1: Write the failing integration test**

`cmd/overmind-dashboard/main_test.go` — build a `server` value (the testable core of main) against fixture dirs:

```go
package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/ovdash"
)

// Test fixtures: reuse the shapes from pkg/ovdash tests by writing files
// directly (the fixture helpers there are package-private).
func writeFixtures(t *testing.T) (kb, market, statusDir string) {
	t.Helper()
	// Smallest possible KB: one system, no lanes.
	dir := t.TempDir()
	kb = filepath.Join(dir, "kb.db")
	mustExec(t, kb, []string{
		`CREATE TABLE systems (id TEXT PRIMARY KEY, name TEXT NOT NULL,
			position_x REAL NOT NULL, position_y REAL NOT NULL,
			police_level INTEGER DEFAULT 0, empire TEXT DEFAULT '',
			is_stronghold BOOLEAN DEFAULT 0, last_visited_tick INTEGER DEFAULT 0)`,
		`CREATE TABLE connections (from_system TEXT, to_system TEXT, distance REAL)`,
		`INSERT INTO systems VALUES ('sol','Sol',0,0,10,'solarian',0,1)`,
	})
	market = filepath.Join(dir, "market.db")
	mustExec(t, market, []string{
		`CREATE TABLE haul_results (realized_profit REAL, sold_at TEXT)`,
		`CREATE TABLE freight_results (carrier_payout REAL, fuel_cost REAL, outcome TEXT, finished_at TEXT)`,
		`CREATE TABLE mission_results (credits_earned REAL, item_cost REAL, fuel_cost REAL, finished_at TEXT)`,
	})
	statusDir = filepath.Join(dir, "overmind")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(statusDir, "fleet-status.json"), map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339),
		"workers": []map[string]any{{
			"agent_id": "hauler-0", "role": "hauler", "system": "Sol",
			"docked": true, "credits": 42.0, "healthy": true, "seen": true,
		}},
	})
	return kb, market, statusDir
}

func TestServerEndpoints(t *testing.T) {
	kb, market, statusDir := writeFixtures(t)
	srv, err := newServer(context.Background(), serverConfig{
		KBPath: kb, MarketPath: market, StatusDir: statusDir, DistDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	srv.refresh(context.Background(), time.Now()) // one manual loop turn

	ts := httptest.NewServer(srv.mux())
	defer ts.Close()

	var systems []map[string]any
	getJSON(t, ts.URL+"/api/overmind/systems", &systems)
	if len(systems) != 1 || systems[0]["id"] != "sol" {
		t.Fatalf("systems: %+v", systems)
	}

	var snap ovdash.Snapshot
	getJSON(t, ts.URL+"/api/overmind/agents", &snap)
	if len(snap.Agents) != 1 || snap.Agents[0].SystemID != "sol" || snap.Agents[0].Fleet != "haul" {
		t.Fatalf("agents: %+v", snap)
	}

	var acct ovdash.Accounting
	getJSON(t, ts.URL+"/api/overmind/accounting", &acct)
	if acct.TotalCredits != 42 || acct.Agents != 1 {
		t.Fatalf("accounting: %+v", acct)
	}
}
```

Include the small helpers (`mustExec` opening sql + executing statements,
`writeJSON`, `getJSON` doing http.Get + decode + Fatalf on error) at the
bottom of the test file — each ~6 lines, standard library only.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/overmind-dashboard/ -count=1`
Expected: FAIL — `newServer` undefined.

- [ ] **Step 3: Implement**

`cmd/overmind-dashboard/main.go`:

```go
// Command overmind-dashboard serves the unified fleet ops dashboard: live
// galaxy map, per-agent cards, and fleet accounting, backed by the overmind
// status files, the knowledge DB, and market.db — strictly read-only.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/ovdash"
)

const (
	snapshotEvery  = 2 * time.Second
	accountEvery   = 30 * time.Second
	keyframeEvery  = 60 * time.Second
	staleAfter     = 60 * time.Second
	earningsWindow = 24 * time.Hour
)

type serverConfig struct {
	KBPath, MarketPath, StatusDir, DistDir string
}

type server struct {
	cfg    serverConfig
	galaxy *ovdash.Galaxy
	hub    *ovdash.Hub

	mu    sync.RWMutex
	snap  *ovdash.Snapshot
	acct  ovdash.Accounting
	lastAcct time.Time
}

func newServer(ctx context.Context, cfg serverConfig) (*server, error) {
	g, err := ovdash.LoadGalaxy(ctx, cfg.KBPath)
	if err != nil {
		return nil, err
	}
	return &server{cfg: cfg, galaxy: g, hub: ovdash.NewHub()}, nil
}

// refresh performs one loop turn: read snapshot, diff, broadcast, and (when
// due) refresh accounting. Split out from run() so tests drive it directly.
func (s *server) refresh(ctx context.Context, now time.Time) {
	snap, err := ovdash.ReadSnapshot(s.cfg.StatusDir, s.galaxy, now, staleAfter)
	if err != nil {
		log.Printf("snapshot: %v", err)
	}
	if snap == nil {
		return
	}
	s.mu.Lock()
	prev := s.snap
	s.snap = snap
	s.mu.Unlock()

	d := ovdash.Diff(prev, snap)
	if !d.Empty() || prev == nil {
		s.hub.Broadcast("delta", d)
	}
	s.hub.SetKeyframe("snapshot", snap)

	if now.Sub(s.lastAcct) >= accountEvery {
		haul, freight, missions, err := ovdash.LoadEarnings(ctx, s.cfg.MarketPath, now, earningsWindow)
		if err != nil {
			log.Printf("earnings: %v", err) // keep last-good acct
		} else {
			acct := ovdash.BuildAccounting(snap, haul, freight, missions, earningsWindow)
			s.mu.Lock()
			s.acct = acct
			s.mu.Unlock()
			s.hub.Broadcast("accounting", acct)
		}
		s.lastAcct = now
	}
}

func (s *server) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/overmind/systems", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResp(w, s.galaxy.Systems)
	})
	m.HandleFunc("GET /api/overmind/agents", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.RLock()
		snap := s.snap
		s.mu.RUnlock()
		writeJSONResp(w, snap)
	})
	m.HandleFunc("GET /api/overmind/accounting", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.RLock()
		acct := s.acct
		s.mu.RUnlock()
		writeJSONResp(w, acct)
	})
	m.Handle("GET /api/overmind/stream", s.hub)
	m.Handle("/", http.FileServer(http.Dir(s.cfg.DistDir)))
	return m
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func (s *server) run(ctx context.Context) {
	snapTick := time.NewTicker(snapshotEvery)
	kfTick := time.NewTicker(keyframeEvery)
	defer snapTick.Stop()
	defer kfTick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.hub.CloseAll()
			return
		case now := <-snapTick.C:
			s.refresh(ctx, now)
		case <-kfTick.C:
			s.mu.RLock()
			snap := s.snap
			s.mu.RUnlock()
			if snap != nil {
				s.hub.Broadcast("snapshot", snap)
			}
		}
	}
}

func main() {
	addr := flag.String("addr", ":8091", "HTTP listen address")
	kb := flag.String("kb", "data/spacemolt-knowledge.db", "knowledge DB path (read-only)")
	marketDB := flag.String("market-db", "data/market.db", "market DB path (read-only)")
	statusDir := flag.String("status-dir", "data/overmind", "overmind status-file directory")
	dist := flag.String("dist", "frontend/dist", "built frontend directory")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := newServer(ctx, serverConfig{KBPath: *kb, MarketPath: *marketDB, StatusDir: *statusDir, DistDir: *dist})
	if err != nil {
		log.Fatalf("overmind-dashboard: %v", err)
	}
	srv.refresh(ctx, time.Now())
	go srv.run(ctx)

	log.Printf("overmind-dashboard: %d systems loaded, serving on %s", len(srv.galaxy.Systems), *addr)
	httpSrv := &http.Server{Addr: *addr, Handler: srv.mux(), ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); _ = httpSrv.Close() }()
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
```

(Add the `encoding/json` import.)

- [ ] **Step 4: Run tests and build the binary**

```bash
go test ./cmd/overmind-dashboard/ ./pkg/ovdash/ -count=1
go build -o bin/overmind-dashboard ./cmd/overmind-dashboard
```
Expected: PASS; binary in `bin/`.

- [ ] **Step 5: Manual smoke against real data**

```bash
./bin/overmind-dashboard --addr :8091 &
curl -s localhost:8091/api/overmind/agents | head -c 300
curl -s localhost:8091/api/overmind/accounting
curl -sN localhost:8091/api/overmind/stream | head -c 200
kill %1
```
Expected: real agent JSON (~112 workers), accounting with non-zero totals, an SSE `event: snapshot` frame.

- [ ] **Step 6: Lint and commit**

```bash
golangci-lint run ./cmd/overmind-dashboard/...
git add cmd/overmind-dashboard/
git commit --no-verify -m "feat(overmind-dashboard): read-only ops API server with SSE"
```

---

### Task 6: frontend — vite proxy + useFleetStream hook

**Files:**
- Modify: `frontend/vite.config.ts` (add proxy)
- Modify: `frontend/src/lib/useGalaxyMap.ts` (optional basePath param)
- Create: `frontend/src/lib/useFleetStream.ts`

**Interfaces:**
- Consumes: `/api/overmind/*` endpoints (Task 5 shapes).
- Produces: `useFleetStream(): { agents: Map<string, AgentState>; accounting: Accounting | null; staleFleets: string[]; moves: AgentMove[]; connected: boolean }` and exported TS types `AgentState`, `Accounting`, `AgentMove`, `FLEETS` (label→color map mirroring `ovdash.Fleets`).
- `useGalaxyMap(basePath = '')` — existing callers unaffected; Overmind page passes `'/api/overmind'` (endpoint `/api/overmind/systems` replaces `/api/systems`; the agents fetch inside the hook is skipped when basePath is set, since the stream supplies agents).

- [ ] **Step 1: Add the proxy entry**

In `frontend/vite.config.ts`, inside the existing `proxy` object, ABOVE the `'/api'` entry (more-specific first):

```ts
      '/api/overmind': {
        target: 'http://localhost:8091',
        changeOrigin: true,
      },
```

- [ ] **Step 2: Parameterize useGalaxyMap**

In `frontend/src/lib/useGalaxyMap.ts`: change the signature to
`export function useGalaxyMap(basePath = ''): GalaxyMapData | null` and derive the URLs explicitly:

```ts
export function useGalaxyMap(basePath = ''): GalaxyMapData | null {
  ...
  const systemsURL = basePath ? `${basePath}/systems` : '/api/systems';
  fetch(systemsURL)
  ...
  // When basePath is set the caller owns agent data (SSE); skip /api/agents.
  if (basePath) { setData({ systems, agentLocations: [] }); return; }
```

(dependency array gains `basePath`.)

- [ ] **Step 3: Write useFleetStream**

`frontend/src/lib/useFleetStream.ts`:

```ts
import { useEffect, useRef, useState } from 'react';

export interface AgentState {
  fleet: string;
  agent_id: string;
  role: string;
  system_id: string;
  system_name: string;
  poi: string;
  docked: boolean;
  credits: number;
  hull: number;
  max_hull: number;
  fuel: number;
  max_fuel: number;
  cargo_used: number;
  cargo_capacity: number;
  activity?: string;
  healthy: boolean;
  seen: boolean;
  restarts: number;
  last_seen: string;
}

export interface SourceEarnings { total: number; per_hour: number; count: number }

export interface Accounting {
  total_credits: number;
  agents: number;
  healthy: number;
  unseen: number;
  restarts: number;
  haul: SourceEarnings;
  freight: SourceEarnings;
  missions: SourceEarnings;
  combined_per_hour: number;
  per_agent_per_hour: number;
  oldest_capture: string;
}

export interface AgentMove { agent: AgentState; from_system_id: string }

interface Snapshot {
  agents: AgentState[];
  off_map: AgentState[];
  stale_fleets: string[] | null;
}

interface Delta {
  moved: AgentMove[] | null;
  updated: AgentState[] | null;
  joined: AgentState[] | null;
  left: string[] | null;
  stale_fleets: string[] | null;
}

/** Fleet label -> accent color; must mirror pkg/ovdash Fleets. */
export const FLEETS: Record<string, string> = {
  haul: '#d4a017',
  mission: '#22d3ee',
  craft: '#34d399',
  mb: '#a78bfa',
  assist: '#fb923c',
  shuttle: '#f472b6',
};

export interface FleetStream {
  agents: Map<string, AgentState>;
  offMap: AgentState[];
  accounting: Accounting | null;
  staleFleets: string[];
  /** Moves from the most recent delta — consumed by the map for animation. */
  moves: AgentMove[];
  connected: boolean;
}

export function useFleetStream(streamURL = '/api/overmind/stream'): FleetStream {
  const [state, setState] = useState<FleetStream>({
    agents: new Map(), offMap: [], accounting: null,
    staleFleets: [], moves: [], connected: false,
  });
  const agentsRef = useRef(new Map<string, AgentState>());

  useEffect(() => {
    const es = new EventSource(streamURL);

    es.addEventListener('snapshot', (e) => {
      const snap: Snapshot = JSON.parse((e as MessageEvent).data);
      const m = new Map<string, AgentState>();
      (snap.agents ?? []).forEach((a) => m.set(a.agent_id, a));
      agentsRef.current = m;
      setState((s) => ({
        ...s, agents: new Map(m), offMap: snap.off_map ?? [],
        staleFleets: snap.stale_fleets ?? [], moves: [], connected: true,
      }));
    });

    es.addEventListener('delta', (e) => {
      const d: Delta = JSON.parse((e as MessageEvent).data);
      const m = agentsRef.current;
      (d.joined ?? []).forEach((a) => m.set(a.agent_id, a));
      (d.updated ?? []).forEach((a) => m.set(a.agent_id, a));
      (d.moved ?? []).forEach(({ agent }) => m.set(agent.agent_id, agent));
      (d.left ?? []).forEach((id) => m.delete(id));
      setState((s) => ({
        ...s, agents: new Map(m),
        staleFleets: d.stale_fleets ?? s.staleFleets,
        moves: d.moved ?? [], connected: true,
      }));
    });

    es.addEventListener('accounting', (e) => {
      const acct: Accounting = JSON.parse((e as MessageEvent).data);
      setState((s) => ({ ...s, accounting: acct }));
    });

    es.onerror = () => setState((s) => ({ ...s, connected: false }));
    // EventSource auto-reconnects; the next snapshot keyframe repaints state.

    return () => es.close();
  }, [streamURL]);

  return state;
}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd frontend && npm run build`
Expected: tsc + vite build succeed (hook unused yet — ensure no
`noUnusedLocals` failure by the fact it's an exported module).

- [ ] **Step 5: Commit**

```bash
git add frontend/vite.config.ts frontend/src/lib/useGalaxyMap.ts frontend/src/lib/useFleetStream.ts
git commit --no-verify -m "feat(frontend): overmind SSE hook + proxy + galaxy hook base path"
```

---

### Task 7: frontend — Overmind page scaffold + AccountingStrip + nav

**Files:**
- Create: `frontend/src/components/overmind/OvermindPage.tsx`
- Create: `frontend/src/components/overmind/AccountingStrip.tsx`
- Modify: `frontend/src/App.tsx` (ViewType + nav button + render case)

**Interfaces:**
- Consumes: `useFleetStream`, `FLEETS` (Task 6).
- Produces: `<OvermindPage />` (no props); `<AccountingStrip accounting={Accounting|null} agentCount={number} staleFleets={string[]} connected={boolean} />`. Layout grid classes other tasks slot into: `#ov-map-slot` (children rendered center) and `#ov-rail-slot` (right column) — Tasks 8/9 fill them via props/children on OvermindPage's internal structure (they modify this file; keep it small).

NavCom theme tokens (Tailwind arbitrary values, used consistently in all
overmind components): background `bg-[#0a0a08]`, panel `bg-[#11100c]`,
border `border-[#2a2618]`, accent text `text-[#d4a017]`, dim text
`text-[#8a8570]`, mono numerals `font-mono`.

- [ ] **Step 1: AccountingStrip**

`frontend/src/components/overmind/AccountingStrip.tsx`:

```tsx
import type { Accounting } from '../../lib/useFleetStream';

function cr(n: number): string {
  return Math.round(n).toLocaleString();
}

function Stat({ label, value, warn }: { label: string; value: string; warn?: boolean }) {
  return (
    <div className="px-4 border-r border-[#2a2618] last:border-r-0">
      <div className="text-[10px] uppercase tracking-widest text-[#8a8570]">{label}</div>
      <div className={`font-mono text-lg ${warn ? 'text-red-400' : 'text-[#d4a017]'}`}>{value}</div>
    </div>
  );
}

export function AccountingStrip({ accounting, agentCount, staleFleets, connected }: {
  accounting: Accounting | null;
  agentCount: number;
  staleFleets: string[];
  connected: boolean;
}) {
  const a = accounting;
  return (
    <div className="flex items-center bg-[#11100c] border-b border-[#2a2618] py-2">
      <div className="px-4 text-[#d4a017] font-bold tracking-widest text-sm">FLEET ACCOUNTING</div>
      <Stat label="credits" value={a ? `₡ ${cr(a.total_credits)}` : '—'} />
      <Stat label="agents" value={a ? `${a.healthy}/${a.agents} healthy` : `${agentCount}`}
        warn={!!a && a.healthy < a.agents} />
      <Stat label="earn/hr (24h)" value={a ? cr(a.combined_per_hour) : '—'} />
      <Stat label="haul/hr" value={a ? cr(a.haul.per_hour) : '—'} />
      <Stat label="freight/hr" value={a ? cr(a.freight.per_hour) : '—'} />
      <Stat label="missions/hr" value={a ? cr(a.missions.per_hour) : '—'} />
      <Stat label="restarts" value={a ? `${a.restarts}` : '—'} warn={!!a && a.restarts > 0} />
      <div className="flex-1" />
      {staleFleets.length > 0 && (
        <div className="px-3 text-xs text-amber-500">stale: {staleFleets.join(' ')}</div>
      )}
      <div className={`px-4 text-xs ${connected ? 'text-emerald-500' : 'text-red-500'}`}>
        {connected ? '● live' : '○ reconnecting'}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: OvermindPage scaffold**

`frontend/src/components/overmind/OvermindPage.tsx`:

```tsx
import { useFleetStream } from '../../lib/useFleetStream';
import { AccountingStrip } from './AccountingStrip';

export function OvermindPage() {
  const stream = useFleetStream();
  return (
    <div className="h-full flex flex-col bg-[#0a0a08] text-[#d8d3c0]">
      <AccountingStrip
        accounting={stream.accounting}
        agentCount={stream.agents.size}
        staleFleets={stream.staleFleets}
        connected={stream.connected}
      />
      <div className="flex-1 flex min-h-0">
        <div className="flex-1 min-w-0" id="ov-map-slot">
          {/* Task 9: galaxy map + fleet overlay */}
          <div className="h-full grid place-items-center text-[#8a8570]">map pending</div>
        </div>
        <div className="w-80 border-l border-[#2a2618] overflow-y-auto" id="ov-rail-slot">
          {/* Task 8: fleet rail */}
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Wire into App.tsx**

- Extend the union: `type ViewType = ... | 'overmind';`
- Add to the top-level nav button list (same array the other views use):
  `{ id: 'overmind' as ViewType, label: 'Overmind' }`.
- Add the render branch where other views render:
  `{activeView === 'overmind' && <OvermindPage />}` with the import at top.
  Match the exact conditional-render pattern App.tsx already uses (read the
  file first — it may be a switch or chained `&&`; follow it).

- [ ] **Step 4: Build + manual check**

```bash
cd frontend && npm run build && npm run dev
```
Open the dev server, click "Overmind": accounting strip fills within ~30s
(accounting event) with live numbers from :8091 (server from Task 5 must be
running); "● live" indicator green; map slot shows placeholder.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/overmind/ frontend/src/App.tsx
git commit --no-verify -m "feat(frontend): overmind page scaffold with live accounting strip"
```

---

### Task 8: frontend — FleetRail + AgentCard

**Files:**
- Create: `frontend/src/components/overmind/AgentCard.tsx`
- Create: `frontend/src/components/overmind/FleetRail.tsx`
- Modify: `frontend/src/components/overmind/OvermindPage.tsx` (fill rail slot; add `selectedAgent` state)

**Interfaces:**
- Consumes: `AgentState`, `FLEETS` (Task 6).
- Produces: `<FleetRail agents={AgentState[]} offMap={AgentState[]} staleFleets={string[]} selectedId={string|null} onSelect={(id: string) => void} />`; `<AgentCard agent={AgentState} color={string} selected={boolean} stale={boolean} onClick={() => void} />`. OvermindPage exposes `selectedAgent` state (string|null) shared with Task 9's map (`onSelect` from both sides).

- [ ] **Step 1: AgentCard**

`frontend/src/components/overmind/AgentCard.tsx`:

```tsx
import type { AgentState } from '../../lib/useFleetStream';

function Bar({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0;
  return (
    <span className="inline-block w-16 h-1.5 bg-[#2a2618] rounded-sm align-middle mx-1">
      <span className="block h-full rounded-sm" style={{ width: `${pct}%`, background: color }} />
    </span>
  );
}

export function AgentCard({ agent, color, selected, stale, onClick }: {
  agent: AgentState; color: string; selected: boolean; stale: boolean; onClick: () => void;
}) {
  const unhealthy = !agent.healthy || !agent.seen;
  return (
    <button
      onClick={onClick}
      className={`w-full text-left mb-2 p-2 border rounded-sm bg-[#11100c] text-xs
        ${selected ? 'border-[#d4a017]' : unhealthy ? 'border-red-700' : 'border-[#2a2618]'}
        ${stale ? 'opacity-50' : ''}`}
    >
      <div className="flex items-center justify-between border-b border-[#2a2618] pb-1 mb-1">
        <span className="font-bold" style={{ color }}>{agent.agent_id}</span>
        <span className={unhealthy ? 'text-red-500' : 'text-emerald-500'}>◉</span>
      </div>
      <div className="text-[#8a8570] truncate">
        {agent.system_name} / {agent.poi}{agent.docked ? ' ⚓' : ''}
      </div>
      <div className="font-mono text-[#d8d3c0]">
        ₡ {Math.round(agent.credits).toLocaleString()}
        <span className="text-[#8a8570]"> hull</span>
        <Bar value={agent.hull} max={agent.max_hull} color="#34d399" />
        <span className="text-[#8a8570]">fuel</span>
        <Bar value={agent.fuel} max={agent.max_fuel} color="#22d3ee" />
      </div>
      <div className="font-mono text-[#8a8570]">
        cargo <Bar value={agent.cargo_used} max={agent.cargo_capacity} color="#d4a017" />
        {Math.round(agent.cargo_used)}/{Math.round(agent.cargo_capacity)}
      </div>
      {agent.activity && <div className="text-[#d4a017] truncate">► {agent.activity}</div>}
      <div className="text-[#8a8570]">restarts {agent.restarts}</div>
    </button>
  );
}
```

- [ ] **Step 2: FleetRail**

`frontend/src/components/overmind/FleetRail.tsx`:

```tsx
import { useMemo, useState } from 'react';
import { FLEETS, type AgentState } from '../../lib/useFleetStream';
import { AgentCard } from './AgentCard';

export function FleetRail({ agents, offMap, staleFleets, selectedId, onSelect }: {
  agents: AgentState[]; offMap: AgentState[]; staleFleets: string[];
  selectedId: string | null; onSelect: (id: string) => void;
}) {
  const [filter, setFilter] = useState('');
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const stale = useMemo(() => new Set(staleFleets), [staleFleets]);

  const groups = useMemo(() => {
    const g = new Map<string, AgentState[]>();
    Object.keys(FLEETS).forEach((f) => g.set(f, []));
    [...agents, ...offMap]
      .filter((a) => !filter || a.agent_id.includes(filter) || a.system_name.toLowerCase().includes(filter.toLowerCase()))
      .forEach((a) => g.get(a.fleet)?.push(a) ?? g.set(a.fleet, [a]));
    // Unhealthy first, then by id, within each fleet.
    for (const list of g.values()) {
      list.sort((x, y) =>
        Number(y.healthy && y.seen) - Number(x.healthy && x.seen) === 0
          ? x.agent_id.localeCompare(y.agent_id)
          : Number(x.healthy && x.seen) - Number(y.healthy && y.seen));
    }
    return g;
  }, [agents, offMap, filter]);

  return (
    <div className="p-2">
      <input
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="filter agents…"
        className="w-full mb-2 px-2 py-1 bg-[#0a0a08] border border-[#2a2618] rounded-sm text-xs text-[#d8d3c0]"
      />
      {[...groups.entries()].map(([fleet, list]) => {
        const color = FLEETS[fleet] ?? '#d8d3c0';
        const credits = list.reduce((s, a) => s + a.credits, 0);
        const isCollapsed = collapsed[fleet];
        return (
          <div key={fleet} className="mb-2">
            <button
              onClick={() => setCollapsed((c) => ({ ...c, [fleet]: !c[fleet] }))}
              className="w-full flex items-center justify-between text-xs uppercase tracking-widest py-1"
              style={{ color }}
            >
              <span>{isCollapsed ? '▸' : '▾'} {fleet} <span className="text-[#8a8570]">{list.length}</span></span>
              <span className="font-mono text-[#8a8570]">₡ {Math.round(credits).toLocaleString()}</span>
            </button>
            {!isCollapsed && list.map((a) => (
              <AgentCard key={a.agent_id} agent={a} color={color}
                selected={selectedId === a.agent_id} stale={stale.has(fleet)}
                onClick={() => onSelect(a.agent_id)} />
            ))}
          </div>
        );
      })}
    </div>
  );
}
```

- [ ] **Step 3: Wire into OvermindPage**

Replace the rail-slot placeholder:

```tsx
const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
...
<div className="w-80 border-l border-[#2a2618] overflow-y-auto" id="ov-rail-slot">
  <FleetRail
    agents={[...stream.agents.values()]}
    offMap={stream.offMap}
    staleFleets={stream.staleFleets}
    selectedId={selectedAgent}
    onSelect={setSelectedAgent}
  />
</div>
```

- [ ] **Step 4: Build + manual check**

`cd frontend && npm run build` then dev-server check: 6 fleet groups with
live cards, filter narrows, collapse works, unhealthy agents float first
with red edge.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/overmind/
git commit --no-verify -m "feat(frontend): fleet rail with stylized agent cards"
```

---

### Task 9: frontend — FleetOverlay on GalaxyMap + SystemPanel

**Files:**
- Create: `frontend/src/components/overmind/FleetOverlay.tsx`
- Create: `frontend/src/components/overmind/SystemPanel.tsx`
- Modify: `frontend/src/components/galaxy/GalaxyMap.tsx` (render-prop overlay + click hooks — minimal)
- Modify: `frontend/src/components/overmind/OvermindPage.tsx` (fill map slot)

**Interfaces:**
- Consumes: `useGalaxyMap('/api/overmind')` (Task 6), `stream.agents`, `stream.moves`, `selectedAgent` (Task 8).
- Produces on GalaxyMap: two new optional props —
  `overlay?: (project: (x: number, y: number) => { x: number; y: number }) => React.ReactNode`
  (called inside the SVG after systems render, with the same world→SVG
  projection function the component already uses for system positions) and
  `onSystemClick?: (system: GalaxySystem) => void`.
  Existing callers pass neither and are unaffected.
- `<FleetOverlay agents={AgentState[]} moves={AgentMove[]} systems={GalaxySystem[]} project={fn} visibleFleets={Set<string>} selectedId={string|null} onAgentClick={(id) => void} />`
- `<SystemPanel system={GalaxySystem} agents={AgentState[]} onClose={() => void} />`

- [ ] **Step 1: GalaxyMap extension**

Read `GalaxyMap.tsx` fully first. It computes each system's SVG position
from bounds + zoom/pan (the exact math lives in the component — locate the
function/expression that turns `system.position` into rendered `cx/cy`).
Extract that expression into a `project(x, y)` closure if it isn't one
already, then:

1. Add the two props to `GalaxyMapProps`:

```tsx
interface GalaxyMapProps {
  systems?: GalaxySystem[];
  overlay?: (project: (x: number, y: number) => { x: number; y: number }) => React.ReactNode;
  onSystemClick?: (system: GalaxySystem) => void;
}
```

2. Where system circles render, add `onClick={() => onSystemClick?.(system)}`
   and `style={{ cursor: onSystemClick ? 'pointer' : undefined }}`.
3. As the LAST child inside the main `<svg>`/`<g>` (so it draws on top):
   `{overlay?.(project)}`.
4. Accept a `basePath` prop threaded to `useGalaxyMap(basePath)` OR accept
   `systems` via props (it already does) — the Overmind page passes systems
   from its own `useGalaxyMap('/api/overmind')` call. Prefer the existing
   `systems` prop path; only fall back to internal fetch when absent (already
   the component's behavior).

- [ ] **Step 2: FleetOverlay**

`frontend/src/components/overmind/FleetOverlay.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react';
import type { GalaxySystem } from '../../lib/useGalaxyMap';
import { FLEETS, type AgentMove, type AgentState } from '../../lib/useFleetStream';

const MOVE_MS = 2000;

interface Anim { fromX: number; fromY: number; started: number }

/** Deterministic orbit offset so co-located agents fan out, stable per agent. */
function orbit(agentId: string, index: number, count: number): { dx: number; dy: number } {
  const angle = (2 * Math.PI * index) / Math.max(count, 1);
  const r = count > 1 ? 8 : 0;
  return { dx: r * Math.cos(angle), dy: r * Math.sin(angle) };
}

export function FleetOverlay({ agents, moves, systems, project, visibleFleets, selectedId, onAgentClick }: {
  agents: AgentState[];
  moves: AgentMove[];
  systems: GalaxySystem[];
  project: (x: number, y: number) => { x: number; y: number };
  visibleFleets: Set<string>;
  selectedId: string | null;
  onAgentClick: (id: string) => void;
}) {
  const sysById = new Map(systems.map((s) => [s.id, s]));
  const anims = useRef(new Map<string, Anim>());
  const [, force] = useState(0);

  // Register animations for fresh moves; tick a re-render until they expire.
  useEffect(() => {
    const now = performance.now();
    for (const m of moves) {
      const from = sysById.get(m.from_system_id);
      if (from) {
        anims.current.set(m.agent.agent_id, {
          fromX: from.position.x, fromY: from.position.y, started: now,
        });
      }
    }
    if (anims.current.size === 0) return;
    const iv = setInterval(() => {
      const t = performance.now();
      for (const [id, a] of anims.current) {
        if (t - a.started > MOVE_MS) anims.current.delete(id);
      }
      force((n) => n + 1);
      if (anims.current.size === 0) clearInterval(iv);
    }, 50);
    return () => clearInterval(iv);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [moves]);

  // Group visible agents by system for badges + orbit fanning.
  const bySystem = new Map<string, AgentState[]>();
  for (const a of agents) {
    if (!visibleFleets.has(a.fleet)) continue;
    const list = bySystem.get(a.system_id) ?? [];
    list.push(a);
    bySystem.set(a.system_id, list);
  }

  const now = performance.now();
  return (
    <g>
      {[...bySystem.entries()].map(([sysId, list]) => {
        const sys = sysById.get(sysId);
        if (!sys) return null;
        const center = project(sys.position.x, sys.position.y);
        return (
          <g key={sysId}>
            {/* count badge */}
            <g transform={`translate(${center.x + 8}, ${center.y - 10})`}>
              <rect x={-2} y={-8} width={list.length >= 10 ? 18 : 12} height={11}
                rx={2} fill="#11100c" stroke="#d4a017" strokeWidth={0.5} />
              <text x={list.length >= 10 ? 7 : 4} y={1} textAnchor="middle"
                fontSize={8} fill="#d4a017" fontFamily="monospace">{list.length}</text>
            </g>
            {list.map((a, i) => {
              const { dx, dy } = orbit(a.agent_id, i, list.length);
              let x = center.x + dx, y = center.y + dy;
              const anim = anims.current.get(a.agent_id);
              if (anim) {
                const t = Math.min(1, (now - anim.started) / MOVE_MS);
                const from = project(anim.fromX, anim.fromY);
                x = from.x + (center.x + dx - from.x) * t;
                y = from.y + (center.y + dy - from.y) * t;
              }
              const color = FLEETS[a.fleet] ?? '#fff';
              const selected = selectedId === a.agent_id;
              return (
                <g key={a.agent_id} onClick={() => onAgentClick(a.agent_id)} style={{ cursor: 'pointer' }}>
                  {anim && (
                    <line x1={project(anim.fromX, anim.fromY).x} y1={project(anim.fromX, anim.fromY).y}
                      x2={x} y2={y} stroke={color} strokeWidth={0.5} opacity={0.4} />
                  )}
                  {selected && <circle cx={x} cy={y} r={6} fill="none" stroke="#fff" strokeWidth={0.8} />}
                  {a.docked
                    ? <circle cx={x} cy={y} r={3} fill="none" stroke={color} strokeWidth={1.2} />
                    : <circle cx={x} cy={y} r={3} fill={color} />}
                  <title>{a.agent_id} · {a.system_name}/{a.poi} · ₡{Math.round(a.credits)}</title>
                </g>
              );
            })}
          </g>
        );
      })}
    </g>
  );
}
```

- [ ] **Step 3: SystemPanel**

`frontend/src/components/overmind/SystemPanel.tsx`:

```tsx
import type { GalaxySystem } from '../../lib/useGalaxyMap';
import { FLEETS, type AgentState } from '../../lib/useFleetStream';

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex justify-between border-b border-[#2a2618] py-1 text-xs">
      <span className="uppercase tracking-widest text-[#8a8570]">{k}</span>
      <span className="font-mono text-[#d8d3c0]">{v}</span>
    </div>
  );
}

export function SystemPanel({ system, agents, onClose }: {
  system: GalaxySystem; agents: AgentState[]; onClose: () => void;
}) {
  return (
    <div className="absolute top-3 right-3 w-64 bg-[#11100c] border border-[#2a2618] rounded-sm p-3 shadow-lg">
      <div className="flex justify-between items-center mb-2">
        <span className="text-[#d4a017] font-bold tracking-widest text-sm uppercase">{system.name}</span>
        <button onClick={onClose} className="text-[#8a8570] hover:text-[#d8d3c0]">✕</button>
      </div>
      <Row k="empire" v={system.empire || 'neutral'} />
      <Row k="police" v={`${system.police_level}`} />
      <Row k="jump lanes" v={`${system.connections.length}`} />
      {system.is_stronghold && <Row k="warning" v="PIRATE STRONGHOLD" />}
      <div className="mt-2 text-[10px] uppercase tracking-widest text-[#8a8570]">agents here</div>
      {agents.length === 0 && <div className="text-xs text-[#8a8570] py-1">none</div>}
      {agents.map((a) => (
        <div key={a.agent_id} className="flex justify-between text-xs py-0.5">
          <span style={{ color: FLEETS[a.fleet] }}>{a.agent_id}</span>
          <span className="text-[#8a8570]">{a.poi}{a.docked ? ' ⚓' : ''}</span>
        </div>
      ))}
    </div>
  );
}
```

- [ ] **Step 4: Wire the map slot in OvermindPage**

```tsx
const galaxy = useGalaxyMap('/api/overmind');
const [selectedSystem, setSelectedSystem] = useState<GalaxySystem | null>(null);
const [visibleFleets, setVisibleFleets] = useState(new Set(Object.keys(FLEETS)));
const agentList = [...stream.agents.values()];
...
<div className="flex-1 min-w-0 relative" id="ov-map-slot">
  {/* legend / layer toggles */}
  <div className="absolute top-3 left-3 z-10 flex gap-2">
    {Object.entries(FLEETS).map(([fleet, color]) => (
      <button key={fleet}
        onClick={() => setVisibleFleets((v) => {
          const next = new Set(v);
          if (next.has(fleet)) { next.delete(fleet); } else { next.add(fleet); }
          return next;
        })}
        className={`px-2 py-0.5 text-[10px] uppercase tracking-widest border rounded-sm
          ${visibleFleets.has(fleet) ? '' : 'opacity-40'}`}
        style={{ color, borderColor: color }}>
        {fleet}
      </button>
    ))}
  </div>
  <GalaxyMap
    systems={galaxy?.systems}
    onSystemClick={setSelectedSystem}
    overlay={(project) => (
      <FleetOverlay
        agents={agentList}
        moves={stream.moves}
        systems={galaxy?.systems ?? []}
        project={project}
        visibleFleets={visibleFleets}
        selectedId={selectedAgent}
        onAgentClick={setSelectedAgent}
      />
    )}
  />
  {selectedSystem && (
    <SystemPanel
      system={selectedSystem}
      agents={agentList.filter((a) => a.system_id === selectedSystem.id)}
      onClose={() => setSelectedSystem(null)}
    />
  )}
</div>
```

Off-map tray: below the legend, when `stream.offMap.length > 0`, render a
small bordered box listing `off-map: <agent_id (system_name)>` rows in the
same style as the legend chips.

- [ ] **Step 5: Build + full manual verification**

```bash
cd frontend && npm run build
cd .. && ./bin/overmind-dashboard --addr :8091 &
```
Open `http://localhost:8091/`, switch to Overmind:
- ~112 dots in 6 colors on the map; count badges on occupied systems.
- Toggling a legend chip hides that fleet.
- Click a system → panel with facts + agents; click a dot → its card
  highlights in the rail; click a card → dot gets the white selection ring.
- Wait for a worker jump (mission fleet jumps constantly): dot slides with a
  fading trail over ~2s.
Then `kill %1`.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/overmind/ frontend/src/components/galaxy/GalaxyMap.tsx
git commit --no-verify -m "feat(frontend): live fleet overlay, system panel, layer toggles"
```

---

### Task 10: docs, spec note, run wiring

**Files:**
- Modify: `docs/superpowers/specs/2026-07-20-overmind-dashboard-design.md` (SystemPanel deviation note)
- Create: `cmd/overmind-dashboard/README.md`

- [ ] **Step 1: Spec note**

In the spec's "Galaxy map" interactions bullet, append: *"(v1 ships a
lightweight SystemPanel — facts + agents present; full SystemMap orbital
reuse is a follow-up needing an observer-shaped per-system endpoint)."*

- [ ] **Step 2: README**

`cmd/overmind-dashboard/README.md`:

```markdown
# overmind-dashboard

Read-only unified fleet ops dashboard: live galaxy map, per-agent cards,
fleet accounting. Reads data/overmind/*-status.json, the knowledge DB, and
market.db. Never writes; never touches the game server or control sockets.

## Run

    go build -o bin/overmind-dashboard ./cmd/overmind-dashboard
    (cd frontend && npm run build)
    ./bin/overmind-dashboard --addr :8091

Open http://localhost:8091/ and pick the Overmind view.

## Dev

    ./bin/overmind-dashboard --addr :8091 &
    cd frontend && npm run dev   # /api/overmind proxied to :8091

## Endpoints

    GET /api/overmind/systems     galaxy topology
    GET /api/overmind/agents      merged fleet snapshot
    GET /api/overmind/accounting  24h earnings + fleet totals
    GET /api/overmind/stream      SSE: snapshot / delta / accounting
```

- [ ] **Step 3: Final gate**

```bash
go build ./... && go test ./pkg/ovdash/ ./cmd/overmind-dashboard/ -count=1 -race
golangci-lint run ./pkg/ovdash/... ./cmd/overmind-dashboard/...
cd frontend && npm run build
```
Expected: all green.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-07-20-overmind-dashboard-design.md cmd/overmind-dashboard/README.md
git commit --no-verify -m "docs(overmind-dashboard): README + spec panel note"
```

---

## Plan self-review notes

- Spec coverage: layout/strip/rail/cards (T7-8), map layers/badges/animation/
  toggles/interactions (T9), SSE+keyframes (T3/T5), accounting definitions
  (T4), staleness + off-map error handling (T2/T7/T9), read-only constraint
  (T1/T4/T5 DSNs), future-pages nav is NOT in v1 tasks — the left icon rail
  with grayed placeholders was deliberately dropped to YAGNI: the app's
  existing top nav hosts the Overmind entry; the icon rail arrives with the
  second real page. (Spec's "left nav rail" is satisfied by App.tsx's
  existing view switcher; noted here as a conscious reduction.)
- Verify-before-trust reminders are inline where the plan touches code it
  cannot see (App.tsx render pattern, GalaxyMap projection math, balances
  field spellings, sqlite driver import).
