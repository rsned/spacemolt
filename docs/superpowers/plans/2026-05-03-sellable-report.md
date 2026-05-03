# `sellable` report Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** New `play_as` REPL command `sellable` that shows the operator what they can sell at the current docked station — pairing ship cargo + station storage against the live market order book and emitting a per-item plan with sellable quantity, weighted-average fill price, and total proceeds.

**Architecture:** Single new file `cmd/tools/play_as/sellable.go` housing a thin orchestrator (calls `view_market` + `get_cargo` + `view_storage` sequentially, parses raw JSON), a pure-function plan builder (where the test coverage lives), and styled + JSON renderers. The `executeCommand` switch in `main.go` adds a `case "sellable"` that delegates to it. No KB writes, no command queue, no auto-execute in v1.

**Tech Stack:** Go 1.24, `serverapi.ViewMarketItem`/`MarketOrder` (already exist), `storageItem` (already exists), standard `encoding/json` + `slices` + `strings.Builder`.

---

## Files

- **Create:** `cmd/tools/play_as/sellable.go` — orchestrator (`runSellable`), pure builder (`buildSellablePlan`, `fillItem`), renderers (`renderSellableStyled`, `renderSellableJSON`), plan types (`sellablePlan`, `sellableRow`, `sellableFill`).
- **Create:** `cmd/tools/play_as/sellable_test.go` — table-driven tests for `fillItem` and `buildSellablePlan`; snapshot-style tests for the two renderers.
- **Modify:** `cmd/tools/play_as/main.go` — add `case "sellable":` in `executeCommand`, and a `sellable` line in the help block.

Spec reference: `docs/superpowers/specs/2026-05-03-sellable-report-design.md`.

---

## Task 1: Stub `sellable` command, wire pre-check

**Goal:** Get the command name routed and the not-docked guard in place. No business logic yet. This is the smallest change that puts a working command in front of the user — running `sellable` while undocked produces a clean error, running it while docked produces a placeholder "not implemented" line.

**Files:**
- Create: `cmd/tools/play_as/sellable.go`
- Modify: `cmd/tools/play_as/main.go` (add a switch case in `executeCommand`)

- [ ] **Step 1: Create `cmd/tools/play_as/sellable.go` with the entry point and not-docked check**

```go
package main

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// sellableOptions mirrors the flags accepted on the `sellable` REPL command.
// Filled in over later tasks; the v1 surface is small.
type sellableOptions struct {
	detail       bool
	minProceeds  int64
}

// runSellable is the REPL entry point for the `sellable` command. It is the
// only function `executeCommand` calls — every other piece of this file is
// either pure (testable without a network) or a renderer.
func runSellable(client game.GameClient, ctx context.Context, opts sellableOptions, format outputFormat) error {
	state := client.GetState()
	if state == nil || !state.Doc {
		return fmt.Errorf("sellable: must be docked at a station with a market service")
	}
	// Subsequent tasks fill in: fetch (market+cargo+storage), build plan, render.
	_ = opts
	_ = format
	return fmt.Errorf("sellable: not implemented yet")
}
```

- [ ] **Step 2: Add the dispatch case in `executeCommand` (`cmd/tools/play_as/main.go`)**

Find the existing `case "facility":` block and add the `sellable` case directly after it (before the appearance section):

```go
	case "sellable":
		return runSellable(client, ctx, sellableOptions{}, format)
```

- [ ] **Step 3: Build to confirm wiring compiles**

```bash
go build ./...
```
Expected: exits 0, no output.

- [ ] **Step 4: Manual sanity check (no test required for the dispatch wiring itself)**

The not-docked path is exercised by Task 6's tests once flag parsing exists; for now we just verify the case doesn't break the build. Smoke-test by running `play_as` (out of band) and typing `sellable` — it should print `❌ sellable: not implemented yet` (or the not-docked error if undocked). Skip if the agent can't run the binary; the build pass is sufficient gating.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/sellable.go cmd/tools/play_as/main.go
git commit -m "feat(play_as): stub sellable command with not-docked precheck"
```

---

## Task 2: Pure `fillItem` (TDD) — the per-item buyer-walk

**Goal:** Implement the function that, given a cargo quantity and a list of `serverapi.MarketOrder` buy orders, returns the fill plan: sellable qty, total proceeds, weighted-average price, and the per-buyer fills. This is the testable core of the feature; everything else is plumbing.

**Files:**
- Modify: `cmd/tools/play_as/sellable.go` (add types + `fillItem`)
- Create: `cmd/tools/play_as/sellable_test.go`

- [ ] **Step 1: Write failing tests for `fillItem`**

Create `cmd/tools/play_as/sellable_test.go`:

```go
package main

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestFillItem(t *testing.T) {
	cases := []struct {
		name        string
		cargo       float64
		orders      []serverapi.MarketOrder
		wantQty     float64
		wantProceeds float64
		wantAvg     float64
		wantFills   []sellableFill
	}{
		{
			name:  "zero cargo returns empty",
			cargo: 0,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 0, wantProceeds: 0, wantAvg: 0, wantFills: nil,
		},
		{
			name:  "no buy orders returns empty",
			cargo: 50,
			orders: nil,
			wantQty: 0, wantProceeds: 0, wantAvg: 0, wantFills: nil,
		},
		{
			name:  "single buyer exact match",
			cargo: 100,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 100, wantProceeds: 1000, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 100, Proceeds: 1000}},
		},
		{
			name:  "single buyer cargo less than order",
			cargo: 30,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 30, wantProceeds: 300, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 30, Proceeds: 300}},
		},
		{
			name:  "single buyer cargo greater than order — leftover unsold",
			cargo: 200,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 100, wantProceeds: 1000, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 100, Proceeds: 1000}},
		},
		{
			name:  "multi-buyer ladder — cargo exceeds total demand, all consumed, sorted desc",
			cargo: 5000,
			orders: []serverapi.MarketOrder{
				// Intentionally out of order so the test exercises the sort.
				{PriceEach: 14, Quantity: 2246},
				{PriceEach: 26, Quantity: 676},
				{PriceEach: 20, Quantity: 1570},
			},
			// 676*26 + 1570*20 + 2246*14 = 17576 + 31400 + 31444 = 80420; qty = 4492; avg = 17.9074...
			wantQty: 4492, wantProceeds: 80420, wantAvg: 80420.0 / 4492.0,
			wantFills: []sellableFill{
				{Price: 26, Qty: 676, Proceeds: 17576},
				{Price: 20, Qty: 1570, Proceeds: 31400},
				{Price: 14, Qty: 2246, Proceeds: 31444},
			},
		},
		{
			name:  "multi-buyer ladder — cargo exhausts mid-ladder",
			cargo: 1000,
			orders: []serverapi.MarketOrder{
				{PriceEach: 26, Quantity: 676},
				{PriceEach: 20, Quantity: 1570},
				{PriceEach: 14, Quantity: 2246},
			},
			// 676 @ 26 = 17576, then 324 @ 20 = 6480; total 24056; qty 1000; avg 24.056
			wantQty: 1000, wantProceeds: 24056, wantAvg: 24.056,
			wantFills: []sellableFill{
				{Price: 26, Qty: 676, Proceeds: 17576},
				{Price: 20, Qty: 324, Proceeds: 6480},
			},
		},
		{
			name:  "ties at same price keep server order",
			cargo: 200,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 50, Source: "station"},
				{PriceEach: 10, Quantity: 50, Source: "player"},
				{PriceEach: 10, Quantity: 50, Source: "station"},
			},
			wantQty: 150, wantProceeds: 1500, wantAvg: 10,
			wantFills: []sellableFill{
				{Price: 10, Qty: 50, Proceeds: 500},
				{Price: 10, Qty: 50, Proceeds: 500},
				{Price: 10, Qty: 50, Proceeds: 500},
			},
		},
		{
			name:  "skips zero-price and zero-quantity orders defensively",
			cargo: 100,
			orders: []serverapi.MarketOrder{
				{PriceEach: 0, Quantity: 50},
				{PriceEach: 5, Quantity: 0},
				{PriceEach: 10, Quantity: 30},
			},
			wantQty: 30, wantProceeds: 300, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 30, Proceeds: 300}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQty, gotProceeds, gotAvg, gotFills := fillItem(tc.cargo, tc.orders)
			if gotQty != tc.wantQty {
				t.Errorf("sellable_qty = %v, want %v", gotQty, tc.wantQty)
			}
			if gotProceeds != tc.wantProceeds {
				t.Errorf("total_proceeds = %v, want %v", gotProceeds, tc.wantProceeds)
			}
			// Use a small epsilon for the weighted average.
			if abs(gotAvg-tc.wantAvg) > 1e-6 {
				t.Errorf("avg_price = %v, want %v", gotAvg, tc.wantAvg)
			}
			if len(gotFills) != len(tc.wantFills) {
				t.Fatalf("fills len = %d, want %d (got %+v)", len(gotFills), len(tc.wantFills), gotFills)
			}
			for i, f := range gotFills {
				if f != tc.wantFills[i] {
					t.Errorf("fill[%d] = %+v, want %+v", i, f, tc.wantFills[i])
				}
			}
		})
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
```

- [ ] **Step 2: Run the tests and confirm they fail with "undefined" symbols**

```bash
go test ./cmd/tools/play_as/... -run TestFillItem -v
```
Expected: build error / undefined: `sellableFill`, `fillItem`.

- [ ] **Step 3: Add the types and `fillItem` to `cmd/tools/play_as/sellable.go`**

Append to the file (after the existing imports, add `slices` and `github.com/rsned/spacemolt/pkg/game/serverapi`):

```go
import (
	"context"
	"fmt"
	"slices"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// sellableFill records one match of cargo against a single buy order.
type sellableFill struct {
	Price    float64 `json:"price"`
	Qty      float64 `json:"qty"`
	Proceeds float64 `json:"proceeds"`
}

// fillItem walks a sorted-by-price-desc copy of orders, taking
// min(remaining_cargo, order.quantity) from each, until cargo is exhausted
// or orders run out. Pure: no I/O, no globals. Tests live in
// sellable_test.go::TestFillItem.
//
// Returns: total filled quantity, total proceeds (price*qty summed), the
// proceeds-weighted average price (0 when nothing filled), and the per-fill
// breakdown in fill order.
//
// Defensive: skips orders whose PriceEach <= 0 or Quantity <= 0 so bad data
// can't manufacture phantom credits or burn cargo on no-op fills.
func fillItem(cargo float64, orders []serverapi.MarketOrder) (qty, proceeds, avg float64, fills []sellableFill) {
	if cargo <= 0 || len(orders) == 0 {
		return 0, 0, 0, nil
	}
	sorted := slices.Clone(orders)
	slices.SortStableFunc(sorted, func(a, b serverapi.MarketOrder) int {
		// Higher price first; stable so server order breaks ties.
		switch {
		case a.PriceEach > b.PriceEach:
			return -1
		case a.PriceEach < b.PriceEach:
			return 1
		default:
			return 0
		}
	})
	remaining := cargo
	for _, o := range sorted {
		if remaining <= 0 {
			break
		}
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		take := o.Quantity
		if take > remaining {
			take = remaining
		}
		fills = append(fills, sellableFill{
			Price:    o.PriceEach,
			Qty:      take,
			Proceeds: take * o.PriceEach,
		})
		qty += take
		proceeds += take * o.PriceEach
		remaining -= take
	}
	if qty > 0 {
		avg = proceeds / qty
	}
	return qty, proceeds, avg, fills
}
```

Note: this Step 3 supersedes the import block from Task 1. After applying, the file should have the imports listed above (the `_ = opts; _ = format` lines from Task 1 stay until Task 6 wires them up).

- [ ] **Step 4: Run tests, confirm they pass**

```bash
go test ./cmd/tools/play_as/... -run TestFillItem -v
```
Expected: PASS for every subtest.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/sellable.go cmd/tools/play_as/sellable_test.go
git commit -m "feat(play_as): pure fillItem for sellable buyer-walk"
```

---

## Task 3: Pure `buildSellablePlan` (TDD) — inventory union + per-item fills

**Goal:** The next layer up: take parsed market items + cargo + storage, produce a complete `sellablePlan` (sorted rows, totals, station_id). Continue TDD.

**Files:**
- Modify: `cmd/tools/play_as/sellable.go` (add `sellableRow`, `sellablePlan`, `buildSellablePlan`)
- Modify: `cmd/tools/play_as/sellable_test.go`

- [ ] **Step 1: Write failing tests for `buildSellablePlan`**

Append to `sellable_test.go`:

```go
func TestBuildSellablePlan(t *testing.T) {
	mkOrder := func(price, qty float64) serverapi.MarketOrder {
		return serverapi.MarketOrder{PriceEach: price, Quantity: qty}
	}

	t.Run("inventory union: cargo only, storage only, both", func(t *testing.T) {
		market := []serverapi.ViewMarketItem{
			{ItemID: "iron_ore", ItemName: "Iron Ore",
				BuyOrders: []serverapi.MarketOrder{mkOrder(10, 1000)}},
		}
		cargo := []storageItem{{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 50}}
		storage := []storageItem{
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 200},
			{ItemID: "carbon_ore", Name: "Carbon Ore", Quantity: 75},
		}
		plan := buildSellablePlan("nova_terra_central", market, cargo, storage)

		if plan.StationID != "nova_terra_central" {
			t.Errorf("station_id = %q, want nova_terra_central", plan.StationID)
		}
		if got, want := len(plan.Items), 2; got != want {
			t.Fatalf("len(plan.Items) = %d, want %d", got, want)
		}
		// Sorted by item_id asc → carbon_ore first, iron_ore second.
		if plan.Items[0].ItemID != "carbon_ore" {
			t.Errorf("Items[0].ItemID = %q, want carbon_ore", plan.Items[0].ItemID)
		}
		// iron_ore: cargo 50, storage 200, sellable from cargo only = 50.
		iron := plan.Items[1]
		if iron.Cargo != 50 || iron.Storage != 200 {
			t.Errorf("iron cargo/storage = %v/%v, want 50/200", iron.Cargo, iron.Storage)
		}
		if iron.SellableQty != 50 || iron.TotalProceeds != 500 {
			t.Errorf("iron sellable/proceeds = %v/%v, want 50/500", iron.SellableQty, iron.TotalProceeds)
		}
		// carbon_ore: 75 in storage, 0 in cargo, no market entry → sellable 0.
		carbon := plan.Items[0]
		if carbon.SellableQty != 0 {
			t.Errorf("carbon sellable = %v, want 0", carbon.SellableQty)
		}
	})

	t.Run("name fallback: cargo > storage > market.item_name > item_id", func(t *testing.T) {
		market := []serverapi.ViewMarketItem{
			{ItemID: "x_a", ItemName: "X-A from market"},
			{ItemID: "x_b", ItemName: "X-B from market"},
			{ItemID: "x_c"}, // no item_name
		}
		// x_a: name from cargo
		// x_b: cargo entry has no name → falls back to storage's name
		// x_c: cargo + storage have no name → falls back to market's (empty) → item_id
		cargo := []storageItem{
			{ItemID: "x_a", Name: "Cargo Name", Quantity: 1},
			{ItemID: "x_b", Name: "", Quantity: 1},
			{ItemID: "x_c", Name: "", Quantity: 1},
		}
		storage := []storageItem{
			{ItemID: "x_b", Name: "Storage Name", Quantity: 1},
		}
		plan := buildSellablePlan("s", market, cargo, storage)
		want := map[string]string{"x_a": "Cargo Name", "x_b": "Storage Name", "x_c": "x_c"}
		for _, row := range plan.Items {
			if got := row.Name; got != want[row.ItemID] {
				t.Errorf("name for %s = %q, want %q", row.ItemID, got, want[row.ItemID])
			}
		}
	})

	t.Run("duplicate cargo / storage entries are summed", func(t *testing.T) {
		cargo := []storageItem{
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 30},
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 20},
		}
		storage := []storageItem{
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 5},
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 7},
		}
		plan := buildSellablePlan("s", nil, cargo, storage)
		if got, want := len(plan.Items), 1; got != want {
			t.Fatalf("len = %d, want %d", got, want)
		}
		row := plan.Items[0]
		if row.Cargo != 50 || row.Storage != 12 {
			t.Errorf("cargo/storage = %v/%v, want 50/12", row.Cargo, row.Storage)
		}
	})

	t.Run("plan totals roll up", func(t *testing.T) {
		market := []serverapi.ViewMarketItem{
			{ItemID: "a", BuyOrders: []serverapi.MarketOrder{mkOrder(10, 100)}},
			{ItemID: "b", BuyOrders: []serverapi.MarketOrder{mkOrder(5, 50)}},
		}
		cargo := []storageItem{
			{ItemID: "a", Quantity: 10},
			{ItemID: "b", Quantity: 50},
		}
		plan := buildSellablePlan("s", market, cargo, nil)
		// a: 10 @ 10 = 100; b: 50 @ 5 = 250; total = 350.
		if plan.TotalProceeds != 350 {
			t.Errorf("plan.TotalProceeds = %v, want 350", plan.TotalProceeds)
		}
		if plan.ItemCount != 2 {
			t.Errorf("plan.ItemCount = %v, want 2", plan.ItemCount)
		}
	})

	t.Run("no inventory yields empty items slice", func(t *testing.T) {
		plan := buildSellablePlan("s", nil, nil, nil)
		if len(plan.Items) != 0 {
			t.Errorf("Items len = %d, want 0", len(plan.Items))
		}
		if plan.TotalProceeds != 0 || plan.ItemCount != 0 {
			t.Errorf("totals = %v/%v, want 0/0", plan.TotalProceeds, plan.ItemCount)
		}
	})
}
```

- [ ] **Step 2: Run, confirm failure**

```bash
go test ./cmd/tools/play_as/... -run TestBuildSellablePlan -v
```
Expected: undefined `sellablePlan`, `sellableRow`, `buildSellablePlan`.

- [ ] **Step 3: Add types and `buildSellablePlan` to `sellable.go`**

Append after `fillItem`:

```go
// sellableRow is one item's full sellability picture: what's on hand, what
// can be moved at the current station's market, and the per-buyer fills.
type sellableRow struct {
	ItemID        string         `json:"item_id"`
	Name          string         `json:"name"`
	Cargo         float64        `json:"cargo"`
	Storage       float64        `json:"storage"`
	SellableQty   float64        `json:"sellable_qty"`
	TotalProceeds float64        `json:"total_proceeds"`
	AvgPrice      float64        `json:"avg_price"`
	Fills         []sellableFill `json:"fills,omitempty"`
}

// sellablePlan is the rendered/serialized result of `sellable`. Sort order
// of Items: ItemID ascending. Totals roll up across all rows.
type sellablePlan struct {
	StationID      string        `json:"station_id"`
	ItemCount      int           `json:"item_count"`
	TotalProceeds  float64       `json:"total_proceeds"`
	Items          []sellableRow `json:"items"`
}

// buildSellablePlan unions cargo+storage by item_id, looks up each item's
// market order book, runs fillItem against cargo only (sell pulls from
// cargo, not storage), and emits a per-row plan plus rolled-up totals.
//
// Pure function: every input is plain data; no game.Client / context /
// network involvement. The orchestrator is responsible for fetching the
// inputs.
func buildSellablePlan(stationID string, market []serverapi.ViewMarketItem, cargo, storage []storageItem) sellablePlan {
	// Index market by item_id for O(1) lookup.
	byID := make(map[string]serverapi.ViewMarketItem, len(market))
	for _, m := range market {
		byID[m.ItemID] = m
	}

	// Inventory union — sum duplicates defensively. Tracks cargo qty,
	// storage qty, and the best name encountered (cargo > storage > market
	// > item_id).
	type acc struct {
		cargo   float64
		storage float64
		name    string
	}
	inv := make(map[string]*acc)
	get := func(id string) *acc {
		a, ok := inv[id]
		if !ok {
			a = &acc{}
			inv[id] = a
		}
		return a
	}
	for _, c := range cargo {
		a := get(c.ItemID)
		a.cargo += c.Quantity
		if a.name == "" {
			a.name = c.Name
		}
	}
	for _, s := range storage {
		a := get(s.ItemID)
		a.storage += s.Quantity
		if a.name == "" {
			a.name = s.Name
		}
	}

	plan := sellablePlan{StationID: stationID}
	for id, a := range inv {
		row := sellableRow{
			ItemID:  id,
			Cargo:   a.cargo,
			Storage: a.storage,
		}
		// Resolve name with the documented fallback chain.
		switch {
		case a.name != "":
			row.Name = a.name
		case byID[id].ItemName != "":
			row.Name = byID[id].ItemName
		default:
			row.Name = id
		}
		// Run fillItem against the market entry (if any) using cargo only.
		if mkt, ok := byID[id]; ok {
			qty, proceeds, avg, fills := fillItem(a.cargo, mkt.BuyOrders)
			row.SellableQty = qty
			row.TotalProceeds = proceeds
			row.AvgPrice = avg
			row.Fills = fills
		}
		plan.Items = append(plan.Items, row)
	}
	slices.SortFunc(plan.Items, func(x, y sellableRow) int {
		switch {
		case x.ItemID < y.ItemID:
			return -1
		case x.ItemID > y.ItemID:
			return 1
		default:
			return 0
		}
	})
	plan.ItemCount = len(plan.Items)
	for _, r := range plan.Items {
		plan.TotalProceeds += r.TotalProceeds
	}
	return plan
}
```

- [ ] **Step 4: Run tests, confirm they pass**

```bash
go test ./cmd/tools/play_as/... -run TestBuildSellablePlan -v
```
Expected: PASS for every subtest.

- [ ] **Step 5: Run all play_as tests to confirm nothing else broke**

```bash
go test ./cmd/tools/play_as/...
```
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/sellable.go cmd/tools/play_as/sellable_test.go
git commit -m "feat(play_as): pure buildSellablePlan with inventory union and totals"
```

---

## Task 4: Styled renderer

**Goal:** Take a `sellablePlan` and emit the human table from the spec, with optional `--detail` per-item fill blocks. Snapshot-style tests against fixed plans.

**Files:**
- Modify: `cmd/tools/play_as/sellable.go`
- Modify: `cmd/tools/play_as/sellable_test.go`

- [ ] **Step 1: Write failing tests for `renderSellableStyled`**

Append to `sellable_test.go`:

```go
func TestRenderSellableStyledEmpty(t *testing.T) {
	plan := sellablePlan{StationID: "nova_terra_central"}
	got := renderSellableStyled(plan, false)
	want := "(no cargo or storage at this station)\n"
	if got != want {
		t.Errorf("empty render = %q, want %q", got, want)
	}
}

func TestRenderSellableStyledHeaderAndRows(t *testing.T) {
	plan := sellablePlan{
		StationID:     "nova_terra_central",
		ItemCount:     2,
		TotalProceeds: 80602,
		Items: []sellableRow{
			{
				ItemID: "aluminum_ore", Name: "Aluminum Ore",
				Cargo: 4865, Storage: 1000,
				SellableQty: 4492, TotalProceeds: 80420, AvgPrice: 80420.0 / 4492.0,
				Fills: []sellableFill{
					{Price: 26, Qty: 676, Proceeds: 17576},
					{Price: 20, Qty: 1570, Proceeds: 31400},
					{Price: 14, Qty: 2246, Proceeds: 31444},
				},
			},
			{
				ItemID: "steel_plate", Name: "Steel Plate",
				Cargo: 7, Storage: 0,
				SellableQty: 7, TotalProceeds: 182, AvgPrice: 26,
				Fills: []sellableFill{{Price: 26, Qty: 7, Proceeds: 182}},
			},
		},
	}
	got := renderSellableStyled(plan, false)
	// Spot-check the parts that matter without locking in every space.
	checks := []string{
		"Sellable @ nova_terra_central",
		"2 items",
		"80,602 cr",
		"aluminum_ore",
		"Aluminum Ore",
		"steel_plate",
		"Total:",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("styled render missing %q\n--- output ---\n%s", want, got)
		}
	}
	// --detail OFF: no per-fill block.
	if strings.Contains(got, "676 @") {
		t.Errorf("styled render unexpectedly included a detail block:\n%s", got)
	}
}

func TestRenderSellableStyledDetail(t *testing.T) {
	plan := sellablePlan{
		StationID: "s", ItemCount: 1, TotalProceeds: 80420,
		Items: []sellableRow{{
			ItemID: "aluminum_ore", Name: "Aluminum Ore",
			Cargo: 4865, Storage: 0,
			SellableQty: 4492, TotalProceeds: 80420, AvgPrice: 80420.0 / 4492.0,
			Fills: []sellableFill{
				{Price: 26, Qty: 676, Proceeds: 17576},
				{Price: 20, Qty: 1570, Proceeds: 31400},
				{Price: 14, Qty: 2246, Proceeds: 31444},
			},
		}},
	}
	got := renderSellableStyled(plan, true)
	for _, want := range []string{
		"676 @ 26",
		"1570 @ 20",
		"2246 @ 14",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail block missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestRenderSellableStyledSingleBuyerNotDetailed(t *testing.T) {
	plan := sellablePlan{
		StationID: "s", ItemCount: 1, TotalProceeds: 182,
		Items: []sellableRow{{
			ItemID: "steel_plate", Name: "Steel Plate",
			Cargo: 7, SellableQty: 7, TotalProceeds: 182, AvgPrice: 26,
			Fills: []sellableFill{{Price: 26, Qty: 7, Proceeds: 182}},
		}},
	}
	got := renderSellableStyled(plan, true)
	// Single-buyer items must not get an expanded fill block even under --detail.
	if strings.Contains(got, "7 @ 26") {
		t.Errorf("single-buyer item rendered an unwanted detail block:\n%s", got)
	}
}
```

The test file needs `import "strings"` added.

- [ ] **Step 2: Run, confirm failure**

```bash
go test ./cmd/tools/play_as/... -run TestRenderSellableStyled -v
```
Expected: undefined `renderSellableStyled`.

- [ ] **Step 3: Add `renderSellableStyled` to `sellable.go`**

Append (and add `"strings"` to imports if not already there):

```go
// renderSellableStyled formats a sellablePlan as a human-readable table.
// detail=true adds an expanded per-buyer fill block under each multi-buyer
// item; single-buyer items stay inline regardless. The empty-inventory
// branch returns the documented "(no cargo or storage at this station)"
// line so callers don't need to special-case it before printing.
func renderSellableStyled(plan sellablePlan, detail bool) string {
	if len(plan.Items) == 0 {
		return "(no cargo or storage at this station)\n"
	}

	idW, nameW := len("ID"), len("Name")
	cargoW, storageW := len("Cargo"), len("Storage")
	sellW, avgW, proceedsW := len("Sellable"), len("Avg Price"), len("Proceeds")
	for _, r := range plan.Items {
		idW = max(idW, len(r.ItemID))
		nameW = max(nameW, len(r.Name))
		cargoW = max(cargoW, len(formatFloat(r.Cargo)))
		storageW = max(storageW, len(formatFloat(r.Storage)))
		sellW = max(sellW, len(formatFloat(r.SellableQty)))
		// Avg price shown as "—" when nothing sellable.
		if r.SellableQty > 0 {
			avgW = max(avgW, len(formatPrice(r.AvgPrice)))
		}
		proceedsW = max(proceedsW, len(formatCredits(r.TotalProceeds)))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Sellable @ %s — %d items, est. proceeds %s cr\n\n",
		plan.StationID, plan.ItemCount, formatCredits(plan.TotalProceeds))
	fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s | %*s | %*s | %*s\n",
		idW, "ID", nameW, "Name",
		cargoW, "Cargo", storageW, "Storage",
		sellW, "Sellable", avgW, "Avg Price", proceedsW, "Proceeds")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", idW), strings.Repeat("-", nameW),
		strings.Repeat("-", cargoW), strings.Repeat("-", storageW),
		strings.Repeat("-", sellW), strings.Repeat("-", avgW), strings.Repeat("-", proceedsW))

	for _, r := range plan.Items {
		avg := "—"
		if r.SellableQty > 0 {
			avg = formatPrice(r.AvgPrice)
		}
		fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s | %*s | %*s | %*s\n",
			idW, r.ItemID, nameW, r.Name,
			cargoW, formatFloat(r.Cargo), storageW, formatFloat(r.Storage),
			sellW, formatFloat(r.SellableQty),
			avgW, avg,
			proceedsW, formatCredits(r.TotalProceeds))
	}
	fmt.Fprintf(&b, "  %s   Total: %s cr\n",
		strings.Repeat(" ", idW+nameW+cargoW+storageW+sellW+avgW+18),
		formatCredits(plan.TotalProceeds))

	if detail {
		for _, r := range plan.Items {
			if len(r.Fills) <= 1 {
				continue
			}
			fmt.Fprintf(&b, "\n%s — %s / %s sellable, %s cr\n",
				r.ItemID, formatFloat(r.SellableQty), formatFloat(r.Cargo),
				formatCredits(r.TotalProceeds))
			for _, f := range r.Fills {
				fmt.Fprintf(&b, "  %s @ %s = %s cr\n",
					formatFloat(f.Qty), formatPrice(f.Price), formatCredits(f.Proceeds))
			}
		}
	}
	return b.String()
}

// formatPrice renders a price-each value with two decimals (matching the
// existing market-row style). formatFloat / formatCredits already exist
// in main.go and are reused here.
func formatPrice(p float64) string {
	return fmt.Sprintf("%.2f", p)
}
```

- [ ] **Step 4: Run tests, confirm they pass**

```bash
go test ./cmd/tools/play_as/... -run TestRenderSellableStyled -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/sellable.go cmd/tools/play_as/sellable_test.go
git commit -m "feat(play_as): styled renderer for sellable plan"
```

---

## Task 5: JSON renderer

**Goal:** Emit the structured plan as JSON for the `--format json|raw` selections.

**Files:**
- Modify: `cmd/tools/play_as/sellable.go`
- Modify: `cmd/tools/play_as/sellable_test.go`

- [ ] **Step 1: Write failing test**

Append to `sellable_test.go`:

```go
func TestRenderSellableJSON(t *testing.T) {
	plan := sellablePlan{
		StationID: "nova_terra_central", ItemCount: 1, TotalProceeds: 182,
		Items: []sellableRow{{
			ItemID: "steel_plate", Name: "Steel Plate",
			Cargo: 7, Storage: 0,
			SellableQty: 7, TotalProceeds: 182, AvgPrice: 26,
			Fills: []sellableFill{{Price: 26, Qty: 7, Proceeds: 182}},
		}},
	}
	out := renderSellableJSON(plan)
	var round struct {
		StationID     string  `json:"station_id"`
		ItemCount     int     `json:"item_count"`
		TotalProceeds float64 `json:"total_proceeds"`
		Items         []struct {
			ItemID string `json:"item_id"`
			Fills  []struct {
				Price float64 `json:"price"`
				Qty   float64 `json:"qty"`
			} `json:"fills"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &round); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if round.StationID != "nova_terra_central" {
		t.Errorf("station_id = %q, want nova_terra_central", round.StationID)
	}
	if round.ItemCount != 1 || round.TotalProceeds != 182 {
		t.Errorf("totals = %v/%v, want 1/182", round.ItemCount, round.TotalProceeds)
	}
	if len(round.Items) != 1 || round.Items[0].ItemID != "steel_plate" {
		t.Fatalf("items = %+v", round.Items)
	}
	if len(round.Items[0].Fills) != 1 || round.Items[0].Fills[0].Price != 26 {
		t.Errorf("fills[0] = %+v, want price=26 qty=7", round.Items[0].Fills)
	}
}
```

Add `"encoding/json"` to the test file imports.

- [ ] **Step 2: Run, confirm failure**

```bash
go test ./cmd/tools/play_as/... -run TestRenderSellableJSON -v
```
Expected: undefined `renderSellableJSON`.

- [ ] **Step 3: Add `renderSellableJSON`**

Append to `sellable.go` (add `"encoding/json"` to imports):

```go
// renderSellableJSON serializes a plan as pretty-printed JSON. Field tags
// on sellablePlan / sellableRow / sellableFill drive the wire shape.
// Returns "" on marshal error (impossible for the value types involved,
// but explicit to stay symmetric with the styled renderer).
func renderSellableJSON(plan sellablePlan) string {
	out, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return ""
	}
	return string(out) + "\n"
}
```

- [ ] **Step 4: Run tests, confirm they pass**

```bash
go test ./cmd/tools/play_as/... -run TestRenderSellableJSON -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/sellable.go cmd/tools/play_as/sellable_test.go
git commit -m "feat(play_as): JSON renderer for sellable plan"
```

---

## Task 6: Live orchestrator + flag parsing + help text

**Goal:** Replace the `not implemented yet` stub with the real orchestrator: parse flags, fetch the three datasets, build the plan, apply `--min-proceeds` filter, render. Update help text.

**Files:**
- Modify: `cmd/tools/play_as/sellable.go`
- Modify: `cmd/tools/play_as/main.go` (flag parsing in the `case "sellable":` arm and help text)

- [ ] **Step 1: Add flag parsing in `executeCommand`'s `case "sellable":`**

Replace the `case "sellable":` body in `cmd/tools/play_as/main.go` with:

```go
	case "sellable":
		opts := sellableOptions{}
		for i := 1; i < len(parts); i++ {
			arg := parts[i]
			switch {
			case arg == "--detail" || arg == "-d":
				opts.detail = true
			case strings.HasPrefix(arg, "--min-proceeds="):
				v := strings.TrimPrefix(arg, "--min-proceeds=")
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return fmt.Errorf("sellable: --min-proceeds: %w", err)
				}
				opts.minProceeds = n
			case arg == "--min-proceeds":
				if i+1 >= len(parts) {
					return fmt.Errorf("sellable: --min-proceeds requires a value")
				}
				i++
				n, err := strconv.ParseInt(parts[i], 10, 64)
				if err != nil {
					return fmt.Errorf("sellable: --min-proceeds: %w", err)
				}
				opts.minProceeds = n
			default:
				return fmt.Errorf("sellable: unknown flag %q", arg)
			}
		}
		return runSellable(client, ctx, opts, format)
```

- [ ] **Step 2: Replace `runSellable` body in `cmd/tools/play_as/sellable.go`**

The full function (replacing the Task 1 stub):

```go
func runSellable(client game.GameClient, ctx context.Context, opts sellableOptions, format outputFormat) error {
	state := client.GetState()
	if state == nil || !state.Doc {
		return fmt.Errorf("sellable: must be docked at a station with a market service")
	}

	// 1. view_market — full order book at this station.
	if err := client.GetListings(ctx); err != nil {
		return fmt.Errorf("sellable: view_market: %w", err)
	}
	marketRaw := client.GetRawJSON("market")
	var marketResp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if len(marketRaw) > 0 {
		if err := json.Unmarshal(marketRaw, &marketResp); err != nil {
			return fmt.Errorf("sellable: parse market: %w", err)
		}
	}

	// 2. get_cargo — ship cargo.
	if err := client.GetCargo(ctx); err != nil {
		return fmt.Errorf("sellable: get_cargo: %w", err)
	}
	cargoRaw := client.GetRawJSON("cargo")
	var cargoResp struct {
		Cargo []storageItem `json:"cargo"`
	}
	if len(cargoRaw) > 0 {
		if err := json.Unmarshal(cargoRaw, &cargoResp); err != nil {
			return fmt.Errorf("sellable: parse cargo: %w", err)
		}
	}

	// 3. view_storage — current station's storage.
	if err := client.ViewStorage(ctx); err != nil {
		return fmt.Errorf("sellable: view_storage: %w", err)
	}
	storageRaw := client.GetRawJSON("storage")
	var storageResp struct {
		BaseID string        `json:"base_id"`
		Items  []storageItem `json:"items"`
	}
	if len(storageRaw) > 0 {
		if err := json.Unmarshal(storageRaw, &storageResp); err != nil {
			return fmt.Errorf("sellable: parse storage: %w", err)
		}
	}

	stationID := storageResp.BaseID
	if stationID == "" {
		stationID = state.CurrentPOI
	}
	plan := buildSellablePlan(stationID, marketResp.Items, cargoResp.Cargo, storageResp.Items)

	// Apply --min-proceeds filter after computation so individual rows are
	// still correct; we only suppress them from output.
	if opts.minProceeds > 0 {
		filtered := plan.Items[:0]
		for _, r := range plan.Items {
			if int64(r.TotalProceeds) >= opts.minProceeds {
				filtered = append(filtered, r)
			}
		}
		plan.Items = filtered
		plan.ItemCount = len(filtered)
		// Note: TotalProceeds intentionally still reflects the unfiltered
		// total — it's the "what's possible" headline; --min-proceeds is a
		// view filter, not a model change.
	}

	switch format {
	case formatStyled:
		fmt.Print(renderSellableStyled(plan, opts.detail))
	default: // formatRaw and any other future non-styled mode → JSON
		fmt.Print(renderSellableJSON(plan))
	}
	return nil
}
```

Make sure imports include `"encoding/json"`, `"fmt"`, `"slices"`, `"github.com/rsned/spacemolt/pkg/game"`, `"github.com/rsned/spacemolt/pkg/game/serverapi"`. The `context` import becomes unused at this point if Go complains — keep it: `runSellable`'s signature still takes `ctx`.

- [ ] **Step 3: Add help-text line**

In `cmd/tools/play_as/main.go`, find the OTHER section (`fmt.Println("\n=== OTHER ===")`) and add the line right after the `loop` lines:

```go
	fmt.Println("  sellable [-d] [--min-proceeds N]   - What can I sell here? (cargo+storage @ this station's market)")
```

- [ ] **Step 4: Build, test, lint**

```bash
go build ./...
go test ./cmd/tools/play_as/...
golangci-lint run ./cmd/tools/play_as/
```
Expected: build exits 0; tests pass (`ok`); lint reports `0 issues` (pre-existing diagnostics in main.go from prior work are not new).

- [ ] **Step 5: Manual check (out of band)**

Run `play_as` against an explorer agent at a station, type `sellable`. Confirm: header line shows the station id, table renders, total matches by mental arithmetic. Then `sellable -d` and confirm the per-buyer breakdowns appear. Then `sellable --min-proceeds 10000` and confirm low-value rows drop out. If you can't run the binary, the build+test+lint gate is sufficient — the orchestrator code is straightforward enough that ungated risk is low.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/sellable.go cmd/tools/play_as/main.go
git commit -m "feat(play_as): wire sellable orchestrator + flags + help"
```

---

## Self-Review (already applied)

**Spec coverage check.** Every spec section has a task:
- "Pre-check + fetch sequential" → Task 6 orchestrator.
- "Pure builder" → Tasks 2–3 (`fillItem` + `buildSellablePlan`).
- "Styled output incl. `--detail`" → Task 4 + Task 6 (flag parsing).
- "JSON output" → Task 5 + Task 6 (format dispatch).
- "`--min-proceeds` filter applied after build" → Task 6.
- "Edge cases: not docked / no market / empty inventory / no matching market entries / defensive zero-price-or-qty / dup entries summed / fetch errors abort" → Task 1 (not docked), Task 6 (fetch error aborts), Task 2 (defensive zero-price), Task 3 (dup entries summed, no-market entries → 0 sellable), Task 4 empty branch.
- "Name fallback chain" → Task 3 test + impl.
- "Sort by `item_id` asc" → Task 3 test + impl.
- "Render layer tests + table-driven pure tests" → Task 2/3/4/5 tests.
- "Build/test/lint gates" → Task 6 step 4.

**Placeholder scan.** No `TBD`/`TODO`/`add appropriate error handling`/`similar to Task N` patterns. Code blocks are complete.

**Type consistency.** `sellableFill`, `sellableRow`, `sellablePlan`, `sellableOptions`, `runSellable`, `fillItem`, `buildSellablePlan`, `renderSellableStyled`, `renderSellableJSON`, `formatPrice` — same names used everywhere they appear. Fields on `sellableRow` (`ItemID`, `Name`, `Cargo`, `Storage`, `SellableQty`, `TotalProceeds`, `AvgPrice`, `Fills`) match between Tasks 3, 4, 5, 6 and tests.

**Format constants.** `outputFormat` defines `formatRaw` and `formatStyled` only (`cmd/tools/play_as/main.go:58-59`). The Task 6 dispatch is "styled → table; everything else → JSON" — matches the existing `printResponse` convention where the raw mode dumps JSON and the styled mode dispatches a custom formatter.
