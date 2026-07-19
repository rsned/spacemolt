# Depth-Aware Price Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop mission-runners (and haulers) from overpaying for inputs by gating buys against real order-book depth and an absolute surge ceiling, instead of a single best-ask.

**Architecture:** A shared pure `CostToAcquire` depth-walk primitive in `pkg/market`, plus two store queries (`GetAskLadder` for haulers, `GetReferencePrice` for the surge ceiling). The mission-runner gains a local depth-walk gate at its current station (it buys where it stands), reusing the `view_market` it already fetches. Haulers swap `sizeBuy`/`haulGate` from best-ask to the depth-walked average.

**Tech Stack:** Go 1.24+, `modernc.org/sqlite` (in-memory for tests), existing `pkg/market.Collector` + `pkg/worker` engines.

## Global Constraints

- Go 1.24+; benchmarks use `b.Loop()`, not `for range b.N`.
- `golangci-lint` must stay clean (run after each task); `go build ./...` and `go test ./...` before finishing.
- Sleeps use `pkg/game/constants.go` constants only.
- Do NOT commit runtime `data/*.json` churn; stage files explicitly (never `git add -A`).
- Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Reuse existing thresholds: `haulMinMargin = 0.03`, `haulMinNetProfit = 1000`, `missionMinNet` (mission net floor). New knobs: `surgeMult = 4.0`, `referenceLookback = 24h`, reference percentile = 20th.

---

## File Structure

- `pkg/market/depth.go` (new) — `AskLevel`, `CostToAcquire`.
- `pkg/market/depth_test.go` (new) — table tests for `CostToAcquire`.
- `pkg/market/query.go` (modify) — add `GetAskLadder`, `GetReferencePrice`.
- `pkg/market/query_test.go` (modify or new `query_depth_test.go`) — store-query tests.
- `pkg/worker/mission.go` (modify) — ladder fetch + local depth-walk gate; `MissionStore` gains `GetReferencePrice`.
- `pkg/worker/mission_test.go` (modify) — `fakeMissionStore` gains `GetReferencePrice`; gate tests incl. the fighter-4 case.
- `pkg/worker/haul.go` (modify) — `sizeBuy`/`haulGate` depth-walk.
- `pkg/worker/haul_test.go` (modify) — thin-book sizes-down test.

---

## Task 1: `CostToAcquire` depth-walk primitive

**Files:**
- Create: `pkg/market/depth.go`
- Test: `pkg/market/depth_test.go`

**Interfaces:**
- Produces: `type AskLevel struct { PriceEach, Quantity float64 }` and
  `func CostToAcquire(asks []AskLevel, qty float64) (totalCost, filled, avgPrice float64, enoughDepth bool)`.
  `asks` must be sorted ascending by `PriceEach` (callers guarantee this). Walks cheapest-first,
  filling up to `qty`. `avgPrice = totalCost/filled` (0 when `filled==0`). `enoughDepth` is
  `filled >= qty` (within a tiny epsilon).

- [ ] **Step 1: Write the failing test**

```go
package market

import (
	"math"
	"testing"
)

func TestCostToAcquire(t *testing.T) {
	tests := []struct {
		name                       string
		asks                       []AskLevel
		qty                        float64
		wantCost, wantFill, wantAvg float64
		wantEnough                 bool
	}{
		{"empty book", nil, 10, 0, 0, 0, false},
		{"single level exact", []AskLevel{{10, 5}}, 5, 50, 5, 10, true},
		{"single level partial", []AskLevel{{10, 5}}, 3, 30, 3, 10, true},
		{"thin book underfills", []AskLevel{{10, 2}}, 5, 20, 2, 10, false},
		{"walks up the ladder", []AskLevel{{10, 2}, {20, 2}, {2000, 100}}, 5, 10*2 + 20*2 + 2000*1, 5, (20 + 40 + 2000) / 5, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cost, fill, avg, enough := CostToAcquire(tc.asks, tc.qty)
			if math.Abs(cost-tc.wantCost) > 1e-6 || math.Abs(fill-tc.wantFill) > 1e-6 ||
				math.Abs(avg-tc.wantAvg) > 1e-6 || enough != tc.wantEnough {
				t.Fatalf("CostToAcquire(%v,%v) = (%v,%v,%v,%v), want (%v,%v,%v,%v)",
					tc.asks, tc.qty, cost, fill, avg, enough, tc.wantCost, tc.wantFill, tc.wantAvg, tc.wantEnough)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestCostToAcquire -v`
Expected: FAIL — `undefined: CostToAcquire` / `undefined: AskLevel`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package market — order-book depth helpers.
package market

// AskLevel is one price level of the sell side of the book.
type AskLevel struct {
	PriceEach float64
	Quantity  float64
}

// CostToAcquire walks asks cheapest-first, filling up to qty. asks MUST be
// sorted ascending by PriceEach. It returns the total cost of the filled
// units, how many units were fillable, the volume-weighted average price
// (0 when nothing filled), and whether the ladder had enough depth to fill
// qty in full.
func CostToAcquire(asks []AskLevel, qty float64) (totalCost, filled, avgPrice float64, enoughDepth bool) {
	remaining := qty
	for _, lvl := range asks {
		if remaining <= 0 {
			break
		}
		take := lvl.Quantity
		if take > remaining {
			take = remaining
		}
		totalCost += take * lvl.PriceEach
		filled += take
		remaining -= take
	}
	if filled > 0 {
		avgPrice = totalCost / filled
	}
	enoughDepth = filled+1e-9 >= qty
	return totalCost, filled, avgPrice, enoughDepth
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestCostToAcquire -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/market/... && \
git add pkg/market/depth.go pkg/market/depth_test.go && \
git commit -m "feat(market): CostToAcquire depth-walk primitive

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `GetAskLadder` store query (hauler ladder source)

**Files:**
- Modify: `pkg/market/query.go`
- Test: `pkg/market/query_depth_test.go` (new)

**Interfaces:**
- Consumes: `AskLevel` (Task 1).
- Produces: `func (c *Collector) GetAskLadder(ctx context.Context, itemID, stationID string) ([]AskLevel, error)` —
  ascending-price sell-side levels for the item at that station's latest capture; empty slice when none.

**Context:** Mirror the latest-capture-per-station pattern already in `GetItemStationPrices`
(`pkg/market/arbitrage.go:16`). Test collector built via `newTestCollector` (`pkg/market/collector_test.go:340`).

- [ ] **Step 1: Write the failing test**

```go
package market

import (
	"context"
	"testing"
	"time"
)

func TestGetAskLadder(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	now := time.Now().UTC()
	orders := []Order{
		{StationID: "s1", ItemID: "iron_ore", Side: "sell", PriceEach: 2000, Quantity: 100, CapturedAt: now},
		{StationID: "s1", ItemID: "iron_ore", Side: "sell", PriceEach: 10, Quantity: 3, CapturedAt: now},
		{StationID: "s1", ItemID: "iron_ore", Side: "buy", PriceEach: 5, Quantity: 50, CapturedAt: now},
	}
	if err := c.insertOrders(nil, orders); err != nil { // if insertOrders needs a tx, use the collector's capture path instead
		t.Fatalf("seed: %v", err)
	}
	got, err := c.GetAskLadder(ctx, "iron_ore", "s1")
	if err != nil {
		t.Fatalf("GetAskLadder: %v", err)
	}
	if len(got) != 2 || got[0].PriceEach != 10 || got[1].PriceEach != 2000 {
		t.Fatalf("ladder = %+v, want [{10,3},{2000,100}] ascending, sell-side only", got)
	}
}
```

> **Note for implementer:** if `insertOrders` requires a `*sql.Tx` (see `pkg/market/collector.go:267`), seed via the collector's normal capture/insert entry point instead (look for how `collector_test.go` seeds orders — reuse that helper). The assertion is what matters: sell-only, ascending, latest capture.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestGetAskLadder -v`
Expected: FAIL — `undefined: (*Collector).GetAskLadder`.

- [ ] **Step 3: Write minimal implementation** (append to `pkg/market/query.go`)

```go
// GetAskLadder returns the sell-side price levels (ascending by price) for an
// item at a station's latest capture. Empty when the item has no sell orders
// there. Callers pass the result straight to CostToAcquire.
func (c *Collector) GetAskLadder(ctx context.Context, itemID, stationID string) ([]AskLevel, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT o.price_each, o.quantity
		FROM market_orders o
		JOIN (
			SELECT MAX(captured_at) AS mx
			FROM market_orders
			WHERE item_id = ? AND station_id = ?
		) latest ON o.captured_at = latest.mx
		WHERE o.item_id = ? AND o.station_id = ? AND o.side = 'sell'
		  AND o.price_each > 0 AND o.quantity > 0
		ORDER BY o.price_each ASC`, itemID, stationID, itemID, stationID)
	if err != nil {
		return nil, fmt.Errorf("query ask ladder: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AskLevel
	for rows.Next() {
		var p, q float64
		if err := rows.Scan(&p, &q); err != nil {
			return nil, fmt.Errorf("scan ask ladder: %w", err)
		}
		out = append(out, AskLevel{PriceEach: p, Quantity: q})
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestGetAskLadder -v`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/market/... && \
git add pkg/market/query.go pkg/market/query_depth_test.go && \
git commit -m "feat(market): GetAskLadder for depth-aware pricing

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `GetReferencePrice` store query (surge ceiling basis)

**Files:**
- Modify: `pkg/market/query.go`
- Test: `pkg/market/query_depth_test.go`

**Interfaces:**
- Produces: `func (c *Collector) GetReferencePrice(ctx context.Context, itemID string, lookback time.Duration) (float64, bool, error)` —
  a low-percentile (20th) of per-station best-asks captured within `lookback`, so one gouging
  station is an outlier. Returns `(0, false, nil)` when there is no recent data.

**Context:** Model on `GetReferenceAsk` (`pkg/market/query.go:264`). Compute each station's latest
best-ask within the window, then take the 20th-percentile across stations.

- [ ] **Step 1: Write the failing test**

```go
func TestGetReferencePrice(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	now := time.Now().UTC()
	// Five stations offering iron_ore ~5-10, one gouging @2000.
	seed := []Order{
		{StationID: "a", ItemID: "iron_ore", Side: "sell", PriceEach: 6, Quantity: 50, CapturedAt: now},
		{StationID: "b", ItemID: "iron_ore", Side: "sell", PriceEach: 7, Quantity: 50, CapturedAt: now},
		{StationID: "c", ItemID: "iron_ore", Side: "sell", PriceEach: 8, Quantity: 50, CapturedAt: now},
		{StationID: "d", ItemID: "iron_ore", Side: "sell", PriceEach: 9, Quantity: 50, CapturedAt: now},
		{StationID: "e", ItemID: "iron_ore", Side: "sell", PriceEach: 10, Quantity: 50, CapturedAt: now},
		{StationID: "z", ItemID: "iron_ore", Side: "sell", PriceEach: 2000, Quantity: 50, CapturedAt: now},
	}
	seedOrders(t, c, seed) // reuse the same seed helper as Task 2
	ref, ok, err := c.GetReferencePrice(ctx, "iron_ore", 24*time.Hour)
	if err != nil || !ok {
		t.Fatalf("GetReferencePrice: ok=%v err=%v", ok, err)
	}
	if ref > 12 { // 20th pct of {6,7,8,9,10,2000} must sit in the cheap cluster, not near 2000
		t.Fatalf("reference %v; expected cheap-cluster value, gouging station must be an outlier", ref)
	}
	if _, ok, _ := c.GetReferencePrice(ctx, "no_such_item", 24*time.Hour); ok {
		t.Fatalf("expected ok=false for unknown item")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestGetReferencePrice -v`
Expected: FAIL — `undefined: (*Collector).GetReferencePrice`.

- [ ] **Step 3: Write minimal implementation** (append to `pkg/market/query.go`)

```go
// GetReferencePrice returns a robust "cheap" price for an item: the 20th
// percentile of per-station latest best-asks captured within lookback. A
// single gouging station therefore cannot set the reference. Returns
// (0,false,nil) when no recent sell data exists.
func (c *Collector) GetReferencePrice(ctx context.Context, itemID string, lookback time.Duration) (float64, bool, error) {
	cutoff := time.Now().UTC().Add(-lookback).Format(time.RFC3339)
	rows, err := c.db.QueryContext(ctx, `
		SELECT MIN(price_each) AS best_ask
		FROM market_orders
		WHERE item_id = ? AND side = 'sell' AND price_each > 0 AND quantity > 0
		  AND captured_at >= ?
		GROUP BY station_id
		ORDER BY best_ask ASC`, itemID, cutoff)
	if err != nil {
		return 0, false, fmt.Errorf("query reference price: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var asks []float64
	for rows.Next() {
		var a float64
		if err := rows.Scan(&a); err != nil {
			return 0, false, fmt.Errorf("scan reference price: %w", err)
		}
		asks = append(asks, a)
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	if len(asks) == 0 {
		return 0, false, nil
	}
	// asks already ascending; 20th percentile by nearest-rank.
	idx := int(0.20 * float64(len(asks)-1))
	return asks[idx], true, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestGetReferencePrice -v`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/market/... && \
git add pkg/market/query.go pkg/market/query_depth_test.go && \
git commit -m "feat(market): GetReferencePrice (low-percentile) for surge ceiling

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Mission local depth-walk gate

**Files:**
- Modify: `pkg/worker/mission.go` (`MissionStore` iface; `missionFetchMarketSupply` → ladders; availability-gate block ~`:436-472`)
- Test: `pkg/worker/mission_test.go` (`fakeMissionStore` gains `GetReferencePrice`; new gate tests)

**Interfaces:**
- Consumes: `market.CostToAcquire`, `market.AskLevel` (Task 1); `MissionStore.GetReferencePrice` (new).
- Produces: `missionFetchMarketLadders(ctx, deps) (map[string][]market.AskLevel, error)` returning
  per-item ascending ladders from the current station's `view_market`; the availability gate re-prices
  each candidate's `ItemCost`/`Net` from the local ladder and skips on thin depth, surge, or sub-floor net.

**Context:** `missionCandidate` (`pkg/worker/mission_select.go:35`) carries `BuyQty, Reward, ItemCost, FuelCost, Net`.
The market buy happens locally (`Buy` at `mission.go:539`); storage-provided units are withdrawn first, so
the market portion is `BuyQty − min(storage, BuyQty)`. `surgeMult`, `referenceLookback` are new constants.

- [ ] **Step 1: Add `GetReferencePrice` to `MissionStore` + the fake**

In `pkg/worker/mission.go`, extend the interface:

```go
type MissionStore interface {
	RecordMissionResult(ctx context.Context, r market.MissionResult) error
	GetReferenceAsk(ctx context.Context, itemID string) (market.ReferenceAsk, bool, error)
	GetReferencePrice(ctx context.Context, itemID string, lookback time.Duration) (float64, bool, error)
}
```

In `pkg/worker/mission_test.go`, add to `fakeMissionStore` (fields + method):

```go
// add field: refPrices map[string]float64
func (s *fakeMissionStore) GetReferencePrice(ctx context.Context, itemID string, lookback time.Duration) (float64, bool, error) {
	p, ok := s.refPrices[itemID]
	return p, ok, nil
}
```

- [ ] **Step 2: Write the failing test — fighter-4 case (local surge → skip, zero spend)**

Add to `pkg/worker/mission_test.go`. Reuse the existing board/deps helpers; seed a local `view_market`
where the mission item's cheap depth is thin and the rest of the book is a 2000 wall, and a reference of ~7.

```go
func TestMissionSkipsLocalPriceSurge(t *testing.T) {
	// Board: a deliver mission needing 25 iron_ore, reward well under 50k.
	fc := newMissionFakeClient(t, /* board with deliver mission: iron_ore x25, reward 5000 */)
	// Local view_market ladder: 3 @10, then 100 @2000 (surge wall) — same shape as fighter-4.
	fc.setViewMarket(map[string][]serverapi.MarketOrder{
		"iron_ore": {{PriceEach: 10, Quantity: 3}, {PriceEach: 2000, Quantity: 100}},
	})
	store := &fakeMissionStore{
		asks:      map[string]float64{"iron_ore": 8}, // coarse accept-time reference stays cheap
		refPrices: map[string]float64{"iron_ore": 7}, // authoritative surge basis
	}
	deps := missionDeps(fc, store, newFakeKB(t))
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if fc.buyCalls != 0 {
		t.Fatalf("expected zero Buy calls (surge gate), got %d", fc.buyCalls)
	}
	// And the mission must be marked attempted, not accepted.
}
```

> **Implementer:** match the exact board/deps/fake helpers already used by neighboring tests
> (`missionDeps` at `mission_test.go:144`, the `serverapi.ViewMarketItem` builder at `:120`,
> `fakeClient` buy counter in `dispatch_test.go`). Add a `buyCalls` counter to `fakeClient.Buy`
> if one is not already present, and a `setViewMarket` helper if the existing one does not take
> per-item ladders. Keep the reward below the 50k it would cost so the *net* gate would also
> catch it — but assert the surge path so the test pins the ceiling behavior.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run TestMissionSkipsLocalPriceSurge -v`
Expected: FAIL — mission accepted / `buyCalls > 0` (no local gate yet), or compile error until Step 4.

- [ ] **Step 4: Implement `missionFetchMarketLadders` + the gate**

Add near `missionFetchMarketSupply` (`mission.go:983`):

```go
// missionFetchMarketLadders fetches the current station's view_market and
// returns per-item ascending-by-price sell ladders (positive price+qty only).
// A nil map with nil error means "no data" (raw store not settled).
func missionFetchMarketLadders(ctx context.Context, deps MissionDeps) (map[string][]market.AskLevel, error) {
	if err := deps.Client.ViewMarket(ctx, map[string]any{}); err != nil {
		return nil, err
	}
	_ = deps.sleep(ctx, game.SleepQuick)
	raw := deps.Client.GetRawJSON("market")
	if len(raw) == 0 {
		return nil, nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse market: %w", err)
	}
	ladders := make(map[string][]market.AskLevel, len(resp.Items))
	for _, it := range resp.Items {
		var lvls []market.AskLevel
		for _, o := range it.SellOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			lvls = append(lvls, market.AskLevel{PriceEach: o.PriceEach, Quantity: o.Quantity})
		}
		sort.Slice(lvls, func(a, b int) bool { return lvls[a].PriceEach < lvls[b].PriceEach })
		ladders[it.ItemID] = lvls
	}
	return ladders, nil
}
```

Replace the availability-gate body (`mission.go` ~`:452-472`, the `supply, merr := missionFetchMarketSupply(...)`
block) with a ladder-based gate. Keep the "view_market unavailable → skip gate this pass" fail-open:

```go
ladders, merr := missionFetchMarketLadders(ctx, deps)
if merr != nil || ladders == nil {
	fmt.Fprintf(out, "missions: view_market unavailable (%v); availability gate skipped this pass\n", merr) //nolint:errcheck
} else {
	acquirable := make([]missionCandidate, 0, len(set))
	for _, c := range set {
		if c.BuyQty <= 0 {
			acquirable = append(acquirable, c)
			continue
		}
		marketQty := float64(c.BuyQty) - min(storage[c.ItemID], float64(c.BuyQty))
		lad := ladders[c.ItemID]
		localCost, _, avgFill, enough := market.CostToAcquire(lad, marketQty)
		if marketQty > 0 && !enough {
			fmt.Fprintf(out, "missions: skip %s (%s): market here can't fill %.0f %s\n", c.Entry.MissionID, c.Entry.Title, marketQty, c.ItemID) //nolint:errcheck
			deps.State.markAttempted(c.Entry.MissionID)
			continue
		}
		if ref, ok, _ := deps.Market.GetReferencePrice(ctx, c.ItemID, missionReferenceLookback); ok && avgFill > missionSurgeMult*ref {
			fmt.Fprintf(out, "missions: skip %s (%s): %s avg %.0f > %.1fx reference %.0f (surge)\n", c.Entry.MissionID, c.Entry.Title, c.ItemID, avgFill, missionSurgeMult, ref) //nolint:errcheck
			deps.State.markAttempted(c.Entry.MissionID)
			continue
		}
		net := c.Reward - localCost - c.FuelCost
		if net < missionMinNet {
			fmt.Fprintf(out, "missions: skip %s (%s): local net %.0f below floor %.0f (item cost %.0f)\n", c.Entry.MissionID, c.Entry.Title, net, missionMinNet, localCost) //nolint:errcheck
			deps.State.markAttempted(c.Entry.MissionID)
			continue
		}
		c.ItemCost, c.Net = localCost, net // buy step reads ItemCost for unit cost
		acquirable = append(acquirable, c)
	}
	set = acquirable
	if len(set) == 0 {
		fmt.Fprintln(out, "missions: no acquirable missions on this board") //nolint:errcheck
		return missionDryPass(ctx, deps, out)
	}
}
```

Add constants near the mission gate (top of `mission.go` const block):

```go
const (
	missionSurgeMult        = 4.0             // reject local avg-fill > 4x reference price
	missionReferenceLookback = 24 * time.Hour // window for GetReferencePrice
)
```

> **Note:** the `storage` map is populated just above this block (the existing
> `missionFetchStorage` call); keep that call. Remove the now-unused `missionFetchMarketSupply`
> only if nothing else references it (grep first; if still used elsewhere, leave it).

- [ ] **Step 5: Run the gate test + full worker suite**

Run: `go test ./pkg/worker/ -run TestMissionSkipsLocalPriceSurge -v`
Expected: PASS (zero Buy calls).
Run: `go test ./pkg/worker/ -v`
Expected: PASS — existing mission tests still green (adjust any that relied on the old
quantity-only gate to seed `refPrices` and ladders as needed).

- [ ] **Step 6: Lint + commit**

```bash
golangci-lint run ./pkg/worker/... && \
git add pkg/worker/mission.go pkg/worker/mission_test.go && \
git commit -m "feat(mission): local depth-walk price gate + surge ceiling

Prices required inputs against the current station's live order-book ladder
and an absolute reference-price ceiling before buying; skips the mission
(no buy) when depth is thin, price surges, or net falls below floor. Closes
the fighter-4 overpay (iron_ore @2000).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Hauler depth-aware sizing

**Files:**
- Modify: `pkg/worker/haul.go` (`sizeBuy` `:357`, `haulGate` `:94`)
- Test: `pkg/worker/haul_test.go`

**Interfaces:**
- Consumes: `market.CostToAcquire`, `OpportunityStore.GetAskLadder` (Task 2 — add to the `OpportunityStore`
  interface at `haul.go:378` and to the fake).
- Produces: `haulGate` prices the intended quantity via the depth-walked average rather than `BestAsk`,
  so a thin book sizes the buy down / rejects instead of assuming best-ask holds.

**Context:** `haulGate` (`haul.go:94`) currently reads `liveAsk = p.BestAsk` from `[]market.ItemStationPrice`.
The hauler already recaptures the buy station into the store (`RecaptureBuyMarket`), so the ladder is
available via `GetAskLadder(itemID, fromStationID)`.

- [ ] **Step 1: Write the failing test — thin book sizes down vs best-ask**

Add to `pkg/worker/haul_test.go`, following the existing `haulGate`/fake-store test style. Seed a
buy-station ladder that is cheap at the top but a surge wall below; assert the gate uses the
depth-walked average (rejects or sizes down) rather than best-ask.

```go
func TestHaulGateUsesDepthNotBestAsk(t *testing.T) {
	// Ladder: 2 @100 (best-ask), then 1000-wall. Sell bid 150.
	// Best-ask math (100 vs 150) looks profitable; depth-walked avg for the
	// intended qty exceeds 150 → gate must reject (spread too thin).
	// ... build opp + prices/ladder via the existing haul test fakes ...
	// assert ok == false with a "spread too thin" reason.
}
```

> **Implementer:** mirror the existing `haulGate` unit tests (search `haul_test.go` for `haulGate(`).
> Provide the ladder through whatever the gate consumes after this task (either widen the
> `[]market.ItemStationPrice` seed with a ladder field, or pass `[]market.AskLevel` — see Step 3).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run TestHaulGateUsesDepthNotBestAsk -v`
Expected: FAIL — gate passes on best-ask.

- [ ] **Step 3: Implement depth-aware sizing in `haulGate`/`sizeBuy`**

In `haulGate` (`haul.go:94`), after resolving the intended `qty` via `sizeBuy`, compute the effective
ask from the ladder instead of `liveAsk = p.BestAsk`:

```go
// asks: the ascending ladder for opp.ItemID at opp.FromStationID, passed in by
// the caller (from OpportunityStore.GetAskLadder after RecaptureBuyMarket).
_, filled, avgAsk, enough := market.CostToAcquire(asks, qty)
if !enough || filled <= 0 || avgAsk <= 0 {
	return qty, avgAsk, sellBid, false, "insufficient ask depth at buy station"
}
liveAsk = avgAsk // gate margin/net on the real fill price, not top-of-book
```

Thread the ladder into `haulGate` (add an `asks []market.AskLevel` parameter) and populate it at the
call site (`haul.go` ~`:507`, after `RecaptureBuyMarket`) via `deps.Market.GetAskLadder(ctx, opp.ItemID, opp.FromStationID)`.
Add `GetAskLadder` to the `OpportunityStore` interface (`haul.go:378`) and to the hauler test fake.
Optionally apply the same surge ceiling with `GetReferencePrice` for parity with missions (add to
`OpportunityStore` if desired; not required for this task).

- [ ] **Step 4: Run test + full haul suite**

Run: `go test ./pkg/worker/ -run TestHaulGate -v`
Expected: PASS.
Run: `go test ./pkg/worker/ -v`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/worker/... && \
git add pkg/worker/haul.go pkg/worker/haul_test.go && \
git commit -m "feat(haul): gate on depth-walked ask instead of best-ask

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Final Verification

- [ ] `go build ./...` — clean.
- [ ] `go test ./...` — green (note: pre-existing `pkg/game TestServerCommandsCoveredByClient` RED is unrelated API drift).
- [ ] `golangci-lint run` — no new findings.
- [ ] Rollout (post-merge, ops): deploy to the mission-learn pool first, watch dry-pass rate vs. the fighter-4 overpay class; then canary one hauler before the fleet.

## Self-Review Notes

- **Spec coverage:** CostToAcquire (§1) = Task 1; GetReferencePrice (§2) = Task 3; mission local gate (§3) = Task 4; hauler hardening (§4) = Task 5; GetAskLadder store query (§1 hauler ladder) = Task 2. Thresholds (§5) in Global Constraints + Task 4 constants.
- **Type consistency:** `AskLevel`/`CostToAcquire` signatures identical across Tasks 1/2/4/5; `GetReferencePrice` signature identical in Task 3 (market) and Task 4 (MissionStore) and the fake.
- **Storage interaction:** the mission gate prices `marketQty = BuyQty − min(storage, BuyQty)`, matching the withdraw-then-buy execution at `mission.go:516-539`.
