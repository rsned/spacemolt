# Net-of-fuel Hauler Economics (Arbitrage Sub-project B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Haulers rank and gate opportunities by profit *net of fuel cost*, so nearer/cheaper routes beat far/fuel-expensive ones of similar gross, and unprofitable or absurdly long hauls are rejected.

**Architecture:** A per-pass fuel model computed client-side (jump counts from the existing BFS graph × the ship's `fuel_per_jump` from one `find_route` probe × the pickup station's captured all-in price from Sub-project A). Ranking switches from `effectiveGross` to `effectiveNet`; the pre-buy gate subtracts the forward (haul-leg) fuel; a hard `HaulMaxHaulJumps` cap backstops the haul leg. All changes are in `pkg/worker` plus one small `pkg/market` read helper. No persistence, no schema change.

**Tech Stack:** Go 1.24, `pkg/worker/haul.go`, `pkg/market` (SQLite collector), `pkg/navigation` (jump graph/BFS).

## Global Constraints

- Target Go 1.24+; modern features where applicable (`max`, range-over-int).
- New code must pass `golangci-lint` with no new findings.
- Run `go build ./...` and `go test ./...` before every commit.
- `go test ./pkg/worker/` is slow (~120s): use a Bash `timeout` ≥ 300000ms for any command running the worker package tests, and the pre-commit race hook (~140s) also needs ≥ 300000ms on `git commit`.
- Stage ONLY the files each task names (`git add` explicit paths). The tree has unrelated modified runtime files under `data/` — never `git add -A`.
- **Graceful degradation is a hard requirement:** when the fuel rate is 0 (no probe) or no prices are captured, every fuel cost is 0 and ranking/gating must reproduce today's gross-only behavior exactly. Existing `RankHaulOpportunities`/`haulGate` tests that pass no fuel model must keep passing unchanged in behavior.

---

### Task 1: `MedianStationFuelAllIn` read helper

**Files:**
- Modify: `pkg/market/collector.go` (add method)
- Test: `pkg/market/station_fuel_test.go` (add `TestMedianStationFuelAllIn`)

**Interfaces:**
- Consumes: the `station_fuel_prices` table + `UpsertStationFuel`/`StationFuel` (Sub-project A, already on main).
- Produces: `(*Collector).MedianStationFuelAllIn(ctx) (median int, ok bool, err error)`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/market/station_fuel_test.go` (add `"fmt"` to its import block):

```go
func TestMedianStationFuelAllIn(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close() //nolint:errcheck
	ctx := context.Background()

	// Empty -> ok=false, no error.
	if _, ok, err := c.MedianStationFuelAllIn(ctx); err != nil || ok {
		t.Fatalf("empty: want ok=false err=nil, got ok=%v err=%v", ok, err)
	}

	put := func(id string, allIn int) {
		if err := c.UpsertStationFuel(ctx, StationFuel{
			StationID: id, FuelPrice: 1, FuelTaxPerUnit: 1, FuelPriceAllIn: allIn,
			CapturedAt: "2026-07-15T00:00:00Z", CapturedBy: "t",
		}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	// Odd count {3,6,9} (inserted out of order) -> median 6.
	put("s0", 3)
	put("s1", 9)
	put("s2", 6)
	if m, ok, err := c.MedianStationFuelAllIn(ctx); err != nil || !ok || m != 6 {
		t.Fatalf("odd: got m=%d ok=%v err=%v (want 6/true)", m, ok, err)
	}

	// Even count {3,6,9,12} -> median (6+9)/2 = 7 (integer).
	put("s3", 12)
	if m, ok, _ := c.MedianStationFuelAllIn(ctx); !ok || m != 7 {
		t.Fatalf("even: got m=%d ok=%v (want 7/true)", m, ok)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/market/ -run TestMedianStationFuelAllIn -v`
Expected: FAIL — `MedianStationFuelAllIn` undefined.

- [ ] **Step 3: Implement the method**

In `pkg/market/collector.go`, near `GetStationFuelPrice`:

```go
// MedianStationFuelAllIn returns the median captured all-in fuel price across all
// stations. ok is false (no error) when no prices have been captured yet.
func (c *Collector) MedianStationFuelAllIn(ctx context.Context) (median int, ok bool, err error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT fuel_price_all_in FROM station_fuel_prices ORDER BY fuel_price_all_in`)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close() //nolint:errcheck
	var vals []int
	for rows.Next() {
		var v int
		if scanErr := rows.Scan(&v); scanErr != nil {
			return 0, false, scanErr
		}
		vals = append(vals, v)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return 0, false, rowsErr
	}
	if len(vals) == 0 {
		return 0, false, nil
	}
	n := len(vals)
	if n%2 == 1 {
		return vals[n/2], true, nil
	}
	return (vals[n/2-1] + vals[n/2]) / 2, true, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/market/ -run TestMedianStationFuelAllIn -v`
Expected: PASS.

- [ ] **Step 5: Build, package test, lint**

Run: `go build ./... && go test ./pkg/market/ && golangci-lint run ./pkg/market/...`
Expected: build ok; tests pass; no new lint findings.

- [ ] **Step 6: Commit**

```bash
git add pkg/market/collector.go pkg/market/station_fuel_test.go
git commit -m "feat(market): MedianStationFuelAllIn helper for fuel-price fallback"
```

---

### Task 2: Fuel-model primitives in `pkg/worker`

**Files:**
- Create: `pkg/worker/haul_fuel.go`
- Modify: `pkg/worker/haul.go` (add `FuelPrices` field to `HaulDeps`)
- Modify: `pkg/worker/dispatch.go` (wire `FuelPrices: d.Market` into the haul `HaulDeps`)
- Test: `pkg/worker/haul_fuel_test.go` (new)

**Interfaces:**
- Consumes: `(*market.Collector).GetStationFuelPrice` + `MedianStationFuelAllIn` (Task 1); `navigation.BFSJumps`, `navigation.RouteInf`, `navigation.JumpGraph`; `parseFuelEstimates` (existing, `autopilot.go`); `game.GameClient`.
- Produces: `HaulMaxHaulJumps` const; `haulFreeFuelStations` set; `FuelPriceSource` interface; `HaulDeps.FuelPrices FuelPriceSource`; `haulFuel` struct with `legCost(jumps int, pickupStation string) float64` and `haulJumpsBetween(fromSys, toSys string) (int, bool)`; `buildPriceOf(ctx, FuelPriceSource) func(string) float64`; `haulFuelPerJump(ctx, game.GameClient, probeTarget string) int`.

- [ ] **Step 1: Write the failing tests**

Create `pkg/worker/haul_fuel_test.go`:

```go
package worker

import (
	"context"
	"testing"
	"time"
)

// fakeFuelPrices is a stub FuelPriceSource.
type fakeFuelPrices struct {
	prices map[string]int // stationID -> all-in; absent => ok=false
	median int
	hasMed bool
}

func (f *fakeFuelPrices) GetStationFuelPrice(_ context.Context, id string) (int, time.Time, bool, error) {
	if v, ok := f.prices[id]; ok {
		return v, time.Time{}, true, nil
	}
	return 0, time.Time{}, false, nil
}
func (f *fakeFuelPrices) MedianStationFuelAllIn(_ context.Context) (int, bool, error) {
	return f.median, f.hasMed, nil
}

func TestBuildPriceOf(t *testing.T) {
	ctx := context.Background()
	src := &fakeFuelPrices{prices: map[string]int{"sol_central": 8}, median: 6, hasMed: true}
	priceOf := buildPriceOf(ctx, src)

	if got := priceOf("grand_exchange_station"); got != 0 {
		t.Errorf("free pump: want 0, got %v", got)
	}
	if got := priceOf("sol_central"); got != 8 {
		t.Errorf("captured: want 8, got %v", got)
	}
	if got := priceOf("uncaptured_station"); got != 6 {
		t.Errorf("uncaptured -> median: want 6, got %v", got)
	}

	// No median available -> uncaptured resolves to 0.
	noMed := buildPriceOf(ctx, &fakeFuelPrices{prices: map[string]int{}, hasMed: false})
	if got := noMed("anything"); got != 0 {
		t.Errorf("no median: want 0, got %v", got)
	}
	// Nil source -> always 0 (gross-only fallback).
	if got := buildPriceOf(ctx, nil)("sol_central"); got != 0 {
		t.Errorf("nil source: want 0, got %v", got)
	}
}

func TestHaulFuelLegCost(t *testing.T) {
	hf := haulFuel{perJump: 3, priceOf: func(string) float64 { return 5 }}
	if got := hf.legCost(4, "s"); got != 60 { // 4*3*5
		t.Errorf("legCost: want 60, got %v", got)
	}
	if got := hf.legCost(0, "s"); got != 0 {
		t.Errorf("zero jumps: want 0, got %v", got)
	}
	// Zero rate -> zero cost (gross-only fallback), price never consulted.
	zero := haulFuel{perJump: 0, priceOf: func(string) float64 { return 99 }}
	if got := zero.legCost(4, "s"); got != 0 {
		t.Errorf("zero rate: want 0, got %v", got)
	}
}

func TestHaulJumpsBetween(t *testing.T) {
	g, _ := graphFor([]string{"a", "b", "c"}, [2]string{"a", "b"}, [2]string{"b", "c"})
	hf := haulFuel{graph: g}
	if j, ok := hf.haulJumpsBetween("a", "c"); !ok || j != 2 {
		t.Errorf("a->c: want 2/true, got %d/%v", j, ok)
	}
	if _, ok := hf.haulJumpsBetween("a", ""); ok {
		t.Error("empty target: want ok=false")
	}
	if _, ok := hf.haulJumpsBetween("a", "unknown"); ok {
		t.Error("unreachable: want ok=false")
	}
}

func TestHaulFuelPerJumpServerThenFallback(t *testing.T) {
	ctx := context.Background()

	// Server value: find_route probe populates "_last" with fuel_per_jump.
	srv := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"_last": []byte(`{"fuel_per_jump":4}`)},
	}
	if got := haulFuelPerJump(ctx, srv, "b"); got != 4 {
		t.Errorf("server path: want 4, got %d", got)
	}

	// Fallback: no "_last", ship-class formula ceil(scale^1.5 * speed * 10 * 0.10).
	// scale=4, speed=2 -> ceil(8 * 2 * 1.0) = 16.
	fb := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"ship": []byte(`{"class":{"scale":4,"base_speed":2}}`)},
	}
	if got := haulFuelPerJump(ctx, fb, "b"); got != 16 {
		t.Errorf("fallback path: want 16, got %d", got)
	}

	// Neither -> 0.
	none := &fakeClient{state: &game.State{}, raw: map[string][]byte{}}
	if got := haulFuelPerJump(ctx, none, "b"); got != 0 {
		t.Errorf("no data: want 0, got %d", got)
	}
}
```

> Note: the shared `fakeClient` (defined in `dispatch_test.go`) records `find_route` and returns from its `raw` map on `GetRawJSON`. Confirm its `FindRoute` returns a nil error for these tests (it does not populate `"_last"` itself, so the tests pre-seed it). If `FindRoute` on the fake returns an error by default, seed whatever field makes it succeed — the goal is that `haulFuelPerJump` reaches `parseFuelEstimates(client)`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestBuildPriceOf|TestHaulFuelLegCost|TestHaulJumpsBetween|TestHaulFuelPerJump' -v` (timeout 300000)
Expected: FAIL — undefined `buildPriceOf`, `haulFuel`, `haulFuelPerJump`.

- [ ] **Step 3: Create `pkg/worker/haul_fuel.go`**

```go
package worker

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// HaulMaxHaulJumps is the hard backstop on the haul leg (buy->sell) jump count.
// Fuel cost already penalizes long hauls economically; this caps the tail for when
// price/graph data is thin (median ~ 0). The approach leg keeps DefaultHaulMaxJumps.
const HaulMaxHaulJumps = 20

// haulFreeFuelStations are stations where the databot faction refuels for free (its
// ally pump); fuel priced at one of these costs 0. A future refinement can data-drive
// this from a captured ally_fuel signal.
var haulFreeFuelStations = map[string]bool{
	"grand_exchange_station": true,
}

// FuelPriceSource supplies captured station fuel prices for net-of-fuel haul
// economics. Satisfied by *market.Collector. Optional on HaulDeps: a nil source
// makes every fuel cost 0, so ranking/gating fall back to gross-only behavior.
type FuelPriceSource interface {
	GetStationFuelPrice(ctx context.Context, stationID string) (allIn int, capturedAt time.Time, ok bool, err error)
	MedianStationFuelAllIn(ctx context.Context) (median int, ok bool, err error)
}

// haulFuel is the per-pass fuel model: the ship's fuel-per-jump rate, a
// station->price resolver, and the jump graph for leg distances. Built once per
// haul pass. A zero perJump (no probe) makes every cost 0 (gross-only fallback).
type haulFuel struct {
	perJump  int
	priceOf  func(stationID string) float64
	graph    navigation.JumpGraph
	nameToID map[string]string
}

// legCost is the fuel credit cost of traveling `jumps` jumps, refueling at
// pickupStation's price. Zero when the rate or jump count is non-positive.
func (hf haulFuel) legCost(jumps int, pickupStation string) float64 {
	if hf.perJump <= 0 || jumps <= 0 {
		return 0
	}
	return float64(jumps*hf.perJump) * hf.priceOf(pickupStation)
}

// haulJumpsBetween returns the jump count between two system ids. ok is false when
// either id is empty or the target is unreachable.
func (hf haulFuel) haulJumpsBetween(fromSys, toSys string) (int, bool) {
	if fromSys == "" || toSys == "" {
		return 0, false
	}
	d := navigation.BFSJumps(hf.graph, fromSys, []string{toSys})
	j, ok := d[toSys]
	if !ok || j >= navigation.RouteInf {
		return 0, false
	}
	return j, true
}

// buildPriceOf returns a station->creditsPerUnit resolver: 0 for free-pump stations,
// the captured all-in when present, else the galaxy median (probed once here). A nil
// source yields a resolver that always returns 0 (gross-only fallback).
func buildPriceOf(ctx context.Context, src FuelPriceSource) func(string) float64 {
	if src == nil {
		return func(string) float64 { return 0 }
	}
	median, medianOK, _ := src.MedianStationFuelAllIn(ctx)
	return func(stationID string) float64 {
		if haulFreeFuelStations[stationID] {
			return 0
		}
		if allIn, _, ok, err := src.GetStationFuelPrice(ctx, stationID); err == nil && ok {
			return float64(allIn)
		}
		if medianOK {
			return float64(median)
		}
		return 0
	}
}

// haulFuelPerJump probes the ship's fuel-per-jump (ship-constant). It prefers the
// server value from a single find_route to probeTarget (cached under "_last", read by
// parseFuelEstimates); on failure it falls back to the ship-class formula
// ceil(scale^1.5 * base_speed * 10 * 0.10). Returns 0 when neither is available (fuel
// cost then degrades to 0 -> gross-only).
func haulFuelPerJump(ctx context.Context, client game.GameClient, probeTarget string) int {
	if probeTarget != "" {
		if _, err := client.FindRoute(ctx, probeTarget); err == nil {
			if fpj, _, _ := parseFuelEstimates(client); fpj > 0 {
				return fpj
			}
		}
	}
	raw := client.GetRawJSON("ship")
	if len(raw) == 0 {
		return 0
	}
	var shipResp struct {
		Class *struct {
			Scale     int `json:"scale"`
			BaseSpeed int `json:"base_speed"`
		} `json:"class"`
	}
	if err := json.Unmarshal(raw, &shipResp); err != nil || shipResp.Class == nil {
		return 0
	}
	scale, spd := float64(shipResp.Class.Scale), float64(shipResp.Class.BaseSpeed)
	if scale <= 0 || spd <= 0 {
		return 0
	}
	return max(1, int(math.Ceil(math.Pow(scale, 1.5)*spd*10.0*0.10)))
}
```

- [ ] **Step 4: Add the `FuelPrices` field to `HaulDeps`**

In `pkg/worker/haul.go`, in the `HaulDeps` struct, add (near `Treasury`):

```go
	// FuelPrices supplies captured station fuel prices for net-of-fuel ranking and
	// gating. nil disables fuel accounting (ranking/gating fall back to gross-only).
	FuelPrices FuelPriceSource
```

- [ ] **Step 5: Wire the collector in `dispatch.go`**

In `pkg/worker/dispatch.go`, in the `case "haul":` block, add `FuelPrices: d.Market,` to the `HaulDeps{...}` literal (alongside `Treasury: d.treasury,`). `d.Market` is a `*market.Collector`, which satisfies `FuelPriceSource` after Task 1.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestBuildPriceOf|TestHaulFuelLegCost|TestHaulJumpsBetween|TestHaulFuelPerJump' -v` (timeout 300000)
Expected: PASS.

- [ ] **Step 7: Build, full worker test, lint**

Run: `go build ./... && go test ./pkg/worker/ && golangci-lint run ./pkg/worker/...` (timeout 300000)
Expected: build ok; all worker tests pass (nothing else changed behavior yet); no new lint findings.

- [ ] **Step 8: Commit**

```bash
git add pkg/worker/haul_fuel.go pkg/worker/haul.go pkg/worker/dispatch.go pkg/worker/haul_fuel_test.go
git commit -m "feat(worker): fuel-model primitives (rate probe, price resolver, leg cost)"
```

---

### Task 3: Net-of-fuel ranking + haul-jump cap

**Files:**
- Modify: `pkg/worker/haul.go` (`rankedOpp`, `RankHaulOpportunities`, `Haul` ranking wiring)
- Test: `pkg/worker/haul_test.go` (update existing call sites; add net-ranking + cap tests)

**Interfaces:**
- Consumes: `haulFuel`, `buildPriceOf`, `haulFuelPerJump`, `HaulMaxHaulJumps`, `HaulDeps.FuelPrices` (Task 2); `stabilityBoost` (existing).
- Produces: `RankHaulOpportunities(opps, currentSystemID, nameToID, graph, maxJumps, fuelPerJump int, priceOf func(string) float64)` — new trailing `fuelPerJump` and `priceOf` params; ranks by `effectiveNet` and drops candidates whose haul leg exceeds `HaulMaxHaulJumps`.

- [ ] **Step 1: Write the failing tests**

First, the new behavior tests (append to `pkg/worker/haul_test.go`). These assert the ranking flip and the cap. Use the existing `graphFor` and opp-builder helpers; check the builder's signature in the file and set `FromStationID`, `FromSystemName`, `ToSystemName`, `GrossProfit`, `CyclesSeen`, `ID` as needed.

The two opps are deliberately kept in *different* near-tie bands (a wide gross gap) so ordering is driven by gross/net directly, not by the band's proximity/id tiebreak. Near opp: gross 4000, 1 approach + 1 haul jump. Far opp: gross 5200, 1 approach + 3 haul jumps. With `fuelPerJump=10` and price 100: net_near = 4000 − 2·10·100 = 2000; net_far = 5200 − 4·10·100 = 1200 — the flip.

```go
func TestRankNetOfFuelFlipsOrder(t *testing.T) {
	// current=a. near: buy b (a->b=1), sell c (b->c=1), gross 4000.
	//            far:  buy d (a->d=1), sell g (d->e->f->g=3), gross 5200.
	g, n2id := graphFor(
		[]string{"a", "b", "c", "d", "e", "f", "g"},
		[2]string{"a", "b"}, [2]string{"b", "c"},
		[2]string{"a", "d"}, [2]string{"d", "e"}, [2]string{"e", "f"}, [2]string{"f", "g"},
	)
	near := mkRankOpp(1, "b", "c", 4000)
	far := mkRankOpp(2, "d", "g", 5200)
	opps := []market.ArbitrageOpportunity{near, far}

	// Gross-only (fuelPerJump=0): far (5200) leads, near (4000) is in the lower band.
	gross := RankHaulOpportunities(opps, "a", n2id, g, 0, 0, nil)
	if gross[0].ID != 2 {
		t.Fatalf("gross order: want far(2) first, got %d", gross[0].ID)
	}
	// Net-of-fuel (rate 10, price 100): net_near=2000 > net_far=1200 -> near leads.
	net := RankHaulOpportunities(opps, "a", n2id, g, 0, 10, func(string) float64 { return 100 })
	if net[0].ID != 1 {
		t.Fatalf("net order: want near(1) first after fuel, got %d", net[0].ID)
	}
}

func TestRankDropsOverlongHaulLeg(t *testing.T) {
	// Chain buy -> h0 -> h1 -> ... so the haul leg exceeds HaulMaxHaulJumps.
	systems := []string{"a", "buy"}
	pairs := [][2]string{{"a", "buy"}}
	prev := "buy"
	for i := 0; i <= HaulMaxHaulJumps; i++ { // HaulMaxHaulJumps+1 hops buy->sell
		n := fmt.Sprintf("h%d", i)
		systems = append(systems, n)
		pairs = append(pairs, [2]string{prev, n})
		prev = n
	}
	g, n2id := graphFor(systems, pairs...)
	overlong := mkRankOpp(1, "buy", prev, 9999)
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{overlong}, "a", n2id, g, 0, 10, func(string) float64 { return 1 })
	if len(got) != 0 {
		t.Fatalf("want overlong haul dropped, got %d opp(s)", len(got))
	}
}
```

Add this builder (reuse the file's existing opp builder instead if its signature already covers system + station ids). `graphFor` uses each system letter as both id and name, so `FromSystemName` resolves through `nameToID`:

```go
// mkRankOpp builds an opp whose buy/sell SYSTEM ids equal buySys/sellSys and whose
// FromStationID is derived from buySys (priceOf sees a concrete station).
func mkRankOpp(id int, buySys, sellSys string, gross float64) market.ArbitrageOpportunity {
	return market.ArbitrageOpportunity{
		ID: id, GrossProfit: gross, CyclesSeen: 1,
		FromSystemName: buySys, ToSystemName: sellSys,
		FromStationID: buySys + "_st", ToStationID: sellSys + "_st",
	}
}
```

(`TestRankDropsOverlongHaulLeg` uses `fmt.Sprintf`; ensure `haul_test.go` imports `fmt` — it likely already does.)

- [ ] **Step 2: Update existing `RankHaulOpportunities` call sites**

Every existing call in `haul_test.go` (e.g. `TestRankProfitDominant`, `TestRankStabilityBoostPrefersDurable`, `TestRankNearTieProximityTiebreak`, `TestRankChainingTiebreak`, `TestRankSkipsUnresolvedAndUnreachable`, `TestRankDeterministicByID`, `TestRankDistanceCapDropsFarOpps`) must add the two new trailing args `, 0, nil` (fuel rate 0 → gross-only, preserving their existing assertions). Do NOT change their expected results.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestRank' -v` (timeout 300000)
Expected: FAIL — `RankHaulOpportunities` arg count mismatch (compile) until Step 4, then the new tests drive the net/cap logic.

- [ ] **Step 4: Implement the ranking change**

In `pkg/worker/haul.go`:

Add a `haulJumps` field to `rankedOpp`:

```go
type rankedOpp struct {
	opp       market.ArbitrageOpportunity
	buySysID  string
	sellSysID string // "" if unresolved
	jumps     int    // current -> buySys (approach leg)
	haulJumps int    // buySys -> sellSys (-1 = unmeasured/unresolved)
	chain     bool
}
```

Change the signature and body of `RankHaulOpportunities`. Add the two params, compute the haul leg + cap in the reach loop, and replace `effectiveGross` with a local `effNet`:

```go
func RankHaulOpportunities(opps []market.ArbitrageOpportunity, currentSystemID string, nameToID map[string]string, graph navigation.JumpGraph, maxJumps int, fuelPerJump int, priceOf func(stationID string) float64) []market.ArbitrageOpportunity {
	// ... resolved/buyTargets loop unchanged ...
	// ... dist := navigation.BFSJumps(graph, currentSystemID, buyTargets) unchanged ...

	reach := make([]rankedOpp, 0, len(resolved))
	for _, r := range resolved {
		d, ok := dist[r.buySysID]
		if !ok || d >= navigation.RouteInf {
			continue
		}
		if maxJumps > 0 && d > maxJumps {
			continue
		}
		r.jumps = d
		// Haul leg (buy->sell): measure when the sell system resolves and is reachable.
		// Drop candidates whose haul leg exceeds the hard backstop. Leave haulJumps=-1
		// (unmeasured) when the sell system is unknown/unreachable — never dropped, no fuel.
		r.haulJumps = -1
		if r.sellSysID != "" {
			hd := navigation.BFSJumps(graph, r.buySysID, []string{r.sellSysID})
			if hj, hok := hd[r.sellSysID]; hok && hj < navigation.RouteInf {
				if hj > HaulMaxHaulJumps {
					continue
				}
				r.haulJumps = hj
			}
		}
		reach = append(reach, r)
	}
	if len(reach) == 0 {
		return nil
	}

	// effNet is the ranking value: gross minus total (approach+haul) fuel, lifted by
	// the stability streak. fuelPerJump<=0 (no rate) makes fuel 0 -> gross-only.
	effNet := func(r rankedOpp) float64 {
		fuelCost := 0.0
		if fuelPerJump > 0 && priceOf != nil {
			jumps := r.jumps
			if r.haulJumps > 0 {
				jumps += r.haulJumps
			}
			fuelCost = float64(jumps*fuelPerJump) * priceOf(r.opp.FromStationID)
		}
		return (r.opp.GrossProfit - fuelCost) * stabilityBoost(r.opp.CyclesSeen)
	}

	// ... chain computation loop unchanged ...

	// Replace every effectiveGross(r.opp) with effNet(r):
	maxNet := 0.0
	for _, r := range reach {
		if v := effNet(r); v > maxNet {
			maxNet = v
		}
	}
	threshold := maxNet * (1 - haulNearTieFraction)
	// band/rest split uses effNet(r) >= threshold; band and rest sort comparators
	// replace effectiveGross(x.opp) with effNet(x).
	// ... rest of function structure unchanged ...
}
```

Keep the existing `effectiveGross`/`effectiveEffective` helper functions in place if other code references them; if `effectiveGross` becomes unused after this change, delete it (and its now-unused `TestStabilityBoostBoundsAndShape` dependency stays — it tests `stabilityBoost`, which is retained). Run the linter to catch an unused function.

- [ ] **Step 5: Wire the fuel model into `Haul` ranking**

In `pkg/worker/haul.go`, in `Haul()`, immediately after `graph := navigation.JumpGraphFromConnections(conns)` (and the galGraph build), build the per-pass fuel model once, before the held-opportunity early-return so later tasks can reuse it:

```go
	// Per-pass fuel model for net-of-fuel ranking/gating. Probe fuel_per_jump once
	// (ship-constant, tick-free) via any reachable neighbor of current.
	probeTarget := ""
	for _, nb := range graph[current] {
		probeTarget = nb
		break
	}
	fuelPerJump := haulFuelPerJump(ctx, deps.Client, probeTarget)
	priceOf := buildPriceOf(ctx, deps.FuelPrices)
```

Update the ranking call:

```go
	ranked := RankHaulOpportunities(opps, current, nameToID, graph, maxJumps, fuelPerJump, priceOf)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestRank' -v` (timeout 300000)
Expected: PASS — existing rank tests unchanged in behavior (fuel rate 0), new flip + cap tests green.

- [ ] **Step 7: Build, full worker test, lint**

Run: `go build ./... && go test ./pkg/worker/ && golangci-lint run ./pkg/worker/...` (timeout 300000)
Expected: build ok; tests pass; no new lint findings (no unused `effectiveGross`).

- [ ] **Step 8: Commit**

```bash
git add pkg/worker/haul.go pkg/worker/haul_test.go
git commit -m "feat(worker): rank hauls by net-of-fuel profit + haul-leg jump cap"
```

---

### Task 4: Net-of-fuel pre-buy gate (forward haul-leg fuel only)

**Files:**
- Modify: `pkg/worker/haul.go` (`haulGate` signature/body; `runClaimedHaul` signature; the two `runClaimedHaul` call sites; the gate call site)
- Test: `pkg/worker/haul_test.go` (update `TestHaulGate`; add a fuel-rejection + sunk-approach case)

**Interfaces:**
- Consumes: `haulFuel` (Task 2); `fuelPerJump`/`priceOf` built in `Haul` (Task 3); `netProfitFloor`, `sizeBuy` (existing).
- Produces: `haulGate(opp, prices, cargoFree, cargoCap, credits, fuelCost float64)` — new trailing `fuelCost` param subtracted from net; `runClaimedHaul(ctx, deps, out, opp, nameToID, m, fuel haulFuel)` — new trailing `fuel` param.

- [ ] **Step 1: Write the failing tests**

Update `TestHaulGate` call sites to pass a `fuelCost` arg (existing cases pass `0`, preserving their expectations), and add:

```go
func TestHaulGateRejectsOnFuel(t *testing.T) {
	// qty=100, ask=100, bid=110 -> spread net = (110-100)*100 = 1000 (== floor, passes at fuelCost 0).
	opp := market.ArbitrageOpportunity{FromStationID: "buyst", ToStationID: "sellst", ItemID: "x", Quantity: 100}
	prices := []market.ItemStationPrice{
		{StationID: "buyst", HasSell: true, BestAsk: 100},
		{StationID: "sellst", HasBuy: true, BestBid: 110},
	}
	// fuelCost 0 -> passes.
	if _, _, _, ok, _ := haulGate(opp, prices, 100, 100, 1_000_000, 0); !ok {
		t.Fatal("fuelCost=0: expected pass")
	}
	// fuelCost 200 -> net 800 < floor 1000 -> reject.
	if _, _, _, ok, reason := haulGate(opp, prices, 100, 100, 1_000_000, 200); ok {
		t.Fatalf("fuelCost=200: expected reject, got pass")
	} else if reason == "" {
		t.Fatal("expected a rejection reason")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/worker/ -run TestHaulGate -v` (timeout 300000)
Expected: FAIL — `haulGate` arg count mismatch (compile).

- [ ] **Step 3: Add the fuel term to `haulGate`**

In `pkg/worker/haul.go`, change `haulGate` to take `fuelCost float64` and subtract it from the net:

```go
func haulGate(opp market.ArbitrageOpportunity, prices []market.ItemStationPrice, cargoFree, cargoCap, credits, fuelCost float64) (qty, liveAsk, sellBid float64, ok bool, reason string) {
	// ... ask/bid resolution + affordability unchanged ...
	margin := (sellBid - liveAsk) / liveAsk
	net := (sellBid-liveAsk)*qty - fuelCost
	floor := netProfitFloor(cargoCap)
	if margin < haulMinMargin || net < floor {
		return qty, liveAsk, sellBid, false, fmt.Sprintf("spread too thin (margin=%.1f%%, net=%.0f after fuel=%.0f, floor=%.0f)", margin*100, net, fuelCost, floor)
	}
	return qty, liveAsk, sellBid, true, ""
}
```

- [ ] **Step 4: Thread the fuel model into `runClaimedHaul` and compute the haul-leg cost at the gate**

Change `runClaimedHaul`'s signature to accept the fuel model:

```go
func runClaimedHaul(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, nameToID map[string]string, m *haulMetrics, fuel haulFuel) error {
```

At the gate call site (currently haul.go:758), compute the **haul-leg-only** fuel cost (approach is sunk — the hauler is docked at the buy station) and pass it:

```go
	haulLegCost := 0.0
	if buySys := nameToID[opp.FromSystemName]; buySys != "" {
		if sellSys := nameToID[opp.ToSystemName]; sellSys != "" {
			if hj, ok := fuel.haulJumpsBetween(buySys, sellSys); ok {
				haulLegCost = fuel.legCost(hj, opp.FromStationID)
			}
		}
	}
	qty, liveAsk, sellBid, pass, reason := haulGate(opp, prices, cargoFree, state.Ship.CargoCapacity, state.GetCredits(), haulLegCost)
```

- [ ] **Step 5: Build the carrier in `Haul` and pass it to both `runClaimedHaul` call sites**

In `Haul()`, after building `fuelPerJump`/`priceOf` (Task 3, Step 5), construct the carrier:

```go
	fuel := haulFuel{perJump: fuelPerJump, priceOf: priceOf, graph: graph, nameToID: nameToID}
```

Update both `runClaimedHaul(...)` calls — the held-opportunity path (currently haul.go:523) and the main path (currently haul.go:614) — to pass `fuel` as the trailing arg:

```go
	return runClaimedHaul(ctx, deps, out, held[0], nameToID, nil, fuel)
	// ...
	return runClaimedHaul(ctx, deps, out, opp, nameToID, m, fuel)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestHaulGate -v` (timeout 300000)
Expected: PASS — existing gate cases unchanged (fuelCost 0), new fuel-rejection case green.

- [ ] **Step 7: Build, full suite, lint**

Run: `go build ./... && go test ./... && golangci-lint run ./...` (timeout 300000)
Expected: build ok; all tests pass; no new lint findings.

- [ ] **Step 8: Commit**

```bash
git add pkg/worker/haul.go pkg/worker/haul_test.go
git commit -m "feat(worker): subtract forward haul-leg fuel in the pre-buy gate"
```

---

## Deploy (operator-gated, not a task)

Activation is a live redeploy: rebuild `bin/worker` and restart the haul fleet (staggered), same procedure as Sub-project A's mb redeploy (graceful drain → TERM → relaunch). Until then the change is inert for the running fleet. The feature is also self-degrading: with no captured prices or no fuel-rate probe it behaves exactly like today's gross-only hauler, so activation carries no behavioral cliff.

## Notes

- Design spec: `docs/superpowers/specs/2026-07-15-net-of-fuel-hauler-economics-design.md`.
- The sunk-cost split is intentional: **ranking** counts approach+haul fuel (deciding whether to reposition); the **gate** counts haul-only fuel (approach already spent). Confirmed at spec review.
- Future work (not this plan): Sub-project C (fuel-arbitrage chained routing), exact-at-buy `find_route(buy→sell)` re-check, data-driven free-pump set via captured `ally_fuel`.
