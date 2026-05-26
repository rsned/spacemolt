# play_as Crafting Smarts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two new REPL commands to `cmd/tools/play_as` — `craftable` (what can I build now) and `plan <id>` (what am I missing for X) — backed by a new `pkg/craftplan` package whose Engine consumes a mockable `Source` interface.

**Architecture:** `pkg/craftplan` is pure logic with a `Source` interface; `cmd/tools/play_as` provides a `playAsSource` adapter wrapping `game.GameClient` + `*sql.DB` (crafting DB). Server's `get_recipes` is the authoritative recipe catalog; crafting DB's `bill_of_materials` table provides the recursive expansion used by `--reachable`. Missing crafting DB degrades cleanly (direct mode keeps working).

**Tech Stack:** Go 1.24, `modernc.org/sqlite`, `text/tabwriter`, `encoding/json` for `bill_of_materials.recipe_path`. No new external deps.

**Spec:** `docs/superpowers/specs/2026-05-26-play-as-crafting-smarts-design.md`

---

## File Structure

```
pkg/craftplan/
├── types.go               # Inventory, BOMRow, CraftableRow, PlanResult, opts structs
├── source.go              # Source interface + fakeSource (test helper)
├── engine.go              # Engine struct, public entry points (Craftable, Plan)
├── direct.go              # Direct algorithm: skill/legality/material gates
├── reachable.go           # Reachable algorithm using BOM rows
├── resolve.go             # Recipe vs item_id resolution + fuzzy match
├── format.go              # Compact table + detail block renderers
├── types_test.go          # Smoke test that types compile + round-trip
├── direct_test.go         # Buildable + plan-direct table-driven tests
├── reachable_test.go      # Reachable depth, can_make floor, intermediate parsing
├── resolve_test.go        # Ambiguity, fuzzy match
├── format_test.go         # Golden-file rendering tests
└── testdata/
    ├── golden_craftable_compact.txt
    ├── golden_craftable_reachable.txt
    ├── golden_plan_direct_short.txt
    ├── golden_plan_direct_ready.txt
    └── golden_plan_reachable.txt

cmd/tools/play_as/
├── craftable.go           # REPL handlers for "craftable" + "plan" + playAsSource adapter
├── craftable_test.go      # One golden-file integration test from captured JSON fixtures
├── main.go                # Modified: add 2 case branches dispatching to craftable.go,
│                          #           help text update
└── testdata/
    ├── get_recipes.json
    └── view_storage.json
```

---

## Task 1: Package skeleton + core types

**Files:**
- Create: `pkg/craftplan/types.go`
- Create: `pkg/craftplan/types_test.go`

- [ ] **Step 1: Create `pkg/craftplan/types.go` with all shared types**

```go
// Package craftplan computes what an agent can craft right now and what is
// missing to craft a target recipe. The Engine consumes a Source (live game
// client + crafting DB in production; fakes in tests) and returns plain-data
// results that callers render however they like.
package craftplan

import "github.com/rsned/spacemolt/pkg/game/serverapi"

// Inventory is the count of items the agent has access to, split by where
// they live. Plan/Craftable sum the buckets the user opted into; the split
// is preserved so the per-recipe display can show which bucket a material
// is currently sitting in.
type Inventory struct {
	Cargo   map[string]int // item_id → quantity in ship cargo
	Storage map[string]int // item_id → quantity in personal storage at current station
	Faction map[string]int // item_id → quantity in faction storage at current station (may be nil)
}

// total returns the sum across the buckets the caller opted into. Pass
// includeFaction=false to ignore the faction bucket entirely (the default
// when --include-faction is not set).
func (i Inventory) total(itemID string, includeFaction bool) int {
	n := i.Cargo[itemID] + i.Storage[itemID]
	if includeFaction {
		n += i.Faction[itemID]
	}
	return n
}

// BOMRow is one base-material row from the crafting DB's bill_of_materials
// table for a single target recipe. RecipePath lists the recipe_ids that
// connect the target down to this base material (ordered output → leaf;
// length 1 means the target recipe itself consumes the base directly).
type BOMRow struct {
	BaseItemID string
	Quantity   int
	RecipePath []string
}

// CraftableRow is one row in the craftable table.
type CraftableRow struct {
	Recipe         serverapi.Recipe // pulled through so the renderer can reach inputs/outputs without re-lookup
	CanMake        int              // integer batches buildable from the current inventory; math.MaxInt for ∞
	OutputItemID   string           // primary output item_id (recipe.Outputs[0])
	OutputQuantity int              // primary output quantity per batch
	Depth          int              // 1 = direct, 2+ = via N-1 intermediate crafts (only meaningful in --reachable)
}

// PlanInputRow is one row in the plan output. For --reachable mode each row
// represents a base material; for direct mode each row is a direct ingredient.
type PlanInputRow struct {
	ItemID      string
	Need        int
	HaveCargo   int
	HaveStorage int
	HaveFaction int
	Short       int // max(0, Need - (HaveCargo + HaveStorage [+ HaveFaction]))
}

// IntermediateCraft describes one of the sub-crafts the operator would have
// to run to reach the target via --reachable mode.
type IntermediateCraft struct {
	RecipeID       string
	OutputItemID   string
	OutputQuantity int
	BatchesNeeded  int
}

// PlanResult is the full output of Engine.Plan. Ready==true means every
// Inputs row has Short==0 and the agent meets the recipe's skill + legality
// gates.
type PlanResult struct {
	Recipe        serverapi.Recipe
	Quantity      int
	StationID     string
	Inputs        []PlanInputRow
	Intermediates []IntermediateCraft // populated only in --reachable mode
	BlockedSkill  map[string]int      // skill_id → level shortfall; empty if no skill block
	BlockedIllegal bool               // true if recipe is illegal at this station
	Ready         bool
}

// CraftableOpts controls Engine.Craftable.
type CraftableOpts struct {
	Reachable       bool
	IncludeFaction  bool
	CategoryFilter  string // substring match, case-insensitive; empty = no filter
	SearchFilter    string // substring match against name + output item_ids, case-insensitive
	Refresh         bool   // bypass session recipe-catalog cache
	Max             int    // hard cap on rows returned; 0 = engine default (100)
	OneRecipe       string // if non-empty, return only this recipe (still in CraftableRow form); used by --detail --recipe X
}

// PlanOpts controls Engine.Plan.
type PlanOpts struct {
	ID             string // recipe_id OR item_id; recipe wins on tie
	Quantity       int    // batches to plan for; must be ≥ 1
	Reachable      bool
	IncludeFaction bool
	Refresh        bool
}
```

- [ ] **Step 2: Create `pkg/craftplan/types_test.go` to verify the package compiles**

```go
package craftplan

import (
	"math"
	"testing"
)

func TestInventoryTotal(t *testing.T) {
	inv := Inventory{
		Cargo:   map[string]int{"iron_ore": 5},
		Storage: map[string]int{"iron_ore": 10, "copper_ore": 3},
		Faction: map[string]int{"iron_ore": 100},
	}
	if got := inv.total("iron_ore", false); got != 15 {
		t.Errorf("total iron_ore (no faction) = %d, want 15", got)
	}
	if got := inv.total("iron_ore", true); got != 115 {
		t.Errorf("total iron_ore (with faction) = %d, want 115", got)
	}
	if got := inv.total("copper_ore", true); got != 3 {
		t.Errorf("total copper_ore = %d, want 3", got)
	}
	if got := inv.total("missing", true); got != 0 {
		t.Errorf("total missing item = %d, want 0", got)
	}
}

func TestInfinityRepresentation(t *testing.T) {
	// CanMake uses math.MaxInt to mean "infinite" — verify the sentinel is
	// the standard one so format.go can render it correctly.
	row := CraftableRow{CanMake: math.MaxInt}
	if row.CanMake <= 1_000_000_000 {
		t.Fatal("CanMake sentinel should be math.MaxInt, not a small number")
	}
}
```

- [ ] **Step 3: Run tests and confirm they pass**

Run: `go test ./pkg/craftplan/...`
Expected: `ok   github.com/rsned/spacemolt/pkg/craftplan  0.0Ns`

- [ ] **Step 4: Run lint to catch nits early**

Run: `golangci-lint run ./pkg/craftplan/`
Expected: `0 issues.`

- [ ] **Step 5: Commit**

```bash
git add pkg/craftplan/types.go pkg/craftplan/types_test.go
git commit -m "feat(craftplan): package skeleton + shared types"
```

---

## Task 2: Source interface + fake test source

**Files:**
- Create: `pkg/craftplan/source.go`

- [ ] **Step 1: Create `pkg/craftplan/source.go`**

```go
package craftplan

import (
	"context"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Source provides every fact the Engine needs to plan crafts. The real
// implementation in cmd/tools/play_as wraps game.GameClient + *sql.DB; the
// tests in this package use fakeSource so the engine can be exercised
// without spinning up a client or opening a database.
//
// Methods take a context so a slow view_storage call doesn't strand the
// REPL; the engine passes the same context through to every Source call
// for a single Craftable/Plan invocation.
type Source interface {
	// Recipes returns the server's authoritative catalog. The implementation
	// is allowed to cache; refresh=true forces a re-fetch.
	Recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error)

	// Inventory returns the agent's current cargo + station storage. If
	// includeFaction is true the Faction map is populated; otherwise the
	// implementation MAY leave it nil (engine doesn't read it in that case).
	Inventory(ctx context.Context, includeFaction bool) (Inventory, error)

	// Skills returns the agent's current skill levels keyed by skill_id.
	// Missing skills are treated as level 0 by callers.
	Skills(ctx context.Context) (map[string]int, error)

	// CurrentStationID returns the station_id the agent is docked at, used
	// for the illegal-recipes legality gate and the output header. Returns
	// "" if not docked.
	CurrentStationID(ctx context.Context) (string, error)

	// IllegalAt returns the set of recipe_ids that are illegal to craft at
	// the given stationID. Missing crafting DB returns an empty map and
	// nil error (no recipes are illegal if we can't tell — server enforces
	// at craft time anyway).
	IllegalAt(ctx context.Context, stationID string) (map[string]bool, error)

	// BOM returns flattened bill-of-materials rows for the given recipe IDs,
	// keyed by recipe_id. Only target_type='item' rows are returned. Missing
	// crafting DB returns nil + a wrapped error; the engine surfaces this
	// as "BOM unavailable" when --reachable is requested.
	BOM(ctx context.Context, recipeIDs []string) (map[string][]BOMRow, error)
}
```

- [ ] **Step 2: Add `fakeSource` as a test helper at the bottom of `source.go`**

Why in the production file: `fakeSource` is referenced from every test file in the package, so co-locating it next to the interface keeps the type definition close. Build tags would be cleaner but Go's `_test.go` convention for test helpers requires the file be `*_test.go`; we instead make `fakeSource` unexported so it never leaks into the public API surface.

Actually — move `fakeSource` to a `*_test.go` file instead. Production code stays clean.

Skip the fakeSource here; it will be created in Task 3 alongside the first test that uses it.

- [ ] **Step 3: Verify package still compiles**

Run: `go build ./pkg/craftplan/`
Expected: (no output, exit code 0)

- [ ] **Step 4: Commit**

```bash
git add pkg/craftplan/source.go
git commit -m "feat(craftplan): Source interface"
```

---

## Task 3: Direct buildable algorithm (TDD)

**Files:**
- Create: `pkg/craftplan/engine.go`
- Create: `pkg/craftplan/direct.go`
- Create: `pkg/craftplan/direct_test.go`

- [ ] **Step 1: Write the failing test in `pkg/craftplan/direct_test.go`**

```go
package craftplan

import (
	"context"
	"math"
	"sort"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// fakeSource is the test double used across this package's tests. It returns
// canned fixtures from its fields so each test can construct exactly the
// world it wants without database setup.
type fakeSource struct {
	recipes   map[string]serverapi.Recipe
	inventory Inventory
	skills    map[string]int
	stationID string
	illegal   map[string]bool
	bom       map[string][]BOMRow
	bomErr    error // if non-nil, BOM() returns this (simulates missing crafting DB)
}

func (f *fakeSource) Recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error) {
	return f.recipes, nil
}
func (f *fakeSource) Inventory(ctx context.Context, includeFaction bool) (Inventory, error) {
	inv := f.inventory
	if !includeFaction {
		inv.Faction = nil
	}
	return inv, nil
}
func (f *fakeSource) Skills(ctx context.Context) (map[string]int, error) {
	return f.skills, nil
}
func (f *fakeSource) CurrentStationID(ctx context.Context) (string, error) {
	return f.stationID, nil
}
func (f *fakeSource) IllegalAt(ctx context.Context, stationID string) (map[string]bool, error) {
	return f.illegal, nil
}
func (f *fakeSource) BOM(ctx context.Context, recipeIDs []string) (map[string][]BOMRow, error) {
	if f.bomErr != nil {
		return nil, f.bomErr
	}
	return f.bom, nil
}

// recipe constructs a minimal Recipe for tests.
func recipe(id, name, category string, inputs []serverapi.RecipeItem, outputs []serverapi.RecipeItem, skills map[string]int) serverapi.Recipe {
	return serverapi.Recipe{
		ID:             id,
		Name:           name,
		Category:       category,
		Inputs:         inputs,
		Outputs:        outputs,
		RequiredSkills: skills,
		CraftingTime:   6,
	}
}

func item(id string, qty int) serverapi.RecipeItem {
	return serverapi.RecipeItem{ItemID: id, Quantity: qty}
}

func TestCraftable_Direct_AllGates(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"alloy_titanium_ingot": recipe(
				"alloy_titanium_ingot", "Titanium Alloy Ingot", "Refining",
				[]serverapi.RecipeItem{item("iron_ore", 3), item("titanium_ore", 2)},
				[]serverapi.RecipeItem{item("titanium_alloy", 2)},
				nil,
			),
			"build_capital_gun": recipe(
				"build_capital_gun", "Capital Gun", "Weapons",
				[]serverapi.RecipeItem{item("titanium_alloy", 10)},
				[]serverapi.RecipeItem{item("capital_gun", 1)},
				map[string]int{"crafting": 30},
			),
			"build_smuggle_box": recipe(
				"build_smuggle_box", "Smuggle Box", "Contraband",
				[]serverapi.RecipeItem{item("titanium_alloy", 1)},
				[]serverapi.RecipeItem{item("smuggle_box", 1)},
				nil,
			),
			"refining_pure": recipe(
				"refining_pure", "Pure Refining", "Refining",
				nil, // no inputs
				[]serverapi.RecipeItem{item("nothing", 1)},
				nil,
			),
		},
		inventory: Inventory{
			Cargo:   map[string]int{"iron_ore": 6},
			Storage: map[string]int{"titanium_ore": 4, "titanium_alloy": 5},
		},
		skills:    map[string]int{"crafting": 5}, // gates out build_capital_gun (needs 30)
		stationID: "market_prime",
		illegal:   map[string]bool{"build_smuggle_box": true}, // gates out smuggle_box
	}

	eng := New(src)
	rows, err := eng.Craftable(context.Background(), CraftableOpts{})
	if err != nil {
		t.Fatalf("Craftable: %v", err)
	}

	got := map[string]int{}
	for _, r := range rows {
		got[r.Recipe.ID] = r.CanMake
	}

	want := map[string]int{
		"alloy_titanium_ingot": 2,           // min(6/3, 4/2) = min(2, 2) = 2
		"refining_pure":        math.MaxInt, // no inputs → ∞
	}

	if len(got) != len(want) {
		t.Errorf("got %d rows, want %d: %#v", len(got), len(want), got)
	}
	for id, wantCM := range want {
		if got[id] != wantCM {
			t.Errorf("recipe %s: can_make = %d, want %d", id, got[id], wantCM)
		}
	}
	if _, ok := got["build_capital_gun"]; ok {
		t.Error("build_capital_gun should be filtered by skill gate")
	}
	if _, ok := got["build_smuggle_box"]; ok {
		t.Error("build_smuggle_box should be filtered by legality gate")
	}
}

func TestCraftable_Direct_SortStability(t *testing.T) {
	// Three recipes with the same can_make should sort alphabetically by recipe_id.
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"c_recipe": recipe("c_recipe", "C", "Cat", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item("c_out", 1)}, nil),
			"a_recipe": recipe("a_recipe", "A", "Cat", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item("a_out", 1)}, nil),
			"b_recipe": recipe("b_recipe", "B", "Cat", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item("b_out", 1)}, nil),
		},
		inventory: Inventory{Cargo: map[string]int{"ore": 5}},
	}

	eng := New(src)
	rows, _ := eng.Craftable(context.Background(), CraftableOpts{})
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.Recipe.ID
	}
	want := []string{"a_recipe", "b_recipe", "c_recipe"}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("rows not sorted: %v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("position %d: got %s, want %s", i, ids[i], want[i])
		}
	}
}

func TestCraftable_Direct_FilterCategory(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"a": recipe("a", "A", "Refining", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item("a_out", 1)}, nil),
			"b": recipe("b", "B", "Weapons", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item("b_out", 1)}, nil),
		},
		inventory: Inventory{Cargo: map[string]int{"ore": 5}},
	}

	eng := New(src)
	rows, _ := eng.Craftable(context.Background(), CraftableOpts{CategoryFilter: "weap"})
	if len(rows) != 1 || rows[0].Recipe.ID != "b" {
		t.Errorf("CategoryFilter='weap' returned %v, want [b]", rows)
	}
}

func TestCraftable_Direct_MaxCap(t *testing.T) {
	recs := map[string]serverapi.Recipe{}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		recs[id] = recipe(id, id, "X", []serverapi.RecipeItem{item("ore", 1)}, []serverapi.RecipeItem{item(id+"_out", 1)}, nil)
	}
	src := &fakeSource{
		recipes:   recs,
		inventory: Inventory{Cargo: map[string]int{"ore": 100}},
	}
	eng := New(src)
	rows, _ := eng.Craftable(context.Background(), CraftableOpts{Max: 2})
	if len(rows) != 2 {
		t.Errorf("Max=2 returned %d rows, want 2", len(rows))
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails (Engine doesn't exist yet)**

Run: `go test ./pkg/craftplan/ -run TestCraftable_Direct`
Expected: FAIL — `undefined: New`

- [ ] **Step 3: Create `pkg/craftplan/engine.go` with the Engine type and constructor**

```go
package craftplan

import (
	"context"
	"strings"
	"sync"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

const defaultMax = 100

// Engine computes craftable rows and plan results from a Source.
type Engine struct {
	src Source

	// catalogMu guards catalog cache. Engine is REPL-scoped so contention is
	// nil in practice; mutex is for safety against future concurrent calls.
	catalogMu sync.Mutex
	catalog   map[string]serverapi.Recipe // cached result of src.Recipes()
}

// New constructs an Engine. The Source must be non-nil; methods will panic
// otherwise.
func New(src Source) *Engine {
	if src == nil {
		panic("craftplan.New: Source must be non-nil")
	}
	return &Engine{src: src}
}

// recipes returns the cached catalog, fetching on first call or when refresh
// is true. Callers must not mutate the returned map.
func (e *Engine) recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error) {
	e.catalogMu.Lock()
	defer e.catalogMu.Unlock()
	if refresh || e.catalog == nil {
		recs, err := e.src.Recipes(ctx, refresh)
		if err != nil {
			return nil, err
		}
		e.catalog = recs
	}
	return e.catalog, nil
}

// stringMatch reports whether needle appears in haystack case-insensitively.
// Empty needle always matches.
func stringMatch(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
```

- [ ] **Step 4: Create `pkg/craftplan/direct.go` with the direct-buildable algorithm**

```go
package craftplan

import (
	"context"
	"math"
	"sort"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Craftable returns the list of recipes the agent can build right now (or,
// with opts.Reachable, can build via intermediate crafts). Sort:
// can_make DESC, depth ASC, recipe_id ASC.
func (e *Engine) Craftable(ctx context.Context, opts CraftableOpts) ([]CraftableRow, error) {
	recs, err := e.recipes(ctx, opts.Refresh)
	if err != nil {
		return nil, err
	}
	inv, err := e.src.Inventory(ctx, opts.IncludeFaction)
	if err != nil {
		return nil, err
	}
	skills, err := e.src.Skills(ctx)
	if err != nil {
		return nil, err
	}
	stationID, err := e.src.CurrentStationID(ctx)
	if err != nil {
		return nil, err
	}
	illegal, err := e.src.IllegalAt(ctx, stationID)
	if err != nil {
		return nil, err
	}

	// First pass: every recipe that passes skill + legality gates becomes a
	// candidate. The direct-material gate runs next; reachable mode (handled
	// in reachable.go) runs as a separate pass on the same candidate set.
	candidates := make([]serverapi.Recipe, 0, len(recs))
	for _, r := range recs {
		if !meetsSkills(r, skills) {
			continue
		}
		if illegal[r.ID] {
			continue
		}
		if opts.OneRecipe != "" && r.ID != opts.OneRecipe {
			continue
		}
		if !rowMatchesFilter(r, opts) {
			continue
		}
		candidates = append(candidates, r)
	}

	var rows []CraftableRow
	if opts.Reachable {
		rows, err = e.craftableReachable(ctx, candidates, inv, opts.IncludeFaction)
		if err != nil {
			return nil, err
		}
	} else {
		rows = craftableDirect(candidates, inv, opts.IncludeFaction)
	}

	// Sort: can_make DESC, depth ASC (direct first), recipe_id ASC.
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.CanMake != b.CanMake {
			return a.CanMake > b.CanMake
		}
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		return a.Recipe.ID < b.Recipe.ID
	})

	// --max cap. opts.OneRecipe overrides Max because the caller asked for
	// a specific recipe.
	max := opts.Max
	if max == 0 {
		max = defaultMax
	}
	if opts.OneRecipe == "" && len(rows) > max {
		rows = rows[:max]
	}
	return rows, nil
}

func meetsSkills(r serverapi.Recipe, have map[string]int) bool {
	for skillID, lvl := range r.RequiredSkills {
		if have[skillID] < lvl {
			return false
		}
	}
	return true
}

func rowMatchesFilter(r serverapi.Recipe, opts CraftableOpts) bool {
	if !stringMatch(r.Category, opts.CategoryFilter) {
		return false
	}
	if opts.SearchFilter == "" {
		return true
	}
	if stringMatch(r.Name, opts.SearchFilter) {
		return true
	}
	for _, o := range r.Outputs {
		if stringMatch(o.ItemID, opts.SearchFilter) {
			return true
		}
	}
	return false
}

// craftableDirect computes can_make per recipe using direct inputs only.
func craftableDirect(candidates []serverapi.Recipe, inv Inventory, includeFaction bool) []CraftableRow {
	out := make([]CraftableRow, 0, len(candidates))
	for _, r := range candidates {
		cm := canMakeDirect(r, inv, includeFaction)
		if cm < 1 {
			continue
		}
		out = append(out, makeRow(r, cm, 1))
	}
	return out
}

func canMakeDirect(r serverapi.Recipe, inv Inventory, includeFaction bool) int {
	if len(r.Inputs) == 0 {
		return math.MaxInt
	}
	best := math.MaxInt
	for _, in := range r.Inputs {
		if in.Quantity <= 0 {
			continue
		}
		have := inv.total(in.ItemID, includeFaction)
		max := have / in.Quantity
		if max < best {
			best = max
		}
	}
	return best
}

func makeRow(r serverapi.Recipe, canMake, depth int) CraftableRow {
	row := CraftableRow{
		Recipe:  r,
		CanMake: canMake,
		Depth:   depth,
	}
	if len(r.Outputs) > 0 {
		row.OutputItemID = r.Outputs[0].ItemID
		row.OutputQuantity = r.Outputs[0].Quantity
	}
	return row
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/craftplan/ -run TestCraftable_Direct -v`
Expected: All four subtests pass.

- [ ] **Step 6: Run lint**

Run: `golangci-lint run ./pkg/craftplan/`
Expected: `0 issues.`

- [ ] **Step 7: Commit**

```bash
git add pkg/craftplan/engine.go pkg/craftplan/direct.go pkg/craftplan/direct_test.go
git commit -m "feat(craftplan): direct buildable algorithm (skill/legality/material gates)"
```

---

## Task 4: Plan gap analysis (direct mode)

**Files:**
- Modify: `pkg/craftplan/direct.go` (add Plan + helpers)
- Create: `pkg/craftplan/plan_test.go`

- [ ] **Step 1: Write the failing test in `pkg/craftplan/plan_test.go`**

```go
package craftplan

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestPlan_Direct_Ready(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"alloy_titanium_ingot": recipe(
				"alloy_titanium_ingot", "Titanium Alloy Ingot", "Refining",
				[]serverapi.RecipeItem{item("iron_ore", 3), item("titanium_ore", 2)},
				[]serverapi.RecipeItem{item("titanium_alloy", 2)},
				nil,
			),
		},
		inventory: Inventory{
			Cargo:   map[string]int{"iron_ore": 12},
			Storage: map[string]int{"titanium_ore": 380, "iron_ore": 450},
		},
		stationID: "market_prime",
	}
	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "alloy_titanium_ingot", Quantity: 10})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !res.Ready {
		t.Errorf("expected Ready=true, got false; inputs: %+v", res.Inputs)
	}
	if res.Quantity != 10 {
		t.Errorf("Quantity = %d, want 10", res.Quantity)
	}
	if len(res.Inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(res.Inputs))
	}
	for _, row := range res.Inputs {
		if row.Short != 0 {
			t.Errorf("input %s: Short=%d, want 0", row.ItemID, row.Short)
		}
	}
}

func TestPlan_Direct_Short(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"assemble_advanced_repair_kit": recipe(
				"assemble_advanced_repair_kit", "Advanced Repair Kit", "Consumables",
				[]serverapi.RecipeItem{
					item("circuit_board", 1),
					item("flex_polymer", 3),
					item("titanium_alloy", 3),
				},
				[]serverapi.RecipeItem{item("advanced_repair_kit", 1)},
				nil,
			),
		},
		inventory: Inventory{
			Cargo:   map[string]int{"titanium_alloy": 2},
			Storage: map[string]int{"circuit_board": 3, "flex_polymer": 20, "titanium_alloy": 18},
		},
	}
	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "assemble_advanced_repair_kit", Quantity: 5})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Ready {
		t.Error("expected Ready=false for short plan")
	}

	// need 5 circuit_board, have 3 → short 2
	// need 15 flex_polymer, have 20 → short 0
	// need 15 titanium_alloy, have 20 (2+18) → short 0
	byID := map[string]PlanInputRow{}
	for _, r := range res.Inputs {
		byID[r.ItemID] = r
	}
	if byID["circuit_board"].Short != 2 {
		t.Errorf("circuit_board short = %d, want 2", byID["circuit_board"].Short)
	}
	if byID["flex_polymer"].Short != 0 {
		t.Errorf("flex_polymer short = %d, want 0", byID["flex_polymer"].Short)
	}
	if byID["titanium_alloy"].Short != 0 {
		t.Errorf("titanium_alloy short = %d (cargo %d, storage %d), want 0",
			byID["titanium_alloy"].Short, byID["titanium_alloy"].HaveCargo, byID["titanium_alloy"].HaveStorage)
	}
}

func TestPlan_DefaultQuantity(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"r": recipe("r", "R", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("r_out", 1)}, nil),
		},
		inventory: Inventory{Cargo: map[string]int{"a": 5}},
	}
	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "r"}) // Quantity omitted → 1
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Quantity != 1 {
		t.Errorf("Quantity = %d, want 1", res.Quantity)
	}
}

func TestPlan_QuantityValidation(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"r": recipe("r", "R", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("r_out", 1)}, nil),
		},
	}
	eng := New(src)
	if _, err := eng.Plan(context.Background(), PlanOpts{ID: "r", Quantity: -1}); err == nil {
		t.Error("expected error for negative Quantity")
	}
}

func TestPlan_RecipeNotFound(t *testing.T) {
	src := &fakeSource{recipes: map[string]serverapi.Recipe{}}
	eng := New(src)
	_, err := eng.Plan(context.Background(), PlanOpts{ID: "nonexistent"})
	if err == nil {
		t.Error("expected error for missing recipe")
	}
}

func TestPlan_SkillBlocked(t *testing.T) {
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"r": recipe("r", "R", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("r_out", 1)},
				map[string]int{"crafting": 50}),
		},
		inventory: Inventory{Cargo: map[string]int{"a": 5}},
		skills:    map[string]int{"crafting": 5},
	}
	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "r"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Ready {
		t.Error("expected Ready=false when skill-blocked")
	}
	if got := res.BlockedSkill["crafting"]; got != 45 {
		t.Errorf("BlockedSkill[crafting] = %d, want 45", got)
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

Run: `go test ./pkg/craftplan/ -run TestPlan -v`
Expected: FAIL — `eng.Plan undefined`

- [ ] **Step 3: Append the Plan method and helpers to `pkg/craftplan/direct.go`**

```go
// Plan computes the gap between the agent's current inventory and the
// inputs required to craft opts.ID at opts.Quantity. Reachable mode (in
// reachable.go) re-uses these gates and replaces the input walk with a
// BOM walk.
func (e *Engine) Plan(ctx context.Context, opts PlanOpts) (*PlanResult, error) {
	if opts.Quantity == 0 {
		opts.Quantity = 1
	}
	if opts.Quantity < 1 {
		return nil, fmt.Errorf("qty must be a positive integer (got: %d)", opts.Quantity)
	}

	recs, err := e.recipes(ctx, opts.Refresh)
	if err != nil {
		return nil, err
	}
	r, err := e.resolveRecipe(opts.ID, recs)
	if err != nil {
		return nil, err
	}

	inv, err := e.src.Inventory(ctx, opts.IncludeFaction)
	if err != nil {
		return nil, err
	}
	skills, err := e.src.Skills(ctx)
	if err != nil {
		return nil, err
	}
	stationID, err := e.src.CurrentStationID(ctx)
	if err != nil {
		return nil, err
	}
	illegal, err := e.src.IllegalAt(ctx, stationID)
	if err != nil {
		return nil, err
	}

	res := &PlanResult{
		Recipe:         r,
		Quantity:       opts.Quantity,
		StationID:      stationID,
		BlockedSkill:   skillGaps(r, skills),
		BlockedIllegal: illegal[r.ID],
	}

	if opts.Reachable {
		// Implemented in reachable.go; will populate res.Inputs and res.Intermediates.
		if err := e.planReachable(ctx, res, r, inv, opts); err != nil {
			return nil, err
		}
	} else {
		res.Inputs = planDirect(r, opts.Quantity, inv, opts.IncludeFaction)
	}

	res.Ready = len(res.BlockedSkill) == 0 && !res.BlockedIllegal
	for _, row := range res.Inputs {
		if row.Short > 0 {
			res.Ready = false
			break
		}
	}
	return res, nil
}

// skillGaps returns the per-skill shortfall preventing the agent from
// crafting r. Empty map = no skill block.
func skillGaps(r serverapi.Recipe, have map[string]int) map[string]int {
	gaps := map[string]int{}
	for skillID, need := range r.RequiredSkills {
		if got := have[skillID]; got < need {
			gaps[skillID] = need - got
		}
	}
	return gaps
}

// planDirect builds the per-direct-input gap rows.
func planDirect(r serverapi.Recipe, qty int, inv Inventory, includeFaction bool) []PlanInputRow {
	rows := make([]PlanInputRow, 0, len(r.Inputs))
	for _, in := range r.Inputs {
		need := in.Quantity * qty
		row := PlanInputRow{
			ItemID:      in.ItemID,
			Need:        need,
			HaveCargo:   inv.Cargo[in.ItemID],
			HaveStorage: inv.Storage[in.ItemID],
		}
		if includeFaction {
			row.HaveFaction = inv.Faction[in.ItemID]
		}
		total := row.HaveCargo + row.HaveStorage + row.HaveFaction
		if need > total {
			row.Short = need - total
		}
		rows = append(rows, row)
	}
	return rows
}
```

- [ ] **Step 4: Add the missing `fmt` import to `direct.go`**

The file currently imports only `math` and `sort`. Add `fmt`:

```go
import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)
```

- [ ] **Step 5: Add a stub `planReachable` and `resolveRecipe` so the build succeeds**

In `direct.go`, append:

```go
// planReachable is a forward declaration; the real implementation lives in
// reachable.go (Task 6). Returning an error here means an opts.Reachable
// call hits the stub until the real implementation lands; tests that hit
// this path can swap when reachable.go is in.
func (e *Engine) planReachable(ctx context.Context, res *PlanResult, r serverapi.Recipe, inv Inventory, opts PlanOpts) error {
	return fmt.Errorf("reachable mode not yet implemented")
}

// resolveRecipe looks up opts.ID first as a recipe_id, then (if Task 5 has
// landed) as an item_id. For now: recipe_id only; item_id resolution lives
// in resolve.go.
func (e *Engine) resolveRecipe(id string, recs map[string]serverapi.Recipe) (serverapi.Recipe, error) {
	if r, ok := recs[id]; ok {
		return r, nil
	}
	return serverapi.Recipe{}, fmt.Errorf("no recipe %q", id)
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./pkg/craftplan/ -run TestPlan -v`
Expected: All six subtests pass.

- [ ] **Step 7: Run all tests in the package**

Run: `go test ./pkg/craftplan/ -v`
Expected: PASS

- [ ] **Step 8: Run lint**

Run: `golangci-lint run ./pkg/craftplan/`
Expected: `0 issues.`

- [ ] **Step 9: Commit**

```bash
git add pkg/craftplan/direct.go pkg/craftplan/plan_test.go
git commit -m "feat(craftplan): plan direct gap analysis"
```

---

## Task 5: Recipe vs item_id resolution + fuzzy match

**Files:**
- Create: `pkg/craftplan/resolve.go`
- Create: `pkg/craftplan/resolve_test.go`
- Modify: `pkg/craftplan/direct.go` (replace stub `resolveRecipe`)

- [ ] **Step 1: Write the failing test in `pkg/craftplan/resolve_test.go`**

```go
package craftplan

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestPlan_ResolveItemID(t *testing.T) {
	// Recipe outputs "titanium_alloy"; plan("titanium_alloy") should resolve
	// to its recipe.
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"alloy_titanium_ingot": recipe(
				"alloy_titanium_ingot", "Titanium Alloy", "Refining",
				[]serverapi.RecipeItem{item("iron_ore", 3)},
				[]serverapi.RecipeItem{item("titanium_alloy", 2)},
				nil,
			),
		},
		inventory: Inventory{Cargo: map[string]int{"iron_ore": 10}},
	}
	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "titanium_alloy"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Recipe.ID != "alloy_titanium_ingot" {
		t.Errorf("Resolved recipe = %q, want alloy_titanium_ingot", res.Recipe.ID)
	}
}

func TestPlan_RecipeIDWinsOverItemID(t *testing.T) {
	// Edge case: an item and a recipe share the same id. Recipe wins.
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"shared": recipe("shared", "Recipe Shared", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("recipe_out", 1)}, nil),
			"other": recipe("other", "Outputs Shared", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("shared", 1)}, nil), // also outputs "shared"
		},
		inventory: Inventory{Cargo: map[string]int{"a": 10}},
	}
	eng := New(src)
	res, _ := eng.Plan(context.Background(), PlanOpts{ID: "shared"})
	if res.Recipe.ID != "shared" {
		t.Errorf("Recipe-id tie went to %q, want shared", res.Recipe.ID)
	}
}

func TestPlan_AlternativeRecipesPickLowestSkill(t *testing.T) {
	// Two recipes output "widget". One needs crafting=30, the other crafting=5.
	// Plan("widget") should pick the lower-skill one.
	src := &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"build_widget_hard": recipe("build_widget_hard", "Hard", "X",
				[]serverapi.RecipeItem{item("a", 1)},
				[]serverapi.RecipeItem{item("widget", 1)},
				map[string]int{"crafting": 30}),
			"build_widget_easy": recipe("build_widget_easy", "Easy", "X",
				[]serverapi.RecipeItem{item("b", 1)},
				[]serverapi.RecipeItem{item("widget", 1)},
				map[string]int{"crafting": 5}),
		},
		inventory: Inventory{Cargo: map[string]int{"a": 5, "b": 5}},
		skills:    map[string]int{"crafting": 50}, // both unlocked
	}
	eng := New(src)
	res, _ := eng.Plan(context.Background(), PlanOpts{ID: "widget"})
	if res.Recipe.ID != "build_widget_easy" {
		t.Errorf("got %q, want build_widget_easy (lower-skill alternative)", res.Recipe.ID)
	}
}

func TestSuggestCloseMatches(t *testing.T) {
	have := []string{"alloy_titanium_ingot", "assemble_advanced_repair_kit", "build_capital_gun"}
	// Typo with 1-character difference should rank top.
	got := suggestCloseMatches("alloy_titanium_inggot", have, 5)
	if len(got) == 0 || got[0] != "alloy_titanium_ingot" {
		t.Errorf("suggestions = %v, expected alloy_titanium_ingot first", got)
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

Run: `go test ./pkg/craftplan/ -run TestPlan_ResolveItemID -v`
Expected: FAIL — `Resolved recipe = "", want alloy_titanium_ingot`

- [ ] **Step 3: Create `pkg/craftplan/resolve.go`**

```go
package craftplan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// resolveRecipe finds the Recipe that opts.ID refers to. Resolution order:
//   1. recipe_id exact match (wins on tie).
//   2. item_id exact match against any recipe's primary output. If multiple
//      recipes output the item, pick the one with the lowest skill ceiling
//      (max of required_skills values); ties broken alphabetically by
//      recipe_id.
//   3. No match → error with fuzzy suggestions.
//
// Replaces the stub in direct.go once this file is in the package.
func (e *Engine) resolveRecipe(id string, recs map[string]serverapi.Recipe) (serverapi.Recipe, error) {
	if r, ok := recs[id]; ok {
		return r, nil
	}

	// Item-id path: scan outputs.
	var matches []serverapi.Recipe
	for _, r := range recs {
		for _, out := range r.Outputs {
			if out.ItemID == id {
				matches = append(matches, r)
				break
			}
		}
	}
	if len(matches) > 0 {
		sort.Slice(matches, func(i, j int) bool {
			si, sj := skillCeiling(matches[i]), skillCeiling(matches[j])
			if si != sj {
				return si < sj
			}
			return matches[i].ID < matches[j].ID
		})
		return matches[0], nil
	}

	// No exact match anywhere — fall back to suggestions.
	ids := make([]string, 0, len(recs))
	for k := range recs {
		ids = append(ids, k)
	}
	suggest := suggestCloseMatches(id, ids, 5)
	if len(suggest) == 0 {
		return serverapi.Recipe{}, fmt.Errorf("no recipe %q", id)
	}
	return serverapi.Recipe{}, fmt.Errorf("no recipe %q. Did you mean: %s", id, strings.Join(suggest, ", "))
}

// skillCeiling returns the highest required_skills value for r, or 0 if r
// has no skill prereqs.
func skillCeiling(r serverapi.Recipe) int {
	max := 0
	for _, lvl := range r.RequiredSkills {
		if lvl > max {
			max = lvl
		}
	}
	return max
}

// suggestCloseMatches ranks candidates by Levenshtein distance to needle and
// returns up to maxResults. Implementation is intentionally direct (no
// external dep); 528 candidates × needle length keeps total work trivial.
func suggestCloseMatches(needle string, candidates []string, maxResults int) []string {
	type scored struct {
		s string
		d int
	}
	out := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, scored{c, levenshtein(needle, c)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].d != out[j].d {
			return out[i].d < out[j].d
		}
		return out[i].s < out[j].s
	})
	// Drop matches with distance ≥ len(needle) — those are noise.
	cutoff := len(needle)
	if cutoff < 3 {
		cutoff = 3
	}
	res := make([]string, 0, maxResults)
	for _, sc := range out {
		if sc.d >= cutoff {
			break
		}
		res = append(res, sc.s)
		if len(res) == maxResults {
			break
		}
	}
	return res
}

// levenshtein returns the standard edit distance between a and b.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
```

- [ ] **Step 4: Remove the stub `resolveRecipe` from `direct.go`**

The stub added in Task 4 must be deleted now that the real one exists in `resolve.go`. Search for the stub `func (e *Engine) resolveRecipe(id string, recs map[string]serverapi.Recipe) (serverapi.Recipe, error)` block in `direct.go` and remove it (only that one function — leave `planReachable` stub alone, Task 6 replaces it).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./pkg/craftplan/ -v`
Expected: PASS for all tests including resolve.

- [ ] **Step 6: Lint**

Run: `golangci-lint run ./pkg/craftplan/`
Expected: `0 issues.`

- [ ] **Step 7: Commit**

```bash
git add pkg/craftplan/resolve.go pkg/craftplan/resolve_test.go pkg/craftplan/direct.go
git commit -m "feat(craftplan): recipe vs item_id resolution + fuzzy suggestions"
```

---

## Task 6: Reachable algorithm using BOM rows

**Files:**
- Create: `pkg/craftplan/reachable.go`
- Create: `pkg/craftplan/reachable_test.go`
- Modify: `pkg/craftplan/direct.go` (remove the `planReachable` stub)

- [ ] **Step 1: Write the failing test in `pkg/craftplan/reachable_test.go`**

```go
package craftplan

import (
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Three-recipe chain:
//   iron_ore + titanium_ore → titanium_alloy   (alloy_titanium_ingot)
//   titanium_alloy + circuit_board → drone     (build_drone)
//   silicon_ore → circuit_board                (assemble_circuit_board)
func chainFixtures() *fakeSource {
	return &fakeSource{
		recipes: map[string]serverapi.Recipe{
			"alloy_titanium_ingot": recipe(
				"alloy_titanium_ingot", "Titanium Alloy", "Refining",
				[]serverapi.RecipeItem{item("iron_ore", 3), item("titanium_ore", 2)},
				[]serverapi.RecipeItem{item("titanium_alloy", 2)},
				nil,
			),
			"assemble_circuit_board": recipe(
				"assemble_circuit_board", "Circuit Board", "Components",
				[]serverapi.RecipeItem{item("silicon_ore", 4)},
				[]serverapi.RecipeItem{item("circuit_board", 1)},
				nil,
			),
			"build_drone": recipe(
				"build_drone", "Build Drone", "Drones",
				[]serverapi.RecipeItem{item("titanium_alloy", 2), item("circuit_board", 1)},
				[]serverapi.RecipeItem{item("drone", 1)},
				nil,
			),
		},
		// Flattened BOM for build_drone — these are the rows the engine reads.
		bom: map[string][]BOMRow{
			"alloy_titanium_ingot": {
				{BaseItemID: "iron_ore", Quantity: 3, RecipePath: []string{"alloy_titanium_ingot"}},
				{BaseItemID: "titanium_ore", Quantity: 2, RecipePath: []string{"alloy_titanium_ingot"}},
			},
			"assemble_circuit_board": {
				{BaseItemID: "silicon_ore", Quantity: 4, RecipePath: []string{"assemble_circuit_board"}},
			},
			"build_drone": {
				// Per batch of build_drone (yields 1 drone) we need 2 titanium_alloy
				// (each from 3 iron_ore + 2 titanium_ore) and 1 circuit_board (from
				// 4 silicon_ore). BOM rows are pre-flattened so we list the totals.
				{BaseItemID: "iron_ore", Quantity: 3, RecipePath: []string{"build_drone", "alloy_titanium_ingot"}},
				{BaseItemID: "titanium_ore", Quantity: 2, RecipePath: []string{"build_drone", "alloy_titanium_ingot"}},
				{BaseItemID: "silicon_ore", Quantity: 4, RecipePath: []string{"build_drone", "assemble_circuit_board"}},
			},
		},
	}
}

func TestCraftable_Reachable_Depth(t *testing.T) {
	src := chainFixtures()
	src.inventory = Inventory{Storage: map[string]int{
		"iron_ore":     30,
		"titanium_ore": 20,
		"silicon_ore":  40,
	}}

	eng := New(src)
	rows, err := eng.Craftable(context.Background(), CraftableOpts{Reachable: true})
	if err != nil {
		t.Fatalf("Craftable: %v", err)
	}

	depthByID := map[string]int{}
	cmByID := map[string]int{}
	for _, r := range rows {
		depthByID[r.Recipe.ID] = r.Depth
		cmByID[r.Recipe.ID] = r.CanMake
	}

	if depthByID["alloy_titanium_ingot"] != 1 {
		t.Errorf("alloy_titanium_ingot depth = %d, want 1 (direct)", depthByID["alloy_titanium_ingot"])
	}
	if depthByID["assemble_circuit_board"] != 1 {
		t.Errorf("assemble_circuit_board depth = %d, want 1 (direct)", depthByID["assemble_circuit_board"])
	}
	if depthByID["build_drone"] != 2 {
		t.Errorf("build_drone depth = %d, want 2 (+1 craft)", depthByID["build_drone"])
	}

	// build_drone can_make: min(30/3, 20/2, 40/4) = min(10, 10, 10) = 10
	if cmByID["build_drone"] != 10 {
		t.Errorf("build_drone can_make = %d, want 10", cmByID["build_drone"])
	}
}

func TestPlan_Reachable_Shortfall(t *testing.T) {
	src := chainFixtures()
	src.inventory = Inventory{Storage: map[string]int{
		"iron_ore":     30, // OK
		"titanium_ore": 1,  // short — need 2
		"silicon_ore":  4,  // OK
	}}

	eng := New(src)
	res, err := eng.Plan(context.Background(), PlanOpts{ID: "build_drone", Quantity: 1, Reachable: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Ready {
		t.Error("expected Ready=false")
	}
	byID := map[string]int{}
	for _, r := range res.Inputs {
		byID[r.ItemID] = r.Short
	}
	if byID["titanium_ore"] != 1 {
		t.Errorf("titanium_ore short = %d, want 1", byID["titanium_ore"])
	}
	if byID["iron_ore"] != 0 {
		t.Errorf("iron_ore short = %d, want 0", byID["iron_ore"])
	}

	// Intermediate crafts should list alloy_titanium_ingot and assemble_circuit_board.
	gotInter := map[string]bool{}
	for _, ic := range res.Intermediates {
		gotInter[ic.RecipeID] = true
	}
	if !gotInter["alloy_titanium_ingot"] {
		t.Error("expected alloy_titanium_ingot in Intermediates")
	}
	if !gotInter["assemble_circuit_board"] {
		t.Error("expected assemble_circuit_board in Intermediates")
	}
}

func TestCraftable_Reachable_BOMUnavailable(t *testing.T) {
	src := chainFixtures()
	src.bomErr = errors.New("crafting DB not configured")

	eng := New(src)
	_, err := eng.Craftable(context.Background(), CraftableOpts{Reachable: true})
	if err == nil {
		t.Fatal("expected BOM unavailable error")
	}
	if !errors.Is(err, ErrBOMUnavailable) {
		t.Errorf("expected ErrBOMUnavailable, got %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/craftplan/ -run TestCraftable_Reachable -v`
Expected: FAIL — `eng.craftableReachable undefined` or `reachable mode not yet implemented`.

- [ ] **Step 3: Create `pkg/craftplan/reachable.go`**

```go
package craftplan

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ErrBOMUnavailable wraps any error returned by Source.BOM when --reachable
// is requested. Callers (REPL) should check via errors.Is and surface the
// "install/update the crafting DB" hint.
var ErrBOMUnavailable = errors.New("BOM unavailable")

// craftableReachable computes can_make and depth for the candidate recipes
// using their flattened BOM rows. Pre-condition: candidates already passed
// skill + legality gates.
func (e *Engine) craftableReachable(ctx context.Context, candidates []serverapi.Recipe, inv Inventory, includeFaction bool) ([]CraftableRow, error) {
	ids := make([]string, len(candidates))
	for i, r := range candidates {
		ids[i] = r.ID
	}
	bom, err := e.src.BOM(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBOMUnavailable, err)
	}

	out := make([]CraftableRow, 0, len(candidates))
	for _, r := range candidates {
		rows, ok := bom[r.ID]
		if !ok || len(rows) == 0 {
			// BOM table doesn't know about this recipe — fall back to direct
			// algorithm so the recipe still shows up if buildable.
			if cm := canMakeDirect(r, inv, includeFaction); cm >= 1 {
				out = append(out, makeRow(r, cm, 1))
			}
			continue
		}
		cm, depth := canMakeReachable(rows, inv, includeFaction)
		if cm < 1 {
			continue
		}
		// If depth == 1 and BOM's single recipe matches r.ID, the recipe
		// consumes only base materials directly — same depth label as the
		// direct algorithm uses.
		out = append(out, makeRow(r, cm, depth))
	}
	return out, nil
}

// canMakeReachable returns the max batches buildable plus the deepest path
// depth across all BOM rows for one recipe.
func canMakeReachable(rows []BOMRow, inv Inventory, includeFaction bool) (canMake, depth int) {
	canMake = math.MaxInt
	for _, row := range rows {
		if row.Quantity <= 0 {
			continue
		}
		have := inv.total(row.BaseItemID, includeFaction)
		if max := have / row.Quantity; max < canMake {
			canMake = max
		}
		// Depth is the number of unique recipe_ids in the path. The recipe
		// itself is included in RecipePath, so depth == len(unique).
		if d := uniqueLen(row.RecipePath); d > depth {
			depth = d
		}
	}
	if depth == 0 {
		depth = 1
	}
	return canMake, depth
}

func uniqueLen(strs []string) int {
	seen := map[string]struct{}{}
	for _, s := range strs {
		seen[s] = struct{}{}
	}
	return len(seen)
}

// planReachable populates res.Inputs (base-material shortfall) and
// res.Intermediates from BOM rows. Replaces the stub in direct.go.
func (e *Engine) planReachable(ctx context.Context, res *PlanResult, r serverapi.Recipe, inv Inventory, opts PlanOpts) error {
	bomMap, err := e.src.BOM(ctx, []string{r.ID})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBOMUnavailable, err)
	}
	rows, ok := bomMap[r.ID]
	if !ok || len(rows) == 0 {
		// Fall back to direct inputs; tag the result so callers can render
		// the "BOM doesn't know about this recipe — showing direct inputs"
		// message. The PlanResult shape already supports this implicitly:
		// Intermediates stays empty.
		res.Inputs = planDirect(r, opts.Quantity, inv, opts.IncludeFaction)
		return nil
	}

	res.Inputs = make([]PlanInputRow, 0, len(rows))
	for _, row := range rows {
		need := row.Quantity * opts.Quantity
		pir := PlanInputRow{
			ItemID:      row.BaseItemID,
			Need:        need,
			HaveCargo:   inv.Cargo[row.BaseItemID],
			HaveStorage: inv.Storage[row.BaseItemID],
		}
		if opts.IncludeFaction {
			pir.HaveFaction = inv.Faction[row.BaseItemID]
		}
		total := pir.HaveCargo + pir.HaveStorage + pir.HaveFaction
		if need > total {
			pir.Short = need - total
		}
		res.Inputs = append(res.Inputs, pir)
	}

	res.Intermediates = collectIntermediates(rows, r.ID, opts.Quantity, e.catalog)
	return nil
}

// collectIntermediates walks RecipePath entries (excluding the target itself)
// and returns one IntermediateCraft per unique sub-recipe. Output ordering
// matches the order they first appear in BOM rows so the renderer can present
// them as "in order".
func collectIntermediates(rows []BOMRow, targetID string, batches int, catalog map[string]serverapi.Recipe) []IntermediateCraft {
	seen := map[string]struct{}{}
	out := []IntermediateCraft{}
	for _, row := range rows {
		for _, rid := range row.RecipePath {
			if rid == targetID {
				continue
			}
			if _, dup := seen[rid]; dup {
				continue
			}
			seen[rid] = struct{}{}
			ic := IntermediateCraft{
				RecipeID:      rid,
				BatchesNeeded: batches, // best-effort; precise per-step batching is out of v1 scope
			}
			if r, ok := catalog[rid]; ok && len(r.Outputs) > 0 {
				ic.OutputItemID = r.Outputs[0].ItemID
				ic.OutputQuantity = r.Outputs[0].Quantity
			}
			out = append(out, ic)
		}
	}
	return out
}
```

- [ ] **Step 4: Remove the `planReachable` stub from `direct.go`**

Delete the stub function added in Task 4:

```go
// planReachable is a forward declaration; the real implementation lives in
// reachable.go (Task 6). [delete this block]
func (e *Engine) planReachable(ctx context.Context, res *PlanResult, r serverapi.Recipe, inv Inventory, opts PlanOpts) error {
	return fmt.Errorf("reachable mode not yet implemented")
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./pkg/craftplan/ -v`
Expected: All tests pass including the three new reachable tests.

- [ ] **Step 6: Lint**

Run: `golangci-lint run ./pkg/craftplan/`
Expected: `0 issues.`

- [ ] **Step 7: Commit**

```bash
git add pkg/craftplan/reachable.go pkg/craftplan/reachable_test.go pkg/craftplan/direct.go
git commit -m "feat(craftplan): reachable algorithm via bill_of_materials"
```

---

## Task 7: Output formatters (compact table + detail block)

**Files:**
- Create: `pkg/craftplan/format.go`
- Create: `pkg/craftplan/format_test.go`
- Create: `pkg/craftplan/testdata/golden_craftable_compact.txt`
- Create: `pkg/craftplan/testdata/golden_plan_direct_short.txt`
- Create: `pkg/craftplan/testdata/golden_plan_direct_ready.txt`

- [ ] **Step 1: Write the failing test in `pkg/craftplan/format_test.go`**

```go
package craftplan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestFormatCraftableCompact(t *testing.T) {
	rows := []CraftableRow{
		{
			Recipe:         recipe("alloy_titanium_ingot", "Titanium Alloy Ingot", "Refining", nil, []serverapi.RecipeItem{item("titanium_alloy", 2)}, nil),
			CanMake:        47,
			OutputItemID:   "titanium_alloy",
			OutputQuantity: 2,
			Depth:          1,
		},
		{
			Recipe:         recipe("assemble_advanced_repair_kit", "Adv Repair Kit", "Consumables", nil, []serverapi.RecipeItem{item("advanced_repair_kit", 1)}, nil),
			CanMake:        31,
			OutputItemID:   "advanced_repair_kit",
			OutputQuantity: 1,
			Depth:          1,
		},
	}
	// CraftingTime is set on the recipe.
	rows[0].Recipe.CraftingTime = 6
	rows[1].Recipe.CraftingTime = 12

	got := FormatCraftableCompact(rows, FormatCraftableOpts{StationID: "market_prime_exchange", Reachable: false})
	want := loadGolden(t, "golden_craftable_compact.txt")
	if got != want {
		t.Errorf("compact format mismatch.\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestFormatPlanDirectShort(t *testing.T) {
	res := &PlanResult{
		Recipe: recipe("assemble_advanced_repair_kit", "Adv Repair Kit", "Consumables",
			[]serverapi.RecipeItem{item("circuit_board", 1), item("flex_polymer", 3), item("titanium_alloy", 3)},
			[]serverapi.RecipeItem{item("advanced_repair_kit", 1)},
			nil),
		Quantity:  5,
		StationID: "market_prime",
		Inputs: []PlanInputRow{
			{ItemID: "circuit_board", Need: 5, HaveCargo: 0, HaveStorage: 3, Short: 2},
			{ItemID: "flex_polymer", Need: 15, HaveCargo: 0, HaveStorage: 20},
			{ItemID: "titanium_alloy", Need: 15, HaveCargo: 2, HaveStorage: 18},
		},
		Ready: false,
	}
	res.Recipe.CraftingTime = 12

	got := FormatPlan(res)
	want := loadGolden(t, "golden_plan_direct_short.txt")
	if got != want {
		t.Errorf("plan-short format mismatch.\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestFormatPlanDirectReady(t *testing.T) {
	res := &PlanResult{
		Recipe: recipe("alloy_titanium_ingot", "Titanium Alloy Ingot", "Refining",
			[]serverapi.RecipeItem{item("iron_ore", 3), item("titanium_ore", 2)},
			[]serverapi.RecipeItem{item("titanium_alloy", 2)}, nil),
		Quantity: 10,
		Inputs: []PlanInputRow{
			{ItemID: "iron_ore", Need: 30, HaveCargo: 12, HaveStorage: 450},
			{ItemID: "titanium_ore", Need: 20, HaveStorage: 380},
		},
		Ready: true,
	}
	res.Recipe.CraftingTime = 6

	got := FormatPlan(res)
	want := loadGolden(t, "golden_plan_direct_ready.txt")
	if got != want {
		t.Errorf("plan-ready format mismatch.\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

// loadGolden reads a testdata fixture; with -update it writes the actual
// output back so the goldens can be regenerated when the format intentionally
// changes.
var updateGoldens = false

func loadGolden(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	if updateGoldens {
		t.Fatalf("regenerate %s manually if format changes", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
```

- [ ] **Step 2: Run the test to confirm it fails**

Run: `go test ./pkg/craftplan/ -run TestFormat -v`
Expected: FAIL — `undefined: FormatCraftableCompact` or `read testdata/golden_craftable_compact.txt: no such file`.

- [ ] **Step 3: Create `pkg/craftplan/format.go`**

```go
package craftplan

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
	"text/tabwriter"
)

// FormatCraftableOpts controls FormatCraftableCompact rendering.
type FormatCraftableOpts struct {
	StationID string
	Reachable bool
}

// FormatCraftableCompact renders the compact table view of craftable rows.
// Matches the layout in the design spec.
func FormatCraftableCompact(rows []CraftableRow, opts FormatCraftableOpts) string {
	var b bytes.Buffer

	station := opts.StationID
	if station == "" {
		station = "(not docked)"
	}

	if opts.Reachable {
		direct := 0
		for _, r := range rows {
			if r.Depth <= 1 {
				direct++
			}
		}
		fmt.Fprintf(&b, "%d recipes reachable at %s (%d direct, %d via 1+ intermediate craft)\n\n",
			len(rows), station, direct, len(rows)-direct)
	} else {
		fmt.Fprintf(&b, "%d recipes buildable at %s (cargo + storage; skill-gated; legal)\n\n",
			len(rows), station)
	}

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	if opts.Reachable {
		fmt.Fprintln(tw, "RECIPE\tOUTPUT\tCATEGORY\tCAN_MAKE\tVIA\tTIME")
	} else {
		fmt.Fprintln(tw, "RECIPE\tOUTPUT\tCATEGORY\tCAN_MAKE\tTIME")
	}
	for _, r := range rows {
		output := fmt.Sprintf("%s x%d", r.OutputItemID, r.OutputQuantity)
		cm := canMakeStr(r.CanMake)
		if opts.Reachable {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%ds\n",
				r.Recipe.ID, output, r.Recipe.Category, cm, depthStr(r.Depth), r.Recipe.CraftingTime)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%ds\n",
				r.Recipe.ID, output, r.Recipe.Category, cm, r.Recipe.CraftingTime)
		}
	}
	tw.Flush()

	fmt.Fprintf(&b, "\n(showing %d / %d; sort: can_make desc. Pass --max N to widen, --detail to drill in.)\n",
		len(rows), len(rows))
	return b.String()
}

func canMakeStr(n int) string {
	if n == math.MaxInt {
		return "∞"
	}
	return fmt.Sprintf("%d", n)
}

func depthStr(d int) string {
	switch {
	case d <= 1:
		return "direct"
	default:
		return fmt.Sprintf("+%d crafts", d-1)
	}
}

// FormatPlan renders a PlanResult to a string matching the design spec.
func FormatPlan(res *PlanResult) string {
	var b bytes.Buffer

	fmt.Fprintf(&b, "plan: %s x%d  (%s, %ds/batch)\n\n",
		res.Recipe.ID, res.Quantity, res.Recipe.Category, res.Recipe.CraftingTime)

	if len(res.BlockedSkill) > 0 {
		// Sort skill IDs so output is stable.
		keys := make([]string, 0, len(res.BlockedSkill))
		for k := range res.BlockedSkill {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintln(&b, "blocked by skill:")
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: need +%d levels\n", k, res.BlockedSkill[k])
		}
		fmt.Fprintln(&b)
	}
	if res.BlockedIllegal {
		fmt.Fprintf(&b, "blocked: recipe is illegal at this station (%s)\n\n", res.StationID)
	}

	// Inputs table.
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	hasFaction := false
	for _, row := range res.Inputs {
		if row.HaveFaction > 0 {
			hasFaction = true
			break
		}
	}
	if hasFaction {
		fmt.Fprintln(tw, "ITEM\tNEED\tCARGO\tSTORAGE\tFACTION\tSHORT")
	} else {
		fmt.Fprintln(tw, "ITEM\tNEED\tCARGO\tSTORAGE\tSHORT")
	}
	short := 0
	for _, row := range res.Inputs {
		shortStr := "–"
		if row.Short > 0 {
			shortStr = fmt.Sprintf("%d ✗", row.Short)
			short++
		}
		if hasFaction {
			fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\n", row.ItemID, row.Need, row.HaveCargo, row.HaveStorage, row.HaveFaction, shortStr)
		} else {
			fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%s\n", row.ItemID, row.Need, row.HaveCargo, row.HaveStorage, shortStr)
		}
	}
	tw.Flush()

	// Intermediates (reachable mode only).
	if len(res.Intermediates) > 0 {
		fmt.Fprintln(&b, "\nintermediate crafts needed (in order):")
		for i, ic := range res.Intermediates {
			fmt.Fprintf(&b, "  %d. %s\tx%d\t→ %s\n", i+1, ic.RecipeID, ic.BatchesNeeded, ic.OutputItemID)
		}
	}

	// Footer.
	fmt.Fprintln(&b)
	switch {
	case res.Ready:
		fmt.Fprintln(&b, "summary: ✓ ready to craft")
		fmt.Fprintf(&b, "→ craft %s %d\n", res.Recipe.ID, res.Quantity)
	case short > 0:
		// Build a brief "(need N x, M y)" tail.
		var tail []string
		for _, row := range res.Inputs {
			if row.Short > 0 {
				tail = append(tail, fmt.Sprintf("%d %s", row.Short, row.ItemID))
				if len(tail) == 3 {
					break
				}
			}
		}
		fmt.Fprintf(&b, "summary: ✗ %d input(s) short (need %s)\n", short, strings.Join(tail, ", "))
	default:
		fmt.Fprintln(&b, "summary: ✗ blocked (see notes above)")
	}
	return b.String()
}
```

- [ ] **Step 4: Create the golden files**

Run the tests once with output captured to generate the expected text manually, then save. The exact content (with `text/tabwriter` settings padding=2, minwidth=0) is what the assertions expect. Create:

`pkg/craftplan/testdata/golden_craftable_compact.txt`:

```
2 recipes buildable at market_prime_exchange (cargo + storage; skill-gated; legal)

RECIPE                        OUTPUT                  CATEGORY     CAN_MAKE  TIME
alloy_titanium_ingot          titanium_alloy x2       Refining     47        6s
assemble_advanced_repair_kit  advanced_repair_kit x1  Consumables  31        12s

(showing 2 / 2; sort: can_make desc. Pass --max N to widen, --detail to drill in.)
```

`pkg/craftplan/testdata/golden_plan_direct_short.txt`:

```
plan: assemble_advanced_repair_kit x5  (Consumables, 12s/batch)

ITEM            NEED  CARGO  STORAGE  SHORT
circuit_board   5     0      3        2 ✗
flex_polymer    15    0      20       –
titanium_alloy  15    2      18       –

summary: ✗ 1 input(s) short (need 2 circuit_board)
```

`pkg/craftplan/testdata/golden_plan_direct_ready.txt`:

```
plan: alloy_titanium_ingot x10  (Refining, 6s/batch)

ITEM          NEED  CARGO  STORAGE  SHORT
iron_ore      30    12     450      –
titanium_ore  20    0      380      –

summary: ✓ ready to craft
→ craft alloy_titanium_ingot 10
```

Notes on whitespace: tabwriter chooses minimum padding such that every column-separator is a multiple of `tabwidth=0` (so columns abut by `padding=2`). When generating these for the first time, run `go test ./pkg/craftplan/ -run TestFormatCraftableCompact -v` once, copy the `GOT:` output verbatim into the golden file, then re-run to confirm.

- [ ] **Step 5: Generate the goldens by running each test once and copying the actual output**

Run: `go test ./pkg/craftplan/ -run TestFormatCraftableCompact -v 2>&1 | tee /tmp/got1.txt`
Then extract the `GOT:` section and save it as the golden. Repeat for the other two tests.

- [ ] **Step 6: Run all tests to confirm they pass**

Run: `go test ./pkg/craftplan/ -v`
Expected: PASS for all tests.

- [ ] **Step 7: Lint**

Run: `golangci-lint run ./pkg/craftplan/`
Expected: `0 issues.`

- [ ] **Step 8: Commit**

```bash
git add pkg/craftplan/format.go pkg/craftplan/format_test.go pkg/craftplan/testdata/
git commit -m "feat(craftplan): compact table + plan renderers with golden tests"
```

---

## Task 8: play_as Source adapter

**Files:**
- Create: `cmd/tools/play_as/craftable.go`

- [ ] **Step 1: Create `cmd/tools/play_as/craftable.go` with the adapter and helpers**

```go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/rsned/spacemolt/pkg/craftplan"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// playAsSource adapts game.GameClient + the crafting DB to craftplan.Source.
// One instance is created per `craftable` / `plan` REPL invocation; the
// Engine on top caches the recipe catalog across calls within the session.
type playAsSource struct {
	client     game.GameClient
	craftingDB *sql.DB // may be nil → BOM and IllegalAt return empty/errors
}

func newPlayAsSource(client game.GameClient, craftingDB *sql.DB) *playAsSource {
	return &playAsSource{client: client, craftingDB: craftingDB}
}

func (s *playAsSource) Recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error) {
	if err := s.client.GetRecipes(ctx); err != nil {
		return nil, fmt.Errorf("get_recipes: %w", err)
	}
	raw := s.client.GetRawJSON("recipes")
	if len(raw) == 0 {
		return nil, fmt.Errorf("get_recipes returned empty payload")
	}
	var resp serverapi.GetRecipesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode get_recipes: %w", err)
	}
	return resp.Recipes, nil
}

func (s *playAsSource) Inventory(ctx context.Context, includeFaction bool) (craftplan.Inventory, error) {
	inv := craftplan.Inventory{
		Cargo:   map[string]int{},
		Storage: map[string]int{},
	}

	// Cargo from current state.
	state := s.client.GetState()
	if state != nil {
		for _, c := range state.Ship.Cargo {
			inv.Cargo[c.ItemID] += int(c.Quantity)
		}
	}

	// Personal storage at current station.
	if err := s.client.ViewStorage(ctx); err != nil {
		return inv, fmt.Errorf("view_storage: %w", err)
	}
	if raw := s.client.GetRawJSON("storage"); len(raw) > 0 {
		var st serverapi.ViewStorageResponse
		if err := json.Unmarshal(raw, &st); err == nil {
			for _, item := range st.Items {
				inv.Storage[item.ItemID] += item.Quantity
			}
		}
	}

	if includeFaction {
		inv.Faction = map[string]int{}
		if err := s.client.ViewFactionStorage(ctx); err == nil {
			if raw := s.client.GetRawJSON("faction_storage"); len(raw) > 0 {
				var fs serverapi.ViewStorageResponse
				if err := json.Unmarshal(raw, &fs); err == nil {
					for _, item := range fs.Items {
						inv.Faction[item.ItemID] += item.Quantity
					}
				}
			}
		}
		// Errors (e.g. not in a faction) are silently ignored; faction stays empty.
	}
	return inv, nil
}

func (s *playAsSource) Skills(ctx context.Context) (map[string]int, error) {
	state := s.client.GetState()
	if state == nil {
		return map[string]int{}, nil
	}
	out := make(map[string]int, len(state.Player.Skills))
	for id, skill := range state.Player.Skills {
		out[id] = int(skill.Level)
	}
	return out, nil
}

func (s *playAsSource) CurrentStationID(ctx context.Context) (string, error) {
	state := s.client.GetState()
	if state == nil {
		return "", nil
	}
	// POI is the docked station id when Doc=true.
	if !state.Doc {
		return "", nil
	}
	return state.POI, nil
}

func (s *playAsSource) IllegalAt(ctx context.Context, stationID string) (map[string]bool, error) {
	if s.craftingDB == nil || stationID == "" {
		return map[string]bool{}, nil
	}
	rows, err := s.craftingDB.QueryContext(ctx,
		`SELECT recipe_id FROM illegal_recipes WHERE legal_location != ? OR legal_location = '' OR legal_location IS NULL`,
		stationID,
	)
	if err != nil {
		return nil, fmt.Errorf("query illegal_recipes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan illegal_recipes: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (s *playAsSource) BOM(ctx context.Context, recipeIDs []string) (map[string][]craftplan.BOMRow, error) {
	if s.craftingDB == nil {
		return nil, fmt.Errorf("crafting DB not configured (pass --crafting-db or set CRAFTING_DB)")
	}
	if len(recipeIDs) == 0 {
		return map[string][]craftplan.BOMRow{}, nil
	}

	placeholders := make([]string, len(recipeIDs))
	args := make([]any, len(recipeIDs))
	for i, id := range recipeIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := fmt.Sprintf(
		`SELECT target_id, base_item_id, quantity, recipe_path
		 FROM bill_of_materials
		 WHERE target_type = 'item' AND target_id IN (%s)`,
		joinStrings(placeholders, ","),
	)

	rows, err := s.craftingDB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query bill_of_materials: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string][]craftplan.BOMRow{}
	for rows.Next() {
		var targetID, baseItem, pathJSON string
		var qty int
		if err := rows.Scan(&targetID, &baseItem, &qty, &pathJSON); err != nil {
			return nil, fmt.Errorf("scan bill_of_materials: %w", err)
		}
		var path []string
		if pathJSON != "" {
			if err := json.Unmarshal([]byte(pathJSON), &path); err != nil {
				// recipe_path malformed → skip just the path; row still counts toward base materials.
				path = nil
			}
		}
		out[targetID] = append(out[targetID], craftplan.BOMRow{
			BaseItemID: baseItem,
			Quantity:   qty,
			RecipePath: path,
		})
	}
	return out, rows.Err()
}

// joinStrings is a small helper because cmd/tools/play_as doesn't import
// strings in this file path and we want to keep this file's import block
// minimal. Equivalent to strings.Join.
func joinStrings(parts []string, sep string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	n := len(sep) * (len(parts) - 1)
	for _, p := range parts {
		n += len(p)
	}
	b := make([]byte, 0, n)
	b = append(b, parts[0]...)
	for _, p := range parts[1:] {
		b = append(b, sep...)
		b = append(b, p...)
	}
	return string(b)
}
```

- [ ] **Step 2: Verify the file compiles**

Run: `go build ./cmd/tools/play_as/...`
Expected: (no output, exit code 0). If the build fails with "no field POI on State" or "no method ViewFactionStorage", check the actual struct/method names in the live codebase and adjust — the spec assumes the names but the codebase is the ground truth.

- [ ] **Step 3: Resolve any name mismatches**

Run: `grep -nE "type State struct|func \(c \*Client\) ViewFactionStorage|func \(c \*Client\) ViewStorage" pkg/game/*.go`

Use the actual field/method names. Common fixes:
- `state.POI` may be `state.System.DockedPOI` or `state.CurrentPOI`. Update accordingly.
- `state.Ship.Cargo` may be `state.Ship.CargoItems`.
- `ViewFactionStorage` may not exist — if so, remove the faction branch and document the limitation in the help text.

- [ ] **Step 4: Lint**

Run: `golangci-lint run ./cmd/tools/play_as/`
Expected: `0 issues.`

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/craftable.go
git commit -m "feat(play_as): craftplan Source adapter wrapping GameClient + crafting DB"
```

---

## Task 9: REPL command wiring + flag parsing

**Files:**
- Modify: `cmd/tools/play_as/craftable.go` (add the REPL handlers)
- Modify: `cmd/tools/play_as/main.go` (dispatch + help text)

- [ ] **Step 1: Add the two REPL handlers at the bottom of `cmd/tools/play_as/craftable.go`**

```go
// handleCraftable wires the `craftable` REPL command. parts[0] is the verb,
// parts[1:] are flags / args.
func handleCraftable(client game.GameClient, ctx context.Context, parts []string, craftingDB *sql.DB, format outputFormat) error {
	flags := parseFlagArgs(parts[1:],
		"reachable", "category", "search", "include-faction",
		"detail", "recipe", "refresh", "max",
	)

	opts := craftplan.CraftableOpts{
		Reachable:      flagBool(flags["reachable"]),
		IncludeFaction: flagBool(flags["include-faction"]),
		Refresh:        flagBool(flags["refresh"]),
	}
	if v, ok := flags["category"]; ok {
		opts.CategoryFilter, _ = flagString(v)
	}
	if v, ok := flags["search"]; ok {
		opts.SearchFilter, _ = flagString(v)
	}
	if v, ok := flags["recipe"]; ok {
		opts.OneRecipe, _ = flagString(v)
	}
	if v, ok := flags["max"]; ok {
		if n, ok := flagInt(v); ok {
			opts.Max = n
		}
	}
	detail := flagBool(flags["detail"])

	src := newPlayAsSource(client, craftingDB)
	eng := craftplan.New(src)
	rows, err := eng.Craftable(ctx, opts)
	if err != nil {
		// Pretty-print BOM unavailability instead of a raw error.
		if errors.Is(err, craftplan.ErrBOMUnavailable) {
			fmt.Printf("BOM unavailable: %v\n  Install/update the crafting DB or omit --reachable.\n", err)
			return nil
		}
		return err
	}

	stationID, _ := src.CurrentStationID(ctx)
	if detail {
		fmt.Print(craftplan.FormatCraftableDetail(rows, craftplan.FormatCraftableOpts{
			StationID: stationID,
			Reachable: opts.Reachable,
		}))
	} else {
		fmt.Print(craftplan.FormatCraftableCompact(rows, craftplan.FormatCraftableOpts{
			StationID: stationID,
			Reachable: opts.Reachable,
		}))
	}
	return nil
}

// handlePlan wires the `plan <id> [qty]` REPL command.
func handlePlan(client game.GameClient, ctx context.Context, parts []string, craftingDB *sql.DB, format outputFormat) error {
	positional, flags := partitionFlags(parts[1:])
	if len(positional) < 1 {
		return fmt.Errorf("usage: plan <recipe-id-or-item-id> [qty] [--reachable] [--include-faction] [--detail]")
	}

	opts := craftplan.PlanOpts{
		ID:             positional[0],
		Quantity:       1,
		Reachable:      strings.EqualFold(flags["reachable"], "true") || flags["reachable"] == "1" || flags["reachable"] == "",
		IncludeFaction: strings.EqualFold(flags["include-faction"], "true") || flags["include-faction"] == "1",
		Refresh:        strings.EqualFold(flags["refresh"], "true") || flags["refresh"] == "1",
	}
	// flags from partitionFlags are string-keyed strings; --reachable with no
	// value should mean "true", but the above tri-state catches the "no value"
	// case via flags["reachable"]==""; if reachable isn't in flags at all, ok==false.
	if _, has := flags["reachable"]; !has {
		opts.Reachable = false
	}
	if _, has := flags["include-faction"]; !has {
		opts.IncludeFaction = false
	}
	if _, has := flags["refresh"]; !has {
		opts.Refresh = false
	}
	if len(positional) >= 2 {
		qty, err := strconv.Atoi(positional[1])
		if err != nil {
			return fmt.Errorf("invalid qty %q: %w", positional[1], err)
		}
		opts.Quantity = qty
	}

	src := newPlayAsSource(client, craftingDB)
	eng := craftplan.New(src)
	res, err := eng.Plan(ctx, opts)
	if err != nil {
		if errors.Is(err, craftplan.ErrBOMUnavailable) {
			fmt.Printf("BOM unavailable: %v\n  Install/update the crafting DB or omit --reachable.\n", err)
			return nil
		}
		return err
	}
	fmt.Print(craftplan.FormatPlan(res))
	return nil
}
```

- [ ] **Step 2: Add the imports the new code needs to `cmd/tools/play_as/craftable.go`**

Top of the file:

```go
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/craftplan"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)
```

- [ ] **Step 3: Add `FormatCraftableDetail` to `pkg/craftplan/format.go`**

Append:

```go
// FormatCraftableDetail renders each row as its own per-recipe block instead
// of one table. Use when the operator wants depth over breadth (<5 results
// or one --recipe).
func FormatCraftableDetail(rows []CraftableRow, opts FormatCraftableOpts) string {
	var b bytes.Buffer
	station := opts.StationID
	if station == "" {
		station = "(not docked)"
	}
	fmt.Fprintf(&b, "%d recipe(s) at %s\n", len(rows), station)
	for i, r := range rows {
		if i > 0 {
			fmt.Fprintln(&b, "─────────────────────────────────────────────")
		}
		fmt.Fprintf(&b, "\n%s — %s (%s, %ds/batch, can_make=%s)\n",
			r.Recipe.ID, r.Recipe.Name, r.Recipe.Category, r.Recipe.CraftingTime, canMakeStr(r.CanMake))
		if len(r.Recipe.Inputs) > 0 {
			fmt.Fprintln(&b, "  inputs:")
			for _, in := range r.Recipe.Inputs {
				fmt.Fprintf(&b, "    %s x%d\n", in.ItemID, in.Quantity)
			}
		}
		if len(r.Recipe.Outputs) > 0 {
			fmt.Fprintln(&b, "  outputs:")
			for _, out := range r.Recipe.Outputs {
				fmt.Fprintf(&b, "    %s x%d\n", out.ItemID, out.Quantity)
			}
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Wire the dispatch in `cmd/tools/play_as/main.go`**

Find the dispatch switch (around the existing `case "craft":` block) and add immediately after the `case "recipes", "get_recipes":` block:

```go
case "craftable":
	return handleCraftable(client, ctx, parts, craftingDB, format)

case "plan":
	return handlePlan(client, ctx, parts, craftingDB, format)
```

`craftingDB` must be available in this scope. Add a `var craftingDB *sql.DB` package-level variable at the top of main.go (near `globalKB`) and open it once during `runREPL` setup using `resolveDBPath` (copy the helper from `cmd/tools/bulk-buy-order/main.go` or import a shared path).

Simplest: copy `resolveDBPath` into `craftable.go` as a small helper and call it lazily from `handleCraftable` / `handlePlan` — opening the DB on first need and caching the handle in a package-level `var`. This avoids a startup penalty for users who never run craft commands.

```go
// In cmd/tools/play_as/craftable.go:
var (
	craftingDBMu sync.Mutex
	craftingDB   *sql.DB
)

func ensureCraftingDB() *sql.DB {
	craftingDBMu.Lock()
	defer craftingDBMu.Unlock()
	if craftingDB != nil {
		return craftingDB
	}
	path := os.Getenv("CRAFTING_DB")
	if path == "" {
		defaultPath := "../../spacemolt-crafting-server/database/crafting.db"
		if _, err := os.Stat(defaultPath); err == nil {
			path = defaultPath
		}
	}
	if path == "" {
		return nil
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil
	}
	craftingDB = db
	return db
}
```

Update the dispatch in main.go to call `ensureCraftingDB()`:

```go
case "craftable":
	return handleCraftable(client, ctx, parts, ensureCraftingDB(), format)

case "plan":
	return handlePlan(client, ctx, parts, ensureCraftingDB(), format)
```

Imports to add to `craftable.go`: `os`, `sync`. Plus the sqlite driver registration (the bulk-buy-order file already imports `_ "modernc.org/sqlite"`; we need the same):

```go
import (
	// ... existing imports ...
	"os"
	"sync"

	_ "modernc.org/sqlite"
)
```

- [ ] **Step 5: Update the `help` command output in main.go**

Find the existing `case "help":` block (or whatever prints the long help) and add two lines near the crafting section:

```
  craftable [--reachable] [--category C] [--search S]  - what you can build now (--detail for per-recipe view)
  plan <recipe-or-item-id> [qty] [--reachable]         - gap analysis to a target; prints craft cmd when ready
```

- [ ] **Step 6: Build everything**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 7: Run all play_as tests**

Run: `go test ./cmd/tools/play_as/...`
Expected: PASS (existing tests untouched, no new tests yet).

- [ ] **Step 8: Lint**

Run: `golangci-lint run ./cmd/tools/play_as/ ./pkg/craftplan/`
Expected: `0 issues.`

- [ ] **Step 9: Commit**

```bash
git add cmd/tools/play_as/main.go cmd/tools/play_as/craftable.go pkg/craftplan/format.go
git commit -m "feat(play_as): craftable + plan REPL commands"
```

---

## Task 10: Integration smoke test in cmd/tools/play_as

**Files:**
- Create: `cmd/tools/play_as/craftable_test.go`
- Create: `cmd/tools/play_as/testdata/get_recipes.json`
- Create: `cmd/tools/play_as/testdata/view_storage.json`

The point: prove the Source adapter wiring is correct by feeding it captured server JSON and asserting the Engine produces the expected output. Does not require a live game server.

- [ ] **Step 1: Create the fixtures**

`cmd/tools/play_as/testdata/get_recipes.json`:

```json
{
  "action": "get_recipes",
  "recipes": {
    "alloy_titanium_ingot": {
      "id": "alloy_titanium_ingot",
      "name": "Titanium Alloy Ingot",
      "category": "Refining",
      "inputs": [
        {"item_id": "iron_ore", "quantity": 3},
        {"item_id": "titanium_ore", "quantity": 2}
      ],
      "outputs": [
        {"item_id": "titanium_alloy", "quantity": 2}
      ],
      "crafting_time": 6
    }
  }
}
```

`cmd/tools/play_as/testdata/view_storage.json`:

```json
{
  "action": "view_storage",
  "items": [
    {"item_id": "iron_ore", "quantity": 450},
    {"item_id": "titanium_ore", "quantity": 380}
  ]
}
```

- [ ] **Step 2: Write the integration test**

```go
package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/rsned/spacemolt/pkg/craftplan"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// fixtureSource is a craftplan.Source backed by the captured JSON fixtures
// in testdata/. Exercises the same code paths the production adapter does
// without needing a live game client or SQLite DB.
type fixtureSource struct {
	recipes map[string]serverapi.Recipe
	storage map[string]int
}

func loadFixtureSource(t *testing.T) *fixtureSource {
	t.Helper()
	rawR, err := os.ReadFile("testdata/get_recipes.json")
	if err != nil {
		t.Fatalf("read get_recipes fixture: %v", err)
	}
	var rResp serverapi.GetRecipesResponse
	if err := json.Unmarshal(rawR, &rResp); err != nil {
		t.Fatalf("decode get_recipes fixture: %v", err)
	}
	rawS, err := os.ReadFile("testdata/view_storage.json")
	if err != nil {
		t.Fatalf("read view_storage fixture: %v", err)
	}
	var sResp serverapi.ViewStorageResponse
	if err := json.Unmarshal(rawS, &sResp); err != nil {
		t.Fatalf("decode view_storage fixture: %v", err)
	}
	storage := map[string]int{}
	for _, it := range sResp.Items {
		storage[it.ItemID] += it.Quantity
	}
	return &fixtureSource{recipes: rResp.Recipes, storage: storage}
}

func (f *fixtureSource) Recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error) {
	return f.recipes, nil
}
func (f *fixtureSource) Inventory(ctx context.Context, includeFaction bool) (craftplan.Inventory, error) {
	return craftplan.Inventory{Cargo: map[string]int{}, Storage: f.storage}, nil
}
func (f *fixtureSource) Skills(ctx context.Context) (map[string]int, error) {
	return map[string]int{}, nil
}
func (f *fixtureSource) CurrentStationID(ctx context.Context) (string, error) {
	return "market_prime", nil
}
func (f *fixtureSource) IllegalAt(ctx context.Context, stationID string) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (f *fixtureSource) BOM(ctx context.Context, recipeIDs []string) (map[string][]craftplan.BOMRow, error) {
	return map[string][]craftplan.BOMRow{}, nil
}

func TestCraftable_FromFixtures(t *testing.T) {
	src := loadFixtureSource(t)
	eng := craftplan.New(src)
	rows, err := eng.Craftable(context.Background(), craftplan.CraftableOpts{})
	if err != nil {
		t.Fatalf("Craftable: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Recipe.ID != "alloy_titanium_ingot" {
		t.Errorf("recipe id = %s, want alloy_titanium_ingot", r.Recipe.ID)
	}
	// can_make = min(450/3, 380/2) = min(150, 190) = 150
	if r.CanMake != 150 {
		t.Errorf("can_make = %d, want 150", r.CanMake)
	}
}
```

- [ ] **Step 3: Run the integration test**

Run: `go test ./cmd/tools/play_as/ -run TestCraftable_FromFixtures -v`
Expected: PASS.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Lint**

Run: `golangci-lint run ./pkg/craftplan/ ./cmd/tools/play_as/`
Expected: `0 issues.`

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/craftable_test.go cmd/tools/play_as/testdata/
git commit -m "test(play_as): integration smoke test for craftable adapter wiring"
```

---

## Task 11: Final manual smoke + README/help update

**Files:**
- Modify: `cmd/tools/play_as/README.md` (if it exists; create entry otherwise)

- [ ] **Step 1: Check for an existing README to update**

Run: `ls cmd/tools/play_as/README.md`

If it exists, add a "Crafting smarts" section. If not, skip this step.

- [ ] **Step 2: Add a Crafting Smarts section to the README**

```markdown
## Crafting smarts

Two commands help decide what to craft and what's blocking a target build.

### `craftable`

List every recipe you can build right now (cargo + current-station storage, skill-gated, station-legal).

```
craftable                       # immediately buildable, compact table
craftable --reachable           # also list recipes reachable via intermediate crafts
craftable --category Weapons    # substring filter on category
craftable --search lance        # substring filter on name and outputs
craftable --include-faction     # also count faction storage
craftable --detail              # per-recipe drill-down (no table)
craftable --recipe <id> --detail  # detail for one specific recipe
craftable --refresh             # bypass session recipe-catalog cache
craftable --max 200             # widen the table (default 100)
```

### `plan <recipe-or-item-id> [qty]`

Gap analysis to a target. If the agent has everything, prints the literal `craft …` command. Otherwise shows the shortfall by item.

```
plan alloy_titanium_ingot 10
plan titanium_alloy            # accepts item_id; picks lowest-skill alternative
plan build_emergency_warp_device --reachable  # flat ore/gas shortfall via BOM
```

Crafting DB: `--reachable` needs the crafting DB (`bill_of_materials` table). Set `CRAFTING_DB=path/to/crafting.db` or keep the default `../../spacemolt-crafting-server/database/crafting.db`.
```

- [ ] **Step 3: Manual smoke (operator runs this; not a CI test)**

```bash
go run ./cmd/tools/play_as/ --debug craftsman-1
```

In the REPL:

```
> craftable
> craftable --reachable --category Refining
> plan assemble_advanced_repair_kit 3
> plan titanium_alloy --reachable
```

Verify the tables look like the spec output examples. If columns drift, regenerate the golden files (`go test -run TestFormat -v ./pkg/craftplan/` and copy `GOT:` blocks).

- [ ] **Step 4: Commit if README changed**

```bash
git add cmd/tools/play_as/README.md
git commit -m "docs(play_as): document craftable + plan commands"
```

---

## Self-Review

Spec coverage walk:

- ✅ `craftable` command with all flags from spec — Tasks 3, 7, 9.
- ✅ `plan` command with all flags from spec — Tasks 4, 7, 9.
- ✅ `pkg/craftplan` with Source interface — Task 2.
- ✅ Direct algorithm (skill/legality/material gates) — Task 3.
- ✅ Plan direct gap math — Task 4.
- ✅ Recipe vs item_id resolution + fuzzy match — Task 5.
- ✅ Reachable algorithm via BOM — Task 6.
- ✅ Output format (compact + detail) — Task 7.
- ✅ play_as adapter — Task 8.
- ✅ REPL wiring + help — Task 9.
- ✅ Fixture-based smoke test — Task 10.
- ✅ Manual smoke + README — Task 11.

Edge cases:
- ✅ Missing crafting DB → ErrBOMUnavailable, friendly REPL message (Tasks 6, 9).
- ✅ Empty inputs → ∞ (Task 3, format.go).
- ✅ Faction off → faction column omitted (Task 4, Task 7).
- ✅ Recipe typo → fuzzy suggestions (Task 5).
- ✅ qty ≤ 0 → friendly error (Task 4).
- ✅ Skill block + illegal flag carried on PlanResult (Task 4).

Type-name consistency: `CraftableRow`, `PlanResult`, `PlanInputRow`, `IntermediateCraft`, `BOMRow`, `Inventory`, `Source`, `Engine` — used identically across Tasks 1–10. `Engine.Craftable` / `Engine.Plan` are the only public methods. `FormatCraftableCompact` / `FormatCraftableDetail` / `FormatPlan` are the only public formatters. No naming drift.

Placeholder scan: no TBD/TODO/"implement later". Every code block contains complete, runnable code or a clear edit instruction with the surrounding context shown.

Two anticipated runtime adjustments flagged for the implementer (Task 8 Step 3): `state.POI` and `state.Ship.Cargo` field names + `ViewFactionStorage` method existence must be verified against the live codebase. The plan tells the implementer how to verify and how to degrade if names differ.
