# Claim Watchdog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** While a hauler transits to sell a claimed arbitrage haul, detect when the destination's demand has degraded below break-even for the cargo on board and react (re-route → continue & sell → cost-price sell order), so haulers stop delivering into markets that evaporated mid-journey.

**Architecture:** A break-even evaluator over the destination's live buy book (from marketbot-fresh `market.db`), invoked from the haul sell leg. Step 1 evaluates **on arrival** inside `haulSellLeg` (full context in scope, no autopilot change). Step 2 adds **per-jump** monitoring + mid-route re-route via an autopilot early-stop. See `docs/superpowers/specs/2026-06-27-claim-watchdog-design.md`.

**Tech Stack:** Go 1.24, `pkg/worker` (haul loop), `pkg/market` (Collector queries), `pkg/navigation` (BFS jumps).

## Global Constraints

- Go 1.24+; use modern features (range-over-int, `b.Loop()` in benchmarks).
- All new code passes `golangci-lint` with no new findings.
- TDD: write the failing test, run it red, implement minimally, run it green.
- `go build ./...` and `go test ./...` must pass before each commit.
- Any compiled binary goes in `bin/`, never the repo root.
- Sleeps/pauses use the constants in `pkg/game/constants.go`.
- Scoped `git add` of only the task's files (never `git add -A`); commit messages end with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Do NOT change the `game.GameClient` interface (breaks mocks across pkg/agent, pkg/skills). Adding to the `OpportunityStore` interface (pkg/worker) IS allowed but the `fakeStore` test double must be updated in the same task.
- The watchdog is best-effort: a market-read or reaction failure logs and falls through to the normal sell — it must never strand cargo unclaimed without a sell plan.

## Reference (verified signatures)

- `market.Order` (types.go:24): `StationID, ItemID, Side string` (`"buy"`/`"sell"`), `PriceEach, Quantity float64`, …
- `market.ArbitrageOpportunity` (types.go:54): `ID int`, `FromStationID, ToStationID, ItemID string`, `BuyPrice, SellPrice, Quantity, GrossProfit, FuelCost float64`, `ToSystemName string`, `Status, ClaimedBy string`, …
- `market.ItemStationPrice` (types.go:113): `StationID, SystemID string`, `BestAsk, AskQty, BestBid, BidQty float64`, `HasBuy, HasSell bool`.
- `(*market.Collector).GetStationOrders(ctx, stationID, itemID string) ([]Order, error)` (query.go:422) — station's latest-capture orders, both sides, ordered by side then price.
- `(*market.Collector).FindBestPrices(ctx, itemID, side string, limit int) ([]BestPrice, error)` (query.go:165) — `side` is `"buy"` or `"sell"`.
- `navigation.BFSJumps(graph JumpGraph, src string, targets []string) map[string]int` (route.go:11) — `RouteInf` (1<<30) when unreachable.
- `pkg/worker/haul.go`: `OpportunityStore` interface (line 293), `HaulDeps` (line 347), `haulMetrics` (line 366), `runClaimedHaul` (line 513), `haulSellLeg` (line 586; transit at 587, cargo at 594 `held := cargoQty(deps.Client.GetState(), opp.ItemID)`, sell at 599, complete at 615), `cargoQty` (line 794), existing cost-order liquidation at line 779.
- `(game.GameClient).CreateSellOrder(ctx, payload map[string]any)` — payload keys `item_id`, `price_each`, `quantity`.
- Test doubles: `fakeStore` (haul_test.go:270, implements `OpportunityStore`), `fakeClient` (dispatch_test.go:18, embeds `game.GameClient`; has `Sell`, `CreateSellOrder`, `GetState`, `GetRawJSON`).

---

# STEP 1 — On-arrival safety net (executable)

Stops a hauler dumping cargo into a dead/thin market on arrival. No autopilot change.

### Task 1: Break-even evaluator (pure)

**Files:**
- Create: `pkg/worker/watchdog.go`
- Test: `pkg/worker/watchdog_test.go`

**Interfaces:**
- Produces: `absorbableProceeds([]market.Order, heldQty float64) float64`; `arrivalDecision(orders []market.Order, heldQty, buyCostPaid float64, cfg watchdogConfig) watchdogAction`; `watchdogAction` enum (`sellAtMarket`, `postCostOrder`); `watchdogConfig{MaxSellLossFrac float64}` with `.defaulted()`.

- [ ] **Step 1: Write the failing test** — `pkg/worker/watchdog_test.go`

```go
package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/market"
)

func buyOrder(price, qty float64) market.Order {
	return market.Order{Side: "buy", PriceEach: price, Quantity: qty}
}

func TestAbsorbableProceedsWalksBidsHighFirst(t *testing.T) {
	orders := []market.Order{buyOrder(100, 5), buyOrder(120, 3), {Side: "sell", PriceEach: 999, Quantity: 10}}
	// hold 6: take 3@120 then 3@100 = 360 + 300 = 660; sell orders ignored.
	if got := absorbableProceeds(orders, 6); got != 660 {
		t.Errorf("got %v, want 660", got)
	}
	// hold 10 but only 8 demand: 3@120 + 5@100 = 860 (capped by book).
	if got := absorbableProceeds(orders, 10); got != 860 {
		t.Errorf("partial sellout: got %v, want 860", got)
	}
}

func TestArrivalDecision(t *testing.T) {
	cfg := watchdogConfig{} // defaults: 10% loss tolerance
	healthy := []market.Order{buyOrder(130, 50)}
	if got := arrivalDecision(healthy, 10, 1000, cfg); got != sellAtMarket {
		t.Errorf("healthy demand: got %v, want sellAtMarket", got)
	}
	// no buyers -> post cost order
	if got := arrivalDecision(nil, 10, 1000, cfg); got != postCostOrder {
		t.Errorf("no buyers: got %v, want postCostOrder", got)
	}
	// proceeds 800 vs buyCost 1000, floor 900 -> loss too deep -> cost order
	deep := []market.Order{buyOrder(80, 10)}
	if got := arrivalDecision(deep, 10, 1000, cfg); got != postCostOrder {
		t.Errorf("deep loss: got %v, want postCostOrder", got)
	}
	// proceeds 950 vs buyCost 1000, floor 900 -> small loss -> sell
	small := []market.Order{buyOrder(95, 10)}
	if got := arrivalDecision(small, 10, 1000, cfg); got != sellAtMarket {
		t.Errorf("small loss: got %v, want sellAtMarket", got)
	}
}
```

- [ ] **Step 2: Run red** — `go test ./pkg/worker/ -run 'TestAbsorbableProceeds|TestArrivalDecision'` → FAIL (undefined).

- [ ] **Step 3: Implement** — `pkg/worker/watchdog.go`

```go
package worker

import (
	"sort"

	"github.com/rsned/spacemolt/pkg/market"
)

// watchdogAction is the chosen reaction when a haul reaches its sell stop.
type watchdogAction int

const (
	sellAtMarket  watchdogAction = iota // proceed with the normal market sell
	postCostOrder                       // demand too thin: list at cost instead of dumping
)

// watchdogConfig holds the tunable thresholds; the zero value uses defaults.
type watchdogConfig struct {
	// MaxSellLossFrac tolerates a market sell losing up to this fraction of the
	// buy cost; beyond it, list the cargo at cost instead of realizing the loss.
	MaxSellLossFrac float64
}

func (c watchdogConfig) defaulted() watchdogConfig {
	if c.MaxSellLossFrac <= 0 {
		c.MaxSellLossFrac = 0.10
	}
	return c
}

// absorbableProceeds sums what a sale of up to heldQty units would earn against
// the destination's live BUY book: highest bid first, taking min(remaining, qty)
// at each price. Only side=="buy" orders (demand) count.
func absorbableProceeds(orders []market.Order, heldQty float64) float64 {
	bids := make([]market.Order, 0, len(orders))
	for _, o := range orders {
		if o.Side == "buy" && o.Quantity > 0 && o.PriceEach > 0 {
			bids = append(bids, o)
		}
	}
	sort.Slice(bids, func(i, j int) bool { return bids[i].PriceEach > bids[j].PriceEach })
	remaining, proceeds := heldQty, 0.0
	for _, b := range bids {
		if remaining <= 0 {
			break
		}
		take := b.Quantity
		if take > remaining {
			take = remaining
		}
		proceeds += take * b.PriceEach
		remaining -= take
	}
	return proceeds
}

// arrivalDecision picks the reaction when a hauler has arrived at its claimed sell
// station holding heldQty units bought for buyCostPaid total. If the live demand
// can't absorb the cargo without a loss beyond tolerance (or has no buyers), it
// returns postCostOrder; otherwise sellAtMarket.
func arrivalDecision(orders []market.Order, heldQty, buyCostPaid float64, cfg watchdogConfig) watchdogAction {
	cfg = cfg.defaulted()
	proceeds := absorbableProceeds(orders, heldQty)
	if proceeds <= 0 {
		return postCostOrder
	}
	if proceeds < buyCostPaid*(1-cfg.MaxSellLossFrac) {
		return postCostOrder
	}
	return sellAtMarket
}
```

- [ ] **Step 4: Run green** — same command → PASS.
- [ ] **Step 5: Commit** — `git add pkg/worker/watchdog.go pkg/worker/watchdog_test.go && git commit` (feat(worker): break-even evaluator for the claim watchdog).

### Task 2: Add `GetStationOrders` to `OpportunityStore` + fake

**Files:**
- Modify: `pkg/worker/haul.go` (interface at line 293)
- Modify: `pkg/worker/haul_test.go` (`fakeStore` at line 270)

**Interfaces:**
- Produces: `OpportunityStore.GetStationOrders(ctx context.Context, stationID, itemID string) ([]market.Order, error)`; `fakeStore.orders []market.Order` returned by its `GetStationOrders`.
- Consumes: the real `(*market.Collector).GetStationOrders` already satisfies it (compile-checked).

- [ ] **Step 1: Write the failing test** — append to `pkg/worker/haul_test.go`:

```go
func TestFakeStoreServesStationOrders(t *testing.T) {
	f := &fakeStore{orders: []market.Order{{Side: "buy", PriceEach: 50, Quantity: 4}}}
	got, err := f.GetStationOrders(context.Background(), "stn", "iron_ore")
	if err != nil || len(got) != 1 || got[0].PriceEach != 50 {
		t.Fatalf("want 1 order @50, got %v err=%v", got, err)
	}
}
```

- [ ] **Step 2: Run red** — `go test ./pkg/worker/ -run TestFakeStoreServesStationOrders` → FAIL (no field `orders` / no method).
- [ ] **Step 3: Implement** — add to the `OpportunityStore` interface in `haul.go`:

```go
	GetStationOrders(ctx context.Context, stationID, itemID string) ([]market.Order, error)
```

  and to `fakeStore` in `haul_test.go`:

```go
	// add field to the struct:
	orders []market.Order
	// add method:
func (f *fakeStore) GetStationOrders(ctx context.Context, stationID, itemID string) ([]market.Order, error) {
	return f.orders, nil
}
```

- [ ] **Step 4: Run green** — `go build ./... && go test ./pkg/worker/ -run TestFakeStoreServesStationOrders` → PASS (Collector satisfies the interface; build proves it).
- [ ] **Step 5: Commit** — `git add pkg/worker/haul.go pkg/worker/haul_test.go && git commit` (feat(worker): OpportunityStore.GetStationOrders).

### Task 3: Wire the on-arrival watchdog into `haulSellLeg`

**Files:**
- Modify: `pkg/worker/haul.go` (`haulSellLeg`, after cargo check ~594, before sell ~599; add a `haulPostCostOrder` helper)
- Test: `pkg/worker/haul_test.go`

**Interfaces:**
- Consumes: `arrivalDecision` (Task 1), `deps.Market.GetStationOrders` (Task 2), existing `cargoQty`, `deps.Client.Sell`, `deps.Client.CreateSellOrder`, `deps.Market.CompleteOpportunity`.
- Produces: `haulPostCostOrder(ctx, deps, out, opp, held, unitBuy float64, m *haulMetrics) error`.

- [ ] **Step 1: Write the failing tests** — append to `pkg/worker/haul_test.go` (model on `TestHaulSellLegRecordsResult`): two cases sharing the fixture but differing in `fakeStore.orders`.
  - *Thin demand* (`orders` = a single `buy @ well below cost`, or empty): assert `slices.Contains(fc.calls, "sell_order:...")` (CreateSellOrder fired) and **no** `sell:<item>` call, and the opp is completed.
  - *Healthy demand* (`orders` = `buy` covering cost): assert `sell:<item>` fired and **no** `sell_order:` call (existing Tier-2 path).

```go
func TestHaulSellLegPostsCostOrderOnThinDemand(t *testing.T) {
	o := opp(7, "b", "a", 100) // iron_ore, sell station a-stn in current system "a"
	fc := &fakeClient{state: &game.State{
		System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
	}, route: []game.RouteStep{{SystemID: "a", Name: "A"}}}
	// bought 10 @100 = 1000; dest demand only 2 units @50 -> proceeds 100 << floor 900.
	f := &fakeStore{orders: []market.Order{{Side: "buy", PriceEach: 50, Quantity: 2}}}
	m := &haulMetrics{buyPrice: 100, qty: 10}
	deps := HaulDeps{Client: fc, Market: f, AgentID: "t"}
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(fc.calls, func(c string) bool { return strings.HasPrefix(c, "sell:iron_ore") }) {
		t.Fatalf("thin demand must NOT market-sell, got %v", fc.calls)
	}
	if !slices.ContainsFunc(fc.calls, func(c string) bool { return strings.HasPrefix(c, "sell_order:iron_ore") }) {
		t.Fatalf("want a cost-price sell_order, got %v", fc.calls)
	}
}

func TestHaulSellLegSellsOnHealthyDemand(t *testing.T) {
	o := opp(7, "b", "a", 100)
	fc := &fakeClient{state: &game.State{
		System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
	}, route: []game.RouteStep{{SystemID: "a", Name: "A"}}}
	f := &fakeStore{orders: []market.Order{{Side: "buy", PriceEach: 130, Quantity: 50}}}
	m := &haulMetrics{buyPrice: 100, qty: 10}
	deps := HaulDeps{Client: fc, Market: f, AgentID: "t"}
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(fc.calls, "sell:iron_ore") {
		t.Fatalf("healthy demand must market-sell, got %v", fc.calls)
	}
}
```

  (Confirm the `fakeClient.CreateSellOrder` call records a `"sell_order:<item>@<price>"` string — it does, dispatch_test.go:43. If its format differs, match the prefix accordingly.)

- [ ] **Step 2: Run red** — `go test ./pkg/worker/ -run 'TestHaulSellLegPostsCostOrder|TestHaulSellLegSellsOnHealthyDemand'` → the thin-demand test FAILS (currently always market-sells).
- [ ] **Step 3: Implement** — in `haulSellLeg`, after `held := cargoQty(...)` and the `held <= 0` guard, before the `Sell` call:

```go
	unitBuy := opp.BuyPrice
	if m != nil && m.buyPrice > 0 {
		unitBuy = m.buyPrice
	}
	if orders, oerr := deps.Market.GetStationOrders(ctx, opp.ToStationID, opp.ItemID); oerr != nil {
		fmt.Fprintf(out, "haul: opp %d demand check failed (%v); selling normally\n", opp.ID, oerr) //nolint:errcheck
	} else if arrivalDecision(orders, held, unitBuy*held, watchdogConfig{}) == postCostOrder {
		fmt.Fprintf(out, "haul: opp %d demand too thin at %s; listing %0.f %s @cost %.0f\n", opp.ID, opp.ToStationID, held, opp.ItemID, unitBuy) //nolint:errcheck
		return haulPostCostOrder(ctx, deps, out, opp, held, unitBuy, m)
	}
	// ...existing Sell path follows unchanged...
```

  Add the helper (model the CreateSellOrder payload on the existing liquidation at haul.go:779, and complete the claim so it isn't re-hauled):

```go
// haulPostCostOrder lists held cargo at the buy price instead of dumping it into
// thin demand (watchdog Tier 3), then completes the claim. The eventual fill is
// captured by the server action log; no haul_result is recorded here (no sale yet).
func haulPostCostOrder(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, held, unitBuy float64, m *haulMetrics) error {
	payload := map[string]any{"item_id": opp.ItemID, "price_each": unitBuy, "quantity": held}
	if err := deps.Client.CreateSellOrder(ctx, payload); err != nil {
		fmt.Fprintf(out, "haul: opp %d cost-order failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}
	if _, err := deps.Market.CompleteOpportunity(ctx, opp.ID, deps.AgentID); err != nil {
		fmt.Fprintf(out, "haul: opp %d complete-after-cost-order failed: %v\n", opp.ID, err) //nolint:errcheck
	}
	return nil
}
```

- [ ] **Step 4: Run green** — `go test ./pkg/worker/...` → PASS (both new tests; existing sell-leg tests still green — they use `fakeStore{}` with no orders, so `GetStationOrders` returns nil → `arrivalDecision(nil,...)` returns `postCostOrder`… **NOTE:** this would change existing tests. Resolve in Step 5 below).
- [ ] **Step 5: Reconcile existing tests** — existing sell-leg tests (`TestHaulSellLegRecordsResult`, the actual-fill test) construct `fakeStore{}` with no orders, which the new logic treats as "no demand → cost order," changing their behavior. Two correct options; pick per reviewer:
  - (a) Give those fixtures a healthy `orders` entry so they exercise the intended market-sell path (preferred — keeps them asserting the sell + record).
  - (b) Treat **nil** orders (read returned nothing) distinctly from an explicit empty book: only divert to cost-order when `orders != nil`. This makes "no market data" fall through to a normal sell (matches the spec's "no/stale data → don't act"). Implement by guarding the divert on `orders != nil`.
  Recommended: **(b)** — it matches the spec's freshness rule (absence of data is not evidence of dead demand) AND leaves existing tests valid. Update Task 3 Step 3 to `} else if orders != nil && arrivalDecision(...) == postCostOrder {`. Re-run `go test ./pkg/worker/...` → PASS.
- [ ] **Step 6: Lint + commit** — `golangci-lint run ./pkg/worker/...`; `git add pkg/worker/haul.go pkg/worker/haul_test.go && git commit` (feat(worker): on-arrival demand watchdog — cost-order thin markets instead of dumping).

---

# STEP 2 — Per-jump monitoring + re-route (follow-on; detail after Step 1 lands)

Adds mid-journey re-routing so a hauler diverts *before* arriving, saving the remaining jumps. Requires an autopilot early-stop. Task-level scope (each becomes a TDD task once Step 1 behavior is observed live):

### Task 4: Autopilot early-stop from a waypoint check
- Extend `AutopilotDeps` with an optional `WaypointCheck func(ctx) (stop bool, err error)` (distinct from the existing capture-only `OnWaypoint`). In the jump loop, after each arrival, call it; if `stop`, return a sentinel (`errAutopilotStopped` or a `Stopped bool` on the result) so the caller can re-plan. Keep `OnWaypoint` semantics unchanged. Tests via `fakeClient` route + a check that stops on hop 2.

### Task 5: Thread the watchdog into the sell-leg autopilot
- In `haulSellLeg`, build a `WaypointCheck` closure capturing `opp`, `sellSys`, `deps`, `m` that runs the break-even evaluator against `deps.Market.GetStationOrders(opp.ToStationID, opp.ItemID)` at each jump and requests stop when below break-even. Only the **sell leg** gets it (buy-leg transit unchanged). On stop, fall into the re-route reaction (Task 7).

### Task 6: Re-route finder (find-a-market) — pure
- `bestReroute(itemID string, held, unitBuy float64, fromSystem string, prices []market.BestPrice, jumps map[string]int, cfg) (target market.BestPrice, ok bool)`: from `FindBestPrices(itemID,"buy")` candidates, filter by reachability (`navigation.BFSJumps` ≤ budget) and projected net (after extra fuel), return the best beating "continue" by the margin (default ≥ `max(15% × buyCost, floor)`). Pure + table tests. Reusable standalone for stale-cargo liquidation.

### Task 7: Tier-1 re-route reaction
- On a watchdog stop with a viable `bestReroute`, repoint the haul: set the new sell system + `ToStationID`, log, and continue the sell leg to the new destination (re-enter `haulAutopilot`/`haulSellLeg` with the new target). Keep the claim coherent (the claim row tracks the opp; the physical cargo is what we're rehoming — do not release-then-lose). Record the final outcome against the actual destination. Tests assert: stop fires → finder consulted → autopilot retargets to the new station.

---

# STEP 3 — Marketbot targeted scan-boost (future, optional)
On claim of a far haul, register `(ToStationID, ItemID)` so the destination marketbot scans it on a tighter cadence (fresher Phase-1/2 reads). Only build if per-jump freshness proves too coarse in practice. Out of scope until Steps 1–2 are live and measured.

## Self-Review

- **Spec coverage:** break-even trigger (Task 1), tiered reaction — continue-sell/cost-order (Task 3, Step 1) + re-route (Tasks 6–7, Step 2), phased mechanism (Step 1 on-arrival / Step 2 per-jump). ✓
- **Freshness rule:** Task 3 Step 5(b) makes absent market data fall through to a normal sell, matching the spec. ✓
- **Type consistency:** `watchdogAction`, `watchdogConfig`, `absorbableProceeds`, `arrivalDecision`, `GetStationOrders` names used identically across tasks. ✓
- **No interface break:** only `OpportunityStore` (worker-local) gains a method, updated in the same task with its fake; `game.GameClient` untouched. ✓
