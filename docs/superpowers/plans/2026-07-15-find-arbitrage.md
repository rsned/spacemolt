# play_as find_arbitrage / claim_arbitrage / release_arbitrage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three `play_as` REPL commands that let a human operator see arbitrage opportunities that are a minimal detour from their travel route, and claim/release them so the hauler fleet skips claimed ones.

**Architecture:** A pure `rankDetourArbitrage` function computes each opportunity's detour over the direct current→destination route, filters by a jump budget, and ranks by marginal net-of-fuel profit. Thin command handlers do the impure work (resolve systems, load the scanner table, probe fuel, build the fuel-price resolver, render) and wrap the store's claim/release. No server or schema change; reads the existing scanner table.

**Tech Stack:** Go 1.24; `pkg/market` (arbitrage store + fuel prices), `pkg/navigation` (BFS jump graph), `pkg/knowledge` (systems/connections), existing `cmd/tools/play_as` helpers.

## Global Constraints

- Go 1.24; all new code passes `golangci-lint` with no new findings; run `go build ./... && go test ./...` before committing.
- Reuse existing `play_as` helpers verbatim — do not reimplement: `currentJumpFuel`, `resolveSystemToken`, `displayName`, `plural`, `partitionFlags`, `globalKB`, `globalMarketCollector`, `globalAgentID`, `outputFormat`/`formatStyled`.
- `navigation.JumpGraph` is `map[string][]string`; `navigation.BFSJumps(graph, src, targets []string) map[string]int`; `navigation.RouteInf = 1<<30`.
- `market.ArbitrageOpportunity` has NO system-id fields — only `FromSystemName`/`ToSystemName` (system names, joined on read), `FromStationID`/`ToStationID`, `ItemID`, `GrossProfit float64`, `Quantity float64`, `ID int`, `Status string`, plus `FromStationName`/`ToStationName`/`ItemName`.
- Store sigs: `GetOpportunities(ctx, status string, limit int) ([]market.ArbitrageOpportunity, error)`; `ClaimOpportunity(ctx, id int, agentID string) (bool, error)`; `ReleaseOpportunity(ctx, id int, agentID string) (bool, error)`; `GetStationFuelPrice(ctx, stationID) (allIn int, capturedAt time.Time, ok bool, err error)`; `MedianStationFuelAllIn(ctx) (median int, ok bool, err error)`.
- Marginal-fuel rule: fuel term uses the **detour** jumps (not the full route). `fuelPerJump<=0` or a nil `priceOf` ⇒ fuel term 0 ⇒ `net == gross` (graceful degradation, same as Sub-project B).
- Spec: `docs/superpowers/specs/2026-07-15-find-arbitrage-design.md`.

---

## File Structure

- `cmd/tools/play_as/arbitrage_cmd.go` (NEW) — `arbRow`, the pure `rankDetourArbitrage` + `arbJumps` helper (Task 1); the free-pump set, `arbFuelPriceSource` + `buildArbPriceOf`, the three handlers + rendering (Task 2).
- `cmd/tools/play_as/arbitrage_cmd_test.go` (NEW) — unit tests for the pure ranker (Task 1).
- `cmd/tools/play_as/main.go` (MODIFY) — REPL dispatch cases + help text (Task 2).

---

## Task 1: Pure detour ranker + fuel-price resolver

**Files:**
- Create: `cmd/tools/play_as/arbitrage_cmd.go`
- Test: `cmd/tools/play_as/arbitrage_cmd_test.go`

**Interfaces:**
- Consumes: `market.ArbitrageOpportunity`, `navigation.JumpGraph`/`BFSJumps`/`RouteInf`.
- Produces (used by Task 2):
  - `type arbRow struct { Opp market.ArbitrageOpportunity; Detour int; Net float64 }`
  - `func rankDetourArbitrage(opps []market.ArbitrageOpportunity, curSys, destSys string, graph navigation.JumpGraph, nameToID map[string]string, budget int, fuelPerJump int, priceOf func(stationID string) float64, limit int) (rows []arbRow, skipped int)`
  - `func arbJumps(graph navigation.JumpGraph, from, to string) int`

**IMPORTANT — everything Task 1 adds must be referenced within Task 1** (the ranker and `arbJumps` are exercised by the tests; the repo's pre-commit hook hard-fails on any `golangci-lint` finding, including unused symbols). The fuel-price resolver (`buildArbPriceOf`, `arbFuelPriceSource`, `arbFreeFuelStations`) is NOT in this task — it needs the client/collector and is only used by Task 2's handler, so it is added there. Do not add it here or the commit's pre-commit hook will reject it as unused.

- [ ] **Step 1: Write the failing test**

Create `cmd/tools/play_as/arbitrage_cmd_test.go`:

```go
package main

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// graph: cur - s1 - s2 - dest ; s2 - s9 - s10 (off-path spur) ; iso isolated.
// baseline cur->dest = 3.
func testArbGraph() navigation.JumpGraph {
	return navigation.JumpGraph{
		"cur":  {"s1"},
		"s1":   {"cur", "s2"},
		"s2":   {"s1", "dest", "s9"},
		"dest": {"s2"},
		"s9":   {"s2", "s10"},
		"s10":  {"s9"},
		"iso":  {},
	}
}

func testArbNameToID() map[string]string {
	return map[string]string{
		"cur system": "cur", "sys1": "s1", "sys2": "s2", "dest system": "dest",
		"sys9": "s9", "sys10": "s10", "isolated": "iso",
	}
}

func opp(id int, from, to string, gross float64) market.ArbitrageOpportunity {
	return market.ArbitrageOpportunity{
		ID: id, FromSystemName: from, ToSystemName: to, FromStationID: "buyst",
		GrossProfit: gross, ItemID: "x", Quantity: 10,
	}
}

func TestRankDetourFiltersByBudget(t *testing.T) {
	g := testArbGraph()
	n2i := testArbNameToID()
	opps := []market.ArbitrageOpportunity{
		opp(1, "sys1", "sys2", 1000), // legs 1+1+1=3, detour 0
		opp(2, "sys1", "sys9", 1500), // legs 1+2+2=5, detour 2
		opp(3, "sys9", "sys10", 900), // legs 3+1+3=7, detour 4 -> dropped at budget 3
	}
	rows, skipped := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 0, nil, 0)
	if skipped != 0 {
		t.Fatalf("skipped=%d, want 0", skipped)
	}
	if len(rows) != 2 {
		t.Fatalf("kept %d rows, want 2 (opp3 exceeds budget)", len(rows))
	}
	for _, r := range rows {
		if r.Opp.ID == 3 {
			t.Fatal("opp3 (detour 4) should be dropped at budget 3")
		}
	}
	// Detours: opp1=0, opp2=2.
	got := map[int]int{}
	for _, r := range rows {
		got[r.Opp.ID] = r.Detour
	}
	if got[1] != 0 || got[2] != 2 {
		t.Fatalf("detours = %v, want opp1=0 opp2=2", got)
	}
}

func TestRankDetourNetOfFuelSortAndDegradation(t *testing.T) {
	g := testArbGraph()
	n2i := testArbNameToID()
	opps := []market.ArbitrageOpportunity{
		opp(1, "sys1", "sys2", 1000), // detour 0 -> net 1000
		opp(2, "sys1", "sys9", 1500), // detour 2 -> fuel 2*2*100=400 -> net 1100
	}
	price := func(string) float64 { return 100 }
	rows, _ := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 2, price, 0)
	if len(rows) != 2 {
		t.Fatalf("kept %d, want 2", len(rows))
	}
	if rows[0].Opp.ID != 2 || rows[1].Opp.ID != 1 {
		t.Fatalf("order = [%d,%d], want [2,1] (net 1100 > 1000)", rows[0].Opp.ID, rows[1].Opp.ID)
	}
	if rows[0].Net != 1100 || rows[1].Net != 1000 {
		t.Fatalf("nets = [%.0f,%.0f], want [1100,1000]", rows[0].Net, rows[1].Net)
	}
	// Degradation: fuelPerJump=0 -> net == gross -> opp2 (1500) leads by gross.
	rows0, _ := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 0, price, 0)
	if rows0[0].Opp.ID != 2 || rows0[0].Net != 1500 {
		t.Fatalf("degraded: leader id=%d net=%.0f, want id=2 net=1500", rows0[0].Opp.ID, rows0[0].Net)
	}
}

func TestRankDetourSkipsUnresolvedAndUnreachable(t *testing.T) {
	g := testArbGraph()
	n2i := testArbNameToID()
	opps := []market.ArbitrageOpportunity{
		opp(1, "sys1", "sys2", 1000),      // ok
		opp(2, "sys1", "nowhere", 5000),   // unresolved sell name -> skipped
		opp(3, "sys1", "isolated", 5000),  // unreachable leg (iso) -> skipped
	}
	rows, skipped := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 0, nil, 0)
	if len(rows) != 1 || rows[0].Opp.ID != 1 {
		t.Fatalf("kept %d rows, want only opp1", len(rows))
	}
	if skipped != 2 {
		t.Fatalf("skipped=%d, want 2", skipped)
	}
}

func TestRankDetourLimit(t *testing.T) {
	g := testArbGraph()
	n2i := testArbNameToID()
	opps := []market.ArbitrageOpportunity{
		opp(1, "sys1", "sys2", 1000),
		opp(2, "sys1", "sys9", 1500),
	}
	rows, _ := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 0, nil, 1)
	if len(rows) != 1 || rows[0].Opp.ID != 2 {
		t.Fatalf("limit 1 returned %d rows (leader id %d), want 1 (id 2)", len(rows), func() int { if len(rows) > 0 { return rows[0].Opp.ID }; return -1 }())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestRankDetour -v` (timeout 300000)
Expected: FAIL — `undefined: rankDetourArbitrage` (and `arbRow`).

- [ ] **Step 3: Write the implementation**

Create `cmd/tools/play_as/arbitrage_cmd.go`:

```go
package main

import (
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// arbRow is one ranked on-the-way arbitrage opportunity.
type arbRow struct {
	Opp    market.ArbitrageOpportunity
	Detour int     // extra jumps the side-trip adds over the direct cur->dest route (>=0)
	Net    float64 // GrossProfit minus the marginal detour fuel cost
}

// rankDetourArbitrage keeps opportunities whose detour over the direct
// cur->dest route is <= budget, orders them by marginal net-of-fuel profit
// descending, and returns the first `limit` (limit<=0 = all).
//
//	detour = (cur->buy) + (buy->sell) + (sell->dest) - (cur->dest), clamped at 0
//	net    = GrossProfit - detour*fuelPerJump*priceOf(buy station)
//
// fuelPerJump<=0 or a nil priceOf disables the fuel term (net == gross), the
// graceful-degradation path. nameToID maps a lowercased system NAME to its
// canonical id. Opportunities whose buy/sell system name does not resolve, or
// whose legs are unreachable, are skipped and counted. If dest is unreachable
// from cur, no rows are returned (the caller reports that separately).
func rankDetourArbitrage(
	opps []market.ArbitrageOpportunity,
	curSys, destSys string,
	graph navigation.JumpGraph,
	nameToID map[string]string,
	budget int,
	fuelPerJump int,
	priceOf func(stationID string) float64,
	limit int,
) (rows []arbRow, skipped int) {
	baseline := arbJumps(graph, curSys, destSys)
	if baseline < 0 {
		return nil, 0
	}
	for _, o := range opps {
		buySys, ok1 := nameToID[strings.ToLower(o.FromSystemName)]
		sellSys, ok2 := nameToID[strings.ToLower(o.ToSystemName)]
		if !ok1 || !ok2 || buySys == "" || sellSys == "" {
			skipped++
			continue
		}
		a := arbJumps(graph, curSys, buySys)
		b := arbJumps(graph, buySys, sellSys)
		c := arbJumps(graph, sellSys, destSys)
		if a < 0 || b < 0 || c < 0 {
			skipped++
			continue
		}
		detour := max(a+b+c-baseline, 0)
		if detour > budget {
			continue
		}
		fuelCost := 0.0
		if fuelPerJump > 0 && priceOf != nil {
			fuelCost = float64(detour*fuelPerJump) * priceOf(o.FromStationID)
		}
		rows = append(rows, arbRow{Opp: o, Detour: detour, Net: o.GrossProfit - fuelCost})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Net != rows[j].Net {
			return rows[i].Net > rows[j].Net
		}
		if rows[i].Detour != rows[j].Detour {
			return rows[i].Detour < rows[j].Detour
		}
		return rows[i].Opp.ID < rows[j].Opp.ID
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, skipped
}

// arbJumps returns the jump count from `from` to `to`, or -1 if unreachable or
// either id is empty.
func arbJumps(graph navigation.JumpGraph, from, to string) int {
	if from == "" || to == "" {
		return -1
	}
	if from == to {
		return 0
	}
	d := navigation.BFSJumps(graph, from, []string{to})
	j, ok := d[to]
	if !ok || j >= navigation.RouteInf {
		return -1
	}
	return j
}
```

The fuel-price resolver (`buildArbPriceOf` + `arbFuelPriceSource` + `arbFreeFuelStations`) is deliberately NOT in this file — it is added in Task 2, where it is used. Everything in this task is referenced by the tests (`rankDetourArbitrage`, and `arbJumps` transitively), so the pre-commit hook's no-unused check passes.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/tools/play_as/ -run TestRankDetour -v` (timeout 300000)
Expected: PASS (all four tests).

- [ ] **Step 5: Build + lint**

Run: `go build ./cmd/tools/play_as/ && golangci-lint run ./cmd/tools/play_as/...` (timeout 300000)
Expected: build ok; no lint findings (every symbol this task adds is used by the tests). If lint reports anything, fix it before committing — the repo's pre-commit hook hard-fails the commit on any golangci-lint finding.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/arbitrage_cmd.go cmd/tools/play_as/arbitrage_cmd_test.go
git commit -m "feat(play_as): pure detour arbitrage ranker + fuel-price resolver"
```

---

## Task 2: Command handlers, rendering, and REPL wiring

**Files:**
- Modify: `cmd/tools/play_as/arbitrage_cmd.go` (append handlers + rendering)
- Modify: `cmd/tools/play_as/main.go` (dispatch cases + help text)

**Interfaces:**
- Consumes: `rankDetourArbitrage`, `arbRow`, `arbJumps` (Task 1); `currentJumpFuel`, `resolveSystemToken`, `displayName`, `plural`, `partitionFlags`, `globalKB`, `globalMarketCollector`, `globalAgentID`, `outputFormat`/`formatStyled` (existing).
- Produces (this task): `buildArbPriceOf`, `arbFuelPriceSource`, `arbFreeFuelStations`, and the three handlers below.
- Produces: `runFindArbitrage(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error`; `runClaimArbitrage(ctx context.Context, parts []string) error`; `runReleaseArbitrage(ctx context.Context, parts []string) error`.

- [ ] **Step 1: Append the handlers to `arbitrage_cmd.go`**

Add these imports to the file's import block (joining the existing `"sort"`, `"strings"`, `market`, `navigation`): `"context"`, `"encoding/json"`, `"fmt"`, `"strconv"`, `"time"`, and `"github.com/rsned/spacemolt/pkg/game"`. Then append the fuel-price resolver (moved here from Task 1 so it is used) followed by the handlers:

```go
// arbFreeFuelStations are stations that refuel for free (the databot faction's
// ally pump); fuel priced at one of these costs 0. Mirrors
// worker.haulFreeFuelStations. A future refinement can data-drive this.
var arbFreeFuelStations = map[string]bool{
	"grand_exchange_station": true,
}

// arbFuelPriceSource is the market subset used to price fuel (satisfied by
// *market.Collector).
type arbFuelPriceSource interface {
	GetStationFuelPrice(ctx context.Context, stationID string) (allIn int, capturedAt time.Time, ok bool, err error)
	MedianStationFuelAllIn(ctx context.Context) (median int, ok bool, err error)
}

// buildArbPriceOf returns a station->creditsPerUnit fuel resolver: 0 for
// free-pump stations, the captured all-in when present, else the galaxy median
// (probed once here), else 0. A nil source yields a constant-0 resolver.
// Mirrors worker.buildPriceOf.
func buildArbPriceOf(ctx context.Context, src arbFuelPriceSource) func(string) float64 {
	if src == nil {
		return func(string) float64 { return 0 }
	}
	median, medianOK, _ := src.MedianStationFuelAllIn(ctx)
	return func(stationID string) float64 {
		if arbFreeFuelStations[stationID] {
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
```

Then the handlers:

```go
// runFindArbitrage lists available arbitrage opportunities that are a minimal
// detour from the operator's current system toward <dest>.
//
// Usage: find_arbitrage <dest> [--detour N] [--limit N]
func runFindArbitrage(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	if globalMarketCollector == nil {
		return fmt.Errorf("find_arbitrage requires the market database, which is not available")
	}
	if globalKB == nil {
		return fmt.Errorf("find_arbitrage requires the knowledge base, which is not available")
	}

	positional, flags := partitionFlags(parts[1:])
	if len(positional) == 0 {
		return fmt.Errorf("usage: find_arbitrage <dest> [--detour N] [--limit N]")
	}
	destToken := strings.Join(positional, " ")
	budget := 3
	if v, ok := flags["detour"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			budget = n
		}
	}
	limit := 10
	if v, ok := flags["limit"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	state := client.GetState()
	if state == nil || state.System.ID == "" {
		return fmt.Errorf("cannot determine current system; try get_status first")
	}
	curID := state.System.ID

	systems, err := globalKB.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("load systems: %w", err)
	}
	byID := make(map[string]string, len(systems))
	byName := make(map[string]string, len(systems))
	nameOf := make(map[string]string, len(systems))
	for _, s := range systems {
		byID[strings.ToLower(s.ID)] = s.ID
		if s.Name != "" {
			byName[strings.ToLower(s.Name)] = s.ID
		}
		nameOf[s.ID] = s.Name
	}
	destID, ok := resolveSystemToken(destToken, byID, byName)
	if !ok {
		return fmt.Errorf("unknown destination system: %q", destToken)
	}

	conns, err := globalKB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("load connections: %w", err)
	}
	graph := navigation.JumpGraphFromConnections(conns)

	baseline := arbJumps(graph, curID, destID)
	if baseline < 0 {
		return fmt.Errorf("no known jump route from %s to %s",
			displayName(curID, nameOf), displayName(destID, nameOf))
	}

	opps, err := globalMarketCollector.GetOpportunities(ctx, "available", 300)
	if err != nil {
		return fmt.Errorf("load opportunities: %w", err)
	}

	fuelPerJump, _ := currentJumpFuel(client, ctx, destID)
	priceOf := buildArbPriceOf(ctx, globalMarketCollector)

	rows, skipped := rankDetourArbitrage(opps, curID, destID, graph, byName, budget, fuelPerJump, priceOf, limit)

	renderArbitrage(rows, skipped, baseline, budget, curID, destID, nameOf, format)
	return nil
}

// renderArbitrage prints the ranked opportunities as a styled table (or JSON).
func renderArbitrage(rows []arbRow, skipped, baseline, budget int, curID, destID string, nameOf map[string]string, format outputFormat) {
	if format != formatStyled {
		type outRow struct {
			ID       int     `json:"id"`
			Item     string  `json:"item"`
			Quantity float64 `json:"quantity"`
			BuyAt    string  `json:"buy_at"`
			SellAt   string  `json:"sell_at"`
			Detour   int     `json:"detour_jumps"`
			Gross    float64 `json:"gross_profit"`
			Net      float64 `json:"net_of_fuel"`
		}
		out := struct {
			BaselineJumps int      `json:"baseline_jumps"`
			DetourBudget  int      `json:"detour_budget"`
			Skipped       int      `json:"skipped"`
			Rows          []outRow `json:"rows"`
		}{BaselineJumps: baseline, DetourBudget: budget, Skipped: skipped}
		for _, r := range rows {
			out.Rows = append(out.Rows, outRow{
				ID: r.Opp.ID, Item: r.Opp.ItemName, Quantity: r.Opp.Quantity,
				BuyAt: r.Opp.FromStationName, SellAt: r.Opp.ToStationName,
				Detour: r.Detour, Gross: r.Opp.GrossProfit, Net: r.Net,
			})
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			fmt.Printf("{\"error\":%q}\n", err.Error())
			return
		}
		fmt.Println(string(b))
		return
	}

	fmt.Printf("\nArbitrage on the way: %s → %s (%d jump%s direct, detour ≤ %d)\n",
		displayName(curID, nameOf), displayName(destID, nameOf), baseline, plural(baseline), budget)
	if len(rows) == 0 {
		fmt.Printf("  No on-the-way opportunities within a %d-jump detour.\n", budget)
	}
	for _, r := range rows {
		item := r.Opp.ItemName
		if item == "" {
			item = r.Opp.ItemID
		}
		fmt.Printf("  #%d  %s x%.0f  buy@%s → sell@%s  +%d jump%s  gross %.0f  net %.0f\n",
			r.Opp.ID, item, r.Opp.Quantity, r.Opp.FromStationName, r.Opp.ToStationName,
			r.Detour, plural(r.Detour), r.Opp.GrossProfit, r.Net)
	}
	if skipped > 0 {
		fmt.Printf("  (%d opportunit%s skipped: unresolved or unreachable systems)\n",
			skipped, map[bool]string{true: "y", false: "ies"}[skipped == 1])
	}
	fmt.Println("  Claim one with: claim_arbitrage <id>")
}

// runClaimArbitrage claims an opportunity for this operator so the hauler fleet
// skips it. Usage: claim_arbitrage <id>
func runClaimArbitrage(ctx context.Context, parts []string) error {
	if globalMarketCollector == nil {
		return fmt.Errorf("claim_arbitrage requires the market database, which is not available")
	}
	if len(parts) < 2 {
		return fmt.Errorf("usage: claim_arbitrage <id>")
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid opportunity id %q (must be an integer)", parts[1])
	}
	ok, err := globalMarketCollector.ClaimOpportunity(ctx, id, globalAgentID)
	if err != nil {
		return fmt.Errorf("claim opportunity %d: %w", id, err)
	}
	if !ok {
		fmt.Printf("Could not claim #%d — already claimed, completed, or gone.\n", id)
		return nil
	}
	fmt.Printf("Claimed #%d — the hauler fleet will now skip it. Release with: release_arbitrage %d\n", id, id)
	return nil
}

// runReleaseArbitrage releases an opportunity this operator previously claimed.
// Usage: release_arbitrage <id>
func runReleaseArbitrage(ctx context.Context, parts []string) error {
	if globalMarketCollector == nil {
		return fmt.Errorf("release_arbitrage requires the market database, which is not available")
	}
	if len(parts) < 2 {
		return fmt.Errorf("usage: release_arbitrage <id>")
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid opportunity id %q (must be an integer)", parts[1])
	}
	ok, err := globalMarketCollector.ReleaseOpportunity(ctx, id, globalAgentID)
	if err != nil {
		return fmt.Errorf("release opportunity %d: %w", id, err)
	}
	if !ok {
		fmt.Printf("Nothing to release for #%d — not held by this agent.\n", id)
		return nil
	}
	fmt.Printf("Released #%d — it is available to the fleet again.\n", id)
	return nil
}
```

- [ ] **Step 2: Wire the REPL dispatch cases**

In `cmd/tools/play_as/main.go`, add these cases next to the other data-tool cases (e.g. right after the `case "plan_route", "plan-route":` block):

```go
	case "find_arbitrage", "find-arbitrage":
		return runFindArbitrage(client, ctx, parts, format)
	case "claim_arbitrage", "claim-arbitrage":
		return runClaimArbitrage(ctx, parts)
	case "release_arbitrage", "release-arbitrage":
		return runReleaseArbitrage(ctx, parts)
```

- [ ] **Step 3: Add help text**

The help block is a series of `fmt.Println` calls in `main.go` (the `plan_route` help line is at approximately `main.go:9379`:
`fmt.Println("  plan_route [--return] <systems...>  - Optimal jump order to visit systems; prints autopilot cmds")`).
Insert these lines immediately after the `auto_explore` help block that follows `plan_route`, matching the `  <cmd>  - <desc>` style:

```go
	fmt.Println("  find_arbitrage <dest> [--detour N] [--limit N]")
	fmt.Println("                            - Arbitrage opportunities on the way to <dest> (detour<=N jumps, default 3)")
	fmt.Println("  claim_arbitrage <id>      - Claim an opportunity so the hauler fleet skips it")
	fmt.Println("  release_arbitrage <id>    - Release an opportunity you claimed")
```

(Use the two-line form for `find_arbitrage` — the same wrap style the existing `auto_explore [--max-hops N]` help line uses.)

- [ ] **Step 4: Build, full test, lint**

Run: `go build ./... && go test ./... && golangci-lint run ./...` (timeout 300000)
Expected: build ok; all tests pass (including Task 1's `TestRankDetour*`); no new lint findings (the Task 1 `buildArbPriceOf`/`arbFuelPriceSource` are now used).

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/arbitrage_cmd.go cmd/tools/play_as/main.go
git commit -m "feat(play_as): find_arbitrage / claim_arbitrage / release_arbitrage commands"
```

---

## Deploy

None. `play_as` is run on demand by the operator via `go run ./cmd/tools/play_as`; the new commands are available on the next run after merge. No fleet redeploy.

## Notes

- Marginal-fuel design point (from spec + final-review parity with Sub-project B): the fuel term prices the **detour** jumps, so `net` answers "is stopping for this haul worth it versus flying straight to dest." Degrades to gross when fuel data is thin.
- `find_arbitrage` reads the scanner table only (operator's choice); no fresh scan.
- Future work (not this plan): `--scan` flag / staleness-triggered refresh; a `my_arbitrage` view of own claims; cargo/affordability sizing; autopilot handoff.
