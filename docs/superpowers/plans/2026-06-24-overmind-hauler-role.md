# Overmind Hauler Role Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an autonomous `hauler` standing role that claims the best reachable arbitrage opportunity, moves the goods buy-station → sell-station, and reports completion — closing the market-intelligence detect→haul→profit loop (Market Intelligence Phase 5).

**Architecture:** A new `pkg/worker/haul.go` mirrors `explore.go`: a pure ranking function (`RankHaulOpportunities`) plus an impure one-step engine (`Haul`) invoked through a new `haul` command in `WorkerDispatch`. The engine reads/claims via the already-wired `market.Collector` (exposed to the engine through a small `OpportunityStore` interface so it stays testable and leaves `pkg/market` unmodified), and reuses the existing `Autopilot` for both transit legs. The role is configured exactly like `explorer` (idle script + roles.yaml + fleet.yaml).

**Tech Stack:** Go 1.24+, `pkg/worker` (runtime), `pkg/market` (arbitrage atoms, read-only here), `pkg/navigation` (jump graph), `pkg/knowledge` (KB), `pkg/game` (client).

## Global Constraints

- Go 1.24+; prefer modern idioms; `b.Loop()` in any benchmarks (none here).
- **No `pkg/market` changes.** Use only the shipped atoms: `GetOpportunities`, `ClaimOpportunity`, `CompleteOpportunity`, `ScanArbitrage`. No schema/migration/new read.
- All new code passes `golangci-lint` with no new findings; run it after each task.
- After each task: `go build ./...` and `go test ./...` must be green before committing.
- Sleeps/pauses use `pkg/game/constants.go` constants (the engine adds none; `Autopilot`/standing loop own timing).
- New shared catalog scripts MUST be added to the `.gitignore` allowlist (`!data/scripts/<name>.smolt`).
- **Base branch:** this work builds on `origin/main` (which has PR #127's `pkg/market` arbitrage atoms). Rebase `feat/overmind-hauler-role` onto `origin/main` before starting Task 1 — local `main` is behind and lacks the atoms.

## File Structure

- `pkg/worker/haul.go` (new) — `RankHaulOpportunities` (pure), `sizeBuy` (pure), `OpportunityStore` interface, `loadAvailable` / `claimBest` (store-only), `Haul` + `runClaimedHaul` (engine).
- `pkg/worker/haul_test.go` (new) — unit tests for the pure + store-only units (reuses `fakeKB` from `explore_test.go`; adds `fakeStore`).
- `pkg/worker/dispatch.go` (modify) — add `AgentID` field to `WorkerDispatch`, `"haul"` to `supported`, `case "haul"` arm.
- `pkg/worker/dispatch_test.go` (modify) — assert `haul` is supported + nil-market `haul` is a safe no-op.
- `cmd/worker/main.go` (modify, ~line 261) — set `dispatch.AgentID = *agentID`.
- `data/scripts/haul.smolt` (new) — `haul` then `update_market`.
- `.gitignore` (modify, ~line 129) — allowlist `haul.smolt`.
- `data/overmind/roles.yaml` (modify) — add `hauler` role.
- `data/overmind/fleet.yaml` (modify) — add hauler agents.

---

### Task 1: `RankHaulOpportunities` — opportunity selection (pure)

**Files:**
- Create: `pkg/worker/haul.go`
- Test: `pkg/worker/haul_test.go`

**Interfaces:**
- Consumes: `market.ArbitrageOpportunity` (fields `ID int`, `FromSystemName`, `ToSystemName`, `FromStationID`, `ToStationID`, `ItemID string`, `GrossProfit`, `Quantity`, `BuyPrice float64`); `navigation.JumpGraph` / `JumpGraphFromConnections` / `BFSJumps` / `RouteInf`; `knowledge.System{ID,Name}`.
- Produces: `func RankHaulOpportunities(opps []market.ArbitrageOpportunity, currentSystemID string, nameToID map[string]string, graph navigation.JumpGraph) []market.ArbitrageOpportunity`; `func buildNameToID(systems []knowledge.System) map[string]string`; consts `DefaultHaulPoolLimit = 50`, `haulNearTieFraction = 0.10`.

- [ ] **Step 1: Write the failing tests**

Create `pkg/worker/haul_test.go`:

```go
package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// graphFor builds a jump graph + name->id map from undirected system-id pairs,
// treating each id as also its display name capitalized-irrelevant (name==id here).
func graphFor(systems []string, pairs ...[2]string) (navigation.JumpGraph, map[string]string) {
	conns := undirected(pairs...) // from explore_test.go
	g := navigation.JumpGraphFromConnections(conns)
	n2id := map[string]string{}
	for _, s := range systems {
		n2id[s] = s // name == id in tests
	}
	return g, n2id
}

func opp(id int, fromSys, toSys string, gross float64) market.ArbitrageOpportunity {
	return market.ArbitrageOpportunity{
		ID: id, FromSystemName: fromSys, ToSystemName: toSys,
		FromStationID: fromSys + "-stn", ToStationID: toSys + "-stn",
		ItemID: "iron_ore", GrossProfit: gross, Quantity: 10, BuyPrice: 5,
	}
}

func ids(opps []market.ArbitrageOpportunity) []int {
	out := make([]int, len(opps))
	for i, o := range opps {
		out[i] = o.ID
	}
	return out
}

func TestRankProfitDominant(t *testing.T) {
	// a-b-c chain. Two opps both bought at b (1 jump from current a). 200 > 100.
	g, n2id := graphFor([]string{"a", "b", "c"}, [2]string{"a", "b"}, [2]string{"b", "c"})
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{
		opp(1, "b", "c", 100),
		opp(2, "b", "c", 200),
	}, "a", n2id, g)
	if len(got) != 2 || got[0].ID != 2 {
		t.Fatalf("want [2 1] by gross, got %v", ids(got))
	}
}

func TestRankNearTieProximityTiebreak(t *testing.T) {
	// Within 10%: 200 vs 195. opp 1 buys at b (1 jump), opp 2 buys at c (2 jumps).
	// Closer buy wins despite slightly lower gross.
	g, n2id := graphFor([]string{"a", "b", "c", "d"},
		[2]string{"a", "b"}, [2]string{"b", "c"}, [2]string{"c", "d"})
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{
		opp(2, "c", "d", 200),
		opp(1, "b", "d", 195),
	}, "a", n2id, g)
	if got[0].ID != 1 {
		t.Fatalf("want closer buy (id 1) first, got %v", ids(got))
	}
}

func TestRankChainingTiebreak(t *testing.T) {
	// Within 10%, equal jumps to buy (both at b). opp 1 sells at c; opp 3 buys at c,
	// so opp 1's drop-off chains into another opp. opp 2 sells at z (no chain).
	g, n2id := graphFor([]string{"a", "b", "c", "z"},
		[2]string{"a", "b"}, [2]string{"b", "c"}, [2]string{"b", "z"})
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{
		opp(2, "b", "z", 200),
		opp(1, "b", "c", 198),
		opp(3, "c", "z", 50), // the chain target (buys at c)
	}, "a", n2id, g)
	// opp 1 and 2 are in the band (>=180); opp 3 (50) is not. opp 1 chains -> first.
	if got[0].ID != 1 {
		t.Fatalf("want chaining opp (id 1) first, got %v", ids(got))
	}
}

func TestRankSkipsUnresolvedAndUnreachable(t *testing.T) {
	// opp 1 buys at "ghost" (not in name map) -> skipped.
	// opp 2 buys at "island" (no graph edge from a) -> unreachable -> skipped.
	// opp 3 buys at b (reachable) -> kept.
	g, n2id := graphFor([]string{"a", "b", "island"}, [2]string{"a", "b"})
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{
		opp(1, "ghost", "b", 999),
		opp(2, "island", "b", 999),
		opp(3, "b", "a", 100),
	}, "a", n2id, g)
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("want only reachable+resolved id 3, got %v", ids(got))
	}
}

func TestRankDeterministicByID(t *testing.T) {
	// Identical gross+jumps+chain -> lower id first.
	g, n2id := graphFor([]string{"a", "b", "z"}, [2]string{"a", "b"}, [2]string{"b", "z"})
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{
		opp(5, "b", "z", 100),
		opp(2, "b", "z", 100),
	}, "a", n2id, g)
	if got[0].ID != 2 {
		t.Fatalf("want lower id 2 first, got %v", ids(got))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run TestRank -v`
Expected: FAIL — `undefined: RankHaulOpportunities`.

- [ ] **Step 3: Write minimal implementation**

Create `pkg/worker/haul.go`:

```go
package worker

import (
	"sort"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultHaulPoolLimit caps how many available opportunities a hauler considers.
const DefaultHaulPoolLimit = 50

// haulNearTieFraction: opportunities within this fraction of the top gross profit
// are reordered by proximity/chaining rather than raw profit.
const haulNearTieFraction = 0.10

// buildNameToID maps system display names to system ids from the KB. The arbitrage
// rows carry system *names*; the jump graph keys on *ids*. Last write wins on the
// (rare) duplicate name.
func buildNameToID(systems []knowledge.System) map[string]string {
	m := make(map[string]string, len(systems))
	for _, s := range systems {
		if s.Name != "" {
			m[s.Name] = s.ID
		}
	}
	return m
}

// rankedOpp pairs an opportunity with its resolved routing facts.
type rankedOpp struct {
	opp       market.ArbitrageOpportunity
	buySysID  string
	sellSysID string // "" if unresolved
	jumps     int    // current -> buySys
	chain     bool   // sellSys at/adjacent to another opp's buySys
}

// RankHaulOpportunities orders available opportunities best-first for a hauler at
// currentSystemID. Primary order is gross_profit descending; opportunities within
// haulNearTieFraction of the top gross are instead ordered by reposition cost
// (jumps current->buy), then a chaining bonus (sell at/adjacent to another opp's
// buy), then id. Opportunities whose buy-system name does not resolve to a known
// system id, or whose buy-system is unreachable, are dropped.
func RankHaulOpportunities(opps []market.ArbitrageOpportunity, currentSystemID string, nameToID map[string]string, graph navigation.JumpGraph) []market.ArbitrageOpportunity {
	resolved := make([]rankedOpp, 0, len(opps))
	buyTargets := make([]string, 0, len(opps))
	for _, o := range opps {
		buyID, ok := nameToID[o.FromSystemName]
		if !ok || buyID == "" {
			continue // can't route to the buy station
		}
		resolved = append(resolved, rankedOpp{opp: o, buySysID: buyID, sellSysID: nameToID[o.ToSystemName]})
		buyTargets = append(buyTargets, buyID)
	}
	if len(resolved) == 0 {
		return nil
	}

	dist := navigation.BFSJumps(graph, currentSystemID, buyTargets)

	reach := make([]rankedOpp, 0, len(resolved))
	for _, r := range resolved {
		d, ok := dist[r.buySysID]
		if !ok || d >= navigation.RouteInf {
			continue
		}
		r.jumps = d
		reach = append(reach, r)
	}
	if len(reach) == 0 {
		return nil
	}

	for i := range reach {
		reach[i].chain = sellChains(reach[i], reach, graph)
	}

	maxGross := 0.0
	for _, r := range reach {
		if r.opp.GrossProfit > maxGross {
			maxGross = r.opp.GrossProfit
		}
	}
	threshold := maxGross * (1 - haulNearTieFraction)

	band := make([]rankedOpp, 0, len(reach))
	rest := make([]rankedOpp, 0, len(reach))
	for _, r := range reach {
		if r.opp.GrossProfit >= threshold {
			band = append(band, r)
		} else {
			rest = append(rest, r)
		}
	}

	sort.SliceStable(band, func(i, j int) bool {
		if band[i].jumps != band[j].jumps {
			return band[i].jumps < band[j].jumps
		}
		if band[i].chain != band[j].chain {
			return band[i].chain // chaining opp sorts first
		}
		return band[i].opp.ID < band[j].opp.ID
	})
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].opp.GrossProfit != rest[j].opp.GrossProfit {
			return rest[i].opp.GrossProfit > rest[j].opp.GrossProfit
		}
		if rest[i].jumps != rest[j].jumps {
			return rest[i].jumps < rest[j].jumps
		}
		return rest[i].opp.ID < rest[j].opp.ID
	})

	out := make([]market.ArbitrageOpportunity, 0, len(reach))
	for _, r := range band {
		out = append(out, r.opp)
	}
	for _, r := range rest {
		out = append(out, r.opp)
	}
	return out
}

// sellChains reports whether r's sell-system is at or within one jump of any OTHER
// opportunity's buy-system (so the next run starts near r's drop-off).
func sellChains(r rankedOpp, all []rankedOpp, graph navigation.JumpGraph) bool {
	if r.sellSysID == "" {
		return false
	}
	for _, other := range all {
		if other.opp.ID == r.opp.ID {
			continue
		}
		if other.buySysID == r.sellSysID {
			return true
		}
		for _, nb := range graph[r.sellSysID] {
			if nb == other.buySysID {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestRank -v`
Expected: PASS (all 5).

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/haul.go pkg/worker/haul_test.go
git commit -m "feat(worker): RankHaulOpportunities arbitrage selection"
```

---

### Task 2: `sizeBuy` — purchase quantity sizing (pure)

**Files:**
- Modify: `pkg/worker/haul.go`
- Test: `pkg/worker/haul_test.go`

**Interfaces:**
- Produces: `func sizeBuy(opp market.ArbitrageOpportunity, cargoFree, credits, askEach float64) float64`.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/worker/haul_test.go`:

```go
func TestSizeBuyQuantityLimited(t *testing.T) {
	o := market.ArbitrageOpportunity{Quantity: 10}
	// Plenty of cargo and credits -> capped by opp quantity.
	if got := sizeBuy(o, 100, 100000, 5); got != 10 {
		t.Fatalf("want 10, got %v", got)
	}
}

func TestSizeBuyCargoLimited(t *testing.T) {
	o := market.ArbitrageOpportunity{Quantity: 10}
	if got := sizeBuy(o, 4, 100000, 5); got != 4 {
		t.Fatalf("want 4 (cargo), got %v", got)
	}
}

func TestSizeBuyCreditLimited(t *testing.T) {
	o := market.ArbitrageOpportunity{Quantity: 10}
	// 23 credits / 5 each = floor 4.
	if got := sizeBuy(o, 100, 23, 5); got != 4 {
		t.Fatalf("want 4 (credits), got %v", got)
	}
}

func TestSizeBuyZeroAsk(t *testing.T) {
	o := market.ArbitrageOpportunity{Quantity: 10}
	if got := sizeBuy(o, 100, 100, 0); got != 0 {
		t.Fatalf("want 0 on non-positive ask, got %v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run TestSizeBuy -v`
Expected: FAIL — `undefined: sizeBuy`.

- [ ] **Step 3: Write minimal implementation**

Add to `pkg/worker/haul.go` (and add `"math"` to the import block):

```go
// sizeBuy returns how many units to buy: the opportunity quantity, capped by free
// cargo space and by what credits afford at askEach. Returns 0 when nothing is
// affordable or askEach is non-positive.
func sizeBuy(opp market.ArbitrageOpportunity, cargoFree, credits, askEach float64) float64 {
	if askEach <= 0 {
		return 0
	}
	qty := opp.Quantity
	if cargoFree < qty {
		qty = cargoFree
	}
	if affordable := math.Floor(credits / askEach); affordable < qty {
		qty = affordable
	}
	if qty < 0 {
		qty = 0
	}
	return qty
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestSizeBuy -v`
Expected: PASS (all 4).

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/haul.go pkg/worker/haul_test.go
git commit -m "feat(worker): sizeBuy purchase sizing helper"
```

---

### Task 3: `OpportunityStore` + `loadAvailable` + `claimBest` (store-only)

**Files:**
- Modify: `pkg/worker/haul.go`
- Test: `pkg/worker/haul_test.go`

**Interfaces:**
- Consumes: `market.Collector` satisfies the new interface (verified via a compile-time assertion in Task 4); `market.ScanOptions`, `market.ScanResult`.
- Produces:
  - `type OpportunityStore interface { GetOpportunities(ctx, status string, limit int) ([]market.ArbitrageOpportunity, error); ClaimOpportunity(ctx, id int, agentID string) (bool, error); CompleteOpportunity(ctx, id int, agentID string) (bool, error); ScanArbitrage(ctx, opts market.ScanOptions) (market.ScanResult, error) }`
  - `func loadAvailable(ctx, store OpportunityStore, limit int) ([]market.ArbitrageOpportunity, error)`
  - `func claimBest(ctx, store OpportunityStore, ranked []market.ArbitrageOpportunity, agentID string) (market.ArbitrageOpportunity, bool, error)`

- [ ] **Step 1: Write the failing tests**

Append to `pkg/worker/haul_test.go` (add `"context"` to its imports):

```go
type fakeStore struct {
	available    []market.ArbitrageOpportunity
	scanPopulate []market.ArbitrageOpportunity
	scanned      int
	claims       map[int]bool // id -> claim succeeds
	completed    []int
}

func (f *fakeStore) GetOpportunities(_ context.Context, status string, _ int) ([]market.ArbitrageOpportunity, error) {
	if status != "available" {
		return nil, nil
	}
	return f.available, nil
}
func (f *fakeStore) ClaimOpportunity(_ context.Context, id int, _ string) (bool, error) {
	return f.claims[id], nil
}
func (f *fakeStore) CompleteOpportunity(_ context.Context, id int, _ string) (bool, error) {
	f.completed = append(f.completed, id)
	return true, nil
}
func (f *fakeStore) ScanArbitrage(_ context.Context, _ market.ScanOptions) (market.ScanResult, error) {
	f.scanned++
	f.available = f.scanPopulate
	return market.ScanResult{}, nil
}

func TestLoadAvailableNonEmptyNoScan(t *testing.T) {
	f := &fakeStore{available: []market.ArbitrageOpportunity{opp(1, "b", "c", 100)}}
	got, err := loadAvailable(context.Background(), f, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || f.scanned != 0 {
		t.Fatalf("want 1 opp and no scan, got %d opps scanned=%d", len(got), f.scanned)
	}
}

func TestLoadAvailableEmptyTriggersScan(t *testing.T) {
	f := &fakeStore{
		available:    nil,
		scanPopulate: []market.ArbitrageOpportunity{opp(7, "b", "c", 100)},
	}
	got, err := loadAvailable(context.Background(), f, 50)
	if err != nil {
		t.Fatal(err)
	}
	if f.scanned != 1 || len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("want 1 scan + opp 7, got scanned=%d opps=%v", f.scanned, ids(got))
	}
}

func TestClaimBestFirstWins(t *testing.T) {
	f := &fakeStore{claims: map[int]bool{1: true, 2: true}}
	ranked := []market.ArbitrageOpportunity{opp(1, "b", "c", 100), opp(2, "b", "c", 90)}
	got, ok, err := claimBest(context.Background(), f, ranked, "hauler-1")
	if err != nil || !ok || got.ID != 1 {
		t.Fatalf("want claim id 1, got id=%d ok=%v err=%v", got.ID, ok, err)
	}
}

func TestClaimBestRaceFallthrough(t *testing.T) {
	// id 1 already taken (false), id 2 succeeds.
	f := &fakeStore{claims: map[int]bool{1: false, 2: true}}
	ranked := []market.ArbitrageOpportunity{opp(1, "b", "c", 100), opp(2, "b", "c", 90)}
	got, ok, err := claimBest(context.Background(), f, ranked, "hauler-1")
	if err != nil || !ok || got.ID != 2 {
		t.Fatalf("want fallthrough to id 2, got id=%d ok=%v err=%v", got.ID, ok, err)
	}
}

func TestClaimBestAllTaken(t *testing.T) {
	f := &fakeStore{claims: map[int]bool{1: false, 2: false}}
	ranked := []market.ArbitrageOpportunity{opp(1, "b", "c", 100), opp(2, "b", "c", 90)}
	_, ok, err := claimBest(context.Background(), f, ranked, "hauler-1")
	if err != nil || ok {
		t.Fatalf("want ok=false when all taken, got ok=%v err=%v", ok, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestLoadAvailable|TestClaimBest' -v`
Expected: FAIL — `undefined: loadAvailable` / `undefined: claimBest`.

- [ ] **Step 3: Write minimal implementation**

Add to `pkg/worker/haul.go` (add `"context"` and `"fmt"` to the import block):

```go
// OpportunityStore is the subset of *market.Collector the hauler needs. Defining it
// here keeps the engine testable with a fake and leaves pkg/market unmodified.
type OpportunityStore interface {
	GetOpportunities(ctx context.Context, status string, limit int) ([]market.ArbitrageOpportunity, error)
	ClaimOpportunity(ctx context.Context, id int, agentID string) (bool, error)
	CompleteOpportunity(ctx context.Context, id int, agentID string) (bool, error)
	ScanArbitrage(ctx context.Context, opts market.ScanOptions) (market.ScanResult, error)
}

// loadAvailable returns available opportunities, running one ScanArbitrage to
// refresh the pool when it is empty (haulers are the periodic scan trigger). Scan
// uses default options; it is idempotent under the write lock, so a redundant scan
// from concurrent haulers is harmless.
func loadAvailable(ctx context.Context, store OpportunityStore, limit int) ([]market.ArbitrageOpportunity, error) {
	opps, err := store.GetOpportunities(ctx, "available", limit)
	if err != nil {
		return nil, fmt.Errorf("haul: get opportunities: %w", err)
	}
	if len(opps) > 0 {
		return opps, nil
	}
	if _, err := store.ScanArbitrage(ctx, market.ScanOptions{}); err != nil {
		return nil, fmt.Errorf("haul: scan arbitrage: %w", err)
	}
	opps, err = store.GetOpportunities(ctx, "available", limit)
	if err != nil {
		return nil, fmt.Errorf("haul: get opportunities (post-scan): %w", err)
	}
	return opps, nil
}

// claimBest claims the first opportunity in ranked order still available. ok=false
// means every candidate was taken by another hauler first.
func claimBest(ctx context.Context, store OpportunityStore, ranked []market.ArbitrageOpportunity, agentID string) (market.ArbitrageOpportunity, bool, error) {
	for _, o := range ranked {
		ok, err := store.ClaimOpportunity(ctx, o.ID, agentID)
		if err != nil {
			return market.ArbitrageOpportunity{}, false, fmt.Errorf("haul: claim %d: %w", o.ID, err)
		}
		if ok {
			return o, true, nil
		}
	}
	return market.ArbitrageOpportunity{}, false, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestLoadAvailable|TestClaimBest' -v`
Expected: PASS (all 5).

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/haul.go pkg/worker/haul_test.go
git commit -m "feat(worker): OpportunityStore + loadAvailable + claimBest"
```

---

### Task 4: `Haul` engine + `WorkerDispatch` wiring

**Files:**
- Modify: `pkg/worker/haul.go`
- Modify: `pkg/worker/dispatch.go` (struct field, `supported`, `case`)
- Modify: `pkg/worker/dispatch_test.go`
- Modify: `cmd/worker/main.go` (~line 261)
- Test: `pkg/worker/dispatch_test.go`

**Interfaces:**
- Consumes: `game.GameClient` (`GetState() *game.State`, `Buy(ctx, itemID string, qty float64) error`, `Sell(ctx, itemID string, qty float64) error`); `game.State` (`System.ID`, `Ship.CargoCapacity/CargoUsed`, `Ship.Cargo []game.CargoItem{ItemID,Quantity}`, `GetCredits() float64`); `Autopilot` + `AutopilotDeps`; `KBUpdateSystem` / `KBUpdatePOI`; `knowledge.Base`.
- Produces: `type HaulDeps struct{ Client game.GameClient; KB knowledge.Base; Market OpportunityStore; Out io.Writer; AgentID string; PoolLimit int }`; `func Haul(ctx context.Context, deps HaulDeps) error`; `WorkerDispatch.AgentID string`; `supported["haul"]`.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/worker/dispatch_test.go`:

```go
func TestHaulIsSupported(t *testing.T) {
	d := NewWorkerDispatch(nil, nil, nil, nil)
	if !d.Supports("haul") {
		t.Fatal("haul should be in the supported command set")
	}
}

func TestHaulNilMarketIsSafeNoop(t *testing.T) {
	// No market collector configured -> haul logs and returns nil, never panics.
	d := NewWorkerDispatch(nil, nil, nil, nil)
	if err := d.Run(context.Background(), []string{"haul"}); err != nil {
		t.Fatalf("haul with nil market should no-op, got %v", err)
	}
}
```

(If `dispatch_test.go` lacks a `"context"` import, add it.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run TestHaul -v`
Expected: FAIL — `haul` not supported / dispatch has no `haul` case.

- [ ] **Step 3a: Add the engine to `pkg/worker/haul.go`**

Add `"io"`, `"github.com/rsned/spacemolt/pkg/game"` to the import block, then append:

```go
// compile-time check that the real collector satisfies the engine's store.
var _ OpportunityStore = (*market.Collector)(nil)

// HaulDeps are the injected collaborators for one Haul step.
type HaulDeps struct {
	Client    game.GameClient
	KB        knowledge.Base
	Market    OpportunityStore
	Out       io.Writer // nil -> io.Discard
	AgentID   string    // claim owner
	PoolLimit int       // 0 -> DefaultHaulPoolLimit
}

// Haul performs one hauling step: load available opportunities (scanning if the
// pool is empty), rank them for the current system, claim the best reachable one,
// and run it (buy -> transit -> sell -> complete). On any mid-run failure it logs
// and returns nil so the worker idles and retries; the claimed row is left claimed
// (harmless — the spread regenerates on the next scan).
func Haul(ctx context.Context, deps HaulDeps) error {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	if deps.Market == nil {
		fmt.Fprintln(out, "haul: market collector not configured; skipping") //nolint:errcheck
		return nil
	}
	if deps.KB == nil {
		fmt.Fprintln(out, "haul: no knowledge base; skipping") //nolint:errcheck
		return nil
	}
	limit := deps.PoolLimit
	if limit <= 0 {
		limit = DefaultHaulPoolLimit
	}
	state := deps.Client.GetState()
	if state == nil || state.System.ID == "" {
		fmt.Fprintln(out, "haul: current system unknown; skipping") //nolint:errcheck
		return nil
	}
	current := state.System.ID

	opps, err := loadAvailable(ctx, deps.Market, limit)
	if err != nil {
		return err
	}
	if len(opps) == 0 {
		fmt.Fprintln(out, "haul: no opportunities available; idling") //nolint:errcheck
		return nil
	}

	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("haul: get systems: %w", err)
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("haul: get connections: %w", err)
	}
	nameToID := buildNameToID(systems)
	graph := navigation.JumpGraphFromConnections(conns)

	ranked := RankHaulOpportunities(opps, current, nameToID, graph)
	if len(ranked) == 0 {
		fmt.Fprintln(out, "haul: no reachable opportunities; idling") //nolint:errcheck
		return nil
	}

	opp, ok, err := claimBest(ctx, deps.Market, ranked, deps.AgentID)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "haul: all candidates already claimed; idling") //nolint:errcheck
		return nil
	}

	return runClaimedHaul(ctx, deps, out, opp, nameToID)
}

// runClaimedHaul executes a claimed opportunity end to end. Any error is logged and
// swallowed (returns nil) so the worker stays alive; the row is left claimed.
// Buy sizing uses the snapshot opp.BuyPrice as the per-unit ask (the server enforces
// the real price; an over-ask buy fails and leaves the row claimed). Live re-pricing
// is a deferred refinement.
func runClaimedHaul(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, nameToID map[string]string) error {
	buySys := nameToID[opp.FromSystemName]
	sellSys := nameToID[opp.ToSystemName]
	if sellSys == "" {
		fmt.Fprintf(out, "haul: opp %d sell system %q unresolved; leaving claimed\n", opp.ID, opp.ToSystemName) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(out, "haul: opp %d %s: buy %.0f @%s -> sell @%s\n", opp.ID, opp.ItemID, opp.Quantity, opp.FromStationName, opp.ToStationName) //nolint:errcheck

	if err := haulAutopilot(ctx, deps, out, buySys, opp.FromStationID); err != nil {
		fmt.Fprintf(out, "haul: opp %d transit to buy failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}
	state := deps.Client.GetState()
	if state == nil {
		fmt.Fprintf(out, "haul: opp %d no state at buy station; leaving claimed\n", opp.ID) //nolint:errcheck
		return nil
	}
	cargoFree := state.Ship.CargoCapacity - state.Ship.CargoUsed
	qty := sizeBuy(opp, cargoFree, state.GetCredits(), opp.BuyPrice)
	if qty < 1 {
		fmt.Fprintf(out, "haul: opp %d unaffordable/no cargo (qty=%.0f); leaving claimed\n", opp.ID, qty) //nolint:errcheck
		return nil
	}
	if err := deps.Client.Buy(ctx, opp.ItemID, qty); err != nil {
		fmt.Fprintf(out, "haul: opp %d buy failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}

	if err := haulAutopilot(ctx, deps, out, sellSys, opp.ToStationID); err != nil {
		fmt.Fprintf(out, "haul: opp %d transit to sell failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}
	held := cargoQty(deps.Client.GetState(), opp.ItemID)
	if held <= 0 {
		fmt.Fprintf(out, "haul: opp %d nothing in cargo to sell; leaving claimed\n", opp.ID) //nolint:errcheck
		return nil
	}
	if err := deps.Client.Sell(ctx, opp.ItemID, held); err != nil {
		fmt.Fprintf(out, "haul: opp %d sell failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}

	if _, err := deps.Market.CompleteOpportunity(ctx, opp.ID, deps.AgentID); err != nil {
		fmt.Fprintf(out, "haul: opp %d complete failed: %v\n", opp.ID, err) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(out, "haul: opp %d complete (sold %.0f %s)\n", opp.ID, held, opp.ItemID) //nolint:errcheck
	return nil
}

// haulAutopilot routes to a station POI within a system, capturing each hop to the KB.
func haulAutopilot(ctx context.Context, deps HaulDeps, out io.Writer, system, poi string) error {
	return Autopilot(ctx, AutopilotDeps{
		Client: deps.Client,
		Out:    out,
		OnWaypoint: func(ctx context.Context) error {
			if deps.KB == nil {
				return nil
			}
			if err := KBUpdateSystem(ctx, deps.Client, deps.KB, ""); err != nil {
				return err
			}
			return KBUpdatePOI(ctx, deps.Client, deps.KB, "")
		},
	}, system, poi)
}

// cargoQty returns how many units of itemID are in the ship's cargo (0 if none).
func cargoQty(state *game.State, itemID string) float64 {
	if state == nil {
		return 0
	}
	for _, c := range state.Ship.Cargo {
		if c.ItemID == itemID {
			return c.Quantity
		}
	}
	return 0
}
```

- [ ] **Step 3b: Wire the dispatch in `pkg/worker/dispatch.go`**

Add the field to the `WorkerDispatch` struct (after `Out io.Writer`):

```go
	AgentID string // claim owner for opportunity-claiming roles (e.g. hauler)
```

Add `"haul": true,` to the `supported` map (alongside `"explore": true, "scan": true,`).

Add this case to `Run`'s switch (next to `case "explore":`):

```go
	case "haul":
		if d.Market == nil {
			fmt.Fprintln(d.Out, "haul: market collector not configured (use --market-db-path)") //nolint:errcheck
			return nil
		}
		return Haul(ctx, HaulDeps{Client: d.Client, KB: d.KB, Market: d.Market, Out: d.Out, AgentID: d.AgentID})
```

- [ ] **Step 3c: Set the agent id in `cmd/worker/main.go`**

Immediately after the `dispatch := worker.NewWorkerDispatch(client, kb, mc, os.Stdout)` line (~261):

```go
			dispatch.AgentID = *agentID
```

- [ ] **Step 4: Run tests + build**

Run: `go test ./pkg/worker/ -run TestHaul -v && go build ./...`
Expected: PASS; build clean. The `var _ OpportunityStore = (*market.Collector)(nil)` line fails to compile if any store signature drifts — that is the intended guard.

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/haul.go pkg/worker/dispatch.go pkg/worker/dispatch_test.go cmd/worker/main.go
git commit -m "feat(worker): Haul engine + dispatch wiring + worker agent id"
```

---

### Task 5: Config — `haul.smolt`, role, fleet roster, drift guard

**Files:**
- Create: `data/scripts/haul.smolt`
- Modify: `.gitignore` (~line 129)
- Modify: `data/overmind/roles.yaml`
- Modify: `data/overmind/fleet.yaml`
- Test: `pkg/worker/roles_test.go` (existing `TestSeededCommandsAreDispatchable` — run, don't rewrite)

**Interfaces:**
- Consumes: `supported["haul"]` and `supported["update_market"]` (both present after Task 4 / already present).

- [ ] **Step 1: Create the standing script**

Create `data/scripts/haul.smolt`:

```
haul
update_market
```

(`haul` runs the full claim→buy→sell cycle; `update_market` then captures the market at wherever the hauler docked — feeding the very data the scanner uses.)

- [ ] **Step 2: Allowlist the script in `.gitignore`**

Add after the `!data/scripts/mine_local.smolt` line:

```
!data/scripts/haul.smolt
```

- [ ] **Step 3: Add the role to `data/overmind/roles.yaml`**

Add under `roles:` (after the `miner: {}` entry):

```yaml
  hauler:
    idle: haul
```

- [ ] **Step 4: Add haulers to `data/overmind/fleet.yaml`**

Add after the miner block (adjust the count/credentials with the user before launch — each `hauler-N` needs a provisioned game account):

```yaml
  # Haulers — mobile arbitrage runners; station label unused (position emerges
  # from where the opportunities are).
  - { agent_id: hauler-1, role: hauler, station: "" }
  - { agent_id: hauler-2, role: hauler, station: "" }
  - { agent_id: hauler-3, role: hauler, station: "" }
```

- [ ] **Step 5: Verify the drift guard + full suite**

Run: `go test ./pkg/worker/ -run TestSeededCommandsAreDispatchable -v && go test ./... && go build ./...`
Expected: PASS — every command in `haul.smolt` (`haul`, `update_market`) is in `supported`; full suite green.

- [ ] **Step 6: Verify the script is tracked (not ignored)**

Run: `git check-ignore data/scripts/haul.smolt; echo "exit=$?"`
Expected: `exit=1` (NOT ignored — the allowlist negation worked).

- [ ] **Step 7: Commit**

```bash
git add data/scripts/haul.smolt .gitignore data/overmind/roles.yaml data/overmind/fleet.yaml
git commit -m "feat(overmind): hauler role config + haul.smolt + fleet roster"
```

---

## Final verification

- [ ] `go build ./...` — clean.
- [ ] `go test ./...` — green.
- [ ] `golangci-lint run pkg/worker/... cmd/worker/...` — no new findings.
- [ ] Update `MEMORY.md` / `project_overmind_fleet_manager.md`: hauler role done (Phase 5 of Market Intelligence); note Phase-5b recovery (sweeper + `failed` status) still owed by the market team.

## Notes / deferred (carried from the spec)

- **Live-validation checklist (first real run):** confirm `market.db` `station_id` == game dock POI id (blind `Autopilot`-to-station depends on it); confirm `Buy(qty)` fill semantics (the sell leg already re-reads held cargo to tolerate partial fills).
- **Buy sizing uses the snapshot `opp.BuyPrice`**, not a live re-quote — deliberate v1 simplification; the server enforces the real charge and an over-ask buy fails safely (row left claimed). Live re-pricing before buy is a future refinement.
- **Recovery (Phase 5b, market team):** orphaned-`claimed` sweeper + a `failed` status migration. The hauler intentionally leaves failed runs `claimed`; they regenerate as fresh `available` rows on the next scan.
- **Producer cadence:** scan-when-empty lives in `loadAvailable`. If many haulers cause redundant scans, move to a single scheduled `scan_arbitrage` owner later.
