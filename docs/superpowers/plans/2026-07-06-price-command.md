# `price` Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `play_as` REPL command `price <item_id>` that suggests a per-unit `create_sell_order` price = component/ore market cost + a separate 20% margin line, and flags items whose current market price far exceeds their build cost (underpriced).

**Architecture:** A pure, unit-tested `pkg/pricing` package does the pricing math and market lookups (reusing `finditem.Find` for ask-side prices, so it needs no navigation code of its own). A thin `cmd/tools/play_as/price.go` glue resolves the item's recipe(s) and bill-of-materials via the existing `craftplan`/`playAsSource` plumbing, calls `pkg/pricing` once per decomposition, and renders the tables.

**Tech Stack:** Go 1.24+, `modernc.org/sqlite`, existing `pkg/market`, `pkg/finditem`, `pkg/knowledge`, `pkg/navigation`, `pkg/craftplan`, `pkg/game/serverapi`.

## Global Constraints

- Go 1.24+; use `range`-over-int and `b.Loop()` where applicable.
- Every new file must pass `golangci-lint` with no new findings.
- Run `go build ./...` and `go test ./...` before each commit.
- Any sleeps/pauses must use constants in `pkg/game/constants.go` (this feature needs none).
- **Normalization (verified against crafting.db + kb `pkg/bom/calculator.go`):**
  - Recipe mode: recipe inputs are **per crafting run**; divide the rolled-up cost by output-units-per-run (`recipe.Outputs[0].Quantity`, default 1) for a per-unit figure.
  - BoM mode: `bill_of_materials.quantity` is **already per single output unit** (`ceilDiv(mat.Quantity, outputQty)` in the generator); use `outputUnits = 1` (no division).
- **Recipe identity:** `recipe.ID` is NOT the output item_id. Resolve recipes for an item by scanning `recipe.Outputs` for a matching `ItemID`.
- **Component pricing (MVP):** single best ask × qty, NOT an order-book depth-walk (deferred — see memory `project_price_command_depthwalk`).

---

## File Structure

- `pkg/pricing/pricing.go` — types (`Component`, `PricedComponent`, `Basis`, `PriceReport`) + `rollUp`, `askStats`, `classify`, `Report`. Pure/DB-backed core.
- `pkg/pricing/pricing_test.go` — unit tests (seeded temp market DB + `MemoryKB`, hand-built `finditem.Result` slices).
- `cmd/tools/play_as/price.go` — `handlePrice` glue: arg parse, recipe/BoM resolution, per-mode `pricing.Report` calls, multi-recipe selection, rendering.
- `cmd/tools/play_as/price_test.go` — tests for the pure glue helpers + rendered-table golden.
- `cmd/tools/play_as/main.go` — add `case "price":` dispatch + one help line (modify).

---

## Task 1: `pkg/pricing` types + roll-up math

**Files:**
- Create: `pkg/pricing/pricing.go`
- Test: `pkg/pricing/pricing_test.go`

**Interfaces:**
- Produces: `Component{ItemID string; Qty float64}`, `PricedComponent{Component; NearbyUnit,MktUnit float64; NearbyFound,MktFound bool}`, `Basis{BuildCost,PerUnit,Margin,Suggested float64; Covered,Total int}` with method `Complete() bool`, and `func rollUp(comps []PricedComponent, outputUnits int, marginPct float64) (nearby, mkt Basis)`.

- [ ] **Step 1: Write the failing test**

Create `pkg/pricing/pricing_test.go`:

```go
package pricing

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestRollUpBothBasesFound(t *testing.T) {
	comps := []PricedComponent{
		{Component: Component{ItemID: "iron_ore", Qty: 20}, NearbyUnit: 12, MktUnit: 14, NearbyFound: true, MktFound: true},
		{Component: Component{ItemID: "copper_ore", Qty: 8}, NearbyUnit: 30, MktUnit: 28, NearbyFound: true, MktFound: true},
	}
	// nearby build = 20*12 + 8*30 = 480; per unit (÷5) = 96; +20% = 19.2; suggested 115.2
	nearby, mkt := rollUp(comps, 5, 20)
	if !approx(nearby.BuildCost, 480) || !approx(nearby.PerUnit, 96) || !approx(nearby.Margin, 19.2) || !approx(nearby.Suggested, 115.2) {
		t.Fatalf("nearby wrong: %+v", nearby)
	}
	if nearby.Covered != 2 || nearby.Total != 2 || !nearby.Complete() {
		t.Fatalf("nearby coverage wrong: %+v", nearby)
	}
	// mkt build = 20*14 + 8*28 = 504; per unit = 100.8
	if !approx(mkt.BuildCost, 504) || !approx(mkt.PerUnit, 100.8) {
		t.Fatalf("mkt wrong: %+v", mkt)
	}
}

func TestRollUpMissingNearbyComponentContributesZeroAndMarksIncomplete(t *testing.T) {
	comps := []PricedComponent{
		{Component: Component{ItemID: "iron_ore", Qty: 10}, NearbyUnit: 5, MktUnit: 5, NearbyFound: true, MktFound: true},
		{Component: Component{ItemID: "rare_ore", Qty: 2}, MktUnit: 100, MktFound: true}, // no nearby price
	}
	nearby, mkt := rollUp(comps, 1, 0)
	if !approx(nearby.BuildCost, 50) { // only iron_ore counted
		t.Fatalf("nearby build should skip missing: %+v", nearby)
	}
	if nearby.Covered != 1 || nearby.Total != 2 || nearby.Complete() {
		t.Fatalf("nearby should be incomplete: %+v", nearby)
	}
	if !approx(mkt.BuildCost, 250) || !mkt.Complete() { // 50 + 200
		t.Fatalf("mkt should be complete: %+v", mkt)
	}
}

func TestRollUpOutputUnitsFloorsAtOne(t *testing.T) {
	comps := []PricedComponent{{Component: Component{ItemID: "x", Qty: 3}, NearbyUnit: 10, MktUnit: 10, NearbyFound: true, MktFound: true}}
	nearby, _ := rollUp(comps, 0, 0) // outputUnits 0 must be treated as 1
	if !approx(nearby.PerUnit, 30) {
		t.Fatalf("perUnit with outputUnits<=0 should divide by 1: %+v", nearby)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/pricing/ -run TestRollUp -v`
Expected: FAIL — `undefined: rollUp` / package has no Go files.

- [ ] **Step 3: Write minimal implementation**

Create `pkg/pricing/pricing.go`:

```go
// Package pricing computes a suggested sell price for a craftable item from
// the live market cost of its inputs plus a profit margin. It powers the
// play_as `price` command: given an item's decomposition (recipe inputs or
// bill-of-materials ore), it prices each component two ways — Nearby (cheapest
// ask within N jumps) and Market-wide (mean ask across stations) — rolls the
// costs up per output unit, adds a margin line, and compares the result to the
// finished good's own current market price.
package pricing

// Component is one input to price: a recipe input or a BoM base ore.
type Component struct {
	ItemID string
	Qty    float64
}

// PricedComponent annotates a Component with its resolved unit prices on each
// basis. A *Found flag is false when no station offered the component on that
// basis; the unit price is then zero and excluded from the roll-up.
type PricedComponent struct {
	Component
	NearbyUnit  float64
	MktUnit     float64
	NearbyFound bool
	MktFound    bool
}

// Basis is the rolled-up cost on one pricing basis (Nearby or Market-wide).
type Basis struct {
	BuildCost float64 // Σ qty×unit over components priced on this basis
	PerUnit   float64 // BuildCost / outputUnits
	Margin    float64 // PerUnit × marginPct/100
	Suggested float64 // PerUnit + Margin
	Covered   int     // components priced on this basis
	Total     int     // total components
}

// Complete reports whether every component was priced on this basis.
func (b Basis) Complete() bool { return b.Total > 0 && b.Covered == b.Total }

// rollUp turns priced components into the Nearby and Market-wide bases.
// outputUnits <= 0 is treated as 1 so the per-unit conversion degrades to
// identity.
func rollUp(comps []PricedComponent, outputUnits int, marginPct float64) (nearby, mkt Basis) {
	if outputUnits <= 0 {
		outputUnits = 1
	}
	nearby.Total = len(comps)
	mkt.Total = len(comps)
	for _, c := range comps {
		if c.NearbyFound {
			nearby.BuildCost += c.Qty * c.NearbyUnit
			nearby.Covered++
		}
		if c.MktFound {
			mkt.BuildCost += c.Qty * c.MktUnit
			mkt.Covered++
		}
	}
	finish := func(b *Basis) {
		b.PerUnit = b.BuildCost / float64(outputUnits)
		b.Margin = b.PerUnit * marginPct / 100
		b.Suggested = b.PerUnit + b.Margin
	}
	finish(&nearby)
	finish(&mkt)
	return nearby, mkt
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/pricing/ -run TestRollUp -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/pricing/pricing.go pkg/pricing/pricing_test.go
git commit -m "feat(pricing): component roll-up math for suggested sell price"
```

---

## Task 2: `askStats` — nearby-min + market-mean over finditem results

**Files:**
- Modify: `pkg/pricing/pricing.go`
- Test: `pkg/pricing/pricing_test.go`

**Interfaces:**
- Consumes: `finditem.Result` (embeds `market.ItemSeller` with `BestPrice float64`, `SystemID string`, plus `Jumps int`; `Jumps == finditem.JumpsUnknown` (-1) when uncomputable, `>= navigation.RouteInf` when unreachable).
- Produces: `func askStats(results []finditem.Result, hops int) (nearbyUnit float64, nearbyFound bool, mktUnit float64, mktFound bool)`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/pricing/pricing_test.go`:

```go
import (
	// add to the existing import block:
	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

func res(price float64, jumps int) finditem.Result {
	return finditem.Result{ItemSeller: market.ItemSeller{BestPrice: price}, Jumps: jumps}
}

func TestAskStatsNearbyMinWithinHopsAndMktMean(t *testing.T) {
	rs := []finditem.Result{
		res(100, 0), // local
		res(80, 2),  // within 2 hops, cheaper
		res(10, 5),  // far — cheapest overall but outside 2 hops
	}
	nu, nf, mu, mf := askStats(rs, 2)
	if !nf || !approx(nu, 80) { // cheapest within <=2 hops
		t.Fatalf("nearby wrong: found=%v unit=%v", nf, nu)
	}
	// mkt mean over all three asks = (100+80+10)/3
	if !mf || !approx(mu, (100+80+10)/3.0) {
		t.Fatalf("mkt wrong: found=%v unit=%v", mf, mu)
	}
}

func TestAskStatsNoNearbyWhenAllTooFarOrUnknown(t *testing.T) {
	rs := []finditem.Result{res(50, finditem.JumpsUnknown), res(60, navigation.RouteInf), res(70, 4)}
	nu, nf, _, mf := askStats(rs, 2)
	if nf || nu != 0 {
		t.Fatalf("expected no nearby, got found=%v unit=%v", nf, nu)
	}
	if !mf {
		t.Fatalf("mkt should still be found from all asks")
	}
}

func TestAskStatsEmpty(t *testing.T) {
	nu, nf, mu, mf := askStats(nil, 2)
	if nf || mf || nu != 0 || mu != 0 {
		t.Fatalf("empty should yield nothing: %v %v %v %v", nu, nf, mu, mf)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/pricing/ -run TestAskStats -v`
Expected: FAIL — `undefined: askStats`.

- [ ] **Step 3: Write minimal implementation**

Append to `pkg/pricing/pricing.go` (add `"github.com/rsned/spacemolt/pkg/finditem"` and `"github.com/rsned/spacemolt/pkg/navigation"` to imports):

```go
// askStats reduces one item's per-station sell asks (as returned by
// finditem.Find) to a Nearby unit price (cheapest ask reachable within hops)
// and a Market-wide unit price (mean ask across every station). A result with
// Jumps == finditem.JumpsUnknown or Jumps >= navigation.RouteInf is outside
// "nearby" but still counts toward the market-wide mean. Asks of 0 are ignored.
func askStats(results []finditem.Result, hops int) (nearbyUnit float64, nearbyFound bool, mktUnit float64, mktFound bool) {
	var sum float64
	var n int
	for _, r := range results {
		if r.BestPrice <= 0 {
			continue
		}
		sum += r.BestPrice
		n++
		if r.Jumps >= 0 && r.Jumps <= hops {
			if !nearbyFound || r.BestPrice < nearbyUnit {
				nearbyUnit, nearbyFound = r.BestPrice, true
			}
		}
	}
	if n > 0 {
		mktUnit, mktFound = sum/float64(n), true
	}
	return nearbyUnit, nearbyFound, mktUnit, mktFound
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/pricing/ -run TestAskStats -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/pricing/pricing.go pkg/pricing/pricing_test.go
git commit -m "feat(pricing): askStats nearby-min and market-mean over sellers"
```

---

## Task 3: `classify` — under/over/fair verdict

**Files:**
- Modify: `pkg/pricing/pricing.go`
- Test: `pkg/pricing/pricing_test.go`

**Interfaces:**
- Produces: constants `ClassUnder = "UNDERPRICED"`, `ClassOver = "OVERPRICED"`, `ClassFair = "FAIRLY PRICED"`, and `func classify(marketAsk, suggested float64) string` (returns `""` when either input is <= 0).

- [ ] **Step 1: Write the failing test**

Append to `pkg/pricing/pricing_test.go`:

```go
func TestClassify(t *testing.T) {
	cases := []struct {
		ask, sug float64
		want     string
	}{
		{450, 130, ClassUnder}, // 3.46× -> underpriced
		{100, 130, ClassOver},  // 0.77× -> overpriced (market below cost+margin)
		{130, 130, ClassFair},  // 1.0×
		{0, 130, ""},           // no market ask
		{130, 0, ""},           // no suggestion
	}
	for _, c := range cases {
		if got := classify(c.ask, c.sug); got != c.want {
			t.Fatalf("classify(%v,%v)=%q want %q", c.ask, c.sug, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/pricing/ -run TestClassify -v`
Expected: FAIL — `undefined: classify`.

- [ ] **Step 3: Write minimal implementation**

Append to `pkg/pricing/pricing.go`:

```go
// Verdicts comparing the finished good's current market ask to the
// cost-plus-margin suggestion.
const (
	ClassUnder = "UNDERPRICED"
	ClassOver  = "OVERPRICED"
	ClassFair  = "FAIRLY PRICED"
)

// classify compares the finished good's current market ask to the suggested
// price. Thresholds are cosmetic: ask >= 1.3× suggested is underpriced (parts
// cheap vs sale), ask <= 0.9× is overpriced (you'd list above the market).
// Returns "" when either figure is missing.
func classify(marketAsk, suggested float64) string {
	if marketAsk <= 0 || suggested <= 0 {
		return ""
	}
	ratio := marketAsk / suggested
	switch {
	case ratio >= 1.3:
		return ClassUnder
	case ratio <= 0.9:
		return ClassOver
	default:
		return ClassFair
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/pricing/ -run TestClassify -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/pricing/pricing.go pkg/pricing/pricing_test.go
git commit -m "feat(pricing): classify under/over/fair verdict"
```

---

## Task 4: `Report` orchestrator — end-to-end pricing against the market DB

**Files:**
- Modify: `pkg/pricing/pricing.go`
- Test: `pkg/pricing/pricing_test.go`

**Interfaces:**
- Consumes: `*market.Collector` (`finditem.Find`, `col.GetItemStationPrices(ctx, itemID) ([]market.ItemStationPrice, error)` where `ItemStationPrice` has `BestBid float64` + `HasBuy bool`), `knowledge.Base`.
- Produces:
  - `PriceReport` struct (fields below).
  - `func Report(ctx context.Context, col *market.Collector, kb knowledge.Base, fromSystem string, hops int, itemID, recipeName string, outputUnits int, comps []Component, marginPct float64) (*PriceReport, error)`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/pricing/pricing_test.go` (add `"context"`, `"path/filepath"`, `"time"`, and `"github.com/rsned/spacemolt/pkg/knowledge"` to imports):

```go
// seedPriceMarket writes sell/buy orders for pricing tests. Graph: home-mid-far.
func seedPriceMarket(t *testing.T) *market.Collector {
	t.Helper()
	c, err := market.Open(market.Config{DBPath: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	now := time.Now().UTC()
	order := func(stn, item, side string, price, qty float64) market.Order {
		return market.Order{StationID: stn, ItemID: item, Side: side, PriceEach: price, Quantity: qty, CapturedAt: now}
	}
	snap := func(stn, sys string, orders ...market.Order) {
		if err := c.WriteSnapshot(context.Background(), market.MarketSnapshot{
			StationID: stn, StationName: stn, SystemID: sys, SystemName: sys, CapturedAt: now, Orders: orders,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// iron_ore: home@12, far@8 (far is 2 jumps). copper_ore: home@30 only.
	// widget (finished good): home sell(ask) 500, home buy(bid) 400.
	snap("home_stn", "home",
		order("home_stn", "iron_ore", "sell", 12, 100),
		order("home_stn", "copper_ore", "sell", 30, 100),
		order("home_stn", "widget", "sell", 500, 5),
		order("home_stn", "widget", "buy", 400, 5),
	)
	snap("far_stn", "far", order("far_stn", "iron_ore", "sell", 8, 100))
	return c
}

func seedPriceKB(t *testing.T) knowledge.Base {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	for _, s := range []struct{ id, to string }{{"home", "mid"}, {"mid", "far"}} {
		if err := kb.RememberSystem(ctx, knowledge.System{ID: s.id, Name: s.id, Connections: []knowledge.SystemConnection{{SystemID: s.to}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "far", Name: "far"}); err != nil {
		t.Fatal(err)
	}
	return kb
}

func TestReportEndToEnd(t *testing.T) {
	col := seedPriceMarket(t)
	kb := seedPriceKB(t)
	comps := []Component{{ItemID: "iron_ore", Qty: 10}, {ItemID: "copper_ore", Qty: 2}}
	// outputUnits 1, margin 20, from "home", hops 2.
	rep, err := Report(context.Background(), col, kb, "home", 2, "widget", "recipe_widget", 1, comps, 20)
	if err != nil {
		t.Fatal(err)
	}
	// Nearby iron_ore: cheapest within 2 hops = far@8. copper_ore only home@30.
	// nearby build = 10*8 + 2*30 = 140; +20% = 168.
	if !approx(rep.Nearby.BuildCost, 140) || !approx(rep.Nearby.Suggested, 168) {
		t.Fatalf("nearby: %+v", rep.Nearby)
	}
	if !rep.Nearby.Complete() {
		t.Fatalf("nearby should be complete: %+v", rep.Nearby)
	}
	// Finished good: nearby ask 500 (home is 0 hops), bid 400.
	if !rep.HasAskNearby || !approx(rep.CurAskNearby, 500) {
		t.Fatalf("ask nearby wrong: %+v", rep)
	}
	if !rep.HasBid || !approx(rep.CurBid, 400) {
		t.Fatalf("bid wrong: %+v", rep)
	}
	// 500 / 168 = 2.98× -> underpriced.
	if rep.Class != ClassUnder {
		t.Fatalf("class: %q", rep.Class)
	}
	if rep.ItemID != "widget" || rep.RecipeName != "recipe_widget" || rep.OutputUnits != 1 {
		t.Fatalf("header fields: %+v", rep)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/pricing/ -run TestReportEndToEnd -v`
Expected: FAIL — `undefined: Report`.

- [ ] **Step 3: Write minimal implementation**

Append to `pkg/pricing/pricing.go` (add `"context"`, `"github.com/rsned/spacemolt/pkg/knowledge"`, `"github.com/rsned/spacemolt/pkg/market"` to imports):

```go
// findLimit is passed to finditem.Find so no station is truncated away before
// we compute the market-wide mean (there are only a few dozen stations).
const findLimit = 1000

// PriceReport is the full pricing result for one decomposition of one item.
type PriceReport struct {
	ItemID      string            `json:"item_id"`
	RecipeName  string            `json:"recipe_name,omitempty"`
	OutputUnits int               `json:"output_units"`
	MarginPct   float64           `json:"margin_pct"`
	Components  []PricedComponent `json:"components"`
	Nearby      Basis             `json:"nearby"`
	Mkt         Basis             `json:"market"`

	CurAskNearby float64 `json:"cur_ask_nearby,omitempty"`
	HasAskNearby bool    `json:"has_ask_nearby"`
	CurAskMkt    float64 `json:"cur_ask_mkt,omitempty"`
	HasAskMkt    bool    `json:"has_ask_mkt"`
	CurBid       float64 `json:"cur_bid,omitempty"`
	HasBid       bool    `json:"has_bid"`
	Class        string  `json:"class,omitempty"`
}

// Report prices every component of one decomposition (comps) on both bases,
// rolls them up per output unit, prices the finished good, and classifies.
// recipeName is surfaced in the header ("" for BoM / no-recipe). outputUnits
// is the recipe's output-per-run for recipe mode, or 1 for BoM mode (its
// quantities are already per unit).
func Report(ctx context.Context, col *market.Collector, kb knowledge.Base, fromSystem string, hops int, itemID, recipeName string, outputUnits int, comps []Component, marginPct float64) (*PriceReport, error) {
	rep := &PriceReport{ItemID: itemID, RecipeName: recipeName, OutputUnits: outputUnits, MarginPct: marginPct}

	priced := make([]PricedComponent, 0, len(comps))
	for _, c := range comps {
		results, err := finditem.Find(ctx, col, kb, c.ItemID, 0, fromSystem, findLimit)
		if err != nil {
			return nil, err
		}
		nu, nf, mu, mf := askStats(results, hops)
		priced = append(priced, PricedComponent{Component: c, NearbyUnit: nu, MktUnit: mu, NearbyFound: nf, MktFound: mf})
	}
	rep.Components = priced
	rep.Nearby, rep.Mkt = rollUp(priced, outputUnits, marginPct)

	// Finished good's own asks (nearby cheapest + market-wide mean).
	goodAsks, err := finditem.Find(ctx, col, kb, itemID, 0, fromSystem, findLimit)
	if err != nil {
		return nil, err
	}
	rep.CurAskNearby, rep.HasAskNearby, rep.CurAskMkt, rep.HasAskMkt = askStats(goodAsks, hops)

	// Finished good's best bid anywhere (instant-sell reference).
	stationPrices, err := col.GetItemStationPrices(ctx, itemID)
	if err != nil {
		return nil, err
	}
	for _, sp := range stationPrices {
		if sp.HasBuy && sp.BestBid > rep.CurBid {
			rep.CurBid, rep.HasBid = sp.BestBid, true
		}
	}

	ref := rep.CurAskMkt
	if rep.HasAskNearby {
		ref = rep.CurAskNearby
	}
	sug := rep.Mkt.Suggested
	if rep.Nearby.Complete() {
		sug = rep.Nearby.Suggested
	} else if !rep.Mkt.Complete() {
		sug = 0 // no complete basis -> no verdict
	}
	rep.Class = classify(ref, sug)
	return rep, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/pricing/ -v`
Expected: PASS (all pricing tests).

- [ ] **Step 5: Lint + commit**

Run: `golangci-lint run ./pkg/pricing/...`
Expected: no findings.

```bash
git add pkg/pricing/pricing.go pkg/pricing/pricing_test.go
git commit -m "feat(pricing): Report orchestrator prices components + finished good"
```

---

## Task 5: play_as glue — arg parse, recipe/BoM resolution, mode selection

**Files:**
- Create: `cmd/tools/play_as/price.go`
- Test: `cmd/tools/play_as/price_test.go`

**Interfaces:**
- Consumes: `serverapi.Recipe` (`ID`, `Name`, `Inputs []RecipeItem{ItemID string; Quantity int}`, `Outputs []RecipeItem`), `pricing.Component`, `pricing.PriceReport`.
- Produces (pure helpers, unit-tested this task):
  - `func resolveRecipesForOutput(recipes map[string]serverapi.Recipe, itemID string) []serverapi.Recipe`
  - `func recipeComponents(r serverapi.Recipe) (comps []pricing.Component, outputUnits int)`
  - `func pickBestRecipe(reports []*pricing.PriceReport) (best int, altMkt int)` — `best` = index of lowest `Nearby.Suggested` (fallback `Mkt.Suggested` when nearby incomplete); `altMkt` = index of a *different* recipe that is cheaper on the market basis, or -1.

- [ ] **Step 1: Write the failing test**

Create `cmd/tools/play_as/price_test.go`:

```go
package main

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/pricing"
)

func TestResolveRecipesForOutput(t *testing.T) {
	recipes := map[string]serverapi.Recipe{
		"r_widget_a": {ID: "r_widget_a", Name: "A", Outputs: []serverapi.RecipeItem{{ItemID: "widget", Quantity: 2}}},
		"r_widget_b": {ID: "r_widget_b", Name: "B", Outputs: []serverapi.RecipeItem{{ItemID: "widget", Quantity: 1}}},
		"r_other":    {ID: "r_other", Name: "O", Outputs: []serverapi.RecipeItem{{ItemID: "gadget", Quantity: 1}}},
	}
	got := resolveRecipesForOutput(recipes, "widget")
	if len(got) != 2 {
		t.Fatalf("want 2 recipes for widget, got %d", len(got))
	}
	// deterministic order by recipe ID
	if got[0].ID != "r_widget_a" || got[1].ID != "r_widget_b" {
		t.Fatalf("unsorted: %s %s", got[0].ID, got[1].ID)
	}
	if len(resolveRecipesForOutput(recipes, "nonexistent")) != 0 {
		t.Fatalf("expected none for nonexistent")
	}
}

func TestRecipeComponents(t *testing.T) {
	r := serverapi.Recipe{
		ID:      "r_widget",
		Inputs:  []serverapi.RecipeItem{{ItemID: "iron_ore", Quantity: 20}, {ItemID: "copper_ore", Quantity: 8}},
		Outputs: []serverapi.RecipeItem{{ItemID: "widget", Quantity: 5}},
	}
	comps, units := recipeComponents(r)
	if units != 5 {
		t.Fatalf("outputUnits want 5 got %d", units)
	}
	if len(comps) != 2 || comps[0].ItemID != "iron_ore" || comps[0].Qty != 20 {
		t.Fatalf("comps wrong: %+v", comps)
	}
	// no declared output quantity -> defaults to 1
	r2 := serverapi.Recipe{Outputs: []serverapi.RecipeItem{{ItemID: "x"}}}
	if _, u := recipeComponents(r2); u != 1 {
		t.Fatalf("default outputUnits want 1 got %d", u)
	}
}

func TestPickBestRecipe(t *testing.T) {
	reports := []*pricing.PriceReport{
		{RecipeName: "A", Nearby: pricing.Basis{Suggested: 200, Covered: 2, Total: 2}, Mkt: pricing.Basis{Suggested: 150, Covered: 2, Total: 2}},
		{RecipeName: "B", Nearby: pricing.Basis{Suggested: 180, Covered: 2, Total: 2}, Mkt: pricing.Basis{Suggested: 190, Covered: 2, Total: 2}},
	}
	best, alt := pickBestRecipe(reports)
	if best != 1 { // B cheaper on nearby (180 < 200)
		t.Fatalf("best want 1 got %d", best)
	}
	if alt != 0 { // A cheaper on market basis (150 < 190) and differs from best
		t.Fatalf("altMkt want 0 got %d", alt)
	}
}

func TestPickBestRecipeSingle(t *testing.T) {
	best, alt := pickBestRecipe([]*pricing.PriceReport{{RecipeName: "only"}})
	if best != 0 || alt != -1 {
		t.Fatalf("single: best=%d alt=%d", best, alt)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run 'TestResolveRecipesForOutput|TestRecipeComponents|TestPickBestRecipe' -v`
Expected: FAIL — `undefined: resolveRecipesForOutput` etc.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/tools/play_as/price.go`:

```go
package main

import (
	"sort"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/pricing"
)

// resolveRecipesForOutput returns every recipe whose outputs include itemID,
// sorted by recipe ID for deterministic selection. recipe.ID is not the output
// item_id, so this scans outputs rather than keying by id.
func resolveRecipesForOutput(recipes map[string]serverapi.Recipe, itemID string) []serverapi.Recipe {
	var out []serverapi.Recipe
	for _, r := range recipes {
		for _, o := range r.Outputs {
			if o.ItemID == itemID {
				out = append(out, r)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// recipeComponents converts a recipe's per-run inputs into pricing components
// and returns the output-units-per-run used to normalize the roll-up to a
// per-unit cost (defaults to 1 when no output quantity is declared).
func recipeComponents(r serverapi.Recipe) (comps []pricing.Component, outputUnits int) {
	for _, in := range r.Inputs {
		comps = append(comps, pricing.Component{ItemID: in.ItemID, Qty: float64(in.Quantity)})
	}
	outputUnits = 1
	if len(r.Outputs) > 0 && r.Outputs[0].Quantity > 0 {
		outputUnits = r.Outputs[0].Quantity
	}
	return comps, outputUnits
}

// suggestedFor returns the report's headline suggested price: the Nearby basis
// when it priced every component, else the Market-wide basis.
func suggestedFor(r *pricing.PriceReport) float64 {
	if r.Nearby.Complete() {
		return r.Nearby.Suggested
	}
	return r.Mkt.Suggested
}

// pickBestRecipe chooses the cheapest recipe by headline suggested price and,
// when a *different* recipe is cheaper on the market-wide basis, returns its
// index as altMkt so the caller can surface it; altMkt is -1 otherwise.
func pickBestRecipe(reports []*pricing.PriceReport) (best, altMkt int) {
	best, altMkt = 0, -1
	for i, r := range reports {
		if suggestedFor(r) < suggestedFor(reports[best]) {
			best = i
		}
	}
	bestMkt := best
	for i, r := range reports {
		if r.Mkt.Suggested < reports[bestMkt].Mkt.Suggested {
			bestMkt = i
		}
	}
	if bestMkt != best {
		altMkt = bestMkt
	}
	return best, altMkt
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run 'TestResolveRecipesForOutput|TestRecipeComponents|TestPickBestRecipe' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/price.go cmd/tools/play_as/price_test.go
git commit -m "feat(play_as): price glue helpers — recipe resolution + selection"
```

---

## Task 6: play_as glue — rendering + `handlePrice` orchestration

**Files:**
- Modify: `cmd/tools/play_as/price.go`
- Test: `cmd/tools/play_as/price_test.go`

**Interfaces:**
- Consumes: `pricing.PriceReport`, `pricing.Basis.Complete()`, `pricing.Class*` constants.
- Produces:
  - `type modeReport struct { Label string; R *pricing.PriceReport }`
  - `func renderPriceText(itemID, fromSystem string, hops int, marginPct float64, modes []modeReport, altNote string) string`
  - `func handlePrice(client game.GameClient, ctx context.Context, parts []string, craftingDB *sql.DB, format outputFormat) error`

- [ ] **Step 1: Write the failing test**

Append to `cmd/tools/play_as/price_test.go` (add `"strings"` to imports):

```go
func TestRenderPriceTextUnderpriced(t *testing.T) {
	rep := &pricing.PriceReport{
		ItemID: "widget", RecipeName: "recipe_widget", OutputUnits: 5, MarginPct: 20,
		Components: []pricing.PricedComponent{
			{Component: pricing.Component{ItemID: "iron_ore", Qty: 20}, NearbyUnit: 8, MktUnit: 14, NearbyFound: true, MktFound: true},
			{Component: pricing.Component{ItemID: "rare_ore", Qty: 2}, MktUnit: 100, MktFound: true}, // no nearby
		},
		Nearby: pricing.Basis{BuildCost: 160, PerUnit: 32, Margin: 6.4, Suggested: 38.4, Covered: 1, Total: 2},
		Mkt:    pricing.Basis{BuildCost: 480, PerUnit: 96, Margin: 19.2, Suggested: 115.2, Covered: 2, Total: 2},
		CurAskNearby: 500, HasAskNearby: true, CurAskMkt: 520, HasAskMkt: true, CurBid: 400, HasBid: true,
		Class: pricing.ClassUnder,
	}
	out := renderPriceText("widget", "sol", 2, 20, []modeReport{{Label: "RECIPE", R: rep}}, "")

	for _, want := range []string{
		"widget", "RECIPE", "recipe_widget", "5 units/run",
		"iron_ore", "rare_ore", "—", // rare_ore has no nearby price -> dash
		"+ 20% margin", "SUGGESTED", "115.20",
		"1/2 priced nearby", "rare_ore", // feasibility line names the gap
		"CURRENT MARKET", "500", "400", "UNDERPRICED",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestRenderPriceText -v`
Expected: FAIL — `undefined: renderPriceText` / `undefined: modeReport`.

- [ ] **Step 3: Write minimal implementation**

Append to `cmd/tools/play_as/price.go`. Final import block for the file should be: `"context"`, `"database/sql"`, `"encoding/json"`, `"fmt"`, `"sort"`, `"strconv"`, `"strings"`, `"github.com/rsned/spacemolt/pkg/game"`, `"github.com/rsned/spacemolt/pkg/game/serverapi"`, `"github.com/rsned/spacemolt/pkg/pricing"`. (Note: `price.go` never names `craftplan.` directly — it ranges over `src.BOM`'s result and reads fields — so it does NOT import `craftplan`; `newPlayAsSource`/`playAsSource` live in `craftable.go`.)

```go
// modeReport pairs a decomposition label ("RECIPE" / "BOM (ore)") with its
// pricing result for rendering.
type modeReport struct {
	Label string
	R     *pricing.PriceReport
}

// money renders a price with two decimals, or an em dash when absent.
func money(v float64, found bool) string {
	if !found {
		return "—"
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// renderPriceText renders the human-readable price report(s). The CURRENT
// MARKET block is item-level and identical across modes, so it is printed once
// from the first mode's report.
func renderPriceText(itemID, fromSystem string, hops int, marginPct float64, modes []modeReport, altNote string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "price %s   (margin %.0f%%, nearby = local + ≤%d hops", itemID, marginPct, hops)
	if fromSystem != "" {
		fmt.Fprintf(&b, " from %s", fromSystem)
	}
	b.WriteString(")\n")

	for _, m := range modes {
		r := m.R
		fmt.Fprintf(&b, "\n%s", m.Label)
		if r.RecipeName != "" {
			fmt.Fprintf(&b, "  %s", r.RecipeName)
		}
		if r.OutputUnits > 1 {
			fmt.Fprintf(&b, " → %d units/run", r.OutputUnits)
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "  %-20s %8s %10s %10s\n", "COMPONENT", "QTY", "NEARBY", "MKT-AVG")
		var missing []string
		for _, c := range r.Components {
			fmt.Fprintf(&b, "  %-20s %8s %10s %10s\n", c.ItemID,
				strconv.FormatFloat(c.Qty, 'f', -1, 64),
				money(c.NearbyUnit, c.NearbyFound), money(c.MktUnit, c.MktFound))
			if !c.NearbyFound {
				missing = append(missing, c.ItemID)
			}
		}
		costLabel := "build cost/run"
		if r.OutputUnits <= 1 {
			costLabel = "build cost"
		}
		fmt.Fprintf(&b, "  %-20s %8s %10s %10s\n", "---- "+costLabel, "", money(r.Nearby.BuildCost, r.Nearby.Covered > 0), money(r.Mkt.BuildCost, r.Mkt.Covered > 0))
		fmt.Fprintf(&b, "  %-20s %8s %10s %10s\n", "---- per unit", "", money(r.Nearby.PerUnit, r.Nearby.Covered > 0), money(r.Mkt.PerUnit, r.Mkt.Covered > 0))
		fmt.Fprintf(&b, "  %-20s %8s %10s %10s\n", fmt.Sprintf("---- + %.0f%% margin", marginPct), "", money(r.Nearby.Margin, r.Nearby.Covered > 0), money(r.Mkt.Margin, r.Mkt.Covered > 0))
		fmt.Fprintf(&b, "  %-20s %8s %10s %10s\n", "= SUGGESTED", "", money(r.Nearby.Suggested, r.Nearby.Complete()), money(r.Mkt.Suggested, r.Mkt.Complete()))
		fmt.Fprintf(&b, "  feasibility (nearby): %d/%d priced nearby", r.Nearby.Covered, r.Nearby.Total)
		if len(missing) > 0 {
			fmt.Fprintf(&b, " — missing: %s", strings.Join(missing, ", "))
		}
		b.WriteString("\n")
	}

	if len(modes) > 0 {
		r := modes[0].R
		b.WriteString("\nCURRENT MARKET  " + itemID + "\n")
		fmt.Fprintf(&b, "  nearby ask %s   best bid %s   mkt-avg ask %s\n",
			money(r.CurAskNearby, r.HasAskNearby), money(r.CurBid, r.HasBid), money(r.CurAskMkt, r.HasAskMkt))
		if r.Class != "" {
			fmt.Fprintf(&b, "  → %s\n", r.Class)
		}
	}
	if altNote != "" {
		b.WriteString("\n" + altNote + "\n")
	}
	return b.String()
}

// handlePrice implements: price <item_id> [--margin=20] [--hops=2] [--mode=both|recipe|bom] [--json]
func handlePrice(client game.GameClient, ctx context.Context, parts []string, craftingDB *sql.DB, format outputFormat) error {
	_ = format
	if globalMarketCollector == nil {
		return fmt.Errorf("price: market DB not available (run with --market-db-path)")
	}
	args := parts[1:]
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return fmt.Errorf("usage: price <item_id> [--margin=20] [--hops=2] [--mode=both|recipe|bom] [--json]")
	}
	itemID := args[0]
	flags, err := parseFlagArgs(args[1:], "margin", "hops", "mode", "json")
	if err != nil {
		return err
	}
	margin := priceFlagFloat(flags["margin"], 20)
	hops := 2
	if n, ok := flagInt(flags["hops"]); ok {
		hops = n
	}
	mode := "both"
	if s, ok := flagString(flags["mode"]); ok && s != "" {
		mode = s
	}
	asJSON := flagBool(flags["json"])

	fromSystem := ""
	if st := client.GetState(); st != nil {
		fromSystem = st.System.ID
	}

	src := newPlayAsSource(client, craftingDB)
	recipes, err := src.Recipes(ctx, false)
	if err != nil {
		return fmt.Errorf("price: load recipes: %w", err)
	}
	candidates := resolveRecipesForOutput(recipes, itemID)

	var modes []modeReport
	var altNote string

	if mode == "both" || mode == "recipe" {
		if len(candidates) == 0 {
			fmt.Printf("price %s: not craftable — no recipe produces it.\n", itemID)
		} else {
			reports := make([]*pricing.PriceReport, 0, len(candidates))
			for _, r := range candidates {
				comps, units := recipeComponents(r)
				rep, rerr := pricing.Report(ctx, globalMarketCollector, globalKB, fromSystem, hops, itemID, r.ID, units, comps, margin)
				if rerr != nil {
					return rerr
				}
				reports = append(reports, rep)
			}
			best, alt := pickBestRecipe(reports)
			modes = append(modes, modeReport{Label: "RECIPE", R: reports[best]})
			if alt >= 0 {
				altNote = fmt.Sprintf("note: on the market-wide basis, recipe %s is cheaper (%s vs %s).",
					reports[alt].RecipeName, money(reports[alt].Mkt.Suggested, true), money(reports[best].Mkt.Suggested, true))
			}
		}
	}

	if mode == "both" || mode == "bom" {
		bom, berr := src.BOM(ctx, []string{itemID})
		switch {
		case berr != nil:
			fmt.Printf("price %s: BoM unavailable (%v)\n", itemID, berr)
		case len(bom[itemID]) == 0:
			fmt.Printf("price %s: no bill-of-materials rows (base material or untracked).\n", itemID)
		default:
			comps := make([]pricing.Component, 0, len(bom[itemID]))
			for _, row := range bom[itemID] {
				comps = append(comps, pricing.Component{ItemID: row.BaseItemID, Qty: float64(row.Quantity)})
			}
			// BoM quantities are already per single output unit -> outputUnits = 1.
			rep, rerr := pricing.Report(ctx, globalMarketCollector, globalKB, fromSystem, hops, itemID, "", 1, comps, margin)
			if rerr != nil {
				return rerr
			}
			modes = append(modes, modeReport{Label: "BOM (ore)", R: rep})
		}
	}

	if len(modes) == 0 {
		return nil // messages already printed
	}
	if asJSON {
		payload := make([]*pricing.PriceReport, 0, len(modes))
		for _, m := range modes {
			payload = append(payload, m.R)
		}
		out, jerr := json.MarshalIndent(payload, "", "  ")
		if jerr != nil {
			return jerr
		}
		fmt.Println(string(out))
		return nil
	}
	fmt.Print(renderPriceText(itemID, fromSystem, hops, margin, modes, altNote))
	return nil
}

// priceFlagFloat reads a parseFlagArgs value as a float with a default.
// parseFlagArgs stores whole numbers as int and everything else as string
// (so `--margin=20` arrives as int 20 and `--margin=17.5` as "17.5"). The
// existing flagInt/flagString/flagBool helpers cover the other flag types.
func priceFlagFloat(v any, def float64) float64 {
	switch tv := v.(type) {
	case int:
		return float64(tv)
	case string:
		if f, err := strconv.ParseFloat(tv, 64); err == nil {
			return f
		}
	}
	return def
}
```

Note: `flagInt`, `flagString`, and `flagBool` already exist in `main.go` (each takes a single `any`) — reuse them; do NOT redeclare. Only `priceFlagFloat` is new (uniquely named to avoid collision). The `craftplan` import is used by `newPlayAsSource`/`playAsSource` already in the package, so `newPlayAsSource(client, craftingDB)` needs no extra import wiring beyond what `craftable.go` provides; if goimports reports `craftplan` unused in `price.go` specifically, drop it from `price.go`'s import list (it's imported by `craftable.go` in the same package).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestRenderPriceText -v`
Expected: PASS.

- [ ] **Step 5: Build the whole package**

Run: `go build ./cmd/tools/play_as/`
Expected: builds clean. (If `flagFloat` etc. collide with existing helpers, delete the local copies.)

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/price.go cmd/tools/play_as/price_test.go
git commit -m "feat(play_as): price rendering + handlePrice orchestration"
```

---

## Task 7: Wire the dispatch + help, then verify end-to-end

**Files:**
- Modify: `cmd/tools/play_as/main.go` (dispatch `case`, help line)

**Interfaces:**
- Consumes: `handlePrice`, `ensureCraftingDB()`.

- [ ] **Step 1: Add the dispatch case**

In `cmd/tools/play_as/main.go`, immediately after the existing `case "plan":` block (around line 6806), add:

```go
	case "price":
		return handlePrice(client, ctx, parts, ensureCraftingDB(), format)
```

- [ ] **Step 2: Add the help line**

Find the help text near the `find_item` help line (search for `find_item <item> [qty]`, ~line 9267) and add directly below it:

```go
	fmt.Println("  price <item> [--margin=20] [--hops=2] [--mode=both|recipe|bom] [--json]  - suggested create_sell_order price from part/ore market cost + margin")
```

- [ ] **Step 3: Build everything**

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 4: Run the full test suite**

Run: `go test ./...`
Expected: PASS (pricing + play_as + everything else).

- [ ] **Step 5: Lint**

Run: `golangci-lint run ./pkg/pricing/... ./cmd/tools/play_as/...`
Expected: no new findings.

- [ ] **Step 6: Manual smoke test (live REPL)**

Run (user executes; needs market.db + crafting.db present):
```
go run ./cmd/tools/play_as <agent> --market-db-path data/market.db
```
Then in the REPL: `price adaptive_shield_i` and `price bioluminescent_culture --hops 3 --mode bom`.
Expected: a RECIPE and BOM table each with COMPONENT/QTY/NEARBY/MKT-AVG rows, per-unit + margin + SUGGESTED lines, a CURRENT MARKET block, and (for underpriced items) an `UNDERPRICED` verdict. Confirm no panic and that a raw ore (e.g. `price iron_ore`) prints "not craftable — no recipe" for recipe mode.

- [ ] **Step 7: Commit**

```bash
git add cmd/tools/play_as/main.go
git commit -m "feat(play_as): register price command + help"
```

---

## Self-Review Notes (completed by plan author)

- **Spec coverage:** two bases (Nearby ≤hops / Market-wide mean) → `askStats`; two decompositions (recipe inputs / BoM ore) → `recipeComponents` + `src.BOM`; per-unit suggested → `rollUp` with verified normalization (recipe ÷output-per-run, BoM ÷1); 20% margin as its own line → `Basis.Margin` + render row; four reference lines → current market ask/bid block, `classify` verdict, per-component table, feasibility line; multi-recipe cheapest-wins + surfaced alt → `pickBestRecipe`/`altNote`; items-only (raw ore → "not craftable") handled; `--json` supported. Ship rejection: ships have no `recipe_outputs` row for a `target_type='item'` BoM and won't resolve a recipe, so they naturally fall through to the "not craftable / no BoM" messages — acceptable for MVP (no explicit ship branch needed).
- **Placeholder scan:** none — every step has complete code/commands.
- **Type consistency:** `pricing.Component`, `PricedComponent`, `Basis`, `PriceReport`, `Report(...)` signatures match across Tasks 1–6; glue helper names (`resolveRecipesForOutput`, `recipeComponents`, `pickBestRecipe`, `renderPriceText`, `modeReport`, `handlePrice`) are consistent between definition and use.
- **Verified against source:** `flagInt(v any)`, `flagString(v any)`, `flagBool(v any)` already exist in `main.go` (single-`any` signatures) and are reused by the glue; only `priceFlagFloat` is newly defined (unique name, no collision). `newPlayAsSource`, `ensureCraftingDB`, `playAsSource.Recipes/BOM`, `parseFlagArgs`, `globalMarketCollector`, `globalKB`, and `outputFormat` all exist in the package as used. `market.Open`/`market.Config`, `col.GetItemStationPrices` (returns `ItemStationPrice` with `BestBid`/`HasBuy`), and `finditem.Find`/`finditem.Result`/`JumpsUnknown` confirmed present with the signatures used above.
