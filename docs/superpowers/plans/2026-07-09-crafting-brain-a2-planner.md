# Crafting Brain A2 — The Planner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `pkg/craftbrain` + a `play_as build` command that turns a target (item/ship/facility) + quantity into a reviewable JSON step-DAG of mine/buy/haul/craft tasks, subtracting stock the fleet already holds.

**Architecture:** An `Engine` reads every fact through a narrow `Source` interface, so the arithmetic (ceil-per-run, demand aggregation, subtract-early, hand-vs-facility) is unit-tested against an in-memory fake with no DB and no network. The expander walks the recipe graph in **topological order** (not a tree walk) so shared intermediates aggregate demand before rounding. A SQL `Source` in `cmd/tools/play_as` wires the real databases.

**Tech Stack:** Go 1.24+, SQLite (`spacemolt-knowledge.db`, `market.db`), existing packages `pkg/knowledge`, `pkg/finditem`, `pkg/navigation`, `pkg/market`.

**Spec:** `docs/superpowers/specs/2026-07-09-crafting-brain-a2-planner-design.md`

## Global Constraints

- Go 1.24+. Use `for i := range n` integer ranging and `b.Loop()` in benchmarks.
- All new code must pass `golangci-lint run` with **zero new findings**.
- After each task: `go build ./... && go test ./...` before committing.
- Any sleep/pause must use a constant from `pkg/game/constants.go`. (This plan adds none.)
- **No skill logic anywhere.** Skills no longer gate crafting; `RequiredSkills` is vestigial.
- The engine **never opens a database** and **never writes** anything. Read-only by construction.
- Compiled binaries go in `bin/`, never the repo root.
- `station_id` values are **base IDs** (`bases.id`), not POI IDs (`pois.id`). Resolve via `bases.poi_id → pois.system_id`.

---

## File Structure

| File | Responsibility |
|---|---|
| `pkg/knowledge/public_facilities.go` (modify) | Expose `DetailsJSON` from `FacilitiesForRecipe` |
| `pkg/craftbrain/plan.go` (create) | Types: `Plan`, `Node`, `Facility`, `Holding`, `Options`, enums |
| `pkg/craftbrain/source.go` (create) | The `Source` interface |
| `pkg/craftbrain/graph.go` (create) | Producer index, reachable union graph, Kahn topo order + cycle break |
| `pkg/craftbrain/expand.go` (create) | `Engine.Plan`: demand aggregation, ceil runs, surplus pool |
| `pkg/craftbrain/inventory.go` (create) | Subtract-early, holder attribution, haul-leg emission |
| `pkg/craftbrain/site.go` (create) | Hand-vs-facility threshold, cheapest facility, buy fallback, BLOCKED |
| `pkg/craftbrain/format.go` (create) | Human rendering + footers |
| `pkg/craftbrain/fake_source_test.go` (create) | In-memory `Source` for all unit tests |
| `cmd/tools/play_as/source_sql.go` (create) | SQL `Source` over knowledge.db + market.db |
| `cmd/tools/play_as/build.go` (create) | The `build` REPL command |
| `cmd/tools/play_as/main.go` (modify) | Register `build` case + help line |
| `data/overmind/roles.yaml` (modify) | Scheduled `view_storage` standing command |

---

## Task 1: Expose facility production details from the KB

`FacilitiesForRecipe` does not return `details_json`, so the planner cannot read `ticks_per_run` / `output_per_run` — both required for the hand-vs-facility decision. The struct field exists but is documented "write-only; not populated by queries".

**Files:**
- Modify: `pkg/knowledge/public_facilities.go:20` (doc comment), `:67-75` (query + scan)
- Test: `pkg/knowledge/public_facilities_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `FacilitiesForRecipe(ctx, recipeID) ([]PublicFacility, error)` where each result now has `DetailsJSON` populated with the raw captured payload.

- [ ] **Step 1: Write the failing test**

Append to `pkg/knowledge/public_facilities_test.go`:

```go
// A2's planner reads ticks_per_run / output_per_run out of details_json to
// decide hand-craft vs facility, so the query must return it.
func TestFacilitiesForRecipe_ReturnsDetailsJSON(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	const details = `{"production":{"ticks_per_run":4.0,"output_per_run":2,"backlog_ticks":0}}`
	if err := kb.UpsertPublicFacilities(ctx, []PublicFacility{
		{StationID: "voss_redoubt_station", FacilityID: "sf-steel-1", RecipeID: "refine_steel",
			Category: "production", Level: 2, RentalFeePerRun: 35, LastSeenTick: 100,
			DetailsJSON: details},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := kb.FacilitiesForRecipe(ctx, "refine_steel")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 facility, got %d", len(got))
	}
	if got[0].DetailsJSON != details {
		t.Errorf("DetailsJSON = %q, want %q", got[0].DetailsJSON, details)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestFacilitiesForRecipe_ReturnsDetailsJSON -v`
Expected: FAIL — `DetailsJSON = "" , want "{\"production\":..."`

- [ ] **Step 3: Write minimal implementation**

In `pkg/craftbrain`-adjacent file `pkg/knowledge/public_facilities.go`, update the struct comment and the query.

Change the field comment (line ~20):

```go
	DetailsJSON string // raw captured payload, forward-compat; holds production.ticks_per_run etc.
```

Change the query in `FacilitiesForRecipe`:

```go
	rows, err := kb.db.QueryContext(ctx, `
		SELECT station_id, facility_id, recipe_id, facility_name, category, owner_faction, level, rental_fee_per_run, last_seen_tick, last_seen_utc, details_json
		FROM public_facilities
		WHERE recipe_id = ? AND public = 1 AND category = 'production'
		ORDER BY last_seen_tick DESC`, recipeID)
```

And the scan:

```go
		if err := rows.Scan(&f.StationID, &f.FacilityID, &f.RecipeID, &f.FacilityName, &f.Category,
			&f.OwnerFaction, &f.Level, &f.RentalFeePerRun, &f.LastSeenTick, &f.LastSeenUTC, &f.DetailsJSON); err != nil {
			return nil, err
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/knowledge/ -run 'PublicFacilit|FacilitiesForRecipe' -v`
Expected: PASS (all four tests, including the pre-existing `TestUpsertAndQueryPublicFacilities`)

- [ ] **Step 5: Commit**

```bash
go build ./... && golangci-lint run pkg/knowledge/...
git add pkg/knowledge/public_facilities.go pkg/knowledge/public_facilities_test.go
git commit -m "feat(kb): FacilitiesForRecipe returns details_json

A2's planner needs production.ticks_per_run / output_per_run to choose
hand-craft vs facility. The column was captured but never selected."
```

---

## Task 2: craftbrain plan types

**Files:**
- Create: `pkg/craftbrain/plan.go`
- Test: `pkg/craftbrain/plan_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Kind`, `Status`, `Node`, `Plan`, `Facility`, `Holding`, `Options`, `Coverage`, `DefaultOptions()`, `Facility.ParseProduction(detailsJSON string) error`.

- [ ] **Step 1: Write the failing test**

Create `pkg/craftbrain/plan_test.go`:

```go
package craftbrain

import (
	"testing"
	"time"
)

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()
	if o.MaxHandTicks != 360 {
		t.Errorf("MaxHandTicks = %v, want 360", o.MaxHandTicks)
	}
	if o.MaxStockAge != 24*time.Hour {
		t.Errorf("MaxStockAge = %v, want 24h", o.MaxStockAge)
	}
}

func TestFacilityParseProduction(t *testing.T) {
	var f Facility
	const details = `{"production":{"ticks_per_run":1.2173913,"output_per_run":2,"backlog_ticks":7,"rental_fee_per_run":350}}`
	if err := f.ParseProduction(details); err != nil {
		t.Fatalf("ParseProduction: %v", err)
	}
	if f.TicksPerRun != 1.2173913 {
		t.Errorf("TicksPerRun = %v, want 1.2173913", f.TicksPerRun)
	}
	if f.OutputPerRun != 2 {
		t.Errorf("OutputPerRun = %d, want 2", f.OutputPerRun)
	}
	if f.BacklogTicks != 7 {
		t.Errorf("BacklogTicks = %v, want 7", f.BacklogTicks)
	}
}

// A facility we have never observed carries no details; OutputPerRun must
// default to 1 rather than 0, or runs = ceil(n/0) divides by zero.
func TestFacilityParseProduction_EmptyDefaultsOutputToOne(t *testing.T) {
	var f Facility
	if err := f.ParseProduction(""); err != nil {
		t.Fatalf("ParseProduction(empty): %v", err)
	}
	if f.OutputPerRun != 1 {
		t.Errorf("OutputPerRun = %d, want 1", f.OutputPerRun)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/craftbrain/ -v`
Expected: FAIL — build error, `undefined: DefaultOptions`, `undefined: Facility`

- [ ] **Step 3: Write minimal implementation**

Create `pkg/craftbrain/plan.go`:

```go
// Package craftbrain plans the full recursive work needed to build a target
// (item, ship, or facility): what to mine, buy, haul, and craft, and where.
//
// The Engine reads every fact through a Source, so the arithmetic is testable
// without a database or a game connection. It is read-only: nothing here
// writes to the KB or talks to the server.
package craftbrain

import (
	"encoding/json"
	"time"
)

// Kind is the action a plan Node represents.
type Kind string

const (
	KindMine    Kind = "mine"
	KindBuy     Kind = "buy"
	KindHaul    Kind = "haul"
	KindCraft   Kind = "craft"
	KindBlocked Kind = "blocked"
)

// Status annotates a Node whose assumptions are weaker than they look.
type Status string

const (
	// StatusOK is a node with fresh data and a resolved site.
	StatusOK Status = "ok"
	// StatusBlocked is facility_only with no known facility and no market seller.
	StatusBlocked Status = "blocked"
	// StatusStale leaned on a holding or facility older than Options.MaxStockAge.
	StatusStale Status = "stale"
	// StatusSlow blew the MaxHandTicks budget but had no facility to escape to.
	StatusSlow Status = "slow"
)

// Holding is stock the fleet already has, attributed to whoever holds it.
// Executor B needs Holder+BaseID to tell an agent what to withdraw and where.
type Holding struct {
	Holder     string // agent_id, or "" for faction storage
	BaseID     string
	Qty        int
	CapturedAt time.Time
}

// Facility is one public production line, as sited by the planner.
//
// TicksPerRun is read per-instance and never derived: measured across the live
// catalog, crafting_time / ticks_per_run / 3^(level-1) takes at least five
// distinct values, so there is no formula.
type Facility struct {
	StationID       string
	FacilityID      string
	Level           int
	RentalFeePerRun int
	TicksPerRun     float64
	OutputPerRun    int
	BacklogTicks    float64
	LastSeenUTC     string
}

// ParseProduction fills the production fields from a captured details_json
// payload. An empty or unparseable payload leaves zero values, except
// OutputPerRun which defaults to 1 so run arithmetic never divides by zero.
func (f *Facility) ParseProduction(detailsJSON string) error {
	f.OutputPerRun = 1
	if detailsJSON == "" {
		return nil
	}
	var envelope struct {
		Production struct {
			TicksPerRun     float64 `json:"ticks_per_run"`
			OutputPerRun    int     `json:"output_per_run"`
			BacklogTicks    float64 `json:"backlog_ticks"`
			RentalFeePerRun int     `json:"rental_fee_per_run"`
		} `json:"production"`
	}
	if err := json.Unmarshal([]byte(detailsJSON), &envelope); err != nil {
		return err
	}
	p := envelope.Production
	f.TicksPerRun = p.TicksPerRun
	f.BacklogTicks = p.BacklogTicks
	if p.OutputPerRun > 0 {
		f.OutputPerRun = p.OutputPerRun
	}
	if f.RentalFeePerRun == 0 {
		f.RentalFeePerRun = p.RentalFeePerRun
	}
	return nil
}

// Node is one step in the plan DAG.
type Node struct {
	ID         string   `json:"id"`
	Kind       Kind     `json:"kind"`
	ItemID     string   `json:"item_id"`
	Qty        int      `json:"qty"`
	Runs       int      `json:"runs,omitempty"`
	RecipeID   string   `json:"recipe_id,omitempty"`
	StationID  string   `json:"station_id,omitempty"`
	FacilityID string   `json:"facility_id,omitempty"`
	FeeTotal   int      `json:"fee_total,omitempty"`
	TicksEst   float64  `json:"ticks_est,omitempty"`
	Holder     string   `json:"holder,omitempty"`
	FromBase   string   `json:"from_base,omitempty"` // haul source
	ToBase     string   `json:"to_base,omitempty"`   // haul destination
	Jumps      int      `json:"jumps,omitempty"`
	Status     Status   `json:"status"`
	Reason     string   `json:"reason,omitempty"` // why blocked/slow/stale
	DependsOn  []string `json:"depends_on,omitempty"`
}

// MineOrBuy carries both options for a raw leaf. The operator decides at
// review; the planner never chooses.
type MineOrBuy struct {
	Mineable    bool    `json:"mineable"`
	MarketPrice float64 `json:"market_price,omitempty"`
	MarketQty   float64 `json:"market_qty,omitempty"`
	BestStation string  `json:"best_station,omitempty"`
}

// Coverage reports how much of the facility catalog the plan could see, so a
// BLOCKED node reads as "unknown" rather than "impossible".
type Coverage struct {
	Stations            int `json:"stations"`
	FacilityOnlyCovered int `json:"facility_only_covered"`
	FacilityOnlyTotal   int `json:"facility_only_total"`
}

// Plan is the full step-DAG plus the honesty footers.
type Plan struct {
	Target         string               `json:"target"`
	Quantity       int                  `json:"quantity"`
	Nodes          []Node               `json:"nodes"`
	Leaves         map[string]MineOrBuy `json:"leaves,omitempty"`
	Surplus        map[string]int       `json:"surplus,omitempty"`
	Diagnostics    []string             `json:"diagnostics,omitempty"`
	TotalFee       int                  `json:"total_fee"`
	TotalTicks     float64              `json:"total_ticks"`
	TotalHaulJumps int                  `json:"total_haul_jumps"`
	Coverage       Coverage             `json:"coverage"`
}

// Options tunes the planner. Zero value is not valid; use DefaultOptions.
type Options struct {
	// MaxHandTicks is the running plan-time budget above which a craft
	// prefers a facility over hand-crafting. A tick is ~10s, so the default
	// 360 is about an hour.
	MaxHandTicks float64
	// MaxStockAge marks a holding stale (still subtracted, but flagged).
	MaxStockAge time.Duration
	// Now is injectable so tests are deterministic. Zero means time.Now().
	Now time.Time
}

// DefaultOptions returns the documented defaults.
func DefaultOptions() Options {
	return Options{MaxHandTicks: 360, MaxStockAge: 24 * time.Hour}
}

func (o Options) now() time.Time {
	if o.Now.IsZero() {
		return time.Now().UTC()
	}
	return o.Now
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/craftbrain/ -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
go build ./... && golangci-lint run pkg/craftbrain/...
git add pkg/craftbrain/plan.go pkg/craftbrain/plan_test.go
git commit -m "feat(craftbrain): plan DAG types"
```

---

## Task 3: The Source interface and its test fake

**Files:**
- Create: `pkg/craftbrain/source.go`, `pkg/craftbrain/fake_source_test.go`

**Interfaces:**
- Consumes: `Facility`, `Holding` (Task 2).
- Produces: `Source` interface; `Engine`; `New(src Source) *Engine`. Test-only: `fakeSource` with fields `recipes map[string]knowledge.RecipeDef`, `facilities map[string][]Facility`, `onHand map[string][]Holding`, `buyable map[string][]finditem.Result`, `systems map[string]string`, `jumps map[string]int`.

- [ ] **Step 1: Write the failing test**

Create `pkg/craftbrain/fake_source_test.go`:

```go
package craftbrain

import (
	"context"
	"time"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// fakeSource is an in-memory Source. Every unit test in this package builds
// one; no test opens a database or contacts the server.
type fakeSource struct {
	recipes    map[string]knowledge.RecipeDef
	facilities map[string][]Facility        // recipe_id -> sites
	onHand     map[string][]Holding         // item_id -> holdings
	buyable    map[string][]finditem.Result // item_id -> sellers
	systems    map[string]string            // station_id -> system_id
	jumps      map[string]int               // system_id -> jumps from origin
	coverage   Coverage
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		recipes:    map[string]knowledge.RecipeDef{},
		facilities: map[string][]Facility{},
		onHand:     map[string][]Holding{},
		buyable:    map[string][]finditem.Result{},
		systems:    map[string]string{},
		jumps:      map[string]int{},
	}
}

// addRecipe registers a recipe producing outQty of outItem per run from ins.
func (f *fakeSource) addRecipe(id, outItem string, outQty int, craftTime float64, facilityOnly bool, ins map[string]int) {
	inputs := make([]knowledge.RecipeIngredient, 0, len(ins))
	for item, q := range ins {
		inputs = append(inputs, knowledge.RecipeIngredient{ItemID: item, Quantity: q})
	}
	f.recipes[id] = knowledge.RecipeDef{
		ID:           id,
		Name:         id,
		CraftingTime: craftTime,
		FacilityOnly: facilityOnly,
		Inputs:       inputs,
		Outputs:      []knowledge.RecipeProduct{{ItemID: outItem, Quantity: outQty}},
	}
}

func (f *fakeSource) Recipes(context.Context) (map[string]knowledge.RecipeDef, error) {
	return f.recipes, nil
}

func (f *fakeSource) Facilities(_ context.Context, recipeID string) ([]Facility, error) {
	return f.facilities[recipeID], nil
}

func (f *fakeSource) OnHand(_ context.Context, itemID string) ([]Holding, error) {
	return f.onHand[itemID], nil
}

func (f *fakeSource) Buyable(_ context.Context, itemID string, _ int) ([]finditem.Result, error) {
	return f.buyable[itemID], nil
}

func (f *fakeSource) SystemOf(_ context.Context, stationID string) (string, error) {
	return f.systems[stationID], nil
}

func (f *fakeSource) Jumps(_ context.Context, _ string, toSystems []string) (map[string]int, error) {
	out := make(map[string]int, len(toSystems))
	for _, s := range toSystems {
		out[s] = f.jumps[s]
	}
	return out, nil
}

func (f *fakeSource) Coverage(context.Context) (Coverage, error) { return f.coverage, nil }

// fresh returns a CapturedAt inside the default MaxStockAge window.
func fresh() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) }

// testNow is the deterministic clock all tests pass via Options.Now.
func testNow() time.Time { return time.Date(2026, 7, 9, 13, 0, 0, 0, time.UTC) }

var _ Source = (*fakeSource)(nil)
```

Add to `pkg/craftbrain/plan_test.go`:

```go
func TestNewEngineRequiresSource(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New(nil) must panic")
		}
	}()
	_ = New(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/craftbrain/ -v`
Expected: FAIL — build error, `undefined: Source`, `undefined: New`

- [ ] **Step 3: Write minimal implementation**

Create `pkg/craftbrain/source.go`:

```go
package craftbrain

import (
	"context"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Source provides every fact the Engine needs. The real implementation lives
// in cmd/tools/play_as and wraps the knowledge DB + market collector; tests
// use an in-memory fake.
//
// Every method is read-only. The Engine never mutates game or KB state.
type Source interface {
	// Recipes returns the whole recipe graph keyed by recipe_id, with Inputs
	// and Outputs hydrated.
	Recipes(ctx context.Context) (map[string]knowledge.RecipeDef, error)

	// Facilities returns known public production sites for recipeID, with
	// production details already parsed. Empty means none are known — which
	// signifies "unknown", not "impossible".
	Facilities(ctx context.Context, recipeID string) ([]Facility, error)

	// OnHand returns stock of itemID the fleet holds, attributed by holder.
	OnHand(ctx context.Context, itemID string) ([]Holding, error)

	// Buyable returns market sellers of itemID with at least qty depth.
	Buyable(ctx context.Context, itemID string, qty int) ([]finditem.Result, error)

	// SystemOf resolves a station_id (a base_id) to its system_id. Returns
	// "" when unknown.
	SystemOf(ctx context.Context, stationID string) (string, error)

	// Jumps returns hop distance from fromSystem to each of toSystems.
	Jumps(ctx context.Context, fromSystem string, toSystems []string) (map[string]int, error)

	// Coverage reports catalog breadth for the plan footer.
	Coverage(ctx context.Context) (Coverage, error)
}

// Engine plans crafts from a Source.
type Engine struct {
	src Source
}

// New constructs an Engine. src must be non-nil.
func New(src Source) *Engine {
	if src == nil {
		panic("craftbrain.New: Source must be non-nil")
	}
	return &Engine{src: src}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/craftbrain/ -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
go build ./... && golangci-lint run pkg/craftbrain/...
git add pkg/craftbrain/source.go pkg/craftbrain/fake_source_test.go pkg/craftbrain/plan_test.go
git commit -m "feat(craftbrain): Source interface + in-memory test fake"
```

---

## Task 4: Producer index, reachable subgraph, topological order with cycle breaking

The expander is **not** a tree walk. It must process an item only after every consumer of that item is decided, so demand aggregates before rounding.

**Files:**
- Create: `pkg/craftbrain/graph.go`, `pkg/craftbrain/graph_test.go`

**Interfaces:**
- Consumes: `knowledge.RecipeDef` (from `Source.Recipes`).
- Produces:
  - `producerIndex(recipes map[string]knowledge.RecipeDef) map[string][]knowledge.RecipeDef` — item_id → recipes that output it, sorted by recipe ID for determinism.
  - `topoOrder(target string, prod map[string][]knowledge.RecipeDef) (order []string, dropped []string)` — items ordered consumers-before-producers; `dropped` names edges removed to break cycles, formatted `"A->B"`.

- [ ] **Step 1: Write the failing test**

Create `pkg/craftbrain/graph_test.go`:

```go
package craftbrain

import (
	"slices"
	"testing"
)

func TestProducerIndex(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("smelt_a", "alloy", 1, 1, false, map[string]int{"ore": 2})
	f.addRecipe("alt_a", "alloy", 3, 5, true, map[string]int{"ore": 4})
	prod := producerIndex(f.recipes)
	got := prod["alloy"]
	if len(got) != 2 {
		t.Fatalf("want 2 producers of alloy, got %d", len(got))
	}
	// Deterministic: sorted by recipe ID.
	if got[0].ID != "alt_a" || got[1].ID != "smelt_a" {
		t.Errorf("producers not sorted by id: %s, %s", got[0].ID, got[1].ID)
	}
	if len(prod["ore"]) != 0 {
		t.Errorf("ore is raw; want no producers, got %d", len(prod["ore"]))
	}
}

// widget <- 2x gadget <- 3x ore, and widget also takes ore directly.
// Order must place widget before gadget before ore.
func TestTopoOrder_ConsumersBeforeProducers(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_widget", "widget", 1, 1, false, map[string]int{"gadget": 2, "ore": 1})
	f.addRecipe("make_gadget", "gadget", 1, 1, false, map[string]int{"ore": 3})
	order, dropped := topoOrder("widget", producerIndex(f.recipes))
	if len(dropped) != 0 {
		t.Fatalf("unexpected dropped edges: %v", dropped)
	}
	iw := slices.Index(order, "widget")
	ig := slices.Index(order, "gadget")
	io := slices.Index(order, "ore")
	if iw < 0 || ig < 0 || io < 0 {
		t.Fatalf("missing items in order %v", order)
	}
	if !(iw < ig && ig < io) {
		t.Errorf("want widget < gadget < ore, got order %v", order)
	}
}

// Only items reachable from the target appear.
func TestTopoOrder_OnlyReachable(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_widget", "widget", 1, 1, false, map[string]int{"ore": 1})
	f.addRecipe("make_unrelated", "unrelated", 1, 1, false, map[string]int{"junk": 1})
	order, _ := topoOrder("widget", producerIndex(f.recipes))
	if slices.Contains(order, "unrelated") || slices.Contains(order, "junk") {
		t.Errorf("unreachable items leaked into order: %v", order)
	}
}

// refine: scrap -> plate ; recycle: plate -> scrap. Must terminate and record
// the broken edge rather than hang or fail.
func TestTopoOrder_BreaksCycle(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("refine", "plate", 1, 1, false, map[string]int{"scrap": 2})
	f.addRecipe("recycle", "scrap", 1, 1, false, map[string]int{"plate": 1})
	order, dropped := topoOrder("plate", producerIndex(f.recipes))
	if len(dropped) == 0 {
		t.Fatal("expected a dropped edge to break the cycle")
	}
	if !slices.Contains(order, "plate") || !slices.Contains(order, "scrap") {
		t.Errorf("both items must still appear, got %v", order)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/craftbrain/ -run 'ProducerIndex|TopoOrder' -v`
Expected: FAIL — build error, `undefined: producerIndex`, `undefined: topoOrder`

- [ ] **Step 3: Write minimal implementation**

Create `pkg/craftbrain/graph.go`:

```go
package craftbrain

import (
	"fmt"
	"sort"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// producerIndex maps item_id -> every recipe that outputs it, sorted by recipe
// ID so planning is deterministic. Items with no entry are raw.
func producerIndex(recipes map[string]knowledge.RecipeDef) map[string][]knowledge.RecipeDef {
	prod := map[string][]knowledge.RecipeDef{}
	for _, r := range recipes {
		for _, out := range r.Outputs {
			prod[out.ItemID] = append(prod[out.ItemID], r)
		}
	}
	for item := range prod {
		rs := prod[item]
		sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
		prod[item] = rs
	}
	return prod
}

// childrenOf returns every input item of every candidate recipe for item, so
// the union graph is independent of which recipe we eventually choose.
func childrenOf(item string, prod map[string][]knowledge.RecipeDef) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range prod[item] {
		for _, in := range r.Inputs {
			if in.ItemID == item || seen[in.ItemID] {
				continue // self-loop contributes nothing
			}
			seen[in.ItemID] = true
			out = append(out, in.ItemID)
		}
	}
	sort.Strings(out)
	return out
}

// topoOrder returns the items reachable from target, ordered so that every
// consumer of an item precedes that item. Demand can therefore be fully
// aggregated at an item before its runs are rounded.
//
// Cycles (refine A->B, recycle B->A) are broken by dropping the edge that
// closes them; each drop is reported as "parent->child".
func topoOrder(target string, prod map[string][]knowledge.RecipeDef) (order []string, dropped []string) {
	// Collect the reachable union subgraph: edges parent -> input.
	edges := map[string][]string{}
	indeg := map[string]int{target: 0}
	queue := []string{target}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if _, done := edges[item]; done {
			continue
		}
		kids := childrenOf(item, prod)
		edges[item] = kids
		for _, k := range kids {
			if _, seen := indeg[k]; !seen {
				indeg[k] = 0
				queue = append(queue, k)
			}
			indeg[k]++
		}
	}

	// Kahn: repeatedly emit an item whose consumers are all emitted.
	var ready []string
	for item, d := range indeg {
		if d == 0 {
			ready = append(ready, item)
		}
	}
	sort.Strings(ready)

	emitted := map[string]bool{}
	for len(emitted) < len(indeg) {
		if len(ready) == 0 {
			// Residual cycle. Break at the lowest-indegree unemitted item,
			// dropping its remaining incoming edges.
			best, bestDeg := "", 1<<30
			for item, d := range indeg {
				if emitted[item] {
					continue
				}
				if d < bestDeg || (d == bestDeg && item < best) {
					best, bestDeg = item, d
				}
			}
			if best == "" {
				break
			}
			for parent, kids := range edges {
				if emitted[parent] {
					continue
				}
				for _, k := range kids {
					if k == best {
						dropped = append(dropped, fmt.Sprintf("%s->%s", parent, best))
					}
				}
			}
			indeg[best] = 0
			ready = append(ready, best)
			continue
		}

		sort.Strings(ready)
		item := ready[0]
		ready = ready[1:]
		if emitted[item] {
			continue
		}
		emitted[item] = true
		order = append(order, item)
		for _, k := range edges[item] {
			if emitted[k] {
				continue
			}
			indeg[k]--
			if indeg[k] <= 0 {
				ready = append(ready, k)
			}
		}
	}
	sort.Strings(dropped)
	return order, dropped
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/craftbrain/ -run 'ProducerIndex|TopoOrder' -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
go build ./... && golangci-lint run pkg/craftbrain/...
git add pkg/craftbrain/graph.go pkg/craftbrain/graph_test.go
git commit -m "feat(craftbrain): topological recipe graph with cycle breaking

Consumers precede producers so shared intermediates aggregate demand
before ceil-rounding runs. Cycles drop the closing edge, not the plan."
```

---

## Task 5: Inventory — subtract early, attribute holders, emit haul legs

**Files:**
- Create: `pkg/craftbrain/inventory.go`, `pkg/craftbrain/inventory_test.go`

**Interfaces:**
- Consumes: `Holding`, `Node`, `Options` (Task 2); `Source.OnHand`, `Source.SystemOf`, `Source.Jumps` (Task 3).
- Produces: `(e *Engine) consumeOnHand(ctx, itemID string, need int, destBase string, opts Options, idGen *idGen) (remaining int, nodes []Node, err error)` and `type idGen struct{ n int }` with `next(prefix string) string`.

`consumeOnHand` draws stock nearest-first (fewest jumps to `destBase`), emits one `KindHaul` node per remote base it draws from, and marks a node `StatusStale` when the holding is older than `opts.MaxStockAge`. Stock already at `destBase` yields no node.

- [ ] **Step 1: Write the failing test**

Create `pkg/craftbrain/inventory_test.go`:

```go
package craftbrain

import (
	"context"
	"testing"
	"time"
)

func TestConsumeOnHand_LocalStockEmitsNoHaul(t *testing.T) {
	f := newFakeSource()
	f.onHand["ore"] = []Holding{{Holder: "hauler-3", BaseID: "hub", Qty: 10, CapturedAt: fresh()}}
	f.systems["hub"] = "sol"
	e := New(f)
	opts := DefaultOptions()
	opts.Now = testNow()

	rem, nodes, err := e.consumeOnHand(context.Background(), "ore", 4, "hub", opts, &idGen{})
	if err != nil {
		t.Fatal(err)
	}
	if rem != 0 {
		t.Errorf("remaining = %d, want 0", rem)
	}
	if len(nodes) != 0 {
		t.Errorf("local stock must emit no haul node, got %d", len(nodes))
	}
}

func TestConsumeOnHand_SplitAcrossBasesEmitsTwoHauls(t *testing.T) {
	f := newFakeSource()
	f.onHand["ore"] = []Holding{
		{Holder: "trader-1", BaseID: "far", Qty: 5, CapturedAt: fresh()},
		{Holder: "trader-2", BaseID: "near", Qty: 3, CapturedAt: fresh()},
	}
	f.systems["dest"], f.systems["near"], f.systems["far"] = "sol", "alpha", "beta"
	f.jumps["alpha"], f.jumps["beta"] = 1, 7
	e := New(f)
	opts := DefaultOptions()
	opts.Now = testNow()

	rem, nodes, err := e.consumeOnHand(context.Background(), "ore", 8, "dest", opts, &idGen{})
	if err != nil {
		t.Fatal(err)
	}
	if rem != 0 {
		t.Errorf("remaining = %d, want 0", rem)
	}
	if len(nodes) != 2 {
		t.Fatalf("want 2 haul nodes, got %d", len(nodes))
	}
	// Nearest first: "near" (1 jump) drains before "far" (7 jumps).
	if nodes[0].FromBase != "near" || nodes[0].Qty != 3 {
		t.Errorf("first haul = %s x%d, want near x3", nodes[0].FromBase, nodes[0].Qty)
	}
	if nodes[1].FromBase != "far" || nodes[1].Qty != 5 {
		t.Errorf("second haul = %s x%d, want far x5", nodes[1].FromBase, nodes[1].Qty)
	}
	if nodes[0].Holder != "trader-2" {
		t.Errorf("holder attribution lost: %q", nodes[0].Holder)
	}
	if nodes[1].Jumps != 7 {
		t.Errorf("jumps = %d, want 7", nodes[1].Jumps)
	}
}

func TestConsumeOnHand_PartialLeavesRemainder(t *testing.T) {
	f := newFakeSource()
	f.onHand["ore"] = []Holding{{Holder: "a", BaseID: "hub", Qty: 2, CapturedAt: fresh()}}
	f.systems["hub"] = "sol"
	e := New(f)
	opts := DefaultOptions()
	opts.Now = testNow()

	rem, _, err := e.consumeOnHand(context.Background(), "ore", 9, "hub", opts, &idGen{})
	if err != nil {
		t.Fatal(err)
	}
	if rem != 7 {
		t.Errorf("remaining = %d, want 7", rem)
	}
}

// Stale stock is still subtracted, but the haul node says so.
func TestConsumeOnHand_StaleHoldingFlagged(t *testing.T) {
	f := newFakeSource()
	old := testNow().Add(-48 * time.Hour)
	f.onHand["ore"] = []Holding{{Holder: "a", BaseID: "far", Qty: 5, CapturedAt: old}}
	f.systems["dest"], f.systems["far"] = "sol", "beta"
	f.jumps["beta"] = 2
	e := New(f)
	opts := DefaultOptions()
	opts.Now = testNow()

	_, nodes, err := e.consumeOnHand(context.Background(), "ore", 5, "dest", opts, &idGen{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	if nodes[0].Status != StatusStale {
		t.Errorf("status = %q, want %q", nodes[0].Status, StatusStale)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/craftbrain/ -run ConsumeOnHand -v`
Expected: FAIL — build error, `e.consumeOnHand undefined`, `undefined: idGen`

- [ ] **Step 3: Write minimal implementation**

Create `pkg/craftbrain/inventory.go`:

```go
package craftbrain

import (
	"context"
	"fmt"
	"sort"

	"github.com/rsned/spacemolt/pkg/navigation"
)

// idGen hands out stable, readable node IDs: "haul-1", "craft-2", ...
type idGen struct{ n int }

func (g *idGen) next(prefix string) string {
	g.n++
	return fmt.Sprintf("%s-%d", prefix, g.n)
}

// consumeOnHand subtracts stock the fleet already holds from need, drawing
// nearest-first so the plan hauls the shortest distance. Stock already at
// destBase costs nothing and emits no node; remote stock emits one haul node
// per source base, attributed to its holder so Executor B knows whom to ask.
//
// Holdings older than opts.MaxStockAge are still consumed — refusing them
// would overstate the work — but their node is tagged StatusStale.
func (e *Engine) consumeOnHand(ctx context.Context, itemID string, need int, destBase string, opts Options, ids *idGen) (int, []Node, error) {
	if need <= 0 {
		return 0, nil, nil
	}
	holdings, err := e.src.OnHand(ctx, itemID)
	if err != nil {
		return need, nil, fmt.Errorf("on-hand %s: %w", itemID, err)
	}
	if len(holdings) == 0 {
		return need, nil, nil
	}

	destSys, err := e.src.SystemOf(ctx, destBase)
	if err != nil {
		return need, nil, fmt.Errorf("system of %s: %w", destBase, err)
	}

	// Resolve each holding's system so we can rank by jumps.
	type cand struct {
		h     Holding
		jumps int
	}
	systems := make([]string, 0, len(holdings))
	sysOf := make(map[string]string, len(holdings))
	for _, h := range holdings {
		s, err := e.src.SystemOf(ctx, h.BaseID)
		if err != nil {
			return need, nil, fmt.Errorf("system of %s: %w", h.BaseID, err)
		}
		sysOf[h.BaseID] = s
		if h.BaseID != destBase {
			systems = append(systems, s)
		}
	}
	dist := map[string]int{}
	if len(systems) > 0 {
		dist, err = e.src.Jumps(ctx, destSys, systems)
		if err != nil {
			return need, nil, fmt.Errorf("jumps from %s: %w", destSys, err)
		}
	}

	cands := make([]cand, 0, len(holdings))
	for _, h := range holdings {
		j := 0
		if h.BaseID != destBase {
			var ok bool
			if j, ok = dist[sysOf[h.BaseID]]; !ok {
				j = navigation.RouteInf
			}
		}
		cands = append(cands, cand{h: h, jumps: j})
	}
	// Nearest first; ties broken by base id for determinism.
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].jumps != cands[j].jumps {
			return cands[i].jumps < cands[j].jumps
		}
		return cands[i].h.BaseID < cands[j].h.BaseID
	})

	now := opts.now()
	var nodes []Node
	for _, c := range cands {
		if need == 0 {
			break
		}
		take := min(c.h.Qty, need)
		if take <= 0 {
			continue
		}
		need -= take
		if c.h.BaseID == destBase {
			continue // already where it is needed
		}
		status := StatusOK
		reason := ""
		if now.Sub(c.h.CapturedAt) > opts.MaxStockAge {
			status = StatusStale
			reason = fmt.Sprintf("stock last seen %s", c.h.CapturedAt.Format("2006-01-02T15:04Z"))
		}
		nodes = append(nodes, Node{
			ID:       ids.next("haul"),
			Kind:     KindHaul,
			ItemID:   itemID,
			Qty:      take,
			Holder:   c.h.Holder,
			FromBase: c.h.BaseID,
			ToBase:   destBase,
			Jumps:    c.jumps,
			Status:   status,
			Reason:   reason,
		})
	}
	return need, nodes, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/craftbrain/ -run ConsumeOnHand -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
go build ./... && golangci-lint run pkg/craftbrain/...
git add pkg/craftbrain/inventory.go pkg/craftbrain/inventory_test.go
git commit -m "feat(craftbrain): subtract-early inventory with haul-leg emission"
```

---

## Task 6: Siting — hand vs facility, buy fallback, BLOCKED

**Files:**
- Create: `pkg/craftbrain/site.go`, `pkg/craftbrain/site_test.go`

**Interfaces:**
- Consumes: `Facility`, `Options`, `Status` (Task 2); `Source.Facilities`, `Source.Buyable` (Task 3).
- Produces:
  - `type siting struct { recipe knowledge.RecipeDef; facility *Facility; runs int; feeTotal int; ticks float64; status Status; reason string }`
  - `(e *Engine) chooseSiting(ctx, item string, demand int, cands []knowledge.RecipeDef, planTicks float64, opts Options) (siting, bool, error)` — the bool is false when no recipe can be sited (caller then tries buy, then BLOCKED).
  - `cheapestFacility(fs []Facility, runs int) *Facility`

Rules, in order:
1. If a non-`facility_only` candidate exists: use it, **unless** `runs*craftingTime + planTicks > opts.MaxHandTicks` **and** some candidate has a known facility → then take the cheapest facility. If the budget is blown but no facility is known, hand-craft anyway with `StatusSlow`.
2. Otherwise (all candidates `facility_only`): cheapest known facility. If none, report not-sited so the caller can try `Buyable`, then BLOCKED.

Cheapest = lowest `RentalFeePerRun * runs`; tie-break lower `TicksPerRun`; then `FacilityID`.

- [ ] **Step 1: Write the failing test**

Create `pkg/craftbrain/site_test.go`:

```go
package craftbrain

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestChooseSiting_PrefersHandUnderBudget(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("smelt", "alloy", 1, 2.0, false, map[string]int{"ore": 1})
	f.facilities["smelt"] = []Facility{{StationID: "hub", FacilityID: "f1", RentalFeePerRun: 10, TicksPerRun: 0.5, OutputPerRun: 1}}
	e := New(f)
	opts := DefaultOptions()

	s, ok, err := e.chooseSiting(context.Background(), "alloy", 10, []knowledge.RecipeDef{f.recipes["smelt"]}, 0, opts)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if s.facility != nil {
		t.Errorf("want hand-craft under budget, got facility %s", s.facility.FacilityID)
	}
	if s.runs != 10 {
		t.Errorf("runs = %d, want 10", s.runs)
	}
	if s.ticks != 20 {
		t.Errorf("ticks = %v, want 20 (10 runs x 2.0)", s.ticks)
	}
	if s.status != StatusOK {
		t.Errorf("status = %q, want ok", s.status)
	}
}

func TestChooseSiting_SwitchesToFacilityOverBudget(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("smelt", "alloy", 1, 2.0, false, map[string]int{"ore": 1})
	f.facilities["smelt"] = []Facility{
		{StationID: "hub", FacilityID: "pricey", RentalFeePerRun: 100, TicksPerRun: 0.1, OutputPerRun: 1},
		{StationID: "far", FacilityID: "cheap", RentalFeePerRun: 5, TicksPerRun: 0.5, OutputPerRun: 1},
	}
	e := New(f)
	opts := DefaultOptions()
	opts.MaxHandTicks = 10 // 500 units x 2.0 ticks = 1000, way over

	s, ok, err := e.chooseSiting(context.Background(), "alloy", 500, []knowledge.RecipeDef{f.recipes["smelt"]}, 0, opts)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if s.facility == nil {
		t.Fatal("want a facility over budget")
	}
	if s.facility.FacilityID != "cheap" {
		t.Errorf("want cheapest by fee*runs, got %s", s.facility.FacilityID)
	}
	if s.feeTotal != 5*500 {
		t.Errorf("feeTotal = %d, want 2500", s.feeTotal)
	}
}

// Budget blown but nothing to escape to: hand-craft anyway, tagged slow.
func TestChooseSiting_OverBudgetNoFacilityIsSlowNotBlocked(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("smelt", "alloy", 1, 2.0, false, map[string]int{"ore": 1})
	e := New(f)
	opts := DefaultOptions()
	opts.MaxHandTicks = 1

	s, ok, err := e.chooseSiting(context.Background(), "alloy", 500, []knowledge.RecipeDef{f.recipes["smelt"]}, 0, opts)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if s.facility != nil {
		t.Error("no facility exists; must hand-craft")
	}
	if s.status != StatusSlow {
		t.Errorf("status = %q, want %q", s.status, StatusSlow)
	}
}

// facility_only with no known facility: not sited, so the caller falls back.
func TestChooseSiting_FacilityOnlyNoFacilityNotSited(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("grow", "matrix", 1, 21, true, map[string]int{"gas": 1})
	e := New(f)

	_, ok, err := e.chooseSiting(context.Background(), "matrix", 3, []knowledge.RecipeDef{f.recipes["grow"]}, 0, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("facility_only with no facility must not be sited")
	}
}

// runs = ceil(demand / output_per_run) at the facility's own output rate.
func TestChooseSiting_FacilityRunsUseFacilityOutput(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("press", "plate", 1, 100, true, map[string]int{"ore": 1})
	f.facilities["press"] = []Facility{{StationID: "hub", FacilityID: "f1", RentalFeePerRun: 2, TicksPerRun: 1, OutputPerRun: 4}}
	e := New(f)

	s, ok, err := e.chooseSiting(context.Background(), "plate", 9, []knowledge.RecipeDef{f.recipes["press"]}, 0, DefaultOptions())
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if s.runs != 3 { // ceil(9/4)
		t.Errorf("runs = %d, want 3", s.runs)
	}
}

func TestCheapestFacility_TieBreakOnTicksThenID(t *testing.T) {
	fs := []Facility{
		{FacilityID: "b", RentalFeePerRun: 10, TicksPerRun: 2, OutputPerRun: 1},
		{FacilityID: "a", RentalFeePerRun: 10, TicksPerRun: 1, OutputPerRun: 1},
		{FacilityID: "c", RentalFeePerRun: 10, TicksPerRun: 1, OutputPerRun: 1},
	}
	got := cheapestFacility(fs, 3)
	if got.FacilityID != "a" {
		t.Errorf("got %s, want a (lowest ticks, then id)", got.FacilityID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/craftbrain/ -run 'ChooseSiting|CheapestFacility' -v`
Expected: FAIL — build error, `e.chooseSiting undefined`, `undefined: cheapestFacility`

- [ ] **Step 3: Write minimal implementation**

Create `pkg/craftbrain/site.go`:

```go
package craftbrain

import (
	"context"
	"sort"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// siting is the decision for one craft node: which recipe, where, how many
// runs, what it costs in fees and time.
type siting struct {
	recipe   knowledge.RecipeDef
	facility *Facility // nil = hand-crafted at any docked station
	runs     int
	feeTotal int
	ticks    float64
	status   Status
	reason   string
}

// ceilDiv returns ceil(a/b) for positive b. Integer runs are why
// bill_of_materials' per-unit table can never be exact.
func ceilDiv(a, b int) int {
	if b <= 0 {
		b = 1
	}
	return (a + b - 1) / b
}

// outputPerRun returns how many units of item one run of r yields.
func outputPerRun(r knowledge.RecipeDef, item string) int {
	for _, o := range r.Outputs {
		if o.ItemID == item && o.Quantity > 0 {
			return o.Quantity
		}
	}
	return 1
}

// cheapestFacility picks the lowest total rent for runs, tie-breaking on
// faster ticks_per_run (a higher-level line) and then facility_id.
//
// NOTE: this scores fee only, not haul cost. A cheap-but-distant facility can
// therefore add a long haul leg; the plan's total-haul-jumps footer is how the
// operator catches that at review.
func cheapestFacility(fs []Facility, runs int) *Facility {
	if len(fs) == 0 {
		return nil
	}
	sorted := make([]Facility, len(fs))
	copy(sorted, fs)
	sort.Slice(sorted, func(i, j int) bool {
		fi, fj := sorted[i].RentalFeePerRun*runs, sorted[j].RentalFeePerRun*runs
		if fi != fj {
			return fi < fj
		}
		if sorted[i].TicksPerRun != sorted[j].TicksPerRun {
			return sorted[i].TicksPerRun < sorted[j].TicksPerRun
		}
		return sorted[i].FacilityID < sorted[j].FacilityID
	})
	best := sorted[0]
	return &best
}

// chooseSiting decides how to make demand units of item.
//
// Skills play no part: the server no longer gates crafting on them. The trade
// is fee versus time — hand-crafting is free but slow, a facility charges rent
// and runs several times faster, tripling again with each level.
func (e *Engine) chooseSiting(ctx context.Context, item string, demand int, cands []knowledge.RecipeDef, planTicks float64, opts Options) (siting, bool, error) {
	if len(cands) == 0 {
		return siting{}, false, nil
	}

	// Gather facilities per candidate once.
	facs := make(map[string][]Facility, len(cands))
	for _, r := range cands {
		fs, err := e.src.Facilities(ctx, r.ID)
		if err != nil {
			return siting{}, false, err
		}
		facs[r.ID] = fs
	}

	// Best hand candidate: lowest crafting_time, then recipe id.
	var hand *knowledge.RecipeDef
	for i := range cands {
		if cands[i].FacilityOnly {
			continue
		}
		if hand == nil || cands[i].CraftingTime < hand.CraftingTime ||
			(cands[i].CraftingTime == hand.CraftingTime && cands[i].ID < hand.ID) {
			hand = &cands[i]
		}
	}

	// Best facility candidate across all recipes.
	bestSiting := siting{}
	haveFacility := false
	for _, r := range cands {
		fs := facs[r.ID]
		if len(fs) == 0 {
			continue
		}
		// Runs depend on the facility's own output_per_run, not the recipe's.
		probe := cheapestFacility(fs, 1)
		runs := ceilDiv(demand, probe.OutputPerRun)
		f := cheapestFacility(fs, runs)
		runs = ceilDiv(demand, f.OutputPerRun)
		cand := siting{
			recipe:   r,
			facility: f,
			runs:     runs,
			feeTotal: f.RentalFeePerRun * runs,
			ticks:    float64(runs)*f.TicksPerRun + f.BacklogTicks,
			status:   StatusOK,
		}
		if !haveFacility || cand.feeTotal < bestSiting.feeTotal ||
			(cand.feeTotal == bestSiting.feeTotal && cand.facility.TicksPerRun < bestSiting.facility.TicksPerRun) {
			bestSiting, haveFacility = cand, true
		}
	}

	if hand != nil {
		runs := ceilDiv(demand, outputPerRun(*hand, item))
		handTicks := float64(runs) * hand.CraftingTime
		overBudget := handTicks+planTicks > opts.MaxHandTicks
		switch {
		case overBudget && haveFacility:
			return bestSiting, true, nil
		case overBudget:
			return siting{
				recipe: *hand, runs: runs, ticks: handTicks,
				status: StatusSlow,
				reason: "hand-craft exceeds time budget; no public facility known",
			}, true, nil
		default:
			return siting{recipe: *hand, runs: runs, ticks: handTicks, status: StatusOK}, true, nil
		}
	}

	if haveFacility {
		return bestSiting, true, nil
	}
	return siting{}, false, nil // facility_only, nowhere to make it
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/craftbrain/ -run 'ChooseSiting|CheapestFacility' -v`
Expected: PASS (6 tests)

- [ ] **Step 5: Commit**

```bash
go build ./... && golangci-lint run pkg/craftbrain/...
git add pkg/craftbrain/site.go pkg/craftbrain/site_test.go
git commit -m "feat(craftbrain): fee-vs-time siting, slow fallback, no skill logic"
```

---

## Task 7: The expander — Engine.Plan

Ties Tasks 4–6 together. Walks items in topological order; at each item aggregates demand, subtracts on-hand, sites the craft, banks surplus, and pushes demand to inputs.

**Files:**
- Create: `pkg/craftbrain/expand.go`, `pkg/craftbrain/expand_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: `(e *Engine) Plan(ctx context.Context, target string, qty int, opts Options) (*Plan, error)`.

- [ ] **Step 1: Write the failing test**

Create `pkg/craftbrain/expand_test.go`:

```go
package craftbrain

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/finditem"
)

func planFor(t *testing.T, f *fakeSource, target string, qty int, opts Options) *Plan {
	t.Helper()
	opts.Now = testNow()
	p, err := New(f).Plan(context.Background(), target, qty, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return p
}

func leafNeed(p *Plan, item string) int {
	n := 0
	for _, nd := range p.Nodes {
		if nd.ItemID == item && (nd.Kind == KindMine || nd.Kind == KindBuy) {
			n += nd.Qty
		}
	}
	return n
}

// THE test this package exists for. alloy_electrum_ingot consumes 2 gold + 3
// silver and yields 2 ingots. bill_of_materials stores per-unit gold 1,
// silver 2 (1.5 rounded up), so 2 units reads as 4 silver. The truth is 3.
func TestPlan_CeilPerRunNotPerUnit(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("alloy_electrum_ingot", "electrum_ingot", 2, 1, false,
		map[string]int{"gold_ore": 2, "silver_ore": 3})
	p := planFor(t, f, "electrum_ingot", 2, DefaultOptions())

	if got := leafNeed(p, "gold_ore"); got != 2 {
		t.Errorf("gold_ore = %d, want 2", got)
	}
	if got := leafNeed(p, "silver_ore"); got != 3 {
		t.Errorf("silver_ore = %d, want 3 (NOT 4 — per-unit rounding is wrong)", got)
	}
}

// One intermediate consumed by three parents: aggregate demand (12) then
// round once -> 4 runs. A tree walk would compute ceil(4/3)=2 runs three
// times = 6 runs = 18 units.
func TestPlan_SharedIntermediateAggregatesBeforeRounding(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_top", "top", 1, 1, false, map[string]int{"a": 1, "b": 1, "c": 1})
	f.addRecipe("make_a", "a", 1, 1, false, map[string]int{"wire": 4})
	f.addRecipe("make_b", "b", 1, 1, false, map[string]int{"wire": 4})
	f.addRecipe("make_c", "c", 1, 1, false, map[string]int{"wire": 4})
	f.addRecipe("make_wire", "wire", 3, 1, false, map[string]int{"copper": 1})

	p := planFor(t, f, "top", 1, DefaultOptions())

	var wireRuns int
	for _, n := range p.Nodes {
		if n.Kind == KindCraft && n.ItemID == "wire" {
			wireRuns = n.Runs
		}
	}
	if wireRuns != 4 {
		t.Errorf("wire runs = %d, want 4 (ceil(12/3)); a tree walk gives 6", wireRuns)
	}
	if got := leafNeed(p, "copper"); got != 4 {
		t.Errorf("copper = %d, want 4", got)
	}
}

// 2-per-run recipe with odd demand leaves a spare that a later sibling uses.
func TestPlan_SurplusIsBankedAndReported(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_pair", "widget", 2, 1, false, map[string]int{"ore": 1})
	p := planFor(t, f, "widget", 3, DefaultOptions())

	if p.Surplus["widget"] != 1 {
		t.Errorf("surplus[widget] = %d, want 1 (2 runs x 2 = 4, need 3)", p.Surplus["widget"])
	}
}

// Stock already held is never re-crafted.
func TestPlan_SubtractEarlySkipsCraft(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_plate", "plate", 1, 1, false, map[string]int{"ore": 1})
	f.onHand["plate"] = []Holding{{Holder: "a", BaseID: "hub", Qty: 5, CapturedAt: fresh()}}
	f.systems["hub"] = "sol"

	p := planFor(t, f, "plate", 5, DefaultOptions())
	for _, n := range p.Nodes {
		if n.Kind == KindCraft {
			t.Errorf("held stock must not be crafted, got %+v", n)
		}
	}
}

// facility_only, no facility, but buyable -> buy node, not BLOCKED.
func TestPlan_DeadEndFallsBackToBuy(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_top", "top", 1, 1, false, map[string]int{"matrix": 2})
	f.addRecipe("grow_matrix", "matrix", 1, 21, true, map[string]int{"gas": 1})
	f.buyable["matrix"] = []finditem.Result{{}}

	p := planFor(t, f, "top", 1, DefaultOptions())
	var kinds []Kind
	for _, n := range p.Nodes {
		if n.ItemID == "matrix" {
			kinds = append(kinds, n.Kind)
		}
	}
	if len(kinds) != 1 || kinds[0] != KindBuy {
		t.Errorf("matrix node kinds = %v, want [buy]", kinds)
	}
}

// facility_only, no facility, not buyable -> BLOCKED, rest of DAG intact.
func TestPlan_DeadEndUnbuyableIsBlockedButPlanSurvives(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("make_top", "top", 1, 1, false, map[string]int{"matrix": 2, "ore": 1})
	f.addRecipe("grow_matrix", "matrix", 1, 21, true, map[string]int{"gas": 1})

	p := planFor(t, f, "top", 1, DefaultOptions())
	var blocked bool
	var sawOre bool
	for _, n := range p.Nodes {
		if n.ItemID == "matrix" && n.Kind == KindBlocked {
			blocked = true
			if n.Status != StatusBlocked {
				t.Errorf("status = %q, want blocked", n.Status)
			}
		}
		if n.ItemID == "ore" {
			sawOre = true
		}
	}
	if !blocked {
		t.Error("want a blocked node for matrix")
	}
	if !sawOre {
		t.Error("rest of the DAG must survive a blocked subtree")
	}
}

// A cycle terminates and is reported, not fatal.
func TestPlan_CycleTerminatesAndReports(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("refine", "plate", 1, 1, false, map[string]int{"scrap": 2})
	f.addRecipe("recycle", "scrap", 1, 1, false, map[string]int{"plate": 1})

	p := planFor(t, f, "plate", 1, DefaultOptions())
	if len(p.Diagnostics) == 0 {
		t.Error("cycle break must be reported in diagnostics")
	}
}

func TestPlan_RejectsBadInput(t *testing.T) {
	f := newFakeSource()
	e := New(f)
	if _, err := e.Plan(context.Background(), "nothing", 1, DefaultOptions()); err == nil {
		t.Error("unknown target must error")
	}
	f.addRecipe("make_x", "x", 1, 1, false, map[string]int{"ore": 1})
	if _, err := e.Plan(context.Background(), "x", 0, DefaultOptions()); err == nil {
		t.Error("qty < 1 must error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/craftbrain/ -run TestPlan_ -v`
Expected: FAIL — build error, `e.Plan undefined`

- [ ] **Step 3: Write minimal implementation**

Create `pkg/craftbrain/expand.go`:

```go
package craftbrain

import (
	"context"
	"fmt"
	"slices"
	"sort"
)

// defaultCraftBase is where hand-crafts are sited. Any docked station will do,
// so the plan names the target's own hub rather than inventing a location.
const defaultCraftBase = "any_docked_station"

// Plan computes the full recursive work to build qty of target.
//
// Items are visited in topological order (consumers before producers), so an
// item's demand is final before its runs are rounded. This is why the expander
// is not a tree walk: a shared intermediate needed 4+4+4 must round once at 12,
// not three times at 4.
func (e *Engine) Plan(ctx context.Context, target string, qty int, opts Options) (*Plan, error) {
	if qty < 1 {
		return nil, fmt.Errorf("quantity must be >= 1, got %d", qty)
	}
	recipes, err := e.src.Recipes(ctx)
	if err != nil {
		return nil, fmt.Errorf("load recipes: %w", err)
	}
	prod := producerIndex(recipes)
	if len(prod[target]) == 0 {
		return nil, fmt.Errorf("no recipe produces %q", target)
	}

	order, dropped := topoOrder(target, prod)

	p := &Plan{
		Target:   target,
		Quantity: qty,
		Leaves:   map[string]MineOrBuy{},
		Surplus:  map[string]int{},
	}
	for _, d := range dropped {
		p.Diagnostics = append(p.Diagnostics, fmt.Sprintf("cycle broken: dropped recipe edge %s", d))
	}
	if cov, err := e.src.Coverage(ctx); err == nil {
		p.Coverage = cov
	}

	demand := map[string]int{target: qty}
	// producedBy maps item -> every node id that makes it available. An item
	// can be BOTH hauled in and crafted, so a consumer must depend on all of
	// them; a single id would silently drop the haul edge.
	producedBy := map[string][]string{}
	ids := &idGen{}

	for _, item := range order {
		need := demand[item]
		if need <= 0 {
			continue
		}

		// Subtract stock we already hold, hauling it in if it sits elsewhere.
		rem, hauls, err := e.consumeOnHand(ctx, item, need, defaultCraftBase, opts, ids)
		if err != nil {
			return nil, err
		}
		p.Nodes = append(p.Nodes, hauls...)
		for _, h := range hauls {
			p.TotalHaulJumps += h.Jumps
			producedBy[item] = append(producedBy[item], h.ID)
		}
		if rem == 0 {
			continue
		}

		// Raw leaf: report mine-and-buy options; the operator decides.
		if len(prod[item]) == 0 {
			node := Node{
				ID: ids.next("mine"), Kind: KindMine, ItemID: item, Qty: rem, Status: StatusOK,
			}
			p.Nodes = append(p.Nodes, node)
			producedBy[item] = append(producedBy[item], node.ID)
			mb := MineOrBuy{Mineable: true}
			// finditem.Result embeds market.ItemSeller: BestPrice / TotalQty,
			// NOT Price / Quantity.
			if sellers, err := e.src.Buyable(ctx, item, rem); err == nil && len(sellers) > 0 {
				mb.MarketPrice = sellers[0].BestPrice
				mb.MarketQty = sellers[0].TotalQty
				mb.BestStation = sellers[0].StationID
			}
			p.Leaves[item] = mb
			continue
		}

		s, sited, err := e.chooseSiting(ctx, item, rem, prod[item], p.TotalTicks, opts)
		if err != nil {
			return nil, err
		}
		if !sited {
			// facility_only with no known facility: try the market.
			sellers, err := e.src.Buyable(ctx, item, rem)
			if err == nil && len(sellers) > 0 {
				node := Node{ID: ids.next("buy"), Kind: KindBuy, ItemID: item, Qty: rem, Status: StatusOK}
				if sellers[0].StationID != "" {
					node.StationID = sellers[0].StationID
				}
				p.Nodes = append(p.Nodes, node)
				producedBy[item] = append(producedBy[item], node.ID)
				continue
			}
			node := Node{
				ID: ids.next("blocked"), Kind: KindBlocked, ItemID: item, Qty: rem,
				Status: StatusBlocked,
				Reason: "facility_only, no public facility known and no market seller",
			}
			p.Nodes = append(p.Nodes, node)
			producedBy[item] = append(producedBy[item], node.ID)
			continue
		}

		node := Node{
			ID: ids.next("craft"), Kind: KindCraft, ItemID: item, Qty: rem,
			Runs: s.runs, RecipeID: s.recipe.ID, FeeTotal: s.feeTotal,
			TicksEst: s.ticks, Status: s.status, Reason: s.reason,
			StationID: defaultCraftBase,
		}
		if s.facility != nil {
			node.StationID = s.facility.StationID
			node.FacilityID = s.facility.FacilityID
		}
		p.Nodes = append(p.Nodes, node)
		producedBy[item] = append(producedBy[item], node.ID)
		p.TotalFee += s.feeTotal
		p.TotalTicks += s.ticks

		// Bank rounding surplus and any secondary outputs; later items in the
		// topological order draw on them before crafting more.
		perRun := outputPerRun(s.recipe, item)
		if made := s.runs * perRun; made > rem {
			p.Surplus[item] += made - rem
		}
		for _, out := range s.recipe.Outputs {
			if out.ItemID == item {
				continue
			}
			p.Surplus[out.ItemID] += s.runs * out.Quantity
		}

		// Push demand to this recipe's inputs, net of banked surplus.
		for _, in := range s.recipe.Inputs {
			want := in.Quantity * s.runs
			if bank := p.Surplus[in.ItemID]; bank > 0 {
				used := min(bank, want)
				p.Surplus[in.ItemID] -= used
				want -= used
			}
			if want > 0 {
				demand[in.ItemID] += want
			}
		}
	}

	// Wire dependencies: a craft depends on every node that makes its inputs
	// available — hauls and crafts and buys alike.
	for i := range p.Nodes {
		n := &p.Nodes[i]
		if n.Kind != KindCraft || n.RecipeID == "" {
			continue
		}
		r := recipes[n.RecipeID]
		var deps []string
		for _, in := range r.Inputs {
			for _, id := range producedBy[in.ItemID] {
				if id != n.ID {
					deps = append(deps, id)
				}
			}
		}
		sort.Strings(deps)
		deps = slices.Compact(deps)
		n.DependsOn = deps
	}

	for item, q := range p.Surplus {
		if q == 0 {
			delete(p.Surplus, item)
		}
	}
	return p, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/craftbrain/ -v`
Expected: PASS (all tests, ~20)

- [ ] **Step 5: Commit**

```bash
go build ./... && go test ./... && golangci-lint run pkg/craftbrain/...
git add pkg/craftbrain/expand.go pkg/craftbrain/expand_test.go
git commit -m "feat(craftbrain): topological expander with exact ceil-per-run

Aggregates shared-intermediate demand before rounding, banks rounding
surplus and secondary outputs, subtracts held stock, and falls back
buy -> BLOCKED at facility dead ends."
```

---

## Task 8: Human formatter

**Files:**
- Create: `pkg/craftbrain/format.go`, `pkg/craftbrain/format_test.go`

**Interfaces:**
- Consumes: `Plan` (Task 2).
- Produces: `Format(p *Plan) string`.

- [ ] **Step 1: Write the failing test**

Create `pkg/craftbrain/format_test.go`:

```go
package craftbrain

import (
	"strings"
	"testing"
)

func TestFormat_ShowsFootersAndStatuses(t *testing.T) {
	p := &Plan{
		Target: "sensor_array", Quantity: 2,
		Nodes: []Node{
			{ID: "mine-1", Kind: KindMine, ItemID: "iron_ore", Qty: 40, Status: StatusOK},
			{ID: "craft-2", Kind: KindCraft, ItemID: "plate", Qty: 4, Runs: 2,
				RecipeID: "forge_plate", StationID: "hub", FacilityID: "f1",
				FeeTotal: 70, TicksEst: 8, Status: StatusOK},
			{ID: "blocked-3", Kind: KindBlocked, ItemID: "matrix", Qty: 2,
				Status: StatusBlocked, Reason: "no public facility known"},
		},
		Surplus:        map[string]int{"plate": 1},
		Diagnostics:    []string{"cycle broken: dropped recipe edge a->b"},
		TotalFee:       70,
		TotalTicks:     8,
		TotalHaulJumps: 5,
		Coverage:       Coverage{Stations: 30, FacilityOnlyCovered: 101, FacilityOnlyTotal: 317},
	}
	out := Format(p)
	for _, want := range []string{
		"sensor_array", "x2",
		"mine", "iron_ore", "40",
		"craft", "forge_plate", "hub", "f1",
		"BLOCKED", "matrix",
		"Total fee: 70",
		"Total haul: 5 jumps",
		"surplus", "plate",
		"30 stations", "101/317",
		"cycle broken",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Format output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormat_EmptyPlanIsReadable(t *testing.T) {
	out := Format(&Plan{Target: "widget", Quantity: 1})
	if !strings.Contains(out, "widget") {
		t.Errorf("want target in output, got %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/craftbrain/ -run TestFormat -v`
Expected: FAIL — build error, `undefined: Format`

- [ ] **Step 3: Write minimal implementation**

Create `pkg/craftbrain/format.go`:

```go
package craftbrain

import (
	"fmt"
	"sort"
	"strings"
)

// Format renders a Plan for human review. The JSON DAG is the contract for
// Executor B; this is for the operator who has to approve it.
func Format(p *Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Build plan: %s x%d\n\n", p.Target, p.Quantity)

	if len(p.Nodes) == 0 {
		b.WriteString("  (nothing to do — already in stock)\n")
	}
	for _, n := range p.Nodes {
		switch n.Kind {
		case KindMine:
			fmt.Fprintf(&b, "  [%s] mine   %-24s x%d\n", n.ID, n.ItemID, n.Qty)
		case KindBuy:
			fmt.Fprintf(&b, "  [%s] buy    %-24s x%d  @ %s\n", n.ID, n.ItemID, n.Qty, orDash(n.StationID))
		case KindHaul:
			fmt.Fprintf(&b, "  [%s] haul   %-24s x%d  %s -> %s (%d jumps, holder %s)%s\n",
				n.ID, n.ItemID, n.Qty, n.FromBase, n.ToBase, n.Jumps, orDash(n.Holder), tag(n.Status))
		case KindCraft:
			site := n.StationID
			if n.FacilityID != "" {
				site = fmt.Sprintf("%s/%s", n.StationID, n.FacilityID)
			}
			fmt.Fprintf(&b, "  [%s] craft  %-24s x%d  %d runs of %s @ %s  fee %d, %.1f ticks%s\n",
				n.ID, n.ItemID, n.Qty, n.Runs, n.RecipeID, site, n.FeeTotal, n.TicksEst, tag(n.Status))
		case KindBlocked:
			fmt.Fprintf(&b, "  [%s] BLOCKED %-23s x%d  %s\n", n.ID, n.ItemID, n.Qty, n.Reason)
		}
	}

	if len(p.Surplus) > 0 {
		items := make([]string, 0, len(p.Surplus))
		for k := range p.Surplus {
			items = append(items, k)
		}
		sort.Strings(items)
		b.WriteString("\nLeftover surplus:\n")
		for _, k := range items {
			fmt.Fprintf(&b, "  %-24s x%d\n", k, p.Surplus[k])
		}
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "Total fee: %d credits\n", p.TotalFee)
	fmt.Fprintf(&b, "Total time: %.1f ticks (makespan estimate)\n", p.TotalTicks)
	fmt.Fprintf(&b, "Total haul: %d jumps\n", p.TotalHaulJumps)
	fmt.Fprintf(&b, "Catalog coverage: %d stations, %d/%d facility_only recipes known\n",
		p.Coverage.Stations, p.Coverage.FacilityOnlyCovered, p.Coverage.FacilityOnlyTotal)
	if p.Coverage.FacilityOnlyTotal > 0 && p.Coverage.FacilityOnlyCovered < p.Coverage.FacilityOnlyTotal {
		b.WriteString("  (a BLOCKED node may mean 'not swept yet', not 'impossible')\n")
	}
	for _, d := range p.Diagnostics {
		fmt.Fprintf(&b, "note: %s\n", d)
	}
	return b.String()
}

func tag(s Status) string {
	if s == StatusOK || s == "" {
		return ""
	}
	return "  [" + strings.ToUpper(string(s)) + "]"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/craftbrain/ -run TestFormat -v`
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
go build ./... && golangci-lint run pkg/craftbrain/...
git add pkg/craftbrain/format.go pkg/craftbrain/format_test.go
git commit -m "feat(craftbrain): human-readable plan formatter with honesty footers"
```

---

## Task 9: SQL Source in play_as

`station_id` in `public_facilities` is a **base id**. Jump routing keys on `pois.system_id`. Resolve `bases.id → bases.poi_id → pois.system_id`. Four fleet stations differ between spellings (`grand_exchange` vs `grand_exchange_station`, `sol_central` vs `confederacy_central_command`, `the_core` vs `central_nexus`, `war_citadel` vs `crimson_war_citadel`).

**Files:**
- Create: `cmd/tools/play_as/source_sql.go`, `cmd/tools/play_as/source_sql_test.go`
- Modify: `pkg/knowledge/sqlite.go` (add the `DB()` accessor — it does not exist today)

**Interfaces:**
- Consumes: `craftbrain.Source` (Task 3), `knowledge.SQLiteKB`, `market.Collector`.
- Produces: `newCraftbrainSource(kb *knowledge.SQLiteKB, col *market.Collector, originSystem string) *craftbrainSource` satisfying `craftbrain.Source`; `(kb *SQLiteKB) DB() *sql.DB`.

**Verified against the codebase — use these exact names:**
- `kb.GetRecipes(ctx) ([]RecipeDef, error)` bulk-hydrates `Inputs` and `Outputs`.
- `kb.GetConnections(ctx) ([]Connection, error)` exists at `pkg/knowledge/sqlite.go:916`.
- `kb.GetAllStorageSnapshots(ctx) ([]StorageSnapshot, error)`; `StorageSnapshot` has `AgentID`, `BaseID`, `Items []StorageSnapshotItem{ItemID, Quantity float64}`, `CapturedAt time.Time`.
- `finditem.Result` embeds `market.ItemSeller`: fields are `StationID`, **`BestPrice`**, **`TotalQty`** (not `Price`/`Quantity`).
- `SQLiteKB.DB()` does **not** exist yet; Step 1 adds it.

- [ ] **Step 1: Add the DB() accessor**

In `pkg/knowledge/sqlite.go`, beside the other `*SQLiteKB` methods:

```go
// DB exposes the underlying handle for callers that need ad-hoc queries the
// Base interface does not cover (e.g. craftbrain's station→system resolution).
func (kb *SQLiteKB) DB() *sql.DB { return kb.db }
```

Run: `go build ./pkg/knowledge/`
Expected: no output.

- [ ] **Step 2: Write the failing test**

Create `cmd/tools/play_as/source_sql_test.go`:

```go
package main

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// station_id is a base id; jumps key on the POI's system. Four fleet stations
// spell these differently, so a naive station_id == poi_id lookup silently
// yields "" and every haul leg costs RouteInf.
func TestCraftbrainSource_SystemOfResolvesBaseToPOISystem(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db := kb.DB()
	// pois requires name, type, position_x, position_y (all NOT NULL).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y)
		 VALUES ('sol_central','sol','Sol Central','station',0,0)`); err != nil {
		t.Fatal(err)
	}
	// bases requires poi_id and name (NOT NULL).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO bases (id, poi_id, name) VALUES ('confederacy_central_command','sol_central','CCC')`); err != nil {
		t.Fatal(err)
	}

	src := newCraftbrainSource(kb, nil, "sol")
	got, err := src.SystemOf(ctx, "confederacy_central_command")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sol" {
		t.Errorf("SystemOf(confederacy_central_command) = %q, want %q", got, "sol")
	}
}

// A station_id that is already a poi id must still resolve.
func TestCraftbrainSource_SystemOfFallsBackToPOIID(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := kb.DB().ExecContext(ctx,
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y)
		 VALUES ('ramens_rest','haven','Ramens Rest','station',0,0)`); err != nil {
		t.Fatal(err)
	}
	src := newCraftbrainSource(kb, nil, "haven")
	got, err := src.SystemOf(ctx, "ramens_rest")
	if err != nil {
		t.Fatal(err)
	}
	if got != "haven" {
		t.Errorf("SystemOf(ramens_rest) = %q, want haven", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestCraftbrainSource -v`
Expected: FAIL — build error, `undefined: newCraftbrainSource`

- [ ] **Step 4: Write minimal implementation**

Create `cmd/tools/play_as/source_sql.go`:

```go
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// craftbrainSource adapts the knowledge DB + market collector to
// craftbrain.Source. Read-only: it never writes.
type craftbrainSource struct {
	kb           *knowledge.SQLiteKB
	col          *market.Collector
	originSystem string

	recipeCache map[string]knowledge.RecipeDef
	graph       navigation.JumpGraph
	sysCache    map[string]string
}

func newCraftbrainSource(kb *knowledge.SQLiteKB, col *market.Collector, originSystem string) *craftbrainSource {
	return &craftbrainSource{kb: kb, col: col, originSystem: originSystem, sysCache: map[string]string{}}
}

func (s *craftbrainSource) Recipes(ctx context.Context) (map[string]knowledge.RecipeDef, error) {
	if s.recipeCache != nil {
		return s.recipeCache, nil
	}
	defs, err := s.kb.GetRecipes(ctx) // hydrates Inputs + Outputs in bulk
	if err != nil {
		return nil, err
	}
	out := make(map[string]knowledge.RecipeDef, len(defs))
	for _, d := range defs {
		out[d.ID] = d
	}
	s.recipeCache = out
	return out, nil
}

func (s *craftbrainSource) Facilities(ctx context.Context, recipeID string) ([]craftbrain.Facility, error) {
	rows, err := s.kb.FacilitiesForRecipe(ctx, recipeID)
	if err != nil {
		return nil, err
	}
	out := make([]craftbrain.Facility, 0, len(rows))
	for _, r := range rows {
		f := craftbrain.Facility{
			StationID:       r.StationID,
			FacilityID:      r.FacilityID,
			Level:           r.Level,
			RentalFeePerRun: r.RentalFeePerRun,
			LastSeenUTC:     r.LastSeenUTC,
		}
		// A malformed payload must not kill the plan; defaults are safe.
		_ = f.ParseProduction(r.DetailsJSON)
		out = append(out, f)
	}
	return out, nil
}

// OnHand sums both pools: personal storage_snapshots (populated, 31 bases) and
// faction_storage_items. Each holding keeps its holder so Executor B knows
// whom to ask.
func (s *craftbrainSource) OnHand(ctx context.Context, itemID string) ([]craftbrain.Holding, error) {
	var out []craftbrain.Holding

	snaps, err := s.kb.GetAllStorageSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	for _, snap := range snaps {
		for _, it := range snap.Items {
			if it.ItemID != itemID || it.Quantity <= 0 {
				continue
			}
			out = append(out, craftbrain.Holding{
				Holder:     snap.AgentID,
				BaseID:     snap.BaseID,
				Qty:        int(it.Quantity),
				CapturedAt: snap.CapturedAt,
			})
		}
	}

	rows, err := s.kb.DB().QueryContext(ctx,
		`SELECT base_id, quantity, captured_utc FROM faction_storage_items WHERE item_id = ? AND quantity > 0`, itemID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var baseID, capturedUTC string
		var qty float64
		if err := rows.Scan(&baseID, &qty, &capturedUTC); err != nil {
			return nil, err
		}
		h := craftbrain.Holding{BaseID: baseID, Qty: int(qty)} // Holder "" = faction pool
		h.CapturedAt = parseUTCOrZero(capturedUTC)
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *craftbrainSource) Buyable(ctx context.Context, itemID string, qty int) ([]finditem.Result, error) {
	if s.col == nil {
		return nil, nil // no market data → caller degrades to BLOCKED
	}
	return finditem.Find(ctx, s.col, s.kb, itemID, float64(qty), s.originSystem, 5)
}

// SystemOf resolves a station_id to a system. station_id is a bases.id, whose
// bases.poi_id points at pois.id; only pois carries system_id. Fall back to
// treating the id as a poi id, since some callers pass one.
func (s *craftbrainSource) SystemOf(ctx context.Context, stationID string) (string, error) {
	if stationID == "" {
		return "", nil
	}
	if v, ok := s.sysCache[stationID]; ok {
		return v, nil
	}
	var sys string
	err := s.kb.DB().QueryRowContext(ctx, `
		SELECT p.system_id FROM bases b JOIN pois p ON p.id = b.poi_id WHERE b.id = ?`, stationID).Scan(&sys)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.kb.DB().QueryRowContext(ctx, `SELECT system_id FROM pois WHERE id = ?`, stationID).Scan(&sys)
	}
	if errors.Is(err, sql.ErrNoRows) {
		s.sysCache[stationID] = ""
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("system of %s: %w", stationID, err)
	}
	s.sysCache[stationID] = sys
	return sys, nil
}

func (s *craftbrainSource) Jumps(ctx context.Context, fromSystem string, toSystems []string) (map[string]int, error) {
	if s.graph == nil {
		conns, err := s.kb.GetConnections(ctx)
		if err != nil {
			return nil, err
		}
		s.graph = navigation.JumpGraphFromConnections(conns)
	}
	return navigation.BFSJumps(s.graph, fromSystem, toSystems), nil
}

func (s *craftbrainSource) Coverage(ctx context.Context) (craftbrain.Coverage, error) {
	var c craftbrain.Coverage
	db := s.kb.DB()
	_ = db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT station_id) FROM public_facilities`).Scan(&c.Stations)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recipes WHERE facility_only = 1`).Scan(&c.FacilityOnlyTotal)
	_ = db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT r.id) FROM recipes r
		JOIN public_facilities f ON f.recipe_id = r.id
		WHERE r.facility_only = 1`).Scan(&c.FacilityOnlyCovered)
	return c, nil
}

var _ craftbrain.Source = (*craftbrainSource)(nil)
```

Add `parseUTCOrZero` to the same file:

```go
// parseUTCOrZero parses an RFC3339 stamp, returning the zero time when the
// column is empty (rows written by a bin/worker predating 21e60dc).
func parseUTCOrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
```

(Add `"time"` to the import block.)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestCraftbrainSource -v`
Expected: PASS (2 tests)

- [ ] **Step 6: Commit**

```bash
go build ./... && golangci-lint run ./cmd/tools/play_as/... ./pkg/knowledge/...
git add cmd/tools/play_as/source_sql.go cmd/tools/play_as/source_sql_test.go pkg/knowledge/sqlite.go
git commit -m "feat(play_as): SQL Source for craftbrain

station_id is a base id; SystemOf resolves bases.poi_id -> pois.system_id
because four fleet stations spell the two differently."
```

---

## Task 10: The `build` command

**Files:**
- Create: `cmd/tools/play_as/build.go`, `cmd/tools/play_as/build_test.go`
- Modify: `cmd/tools/play_as/main.go` (register the case near `"where_facility"` at ~:8386; add a help line near ~:9483)

**Interfaces:**
- Consumes: `newCraftbrainSource` (Task 9), `craftbrain.New`, `craftbrain.Format` (Tasks 3, 8).
- Produces: `runBuild(client game.GameClient, ctx context.Context, args []string) error`, `parseBuildArgs(args []string) (target string, qty int, jsonOut bool, err error)`.

Usage: `build <target> [qty] [--json] [--max-hand-ticks=N]`

- [ ] **Step 1: Write the failing test**

Create `cmd/tools/play_as/build_test.go`:

```go
package main

import "testing"

func TestParseBuildArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		target  string
		qty     int
		jsonOut bool
		wantErr bool
	}{
		{name: "target only defaults qty 1", args: []string{"sensor_array"}, target: "sensor_array", qty: 1},
		{name: "explicit qty", args: []string{"sensor_array", "25"}, target: "sensor_array", qty: 25},
		{name: "json flag", args: []string{"sensor_array", "2", "--json"}, target: "sensor_array", qty: 2, jsonOut: true},
		{name: "no args", args: nil, wantErr: true},
		{name: "zero qty", args: []string{"x", "0"}, wantErr: true},
		{name: "bad qty", args: []string{"x", "abc"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, qty, jsonOut, err := parseBuildArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if target != tt.target || qty != tt.qty || jsonOut != tt.jsonOut {
				t.Errorf("got (%q,%d,%v), want (%q,%d,%v)", target, qty, jsonOut, tt.target, tt.qty, tt.jsonOut)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestParseBuildArgs -v`
Expected: FAIL — build error, `undefined: parseBuildArgs`

- [ ] **Step 3: Write minimal implementation**

Create `cmd/tools/play_as/build.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseBuildArgs parses: build <target> [qty] [--json] [--max-hand-ticks=N]
func parseBuildArgs(args []string) (string, int, bool, error) {
	if len(args) == 0 {
		return "", 0, false, fmt.Errorf("usage: build <target> [qty] [--json]")
	}
	target := args[0]
	qty := 1
	jsonOut := false
	for _, a := range args[1:] {
		switch {
		case a == "--json":
			jsonOut = true
		case strings.HasPrefix(a, "--"):
			// flags handled by the caller; ignore here
		default:
			n, err := strconv.Atoi(a)
			if err != nil {
				return "", 0, false, fmt.Errorf("quantity %q is not a number", a)
			}
			if n < 1 {
				return "", 0, false, fmt.Errorf("quantity must be >= 1, got %d", n)
			}
			qty = n
		}
	}
	return target, qty, jsonOut, nil
}

// runBuild implements: build <target> [qty] [--json]
// Computes the full recursive work to build the target and prints it for
// review. Read-only — it dispatches nothing.
func runBuild(client game.GameClient, ctx context.Context, args []string) error {
	target, qty, jsonOut, err := parseBuildArgs(args)
	if err != nil {
		return err
	}
	if globalKB == nil {
		return fmt.Errorf("build: knowledge DB not available (run with --db-path)")
	}
	sk, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("build: knowledge DB does not support facility queries")
	}

	// GetState() returns *State; State.System is a SystemData VALUE, not a
	// pointer (pkg/game/types.go:367). Nil-check only the state.
	originSystem := ""
	if st := client.GetState(); st != nil {
		originSystem = st.System.ID
	}

	opts := craftbrain.DefaultOptions()
	for _, a := range args[1:] {
		if v, found := strings.CutPrefix(a, "--max-hand-ticks="); found {
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("--max-hand-ticks %q is not a number", v)
			}
			opts.MaxHandTicks = n
		}
	}

	src := newCraftbrainSource(sk, globalMarketCollector, originSystem)
	plan, err := craftbrain.New(src).Plan(ctx, target, qty, opts)
	if err != nil {
		return err
	}

	if jsonOut {
		out, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	fmt.Print(craftbrain.Format(plan))
	return nil
}
```

In `cmd/tools/play_as/main.go`, add the dispatch case immediately after the `where_facility` case (~line 8386):

```go
	case "build":
		return runBuild(client, ctx, parts[1:])
```

And a help line beside the `where_facility` help entry (~line 9483):

```go
	fmt.Println("  build <target> [qty]      - Plan the full recursive build (mine/buy/haul/craft DAG)")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestParseBuildArgs -v`
Expected: PASS (6 subtests)

Then verify the command is reachable:
Run: `go build ./... && go vet ./cmd/tools/play_as/`
Expected: no output

- [ ] **Step 5: Commit**

```bash
go build ./... && go test ./... && golangci-lint run ./cmd/tools/play_as/...
git add cmd/tools/play_as/build.go cmd/tools/play_as/build_test.go cmd/tools/play_as/main.go
git commit -m "feat(play_as): build <target> [qty] — recursive build planner"
```

---

## Task 11: Golden plan against a real-shaped fixture

Locks the whole pipeline. A regression in any stage shows up as a DAG diff.

**Files:**
- Create: `pkg/craftbrain/golden_test.go`, `pkg/craftbrain/testdata/golden_plan.json`

**Interfaces:**
- Consumes: `fakeSource` (Task 3), `Engine.Plan` (Task 7).
- Produces: nothing (test only).

- [ ] **Step 1: Write the failing test**

Create `pkg/craftbrain/golden_test.go`:

```go
package craftbrain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// goldenPlan pins the end-to-end DAG for a multi-level target that exercises
// every stage: ceil-per-run, a shared intermediate, a facility siting, a
// stale remote holding with a haul leg, and a facility_only dead end.
//
// Regenerate with: go test ./pkg/craftbrain/ -run TestGoldenPlan -update
func TestGoldenPlan(t *testing.T) {
	f := newFakeSource()
	// sensor_array <- 2 optics + 1 matrix (facility_only, unbuyable -> BLOCKED)
	f.addRecipe("build_sensor_array", "sensor_array", 1, 4.0, false,
		map[string]int{"optics": 2, "matrix": 1})
	// optics <- 3 wire ; wire is 2-per-run so 6 wire needs 3 runs exactly
	f.addRecipe("build_optics", "optics", 1, 1.0, false, map[string]int{"wire": 3})
	f.addRecipe("draw_wire", "wire", 2, 0.5, true, map[string]int{"copper_ore": 1})
	f.addRecipe("grow_matrix", "matrix", 1, 21.0, true, map[string]int{"neon_gas": 1})

	// wire has a facility; matrix has none and is unbuyable.
	f.facilities["draw_wire"] = []Facility{
		{StationID: "confederacy_central_command", FacilityID: "cheap", RentalFeePerRun: 4, TicksPerRun: 0.05, OutputPerRun: 2},
		{StationID: "grand_exchange_station", FacilityID: "pricey", RentalFeePerRun: 90, TicksPerRun: 0.01, OutputPerRun: 2},
	}
	// A stale remote holding of copper_ore, two jumps out.
	f.onHand["copper_ore"] = []Holding{
		{Holder: "hauler-3", BaseID: "far_base", Qty: 2, CapturedAt: testNow().Add(-72 * time.Hour)},
	}
	f.systems["any_docked_station"] = "sol"
	f.systems["far_base"] = "beta"
	f.jumps["beta"] = 2
	f.coverage = Coverage{Stations: 30, FacilityOnlyCovered: 101, FacilityOnlyTotal: 317}

	opts := DefaultOptions()
	opts.Now = testNow()
	got, err := New(f).Plan(context.Background(), "sensor_array", 2, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	path := filepath.Join("testdata", "golden_plan.json")
	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, append(gotJSON, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden updated")
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (regenerate with UPDATE_GOLDEN=1): %v", err)
	}
	if string(gotJSON)+"\n" != string(want) {
		t.Errorf("plan DAG changed.\n--- got ---\n%s\n--- want ---\n%s", gotJSON, want)
	}
}

// Independent assertions on the same fixture, so a careless golden
// regeneration cannot silently bless a wrong plan.
func TestGoldenPlan_Invariants(t *testing.T) {
	f := newFakeSource()
	f.addRecipe("build_sensor_array", "sensor_array", 1, 4.0, false,
		map[string]int{"optics": 2, "matrix": 1})
	f.addRecipe("build_optics", "optics", 1, 1.0, false, map[string]int{"wire": 3})
	f.addRecipe("draw_wire", "wire", 2, 0.5, true, map[string]int{"copper_ore": 1})
	f.addRecipe("grow_matrix", "matrix", 1, 21.0, true, map[string]int{"neon_gas": 1})
	f.facilities["draw_wire"] = []Facility{
		{StationID: "confederacy_central_command", FacilityID: "cheap", RentalFeePerRun: 4, TicksPerRun: 0.05, OutputPerRun: 2},
		{StationID: "grand_exchange_station", FacilityID: "pricey", RentalFeePerRun: 90, TicksPerRun: 0.01, OutputPerRun: 2},
	}
	f.systems["any_docked_station"] = "sol"
	opts := DefaultOptions()
	opts.Now = testNow()
	p, err := New(f).Plan(context.Background(), "sensor_array", 2, opts)
	if err != nil {
		t.Fatal(err)
	}

	// 2 sensor arrays -> 4 optics -> 12 wire -> ceil(12/2)=6 runs of draw_wire.
	var wireRuns int
	var wireFacility string
	var matrixBlocked bool
	for _, n := range p.Nodes {
		if n.ItemID == "wire" && n.Kind == KindCraft {
			wireRuns, wireFacility = n.Runs, n.FacilityID
		}
		if n.ItemID == "matrix" && n.Kind == KindBlocked {
			matrixBlocked = true
		}
	}
	if wireRuns != 6 {
		t.Errorf("wire runs = %d, want 6", wireRuns)
	}
	if wireFacility != "cheap" {
		t.Errorf("wire sited at %q, want cheap (lowest fee x runs)", wireFacility)
	}
	if !matrixBlocked {
		t.Error("matrix is facility_only with no facility and no seller; want BLOCKED")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/craftbrain/ -run TestGoldenPlan -v`
Expected: FAIL — `read golden (regenerate with UPDATE_GOLDEN=1): ... no such file`

- [ ] **Step 3: Generate the golden and verify the invariants**

Run:
```bash
mkdir -p pkg/craftbrain/testdata
UPDATE_GOLDEN=1 go test ./pkg/craftbrain/ -run TestGoldenPlan
```

Then **read `pkg/craftbrain/testdata/golden_plan.json` and confirm by eye**: 6 runs of `draw_wire` at facility `cheap`, a `blocked` node for `matrix`, a `haul` node for `copper_ore` tagged `stale`, and `total_haul_jumps: 2`. If any of those are wrong, the bug is in the engine, not the golden — fix the engine and regenerate.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/craftbrain/ -v`
Expected: PASS (all tests, including `TestGoldenPlan` and `TestGoldenPlan_Invariants`)

- [ ] **Step 5: Commit**

```bash
go build ./... && go test ./... && golangci-lint run pkg/craftbrain/...
git add pkg/craftbrain/golden_test.go pkg/craftbrain/testdata/golden_plan.json
git commit -m "test(craftbrain): golden plan DAG + independent invariants"
```

---

## Task 12: Schedule `view_storage` so the inventory pool stays fresh

The planner subtracts stock from `storage_snapshots`, which is captured **passively** — `pkg/agent/storage_capture.go:13` persists any `view_storage` response, but nothing runs `view_storage` on a timer, so the newest snapshot is a week old. Scheduling it mirrors exactly how A1 scheduled `facilities`.

**Files:**
- Modify: `data/overmind/roles.yaml` (the `resident`, `resident_gas`, `resident_ice` schedule blocks — the same three that carry `facilities`)
- Test: `pkg/worker/roles_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: an `{ every: hourly, command: "view_storage" }` entry in each resident role. `RunStanding` registers it into each agent's `schedule.json` idempotently (deduped by `freq|command`) on the next worker restart — no forced fleet restart, no hand-editing 34 runtime files.

- [ ] **Step 1: Write the failing test**

Append to `pkg/worker/roles_test.go`:

```go
// A2's planner subtracts held stock from storage_snapshots, which only fills
// when an agent runs view_storage. Without a schedule the pool goes stale and
// every plan overstates the work.
func TestResidentRolesScheduleViewStorage(t *testing.T) {
	roles, err := LoadRoles("../../data/overmind/roles.yaml")
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	for _, name := range []string{"resident", "resident_gas", "resident_ice"} {
		role, ok := roles[name]
		if !ok {
			t.Fatalf("role %q missing", name)
		}
		var found bool
		for _, s := range role.Schedule {
			if s.Command == "view_storage" && s.Every == "hourly" {
				found = true
			}
		}
		if !found {
			t.Errorf("role %q has no hourly view_storage in its schedule", name)
		}
	}
}
```

Verified names (`pkg/worker/roles.go:11-30`): `LoadRoles(path) (map[string]Role, error)`, `Role.Schedule []ScheduleEntry`, `ScheduleEntry{Every, Command string}`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run TestResidentRolesScheduleViewStorage -v`
Expected: FAIL — `role "resident" has no hourly view_storage in its schedule`

- [ ] **Step 3: Write minimal implementation**

In `data/overmind/roles.yaml`, add one line to each of the three resident schedule blocks, directly after the `facilities` line:

```yaml
      - { every: hourly, command: "facilities" }
      - { every: hourly, command: "view_storage" }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/worker/ -run TestResidentRolesScheduleViewStorage -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go build ./... && go test ./... && golangci-lint run ./...
git add data/overmind/roles.yaml pkg/worker/roles_test.go
git commit -m "feat(overmind): schedule hourly view_storage on resident roles

craftbrain subtracts held stock from storage_snapshots, which fills only
passively when an agent runs view_storage. Registers into each agent's
schedule.json on the next restart; no forced fleet restart."
```

---

## Verification

After Task 12:

```bash
go build ./...
go test ./...
golangci-lint run ./...
```

Then exercise the real command against the live KB (read-only, safe):

```bash
go run ./cmd/tools/play_as --db-path data/spacemolt-knowledge.db <agent>
> build sensor_array 2
> build sensor_array 2 --json
> build sensor_array_production_line 1
```

Confirm: the plan prints, `Total fee` / `Total haul` / `Catalog coverage` footers appear, BLOCKED nodes carry a reason, and no node claims a facility that `where_facility <recipe_id>` does not list.

## Out of Scope

- Dispatching the plan (Executor B).
- Auto-recursing into *building* a facility when none exists (surfaced as BLOCKED).
- Haul-aware siting (fee-only, by decision; the haul footer exposes the cost).
- Fixing `pkg/craftplan`'s dead `BlockedSkill` gate.
- Adding faction-storage capture wiring.
- Backfilling `last_seen_utc` on rows written by a pre-`21e60dc` `bin/worker`.
