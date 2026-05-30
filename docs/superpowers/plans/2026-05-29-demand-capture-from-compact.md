# Demand Capture From Compact view_market — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture full, source-classified buy-order demand directly from the compact `view_market` response on every market read, collapse the demand ledger to a single table (`market_buy_orders`), and remove the now-pointless `demand scan` command.

**Architecture:** The compact `view_market` response already returns complete `buy_orders` arrays with `source` ("station" or null) for every item — confirmed against real captures and by the user that per-item deep calls add nothing. So `captureDemand` becomes the only writer: it parses every item's orders and does a transactional replace-by-station into `market_buy_orders`. The `market_buy_demand` summary table and the `demand scan` deep path are deleted.

**Tech Stack:** Go 1.24+, modernc.org/sqlite, `pkg/knowledge` KB, `cmd/tools/play_as` REPL.

**Reference spec:** `docs/superpowers/specs/2026-05-29-demand-capture-from-compact-design.md`

**CRITICAL — green-tree commits:** The pre-commit hook builds and tests the WHOLE module and runs `golangci-lint` (which flags unused symbols). Therefore Task 1 is a single atomic change: removing the KB summary methods breaks their play_as callers, so the KB changes and the play_as rewiring must be committed together. Do NOT try to commit a partial state — `go build ./...` + `go test ./...` + `golangci-lint run ./...` must all be green before each commit.

**Conventions:** KB methods stay on `*SQLiteKB` only (faction pattern); callers type-assert `globalKB.(*knowledge.SQLiteKB)`. Real module path is `github.com/rsned/spacemolt/...`. JSON `null` for `source` unmarshals into the Go `Source string` field as `""`.

---

## File Structure

| File | Change | Task |
|------|--------|------|
| `pkg/knowledge/demand_store.go` | Add `ReplaceStationBuyOrders`; remove `UpsertMarketDemand` + `ReplaceMarketBuyOrders` | 1 |
| `pkg/knowledge/demand_load.go` | Replace `LoadMarketDemand`/`loadMarketDemandSummary` with exported `LoadMarketBuyOrders` | 1 |
| `pkg/knowledge/demand.go` | Remove `MarketDemandRow` struct | 1 |
| `pkg/knowledge/demand_test.go` | Rework: station-replace round-trip, cross-station isolation, prune | 1 |
| `cmd/tools/play_as/demand_capture.go` | Rewrite `parseDemandRows`→`parseStationBuyOrders`; `captureDemand` uses replace-by-station | 1 |
| `cmd/tools/play_as/demand_scan.go` | Delete entirely | 1 |
| `cmd/tools/play_as/demand_report.go` | `buildDemandReport` drops `summary` param; remove `classUnknown` | 1 |
| `cmd/tools/play_as/demand_cmd.go` | `runDemand` uses `LoadMarketBuyOrders` + new builder signature | 1 |
| `cmd/tools/play_as/main.go` | `case "demand"`: remove the `scan` sub-branch | 1 |
| `cmd/tools/play_as/demand_capture_test.go` | Rework for `parseStationBuyOrders`; drop `parseDeepOrders` test | 1 |
| `cmd/tools/play_as/demand_report_test.go` | Rework to orders-only fixtures; drop `classUnknown` case | 1 |
| `pkg/knowledge/sqlite_migrations.go` | Add migration 37 dropping `market_buy_demand` | 2 |
| `scripts/sql/initialize_database.sql` | Regenerate (tooling) | 2 |

---

## Task 1: Collapse to a single source-classified table (atomic)

This whole task is ONE commit. Implement all sub-steps, get the whole tree green, then commit once at the end (Step 16).

**Files:** as listed above (all except the migration files).

### KB store layer

- [ ] **Step 1: Replace the store methods**

Overwrite `pkg/knowledge/demand_store.go` with:

```go
package knowledge

import "context"

// ReplaceStationBuyOrders replaces ALL buy orders for a station with the
// supplied set in one transaction. A full compact view_market read covers every
// item at the station, so replacing by station keeps the snapshot fresh and
// prunes items whose demand has vanished since the last read. Empty
// SystemID/ItemName/Source are stored as "" (never NULL) so loaders scan into
// plain strings.
func (kb *SQLiteKB) ReplaceStationBuyOrders(ctx context.Context, stationID string, orders []MarketBuyOrderRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM market_buy_orders WHERE station_id=?`, stationID); err != nil {
			return err
		}
		for _, o := range orders {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_buy_orders
					(station_id, system_id, item_id, item_name, price_each, quantity, source, captured_utc)
				VALUES (?,?,?,?,?,?,?,?)`,
				o.StationID, o.SystemID, o.ItemID, o.ItemName, o.PriceEach, o.Quantity, o.Source, utc(o.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}
```

(This removes `UpsertMarketDemand` and the per-item `ReplaceMarketBuyOrders`.)

- [ ] **Step 2: Replace the load layer**

Overwrite `pkg/knowledge/demand_load.go` with:

```go
package knowledge

import "context"

// LoadMarketBuyOrders returns every captured buy order across all stations,
// ordered by station, item, then price descending.
func (kb *SQLiteKB) LoadMarketBuyOrders(ctx context.Context) ([]MarketBuyOrderRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT station_id, system_id, item_id, item_name, price_each, quantity, source, captured_utc
		FROM market_buy_orders
		ORDER BY station_id, item_id, price_each DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MarketBuyOrderRow
	for rows.Next() {
		var r MarketBuyOrderRow
		var capStr string
		if err := rows.Scan(&r.StationID, &r.SystemID, &r.ItemID, &r.ItemName,
			&r.PriceEach, &r.Quantity, &r.Source, &capStr); err != nil {
			return nil, err
		}
		r.CapturedAt = parseUTC(capStr)
		out = append(out, r)
	}
	return out, rows.Err()
}
```

(This removes `LoadMarketDemand` and `loadMarketDemandSummary`.)

- [ ] **Step 3: Remove the now-unused `MarketDemandRow` struct**

In `pkg/knowledge/demand.go`, delete the entire `MarketDemandRow` struct and its doc comment. Keep `MarketBuyOrderRow`. The file keeps `import "time"` (still used by `MarketBuyOrderRow.CapturedAt`). Result:

```go
package knowledge

import "time"

// MarketBuyOrderRow is a single buy order captured from a view_market response,
// carrying Source so the report can distinguish Station Manager ("station")
// orders from player orders ("" — null source in the compact response).
type MarketBuyOrderRow struct {
	StationID  string
	SystemID   string
	ItemID     string
	ItemName   string
	PriceEach  float64
	Quantity   float64
	Source     string
	CapturedAt time.Time
}
```

### play_as capture

- [ ] **Step 4: Rewrite the capture parser + writer**

Overwrite `cmd/tools/play_as/demand_capture.go` with:

```go
package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseStationBuyOrders turns a compact view_market response (no item_id) into
// per-order MarketBuyOrderRow values across all items, carrying Source. The
// compact response already contains complete, source-tagged buy_orders, so no
// per-item deep call is needed. Skips orders with non-positive price or qty.
func parseStationBuyOrders(raw []byte, stationID, systemID string, now time.Time) []knowledge.MarketBuyOrderRow {
	if len(raw) == 0 || stationID == "" {
		return nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	var out []knowledge.MarketBuyOrderRow
	for _, it := range resp.Items {
		for _, o := range it.BuyOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			out = append(out, knowledge.MarketBuyOrderRow{
				StationID: stationID,
				SystemID:  systemID,
				ItemID:    it.ItemID,
				ItemName:  it.ItemName,
				PriceEach: o.PriceEach,
				Quantity:  o.Quantity,
				Source:    o.Source,
				CapturedAt: now,
			})
		}
	}
	return out
}

// captureDemand persists the full source-classified buy-order demand from the
// client's most recent (full, no-item_id) view_market response, replacing the
// station's entire order set. Best-effort: silently no-ops when the KB is
// absent, there is no market data, or the player is not at a station.
func captureDemand(client game.GameClient, ctx context.Context) {
	if globalKB == nil {
		return
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return
	}
	state := client.GetState()
	if state == nil {
		return
	}
	orders := parseStationBuyOrders(client.GetRawJSON("market"), state.CurrentPOI, state.CurrentSystem, time.Now())
	if len(orders) == 0 {
		return
	}
	_ = sqlite.ReplaceStationBuyOrders(ctx, state.CurrentPOI, orders)
}
```

- [ ] **Step 5: Delete the deep-scan file**

```bash
git rm cmd/tools/play_as/demand_scan.go
```

### play_as report

- [ ] **Step 6: Simplify the report builder**

In `cmd/tools/play_as/demand_report.go`:

(a) Remove the `classUnknown` constant. The const block becomes:

```go
const (
	classStation demandClass = "STN"    // Source == "station" (Station Manager)
	classAboveSM demandClass = "PLR>SM" // player order priced above the best station order
	classPlayer  demandClass = "PLR"    // player order, no higher station competitor
)
```

(b) Change the `buildDemandReport` signature and drop the summary-seeding loop. Replace the function header and the first block (down through the summary loop) so it reads:

```go
// buildDemandReport scores each (station, item) of captured buy orders against
// on-hand inventory and craftability, applies filters, and sorts. Pure:
// callers pass `now` explicitly for testability.
func buildDemandReport(
	deep []knowledge.MarketBuyOrderRow,
	onHand map[string]float64,
	canCraft map[string]int,
	now time.Time,
	opts demandOptions,
) demandReport {
	key := func(s, i string) string { return s + "\x00" + i }
	agg := map[string]*demandAgg{}

	deepByKey := map[string][]knowledge.MarketBuyOrderRow{}
	for _, o := range deep {
		k := key(o.StationID, o.ItemID)
		deepByKey[k] = append(deepByKey[k], o)
	}
	for k, ords := range deepByKey {
		cls, price, qty := classifyDemand(ords)
		a := &demandAgg{stationID: ords[0].StationID, systemID: ords[0].SystemID, itemID: ords[0].ItemID}
		a.class, a.price, a.qty = cls, price, qty
		for _, o := range ords {
			if o.CapturedAt.After(a.captured) {
				a.captured = o.CapturedAt
			}
			if a.itemName == "" {
				a.itemName = o.ItemName
			}
		}
		agg[k] = a
	}
```

Leave everything from `var rows []demandReportRow` onward unchanged (the filter/score/sort/limit/total block). Verify `classUnknown` no longer appears anywhere in the file.

- [ ] **Step 7: Update the report orchestration**

In `cmd/tools/play_as/demand_cmd.go`, in `runDemand`, the current code is exactly this contiguous block:

```go
	summary, deep, err := sqlite.LoadMarketDemand(ctx)
	if err != nil {
		return fmt.Errorf("demand: load ledger: %w", err)
	}

	onHand := liveOnHand(client, ctx)
	canCraft := liveCanCraft(client, ctx)

	rep := buildDemandReport(summary, deep, onHand, canCraft, time.Now(), opts)
```

Replace that entire block (all 9 lines) with:

```go
	deep, err := sqlite.LoadMarketBuyOrders(ctx)
	if err != nil {
		return fmt.Errorf("demand: load ledger: %w", err)
	}

	onHand := liveOnHand(client, ctx)
	canCraft := liveCanCraft(client, ctx)

	rep := buildDemandReport(deep, onHand, canCraft, time.Now(), opts)
```

Keep the surrounding `globalKB`/type-assert guards and the render `switch` exactly as they are.

- [ ] **Step 8: Remove the `demand scan` dispatch branch**

In `cmd/tools/play_as/main.go`, the `case "demand":` block currently routes `scan` to `runDemandScan`. Replace the whole case with:

```go
	case "demand":
		opts, err := parseDemandOptions(parts[1:])
		if err != nil {
			return err
		}
		return runDemand(client, ctx, opts, format)
```

### Tests

- [ ] **Step 9: Rework the KB test**

Overwrite `pkg/knowledge/demand_test.go` with (keeps the migration-table assertion, drops summary tests, adds station-replace/isolation/prune):

```go
package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestMigration36CreatesDemandTable(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	var name string
	if err := kb.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, "market_buy_orders").Scan(&name); err != nil {
		t.Fatalf("table market_buy_orders not found: %v", err)
	}
}

func TestReplaceStationBuyOrdersRoundTrip(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	orders := []MarketBuyOrderRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: t0},
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 12, Quantity: 20, Source: "", CapturedAt: t0},
		{StationID: "stn1", SystemID: "sys1", ItemID: "copper", ItemName: "Copper", PriceEach: 8, Quantity: 100, Source: "station", CapturedAt: t0},
	}
	if err := kb.ReplaceStationBuyOrders(ctx, "stn1", orders); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err := kb.LoadMarketBuyOrders(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 orders, got %d", len(got))
	}
}

func TestReplaceStationBuyOrdersPrunesAndIsolates(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	// Seed two stations.
	if err := kb.ReplaceStationBuyOrders(ctx, "stnA", []MarketBuyOrderRow{
		{StationID: "stnA", ItemID: "iron_ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: t0},
		{StationID: "stnA", ItemID: "copper", PriceEach: 8, Quantity: 100, Source: "station", CapturedAt: t0},
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := kb.ReplaceStationBuyOrders(ctx, "stnB", []MarketBuyOrderRow{
		{StationID: "stnB", ItemID: "iron_ore", PriceEach: 11, Quantity: 30, Source: "station", CapturedAt: t0},
	}); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	// Replace A with fewer items: copper demand vanished -> must be pruned.
	if err := kb.ReplaceStationBuyOrders(ctx, "stnA", []MarketBuyOrderRow{
		{StationID: "stnA", ItemID: "iron_ore", PriceEach: 10, Quantity: 40, Source: "station", CapturedAt: t0},
	}); err != nil {
		t.Fatalf("replace A: %v", err)
	}

	got, err := kb.LoadMarketBuyOrders(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	counts := map[string]int{}
	for _, o := range got {
		counts[o.StationID]++
		if o.StationID == "stnA" && o.ItemID == "copper" {
			t.Errorf("stnA copper should have been pruned, but survived")
		}
	}
	if counts["stnA"] != 1 {
		t.Errorf("stnA: want 1 order after prune, got %d", counts["stnA"])
	}
	if counts["stnB"] != 1 {
		t.Errorf("stnB: want 1 order (isolated from A replace), got %d", counts["stnB"])
	}
}
```

> Note: `newTestKB(t)` already exists in `pkg/knowledge/seen_players_test.go` (`:memory:`-backed) — reuse it, do not redeclare.

- [ ] **Step 10: Rework the capture test**

Overwrite `cmd/tools/play_as/demand_capture_test.go` with:

```go
package main

import (
	"testing"
	"time"
)

func TestParseStationBuyOrders(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	// Compact response: complete buy_orders per item, source "station" or null.
	raw := []byte(`{"items":[
		{"item_id":"iron_ore","item_name":"Iron Ore","buy_orders":[
			{"price_each":10,"quantity":50,"source":"station"},
			{"price_each":12,"quantity":20,"source":null}
		]},
		{"item_id":"copper","item_name":"Copper","buy_orders":[
			{"price_each":8,"quantity":100,"source":"station"},
			{"price_each":0,"quantity":5,"source":"station"}
		]},
		{"item_id":"junk","item_name":"Junk","buy_orders":[]}
	]}`)

	rows := parseStationBuyOrders(raw, "stn1", "sys1", now)
	if len(rows) != 3 { // 2 iron + 1 copper (zero-price copper + empty junk skipped)
		t.Fatalf("want 3 orders, got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.StationID != "stn1" || r.SystemID != "sys1" || !r.CapturedAt.Equal(now) {
			t.Errorf("row metadata wrong: %+v", r)
		}
	}
	// Null source becomes "".
	var nullSrc bool
	for _, r := range rows {
		if r.ItemID == "iron_ore" && r.PriceEach == 12 {
			if r.Source != "" {
				t.Errorf("null source: want empty string, got %q", r.Source)
			}
			nullSrc = true
		}
	}
	if !nullSrc {
		t.Error("expected the price-12 iron order to be present with empty source")
	}
}

func TestParseStationBuyOrdersEmpty(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	if got := parseStationBuyOrders(nil, "stn1", "sys1", now); got != nil {
		t.Errorf("empty raw: want nil, got %+v", got)
	}
	if got := parseStationBuyOrders([]byte(`{"items":[]}`), "", "sys1", now); got != nil {
		t.Errorf("empty station: want nil, got %+v", got)
	}
}
```

- [ ] **Step 11: Rework the report test**

Overwrite `cmd/tools/play_as/demand_report_test.go` with:

```go
package main

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestBuildDemandReportClassifiesAndFulfills(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-1 * time.Hour)

	deep := []knowledge.MarketBuyOrderRow{
		// Station order at 10, null-source (player) order above it at 12 -> PLR>SM.
		{StationID: "stnA", SystemID: "sysA", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: fresh},
		{StationID: "stnA", SystemID: "sysA", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 12, Quantity: 20, Source: "", CapturedAt: fresh},
		// Pure station demand -> STN.
		{StationID: "stnB", SystemID: "sysB", ItemID: "copper", ItemName: "Copper", PriceEach: 8, Quantity: 100, Source: "station", CapturedAt: fresh},
		// Lone null-source order, no station competitor -> PLR.
		{StationID: "stnC", SystemID: "sysC", ItemID: "titanium", ItemName: "Titanium", PriceEach: 30, Quantity: 40, Source: "", CapturedAt: fresh},
	}
	onHand := map[string]float64{"iron_ore": 30}
	canCraft := map[string]int{"titanium": 5}

	rep := buildDemandReport(deep, onHand, canCraft, now, demandOptions{sort: sortByPrice})

	byItem := map[string]demandReportRow{}
	for _, r := range rep.Rows {
		byItem[r.ItemID] = r
	}
	if byItem["iron_ore"].Class != classAboveSM {
		t.Errorf("iron class: want %s got %s", classAboveSM, byItem["iron_ore"].Class)
	}
	if byItem["iron_ore"].Price != 12 || byItem["iron_ore"].Quantity != 70 {
		t.Errorf("iron price/qty: want 12/70 got %v/%v", byItem["iron_ore"].Price, byItem["iron_ore"].Quantity)
	}
	if byItem["iron_ore"].FulfillQty != 30 || byItem["iron_ore"].FulfillValue != 360 {
		t.Errorf("iron fulfill: want 30/360 got %v/%v", byItem["iron_ore"].FulfillQty, byItem["iron_ore"].FulfillValue)
	}
	if byItem["copper"].Class != classStation {
		t.Errorf("copper class: want %s got %s", classStation, byItem["copper"].Class)
	}
	if byItem["titanium"].Class != classPlayer {
		t.Errorf("titanium class: want %s got %s", classPlayer, byItem["titanium"].Class)
	}
	if byItem["titanium"].CanCraft != 5 {
		t.Errorf("titanium craft: want 5 got %d", byItem["titanium"].CanCraft)
	}
}

func TestBuildDemandReportTieStationWins(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	deep := []knowledge.MarketBuyOrderRow{
		{StationID: "s", ItemID: "x", PriceEach: 10, Quantity: 5, Source: "station", CapturedAt: now},
		{StationID: "s", ItemID: "x", PriceEach: 10, Quantity: 5, Source: "", CapturedAt: now},
	}
	rep := buildDemandReport(deep, nil, nil, now, demandOptions{})
	if len(rep.Rows) != 1 || rep.Rows[0].Class != classStation {
		t.Fatalf("tie should classify STN (station wins), got %+v", rep.Rows)
	}
}

func TestBuildDemandReportFiltersStalenessAndLimit(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)

	deep := []knowledge.MarketBuyOrderRow{
		{StationID: "s1", ItemID: "a", ItemName: "A", PriceEach: 5, Quantity: 10, Source: "station", CapturedAt: stale},
		{StationID: "s1", ItemID: "b", ItemName: "B", PriceEach: 50, Quantity: 10, Source: "station", CapturedAt: fresh},
	}

	// minPrice filters out item a (price 5).
	rep := buildDemandReport(deep, nil, nil, now, demandOptions{minPrice: 10})
	if len(rep.Rows) != 1 || rep.Rows[0].ItemID != "b" {
		t.Fatalf("minPrice filter: want only b, got %+v", rep.Rows)
	}
	// Staleness flag set for the >24h-old row when not filtered out.
	rep2 := buildDemandReport(deep, nil, nil, now, demandOptions{})
	for _, r := range rep2.Rows {
		if want := r.ItemID == "a"; r.AgeStale != want {
			t.Errorf("item %s stale: want %v got %v", r.ItemID, want, r.AgeStale)
		}
	}
	// limit truncates and TotalFulfill reflects only returned rows.
	onHand := map[string]float64{"a": 10, "b": 10}
	rep3 := buildDemandReport(deep, onHand, nil, now, demandOptions{limit: 1, sort: sortByPrice})
	if len(rep3.Rows) != 1 || rep3.Rows[0].ItemID != "b" {
		t.Fatalf("limit: want only top row b, got %+v", rep3.Rows)
	}
	if rep3.TotalFulfill != 500 { // b: 10 * 50, a excluded by limit
		t.Errorf("total-after-limit: want 500, got %v", rep3.TotalFulfill)
	}
}
```

### Build green & commit

- [ ] **Step 12: Build the whole tree**

Run: `go build ./...`
Expected: clean (no output). If anything still references `LoadMarketDemand`, `UpsertMarketDemand`, `ReplaceMarketBuyOrders`, `MarketDemandRow`, `parseDemandRows`, `parseDeepOrders`, `runDemandScan`, or `classUnknown`, fix that reference — `grep -rn` for each across `pkg/knowledge` and `cmd/tools/play_as` to confirm none remain.

- [ ] **Step 13: Run the affected package tests**

Run: `go test ./pkg/knowledge/... ./cmd/tools/play_as/...`
Expected: `ok` for both.

- [ ] **Step 14: Lint**

Run: `golangci-lint run ./pkg/knowledge/... ./cmd/tools/play_as/...`
Expected: `0 issues` (no unused symbols — confirms the old methods/consts were fully removed).

- [ ] **Step 15: Confirm no orphan references remain**

Run: `grep -rn "LoadMarketDemand\|UpsertMarketDemand\|ReplaceMarketBuyOrders\|MarketDemandRow\|parseDemandRows\|parseDeepOrders\|runDemandScan\|classUnknown\|demand scan" pkg/knowledge cmd/tools/play_as`
Expected: no output.

- [ ] **Step 16: Commit (single atomic commit)**

```bash
git add -A
git commit -m "refactor(demand): capture full source-classified orders from compact view_market

Compact view_market already returns complete buy_orders with source per item;
deep per-item scans add nothing. captureDemand now replaces the station's whole
order set on every market read. Removes the redundant market_buy_demand summary
path and the demand scan command."
```

---

## Task 2: Drop the redundant summary table (migration 37)

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go` (append migration 37 after the `version: 36` entry)
- Modify: `scripts/sql/initialize_database.sql` (regenerated by tooling)

- [ ] **Step 1: Add migration 37**

In `pkg/knowledge/sqlite_migrations.go`, inside `migrations()`, add immediately after the `version: 36` struct:

```go
		{
			version: 37,
			name:    "drop_market_buy_demand",
			sql:     `DROP TABLE IF EXISTS market_buy_demand;`,
		},
```

- [ ] **Step 2: Regenerate the initialize SQL**

The repo guards `scripts/sql/initialize_database.sql` with `TestInitializeDatabaseSQLInSync`. Regenerate it so the dropped table is removed from the dump:

```bash
bash scripts/sql/regenerate_initialize_database.sh
```

If that script path differs, find the generator: `ls scripts/sql/` and look for a `regenerate*`/`generate*` script, or run the test to see its sync instructions: `go test ./pkg/knowledge/ -run TestInitializeDatabaseSQLInSync -v`. Do NOT hand-edit the SQL dump.

- [ ] **Step 3: Verify the table is gone and SQL is in sync**

Run: `go test ./pkg/knowledge/ -run 'TestInitializeDatabaseSQLInSync|TestMigration36CreatesDemandTable' -v`
Expected: PASS (the migration-table test asserts `market_buy_orders` exists; the sync test confirms the regenerated dump matches).

Run: `grep -c "market_buy_demand" scripts/sql/initialize_database.sql`
Expected: `0`

- [ ] **Step 4: Lint + commit**

```bash
golangci-lint run ./pkg/knowledge/...
git add -A
git commit -m "feat(knowledge): migration 37 drops redundant market_buy_demand table"
```

---

## Task 3: Full gate + final verification

**Files:** none (verification only).

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 2: Full test suite**

Run: `go test ./...`
Expected: all pass (no failures introduced).

- [ ] **Step 3: Full lint**

Run: `golangci-lint run ./...`
Expected: no new findings.

- [ ] **Step 4: Rebuild the binary to bin/**

Run: `go build -o bin/play_as ./cmd/tools/play_as`
Expected: binary at `bin/play_as`.

- [ ] **Step 5: Manual smoke (requires credentials + a DB path)**

Document, do not script. With a live `play_as ... --db <path>` session:
1. Dock at a station with a market (or run `view_market`) → no errors.
2. Run `demand` → items appear classified `STN` for station orders and `PLR>SM`/`PLR` for any null-source orders that out/under-price them. No `?` rows.
3. Confirm `demand scan` is gone: running it should fall through to flag parsing and error on the unknown `scan` token (or be treated as no matching flag) — i.e. there is no deep-scan behavior.
4. `demand --station-only` shows only `STN`; `demand --only fulfillable` only rows you hold; `format json` then `demand` emits valid JSON.

---

## Self-Review Notes (for the implementer)

- **One atomic commit for Task 1:** the green-tree pre-commit hook forbids a partial state. KB method removal + play_as rewiring + scan deletion + test rework all land together.
- **Type/name consistency:** `MarketBuyOrderRow` is the only KB row type now. New/renamed symbols: `ReplaceStationBuyOrders`, `LoadMarketBuyOrders`, `parseStationBuyOrders`, `buildDemandReport(deep, onHand, canCraft, now, opts)`. Removed: `MarketDemandRow`, `UpsertMarketDemand`, `ReplaceMarketBuyOrders`, `LoadMarketDemand`, `loadMarketDemandSummary`, `parseDemandRows`, `parseDeepOrders`, `runDemandScan`, `classUnknown`, the `demand scan` dispatch branch.
- **source null → "":** `serverapi.MarketOrder.Source` is `string`; JSON `null`/missing unmarshals to `""`. Stored verbatim; `classifyDemand` treats non-`"station"` as player. No `"player"` literal ever appears in real data.
- **Migration after code:** migration 37 (drop table) lands only in Task 2, after Task 1 stopped touching `market_buy_demand`, so no commit ever has live code writing to a dropped table.
- **Phase 2 (faction storage) unaffected** — still a separate future plan.
```
