# Shipping Carrier Sub-project C: Multi-Package Trips — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a freight worker hold a SET of shipping contracts, chain deliveries across multiple destinations with refill at every dock, gated fail-closed by a conservative chain-deadline bound and server capacity limits.

**Architecture:** The single held contract (`missionRunState.heldFreight`) becomes a held set; a new pure-math file (`freight_chain.go`) provides the nearest-first ordering and the round-trip-through-origin cumulative bound; `freightCandidate` gains headroom + chain-feasibility gates and marginal fuel pricing; a new `freightChainRun` loop (deliver due → re-check → refill-accept → nav nearest) replaces `freightRunTrip`. A `--freight-max-packages` cap (default 1) makes everything default to exact v1 behavior; fighter-4 canaries at 3.

**Tech Stack:** Go 1.24 (pkg/worker, pkg/overmind/supervisor, cmd/worker), no new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-22-shipping-carrier-subproject-c-design.md`

## Global Constraints

- `freight_max_packages` default is **1** and MUST reproduce v1 behavior exactly (one contract at a time) for every worker that does not set it.
- The chain bound is `cumulative_i = 2*(h_1+...+h_{i-1}) + h_i` over nearest-first order, feasible iff `DeadlineTick - nowTick >= cumulative_i * freightTicksPerHop * freightDeadlineSlack` (constants already exist in `pkg/worker/freight.go:26,31`; reuse them, do not redefine).
- Headroom = min of: `freightPackagesFit(cargoFree)`, `freight_max_packages` cap, server `active_contract_limit - active_contracts` (skip check when `active_contracts_unlimited`), and per-candidate `ReservedExposure <= remaining_aggregate_liability` (skip when `liability_unlimited` or `aggregate_liability_limit == 0`).
- Per-contract failure isolation: returning/failing ONE contract never touches the others. Global park (freightStepStuck) remains ONLY for `return_failed`.
- `freight_results` schema unchanged: one row per contract.
- Freight must never become a new way for the mission pass to fail (all failures degrade to skip / leave-in-flight / return).
- All new code passes `golangci-lint run` with no new findings; run `gofmt -w` on touched files (pkg/worker and pkg/ovdash have known pre-existing drift — fold fixes into files you touch, do not reformat untouched files).
- After changes: `go build ./...` and `go test ./...` (mocks in pkg/agent + pkg/skills break silently on interface changes; this plan adds NO GameClient methods, so none are expected).
- Commits: stage files explicitly (never `git add -A`; `data/*.json` churn is live runtime state), use `--no-verify` (pre-commit race gate times out under fleet load — user-approved substitute gate is build+lint+targeted tests).
- Test discrimination proof (the missions vacuous-test trap): for every new test that exercises the mission/freight pass, verify it can fail — neuter the line under test (or flip the expected value) and observe red, then restore. A bare `&game.State{}` makes `Missions()` early-return before your code.

## File Structure

| File | Role |
|---|---|
| `pkg/worker/freight_chain.go` (create) | Pure chain math: ordering, cumulative bound, feasibility, marginal hops. No I/O. |
| `pkg/worker/freight_chain_test.go` (create) | Table tests for the math. |
| `pkg/worker/mission.go` (modify) | `missionRunState` held set + accessors; `MissionDeps.FreightMaxPackages`; dock-pass wiring. |
| `pkg/worker/freight.go` (modify) | Gate headroom + marginal pricing; set reconcile; `freightChainRun`; delete `freightRunTrip`/old reconcile pair. |
| `pkg/worker/freight_test.go` (modify) | New gate/reconcile/chain-run tests; migrate held-single tests to set accessors. |
| `pkg/worker/dispatch.go` (modify) | `WorkerDispatch.FreightMaxPackages` → `MissionDeps`. |
| `cmd/worker/main.go` (modify) | `--freight-max-packages` flag. |
| `pkg/overmind/supervisor/config.go` (modify) | `WorkerSpec.FreightMaxPackages` yaml key. |
| `pkg/overmind/supervisor/supervisor.go` (modify) | Forward `--freight-max-packages`. |
| `data/overmind/mission-learn-fleet.yaml` (modify) | fighter-4 `freight_max_packages: 3` (canary). |

---

### Task 1: Chain math (`freight_chain.go`)

**Files:**
- Create: `pkg/worker/freight_chain.go`
- Test: `pkg/worker/freight_chain_test.go`

**Interfaces:**
- Consumes: constants `freightTicksPerHop`, `freightDeadlineSlack` from `pkg/worker/freight.go:26,31`.
- Produces (later tasks rely on these exact signatures):
  - `type chainStop struct { ContractID, DestBaseID string; Hops int; DeadlineTick int64 }`
  - `func chainOrder(stops []chainStop) []chainStop`
  - `func chainCumulative(ordered []chainStop) []int`
  - `func chainFeasible(stops []chainStop, nowTick int64) (bool, string)`
  - `func chainTotalBound(stops []chainStop) int`
  - `func chainMarginalHops(held []chainStop, cand chainStop) int`

- [ ] **Step 1: Write the failing tests**

```go
package worker

import "testing"

func TestChainOrderNearestFirstDeadlineTiebreak(t *testing.T) {
	got := chainOrder([]chainStop{
		{ContractID: "c", Hops: 5, DeadlineTick: 100},
		{ContractID: "a", Hops: 2, DeadlineTick: 900},
		{ContractID: "b", Hops: 5, DeadlineTick: 50},
	})
	want := []string{"a", "b", "c"} // 2 first; ties on 5 by tighter deadline
	for i, s := range got {
		if s.ContractID != want[i] {
			t.Fatalf("order[%d] = %s, want %s", i, s.ContractID, want[i])
		}
	}
}

func TestChainCumulativeRoundTripBound(t *testing.T) {
	cum := chainCumulative([]chainStop{{Hops: 2}, {Hops: 5}, {Hops: 6}})
	// cum_1 = 2; cum_2 = 2*2+5 = 9; cum_3 = 2*(2+5)+6 = 20
	want := []int{2, 9, 20}
	for i := range want {
		if cum[i] != want[i] {
			t.Fatalf("cum[%d] = %d, want %d", i, cum[i], want[i])
		}
	}
}

func TestChainFeasibleFailsOnLaterStop(t *testing.T) {
	// Stop b sits at cumulative 9 hops -> needs 9*19*1.5 = 256.5 ticks.
	stops := []chainStop{
		{ContractID: "a", Hops: 2, DeadlineTick: 1000},
		{ContractID: "b", Hops: 5, DeadlineTick: 250}, // 250 < 256.5 -> infeasible
	}
	if ok, _ := chainFeasible(stops, 0); ok {
		t.Fatal("chain with a blown later-stop deadline reported feasible")
	}
	stops[1].DeadlineTick = 300 // 300 >= 256.5 -> feasible
	if ok, reason := chainFeasible(stops, 0); !ok {
		t.Fatalf("healthy chain reported infeasible: %s", reason)
	}
}

func TestChainFeasibleSkipsZeroDeadline(t *testing.T) {
	// Pre-accept candidates carry DeadlineTick 0 (server sets it at accept);
	// their own check is deferred to freightAccept.
	stops := []chainStop{{ContractID: "cand", Hops: 50, DeadlineTick: 0}}
	if ok, _ := chainFeasible(stops, 0); !ok {
		t.Fatal("zero-deadline stop must not fail feasibility")
	}
}

func TestChainMarginalHops(t *testing.T) {
	held := []chainStop{{ContractID: "h", Hops: 5, DeadlineTick: 1000}}
	// with cand h=2: order [2,5], total = 2*2+5 = 9; without = 5; marginal 4.
	if got := chainMarginalHops(held, chainStop{ContractID: "c", Hops: 2}); got != 4 {
		t.Fatalf("marginal = %d, want 4", got)
	}
	// empty held degenerates to the candidate's own hops (v1 pricing).
	if got := chainMarginalHops(nil, chainStop{ContractID: "c", Hops: 7}); got != 7 {
		t.Fatalf("marginal on empty held = %d, want 7", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run TestChain -v`
Expected: FAIL — `undefined: chainOrder` etc.

- [ ] **Step 3: Implement `pkg/worker/freight_chain.go`**

```go
package worker

// freight_chain.go — pure chain math for multi-package freight trips
// (sub-project C). No I/O and no injected deps: everything here is
// unit-testable with literals.

import (
	"fmt"
	"sort"
)

// chainStop is one destination in a (prospective) delivery chain, priced
// from the CURRENT dock. DeadlineTick 0 means "not known yet" — a board
// candidate whose deadline the server only sets at accept time.
type chainStop struct {
	ContractID   string
	DestBaseID   string
	Hops         int
	DeadlineTick int64
}

// chainOrder returns the visiting order the feasibility bound assumes:
// nearest-first by hops, then earliest deadline, then contract id — fully
// deterministic so repeated passes and tests agree.
func chainOrder(stops []chainStop) []chainStop {
	out := make([]chainStop, len(stops))
	copy(out, stops)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hops != out[j].Hops {
			return out[i].Hops < out[j].Hops
		}
		if out[i].DeadlineTick != out[j].DeadlineTick {
			return out[i].DeadlineTick < out[j].DeadlineTick
		}
		return out[i].ContractID < out[j].ContractID
	})
	return out
}

// chainCumulative returns worst-case cumulative hops to each stop of an
// ORDERED chain. The router prices destinations only from the current dock,
// so a leg between successive stops is bounded by the round trip through
// here: hops(d_i, d_{i+1}) <= h_i + h_{i+1}. Cumulative to stop i is then
// 2*(h_1+...+h_{i-1}) + h_i. Sound (never under-estimates) — accepts fail
// closed, and every later dock re-prices with fresh h values, so the bound
// only tightens as the chain progresses.
func chainCumulative(ordered []chainStop) []int {
	cum := make([]int, len(ordered))
	prefix := 0
	for i, s := range ordered {
		cum[i] = 2*prefix + s.Hops
		prefix += s.Hops
	}
	return cum
}

// chainFeasible reports whether every stop with a known deadline clears the
// worst-case bound at its chain position. Stops with DeadlineTick <= 0 are
// skipped: freightAccept re-runs this with the server-assigned deadline the
// moment it exists.
func chainFeasible(stops []chainStop, nowTick int64) (bool, string) {
	ordered := chainOrder(stops)
	cum := chainCumulative(ordered)
	for i, s := range ordered {
		if s.DeadlineTick <= 0 {
			continue
		}
		needed := float64(cum[i]) * freightTicksPerHop * freightDeadlineSlack
		if float64(s.DeadlineTick-nowTick) < needed {
			return false, fmt.Sprintf("chain bound: %s at position %d needs %.0f ticks, has %d",
				s.ContractID, i+1, needed, s.DeadlineTick-nowTick)
		}
	}
	return true, ""
}

// chainTotalBound is the worst-case total hops to clear the whole set.
func chainTotalBound(stops []chainStop) int {
	cum := chainCumulative(chainOrder(stops))
	if len(cum) == 0 {
		return 0
	}
	return cum[len(cum)-1]
}

// chainMarginalHops prices a candidate by the hops it ADDS to the chain —
// the fuel a bundled contract actually costs, replacing v1's flat
// origin->destination pricing. An empty held set degenerates to cand.Hops,
// which is exactly the v1 number.
func chainMarginalHops(held []chainStop, cand chainStop) int {
	with := make([]chainStop, 0, len(held)+1)
	with = append(with, held...)
	with = append(with, cand)
	return chainTotalBound(with) - chainTotalBound(held)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestChain -v`
Expected: PASS (5 tests)

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run pkg/worker/... && gofmt -l pkg/worker/freight_chain.go pkg/worker/freight_chain_test.go
git add pkg/worker/freight_chain.go pkg/worker/freight_chain_test.go
git commit --no-verify -m "feat(freight): chain math for multi-package trips (order, bound, feasibility, marginal hops)"
```

---

### Task 2: Held-freight set state

**Files:**
- Modify: `pkg/worker/mission.go:78-112` (missionRunState heldFreight field + the three accessors)
- Modify: `pkg/worker/freight.go` call sites: `:344` (`setHeldFreight`), `:407` (`setHeldFreight`), `:411` (`clearHeldFreight`), `:531` (`setHeldFreight`), `:537,545,552` (`clearHeldFreight`), `:671` (`clearHeldFreight`), `:454` (`heldFreightContract`)
- Test: `pkg/worker/mission_test.go` (append), plus fix any freight_test.go compile breaks

**Interfaces:**
- Produces (later tasks rely on these exact signatures on `*missionRunState`, all nil-receiver-safe like the existing accessors):
  - `func (s *missionRunState) addHeldFreight(c *serverapi.ShipmentContract)`
  - `func (s *missionRunState) removeHeldFreight(id string)`
  - `func (s *missionRunState) heldFreightAll() []*serverapi.ShipmentContract` (ID-sorted)
  - `func (s *missionRunState) heldFreightCount() int`
- Transitional (deleted in Task 4): keep `heldFreightContract()` returning the first ID-sorted entry or nil, so `freightReconcile` still compiles unchanged.

- [ ] **Step 1: Write the failing test** (append to `pkg/worker/mission_test.go`)

```go
func TestHeldFreightSetAccessors(t *testing.T) {
	var nilState *missionRunState
	nilState.addHeldFreight(&serverapi.ShipmentContract{ID: "x"}) // must not panic
	nilState.removeHeldFreight("x")
	if nilState.heldFreightCount() != 0 || nilState.heldFreightAll() != nil {
		t.Fatal("nil receiver must read as empty")
	}

	s := &missionRunState{}
	s.addHeldFreight(&serverapi.ShipmentContract{ID: "b"})
	s.addHeldFreight(&serverapi.ShipmentContract{ID: "a"})
	s.addHeldFreight(&serverapi.ShipmentContract{ID: "b"}) // upsert, not dup
	if s.heldFreightCount() != 2 {
		t.Fatalf("count = %d, want 2", s.heldFreightCount())
	}
	all := s.heldFreightAll()
	if len(all) != 2 || all[0].ID != "a" || all[1].ID != "b" {
		t.Fatalf("heldFreightAll not ID-sorted: %v", all)
	}
	s.removeHeldFreight("a")
	if s.heldFreightCount() != 1 || s.heldFreightAll()[0].ID != "b" {
		t.Fatal("remove did not drop exactly one entry")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run TestHeldFreightSetAccessors -v`
Expected: FAIL — `undefined: (*missionRunState).addHeldFreight` (compile error)

- [ ] **Step 3: Replace the field and accessors in `pkg/worker/mission.go`**

Replace the `heldFreight` field (line 84) and the three accessor funcs (lines 87-112) with:

```go
	// heldFreight is the set of in-flight shipping contracts accepted this
	// session, keyed by contract ID. The live canary (2026-07-20) proved the
	// board read NEVER returns our own in_transit contracts, so this
	// in-memory set is the PRIMARY reconcile source; the profile count in
	// freightReconcileSet survives only as the post-restart mismatch detector
	// (and will usually be unrecoverable until captains_log-style server-side
	// resume exists). Sub-project C: was a single *ShipmentContract.
	heldFreight map[string]*serverapi.ShipmentContract
}

// addHeldFreight remembers (or refreshes) a contract we are carrying. No-op
// on a nil receiver (State is optional; tests that don't care omit it).
func (s *missionRunState) addHeldFreight(c *serverapi.ShipmentContract) {
	if s == nil || c == nil {
		return
	}
	if s.heldFreight == nil {
		s.heldFreight = make(map[string]*serverapi.ShipmentContract)
	}
	s.heldFreight[c.ID] = c
}

// removeHeldFreight forgets one contract after its terminal outcome. No-op
// on a nil receiver.
func (s *missionRunState) removeHeldFreight(id string) {
	if s == nil {
		return
	}
	delete(s.heldFreight, id)
}

// heldFreightAll returns the held contracts sorted by ID (deterministic
// iteration for feasibility math, logs and tests). Nil on a nil receiver.
func (s *missionRunState) heldFreightAll() []*serverapi.ShipmentContract {
	if s == nil || len(s.heldFreight) == 0 {
		return nil
	}
	ids := make([]string, 0, len(s.heldFreight))
	for id := range s.heldFreight {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*serverapi.ShipmentContract, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.heldFreight[id])
	}
	return out
}

// heldFreightCount is len(held); 0 on a nil receiver.
func (s *missionRunState) heldFreightCount() int {
	if s == nil {
		return 0
	}
	return len(s.heldFreight)
}

// heldFreightContract returns the single held contract when exactly one is
// held, else the first by ID.
//
// TRANSITIONAL (sub-project C Task 2): exists only so freightReconcile
// compiles until Task 4 replaces it with freightReconcileSet. Do not add
// callers.
func (s *missionRunState) heldFreightContract() *serverapi.ShipmentContract {
	all := s.heldFreightAll()
	if len(all) == 0 {
		return nil
	}
	return all[0]
}
```

Add `"sort"` to mission.go's imports if absent.

- [ ] **Step 4: Migrate the freight.go call sites** (mechanical, same file positions as listed above)

- `freight.go:344` (freightAccept): `deps.State.setHeldFreight(&c)` → `deps.State.addHeldFreight(&c)`
- `freight.go:407` (freightReturn, failed-return branch): `deps.State.setHeldFreight(&c)` → `deps.State.addHeldFreight(&c)`
- `freight.go:411` (freightReturn, clean): `deps.State.clearHeldFreight()` → `deps.State.removeHeldFreight(c.ID)`
- `freight.go:531` (freightReconcileHeld, in_transit refresh): `setHeldFreight(&c)` → `addHeldFreight(&c)`
- `freight.go:537,545,552` (terminal statuses): `clearHeldFreight()` → `removeHeldFreight(c.ID)`
- `freight.go:671` (freightRunTrip, delivered): `deps.State.clearHeldFreight()` → `deps.State.removeHeldFreight(c.ID)`

`freight.go:454` (`heldFreightContract()` in freightReconcile) stays — the transitional shim covers it.

- [ ] **Step 5: Build, full test, verify**

Run: `go build ./... && go test ./pkg/worker/ -v -run 'TestHeldFreight|TestFreight'`
Expected: PASS — the existing freight tests exercise the migrated call sites through accept/return/reconcile paths. If any existing test references `setHeldFreight`/`clearHeldFreight` directly, migrate the reference the same way (add→addHeldFreight, clear→removeHeldFreight(id)).

- [ ] **Step 6: Lint and commit**

```bash
golangci-lint run pkg/worker/... 
git add pkg/worker/mission.go pkg/worker/freight.go pkg/worker/mission_test.go pkg/worker/freight_test.go
git commit --no-verify -m "feat(freight): held-freight set state (single contract -> ID-keyed set)"
```

---

### Task 3: Multi-aware gate — headroom, chain feasibility, marginal pricing

**Files:**
- Modify: `pkg/worker/mission.go:160-210` (MissionDeps — add FreightMaxPackages next to EnableFreight)
- Modify: `pkg/worker/freight.go:128-234` (freightInputs + freightCandidate), `freight.go:66-98` (buildFreightCand), `freight.go:301-346` (freightAccept chain-aware deadline)
- Test: `pkg/worker/freight_test.go`

**Interfaces:**
- Consumes: Task 1 `chainStop`/`chainFeasible`/`chainMarginalHops`; Task 2 set accessors.
- Produces:
  - `MissionDeps.FreightMaxPackages int` (0 and 1 both mean 1; normalize via `freightEffectiveMax`)
  - `func freightEffectiveMax(n int) int` — `max(1, n)`
  - `freightInputs` gains `Held []chainStop` and `NowTick int64`
  - `buildFreightCand(l serverapi.ShippingListing, hops int, held []chainStop, nowTick int64, fuelCostFor func(int) float64) (freightCand, string)` — REPLACES the old 3-arg signature; prices on `chainMarginalHops(held, stop)` and rejects candidates whose insertion breaks a HELD deadline
  - `freightAccept(ctx, deps, cand *freightCand, held []chainStop, out io.Writer) (*serverapi.ShipmentContract, freightStep)` — REPLACES old signature; post-accept deadline check is `chainFeasible(held+acceptedStop, nowTick)` instead of the solo `freightDeadlineOK`

- [ ] **Step 1: Write the failing tests** (append to freight_test.go; follow the scripted-client style of `TestFreightCandidateSkipsWhenContractAlreadyHeld` at freight_test.go:233 for gate tests — same mock client, same profile/board JSON fixtures)

```go
func TestFreightEffectiveMax(t *testing.T) {
	for in, want := range map[int]int{-1: 1, 0: 1, 1: 1, 3: 3, 7: 7} {
		if got := freightEffectiveMax(in); got != want {
			t.Fatalf("freightEffectiveMax(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestBuildFreightCandMarginalPricing(t *testing.T) {
	// Held stop at 5 hops; candidate at 2 hops adds marginal 4 hops
	// (order [2,5]: total 2*2+5=9, minus 5 held-only). Fuel 100/hop.
	held := []chainStop{{ContractID: "h", DestBaseID: "hb", Hops: 5, DeadlineTick: 100000}}
	l := serverapi.ShippingListing{Eligible: true, Contract: serverapi.ShipmentContract{
		ID: "c", DestinationBaseID: "cb", BaseReward: 1000,
	}}
	cand, reason := buildFreightCand(l, 2, held, 0, func(j int) float64 { return float64(j) * 100 })
	if reason != "" {
		t.Fatalf("unexpected reject: %s", reason)
	}
	if cand.FuelCost != 400 { // marginal 4 hops * 100, NOT 2*100
		t.Fatalf("FuelCost = %.0f, want 400 (marginal-hop pricing)", cand.FuelCost)
	}
	if cand.Net != 600 {
		t.Fatalf("Net = %.0f, want 600", cand.Net)
	}
}

func TestBuildFreightCandRejectsWhenHeldDeadlineBreaks(t *testing.T) {
	// Held contract barely feasible alone (5 hops -> needs 142.5 ticks,
	// has 150). Inserting a nearer candidate (2 hops) pushes it to
	// cumulative 9 -> needs 256.5, breaking it. Must reject the CANDIDATE.
	held := []chainStop{{ContractID: "h", DestBaseID: "hb", Hops: 5, DeadlineTick: 150}}
	l := serverapi.ShippingListing{Eligible: true, Contract: serverapi.ShipmentContract{
		ID: "c", DestinationBaseID: "cb", BaseReward: 100000,
	}}
	_, reason := buildFreightCand(l, 2, held, 0, nil)
	if reason == "" {
		t.Fatal("candidate that breaks a held deadline must be rejected")
	}
}

func TestFreightCandidateSkipsAtMaxPackages(t *testing.T) {
	// Gate must refuse to list the board at all when held >= cap.
	// Build deps/in as in TestFreightCandidateSkipsWhenContractAlreadyHeld,
	// with profile JSON reporting active_contracts=1 to match Held, and:
	in := freightInputs{
		CargoFree: 500,
		Held:      []chainStop{{ContractID: "h", DestBaseID: "hb", Hops: 3, DeadlineTick: 100000}},
	}
	deps := freightGateDeps(t, `{"profile":{"active_contracts":1},"capacity":{"active_contracts":1,"active_contracts_unlimited":true,"liability_unlimited":true}}`, "")
	deps.FreightMaxPackages = 1
	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand != nil || !strings.Contains(skip, "at max packages") {
		t.Fatalf("cand=%v skip=%q; want nil + at-max-packages skip", cand, skip)
	}
}

func TestFreightCandidateSkipsWhenProfileExceedsHeld(t *testing.T) {
	// Server says 2 active, we hold 1 -> reconcile gap; fail closed exactly
	// like v1's ActiveContracts>0-with-empty-memory skip.
	in := freightInputs{
		CargoFree: 500,
		Held:      []chainStop{{ContractID: "h", DestBaseID: "hb", Hops: 3, DeadlineTick: 100000}},
	}
	deps := freightGateDeps(t, `{"profile":{"active_contracts":2},"capacity":{"active_contracts":2,"active_contracts_unlimited":true,"liability_unlimited":true}}`, "")
	deps.FreightMaxPackages = 4
	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand != nil || !strings.Contains(skip, "unaccounted") {
		t.Fatalf("cand=%v skip=%q; want nil + unaccounted skip", cand, skip)
	}
}

func TestFreightCandidateSkipsCandidateOverLiability(t *testing.T) {
	// Candidate's ReservedExposure exceeds remaining aggregate liability ->
	// that candidate is skipped (not the whole pass).
	board := `{"shipments":[{"eligible":true,"contract":{"id":"big","destination_base_id":"d1","base_reward":5000,"reserved_exposure":9000}},
	                        {"eligible":true,"contract":{"id":"ok","destination_base_id":"d2","base_reward":4000,"reserved_exposure":100}}]}`
	deps := freightGateDeps(t, `{"profile":{"active_contracts":0},"capacity":{"active_contracts":0,"active_contracts_unlimited":true,"aggregate_liability_limit":10000,"remaining_aggregate_liability":500}}`, board)
	deps.FreightMaxPackages = 4
	in := freightInputs{CargoFree: 500, HopsTo: func(string) (int, bool) { return 1, true }}
	cand, _ := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand == nil || cand.Contract.ID != "ok" {
		t.Fatalf("cand = %+v, want contract ok (big skipped on liability)", cand)
	}
}
```

Note: `freightGateDeps(t, profileJSON, boardJSON)` is a small test helper to add in this task, extracting the repeated mock construction already present inline in `TestFreightCandidateSkipsWhenContractAlreadyHeld` / `TestFreightCandidatePicksBestEligible` (client mock returning the given raw JSON for `shipping_profile` / `shipping_list`, docked state, cargo). Build it by factoring those tests' setup; keep their bodies working through it or leave them untouched — do not duplicate a third copy.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestFreightEffectiveMax|TestBuildFreightCand|TestFreightCandidateSkipsAtMax|TestFreightCandidateSkipsWhenProfileExceeds|TestFreightCandidateSkipsCandidateOverLiability' -v`
Expected: FAIL (compile: wrong arity on buildFreightCand; undefined freightEffectiveMax / freightGateDeps)

- [ ] **Step 3: Implement**

3a. `MissionDeps` (mission.go, directly under the `EnableFreight bool` field at :202):

```go
	// FreightMaxPackages caps concurrent freight contracts (sub-project C
	// multi-package trips). 0 and 1 both mean the v1 single-contract
	// behavior; the cap layers UNDER the server/cargo headroom gates and is
	// never a target. Canary: fighter-4 at 3.
	FreightMaxPackages int
```

3b. `freightEffectiveMax` (freight.go, next to freightPackagesFit):

```go
// freightEffectiveMax normalizes the configured package cap: anything below
// 1 (unset, or nonsense negatives) is the v1 single-contract behavior.
func freightEffectiveMax(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
```

3c. `freightInputs` gains two fields:

```go
	// Held is the current chain (one stop per held contract), priced from
	// this dock. Empty on a v1-equivalent pass.
	Held []chainStop
	// NowTick anchors chain-deadline feasibility (missionTick at pass start).
	NowTick int64
```

3d. `buildFreightCand` — new signature and body:

```go
// buildFreightCand derives economics for one listing given the chain we
// already hold. A non-empty reason means skip, and is logged verbatim so a
// canary pass shows why the board emptied out.
func buildFreightCand(l serverapi.ShippingListing, hops int, held []chainStop, nowTick int64, fuelCostFor func(jumps int) float64) (freightCand, string) {
	if !l.Eligible {
		reason := l.Reason
		if reason == "" {
			reason = "server marked ineligible"
		}
		return freightCand{}, reason
	}
	if l.Contract.DestinationBaseID == "" {
		return freightCand{}, "no destination_base_id"
	}
	stop := chainStop{ContractID: l.Contract.ID, DestBaseID: l.Contract.DestinationBaseID, Hops: hops}
	// Inserting this stop must not break any HELD contract's deadline under
	// the chain bound (the candidate's own deadline does not exist until
	// accept; freightAccept checks it then, chain-aware).
	withCand := make([]chainStop, 0, len(held)+1)
	withCand = append(withCand, held...)
	withCand = append(withCand, stop)
	if ok, reason := chainFeasible(withCand, nowTick); !ok {
		return freightCand{}, "would break held deadline: " + reason
	}
	reward := float64(l.Contract.BaseReward)
	fuel := 0.0
	if fuelCostFor != nil {
		fuel = fuelCostFor(chainMarginalHops(held, stop))
	}
	// max_speed_bonus is deliberately excluded: it is upside, never a reason
	// to take a contract whose base reward does not stand on its own.
	net := reward - fuel
	if net < freightMinNet {
		return freightCand{}, fmt.Sprintf("net %.0f below floor %.0f (reward %.0f, fuel %.0f)", net, freightMinNet, reward, fuel)
	}
	return freightCand{
		Contract:   l.Contract,
		DestBaseID: l.Contract.DestinationBaseID,
		Hops:       hops,
		Reward:     reward,
		FuelCost:   fuel,
		Net:        net,
	}, ""
}
```

3e. `freightCandidate` — replace the concurrency guard (freight.go:176-187) and thread the new fields. The debt guard and profile read above it stay byte-identical. Replace from the `// Concurrency guard…` comment through the `if prof.Profile.ActiveContracts > 0 { … }` block with:

```go
	// Headroom gates (sub-project C). The v1 "one contract at a time" rule
	// is the maxPk==1 special case of these.
	maxPk := freightEffectiveMax(deps.FreightMaxPackages)
	if len(in.Held) >= maxPk {
		reason := fmt.Sprintf("at max packages (%d/%d)", len(in.Held), maxPk)
		fmt.Fprintf(out, "freight: skipping, %s\n", reason) //nolint:errcheck
		return nil, reason
	}
	// Reconcile-gap guard: the server counting more actives than we remember
	// means a contract we cannot see (post-restart amnesia). Accepting more
	// while any contract is unaccounted for is how orphans breach — fail
	// closed exactly as v1 did with ActiveContracts>0 on empty memory.
	if prof.Profile.ActiveContracts > len(in.Held) {
		reason := fmt.Sprintf("%d active contract(s) unaccounted for (holding %d)", prof.Profile.ActiveContracts, len(in.Held))
		fmt.Fprintf(out, "freight: skipping, %s\n", reason) //nolint:errcheck
		return nil, reason
	}
	if !prof.Capacity.ActiveContractsUnlimited && prof.Capacity.ActiveContractLimit > 0 &&
		prof.Profile.ActiveContracts >= prof.Capacity.ActiveContractLimit {
		reason := fmt.Sprintf("server contract limit reached (%d/%d)", prof.Profile.ActiveContracts, prof.Capacity.ActiveContractLimit)
		fmt.Fprintf(out, "freight: skipping, %s\n", reason) //nolint:errcheck
		return nil, reason
	}
```

…and in the candidate loop (freight.go:206-226), add the per-candidate liability skip before `buildFreightCand` and pass the new args:

```go
	for _, l := range board.Shipments {
		if !prof.Capacity.LiabilityUnlimited && prof.Capacity.AggregateLiabilityLimit > 0 &&
			l.Contract.ReservedExposure > prof.Capacity.RemainingAggregateLiability {
			fmt.Fprintf(out, "freight: skip %s: exposure %d over remaining liability %d\n",
				l.Contract.ID, l.Contract.ReservedExposure, prof.Capacity.RemainingAggregateLiability) //nolint:errcheck
			continue
		}
		hops := l.Contract.RouteHops
		if in.HopsTo != nil {
			h, ok := in.HopsTo(l.Contract.DestinationBaseID)
			if !ok {
				fmt.Fprintf(out, "freight: skip %s: no route to %s\n", l.Contract.ID, l.Contract.DestinationBaseID) //nolint:errcheck
				continue
			}
			hops = h
		}
		c, reason := buildFreightCand(l, hops, in.Held, in.NowTick, in.FuelCostFor)
		if reason != "" {
			fmt.Fprintf(out, "freight: skip %s: %s\n", l.Contract.ID, reason) //nolint:errcheck
			continue
		}
		cands = append(cands, c)
	}
```

3f. `freightAccept` — signature gains `held []chainStop`; replace the solo deadline check (freight.go:327-339) with the chain-aware version:

```go
	hops := c.RouteHops
	if hops == 0 {
		hops = cand.Hops
	}
	// missionTick(deps), not c.AcceptedTick: shipping mutations are
	// tick-deferred, so by the time this reply is in hand the current tick is
	// already >= AcceptedTick; AcceptedTick would bias the gate optimistic.
	// Chain-aware: the accepted contract is checked at its position in the
	// held chain, not solo — its deadline exists only now.
	withNew := make([]chainStop, 0, len(held)+1)
	withNew = append(withNew, held...)
	withNew = append(withNew, chainStop{ContractID: c.ID, DestBaseID: c.DestinationBaseID, Hops: hops, DeadlineTick: c.DeadlineTick})
	if c.DeadlineTick <= 0 {
		// Fail closed on a missing deadline, as v1's freightDeadlineOK did.
		fmt.Fprintf(out, "freight: %s infeasible (contract carries no deadline_tick); returning\n", id) //nolint:errcheck
		return nil, freightReturn(ctx, deps, out, c, cand, "returned_infeasible", "contract carries no deadline_tick")
	}
	if ok, reason := chainFeasible(withNew, missionTick(deps)); !ok {
		fmt.Fprintf(out, "freight: %s infeasible (%s); returning\n", id, reason) //nolint:errcheck
		return nil, freightReturn(ctx, deps, out, c, cand, "returned_infeasible", reason)
	}
```

Update the two `freightAccept` callers to pass held stops: `missionTakeFreight` (freight.go:360-372) gains a `held []chainStop` parameter it forwards, and `missionFreightOrDry` (freight.go:383-392) likewise forwards (its callers updated in Task 6; for now pass `nil` from existing call sites in mission.go:479,585,699 — nil held is exactly v1 behavior, so this task leaves runtime behavior unchanged).

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/worker/ -run 'TestFreight|TestBuildFreight|TestChain' -v`
Expected: PASS, including all pre-existing freight tests (`TestFreightCandidateSkipsWhenContractAlreadyHeld` now passes through the profile-exceeds-held guard — if its fixture reports actives with empty Held, the new skip message differs: update that test's expected reason string to `unaccounted`). `TestFreightAcceptReturnsWhenDeadlineInfeasible` exercises the solo case through the chain check (held=nil) — cumulative of a single stop equals its hops, so thresholds are unchanged.

- [ ] **Step 5: Discrimination proof, lint, commit**

Neuter check: comment out the `chainFeasible(withCand…)` reject in buildFreightCand → `TestBuildFreightCandRejectsWhenHeldDeadlineBreaks` must go red; restore.

```bash
go build ./... && golangci-lint run pkg/worker/...
git add pkg/worker/freight.go pkg/worker/mission.go pkg/worker/freight_test.go
git commit --no-verify -m "feat(freight): multi-aware gate (headroom, chain feasibility, marginal pricing)"
```

---

### Task 4: Set reconcile (`freightReconcileSet`)

**Files:**
- Modify: `pkg/worker/freight.go:437-555` — replace `freightReconcile` + `freightReconcileHeld` with `freightReconcileSet` + per-contract `freightVerifyHeld`
- Modify: `pkg/worker/mission.go:87-112` — delete the transitional `heldFreightContract()`
- Test: `pkg/worker/freight_test.go`

**Interfaces:**
- Consumes: Task 2 set accessors.
- Produces: `func freightReconcileSet(ctx context.Context, deps MissionDeps, out io.Writer) []*serverapi.ShipmentContract` — verifies every remembered contract against the server, drops terminals (recording rows exactly as v1 did), returns the surviving in-transit set (ID-sorted). Detection of server-side actives we do NOT remember stays in the gate (Task 3's `unaccounted` skip); reconcile only logs it loudly here.
- Deletes: `freightReconcile`, `freightReconcileHeld`, `missionRunState.heldFreightContract`. Task 6 rewires mission.go:295; until then mission.go still calls `freightReconcile`, so THIS task keeps a one-line compatibility wrapper (deleted in Task 6):

```go
// freightReconcile is the transitional v1 entry point; Task 6 replaces the
// mission.go call site with freightReconcileSet + freightChainRun.
func freightReconcile(ctx context.Context, deps MissionDeps, out io.Writer) (*serverapi.ShipmentContract, bool) {
	held := freightReconcileSet(ctx, deps, out)
	if len(held) == 0 {
		return nil, false
	}
	return held[0], true
}
```

- [ ] **Step 1: Write the failing tests** (append to freight_test.go, reusing the scripted-client fixtures from `TestFreightReconcileFindsHeldContract` at :523)

```go
func TestFreightReconcileSetDropsTerminalKeepsTransit(t *testing.T) {
	// Two remembered contracts: "gone" now reports defaulted server-side
	// (recorded as breached, removed), "live" stays in_transit (refreshed).
	// Mock ShippingGet returns per-ID JSON, as the v1 reconcile tests do.
	deps, store := freightReconcileDeps(t, map[string]string{
		"gone": `{"contract":{"id":"gone","status":"defaulted","destination_base_id":"d1"}}`,
		"live": `{"contract":{"id":"live","status":"in_transit","destination_base_id":"d2","deadline_tick":99999}}`,
	})
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "gone"})
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "live"})
	held := freightReconcileSet(context.Background(), deps, io.Discard)
	if len(held) != 1 || held[0].ID != "live" {
		t.Fatalf("held = %v, want [live]", held)
	}
	if deps.State.heldFreightCount() != 1 {
		t.Fatal("terminal contract not removed from state")
	}
	if got := store.outcomes["gone"]; got != "breached" {
		t.Fatalf("defaulted contract recorded %q, want breached", got)
	}
}

func TestFreightReconcileSetFailOpenOnGetError(t *testing.T) {
	// A transient get failure must not orphan a healthy contract: memory
	// wins and the contract stays held (v1 fail-open rule, per contract).
	deps, _ := freightReconcileDeps(t, map[string]string{}) // gets error
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "x", DestinationBaseID: "d"})
	held := freightReconcileSet(context.Background(), deps, io.Discard)
	if len(held) != 1 || held[0].ID != "x" {
		t.Fatalf("held = %v, want [x] (fail-open)", held)
	}
}
```

`freightReconcileDeps(t, byID)` is a helper like `freightGateDeps`: factor the mock setup from `TestFreightReconcileFindsHeldContract` so ShippingGet returns `byID[contractID]` (error when absent), and expose the recording store's outcome map.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run TestFreightReconcileSet -v`
Expected: FAIL — `undefined: freightReconcileSet`

- [ ] **Step 3: Implement** — replace freight.go:437-555 with:

```go
// freightReconcileSet verifies every remembered in-flight contract against
// the server before the pass takes any new work. An orphaned package rides
// to a default in silence (proven live 2026-07-20), so this runs before
// every pass. Per contract it applies exactly the v1 reconcile rules:
// fail-open on read trouble (memory wins; worst case is one clean deliver
// error), refresh on in_transit, record-and-drop on terminal statuses —
// "defaulted" records outcome "breached" because nothing else will.
// Server-side actives we do NOT remember are unrecoverable from here (own
// contracts never list on the board); the gate's unaccounted-for skip keeps
// us from accepting on top of them, and the loud log line below is the
// operator's rescue cue.
func freightReconcileSet(ctx context.Context, deps MissionDeps, out io.Writer) []*serverapi.ShipmentContract {
	if out == nil {
		out = io.Discard
	}
	for _, held := range deps.State.heldFreightAll() {
		freightVerifyHeld(ctx, deps, held, out)
	}
	survivors := deps.State.heldFreightAll()
	// Loud mismatch detection, log-only: the gate refuses new accepts on a
	// mismatch, so this cannot orphan anything further.
	if err := deps.Client.ShippingProfile(ctx); err == nil {
		var prof serverapi.ShippingProfileResponse
		if raw := deps.Client.GetRawJSON("shipping_profile"); len(raw) > 0 && json.Unmarshal(raw, &prof) == nil {
			if prof.Profile.ActiveContracts > len(survivors) {
				fmt.Fprintf(out, "freight: profile reports %d active contract(s) but memory holds %d — UNRECOVERABLE without operator rescue (own contracts never list; no captains_log resume yet)\n",
					prof.Profile.ActiveContracts, len(survivors)) //nolint:errcheck
			}
		}
	}
	return survivors
}

// freightVerifyHeld re-reads one remembered contract via the synchronous
// `get` and updates held-set state per its status. See freightReconcileSet
// for the rules; this is the v1 freightReconcileHeld body, set-based.
func freightVerifyHeld(ctx context.Context, deps MissionDeps, held *serverapi.ShipmentContract, out io.Writer) {
	if err := deps.Client.ShippingGet(ctx, held.ID); err != nil {
		fmt.Fprintf(out, "freight: reconcile get %s: %v; resuming from memory\n", held.ID, err) //nolint:errcheck
		return
	}
	var resp serverapi.ShippingContractResponse
	if raw := deps.Client.GetRawJSON("shipping_get"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &resp); err != nil {
			fmt.Fprintf(out, "freight: reconcile decode get %s: %v; resuming from memory\n", held.ID, err) //nolint:errcheck
			return
		}
	}
	c := resp.Contract
	if c.ID == "" {
		fmt.Fprintf(out, "freight: reconcile get %s returned no contract; resuming from memory\n", held.ID) //nolint:errcheck
		return
	}
	switch c.Status {
	case "in_transit":
		deps.State.addHeldFreight(&c) // refresh: server deadline etc. authoritative
	case "defaulted":
		fmt.Fprintf(out, "freight: held contract %s DEFAULTED server-side (flat debt; operator settles via pay_debt, package is keepable/unpackable)\n", c.ID) //nolint:errcheck
		freightRecord(ctx, deps, out, c, nil, 0, "breached", "reconciled: server status defaulted")
		deps.State.removeHeldFreight(c.ID)
	case "delivered":
		fmt.Fprintf(out, "freight: held contract %s already delivered server-side; recording without payout\n", c.ID) //nolint:errcheck
		freightRecord(ctx, deps, out, c, nil, 0, "delivered", "reconciled: settlement reply unseen; payout unrecorded")
		deps.State.removeHeldFreight(c.ID)
	default:
		fmt.Fprintf(out, "freight: held contract %s no longer in transit (status %q); releasing\n", c.ID, c.Status) //nolint:errcheck
		deps.State.removeHeldFreight(c.ID)
	}
}
```

Add the transitional `freightReconcile` wrapper from the Interfaces block. Delete `heldFreightContract()` from mission.go (its only caller was old freightReconcile).

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/worker/ -v -run 'TestFreightReconcile'`
Expected: PASS — new set tests AND the pre-existing `TestFreightReconcile*` tests, migrated if they call the deleted two-return functions directly (route them through the wrapper or the set API; keep their scenarios intact — they encode live-proven bugs).

- [ ] **Step 5: Lint and commit**

```bash
go build ./... && golangci-lint run pkg/worker/...
git add pkg/worker/freight.go pkg/worker/mission.go pkg/worker/freight_test.go
git commit --no-verify -m "feat(freight): set-based reconcile (per-contract verify, loud mismatch log)"
```

---

### Task 5: `freightChainRun` — the dock-pass loop

**Files:**
- Modify: `pkg/worker/freight.go` — add `freightLoadPackage`, `freightDeliverDueHere`, `freightChainStops`, `freightChainRun`; `freightRunTrip` stays (deleted in Task 6)
- Test: `pkg/worker/freight_test.go`

**Interfaces:**
- Consumes: Tasks 1-4 surfaces; `freightSettleDock` (freight.go:563), `freightRecord`, `freightReturn`, `cargoCount`, `freightPackageItemID`, `missionTick`.
- Produces:
  - `func freightChainStops(ctx context.Context, deps MissionDeps, hopsTo func(string) (int, bool), out io.Writer) []chainStop` — held set → stops with fresh hops; a held contract with NO route prices at hops 0 with a log line (fail-open: unroutable ≠ infeasible; deliver attempt will resolve it)
  - `func freightLoadPackage(ctx context.Context, deps MissionDeps, c *serverapi.ShipmentContract, cand *freightCand, out io.Writer) freightStep` — withdraw-if-absent (the runTrip:611-620 block extracted verbatim, incl. the reconcile-resume conditional)
  - `func freightDeliverDueHere(ctx context.Context, deps MissionDeps, baseID string, out io.Writer) (delivered int, failed int)` — deliver+record+remove every held contract destined for baseID (the runTrip:639-671 sequence per contract)
  - `func freightChainRun(ctx context.Context, deps MissionDeps, nav func(ctx context.Context, baseID string) error, hopsTo func(string) (int, bool), fuelCostFor func(int) float64, out io.Writer) (freightStep, error)` — the loop; `freightStepProceed` = pass complete or contracts intentionally left in flight; `freightStepStuck` = park (return_failed happened)

- [ ] **Step 1: Write the failing test** (append to freight_test.go; the scripted client mock from `TestFreightRunTripDeliversAndRecordsPayout` at :458 already scripts nav-less deliver flows — extend its fixture map for two contracts)

```go
func TestFreightChainRunDeliversNearestFirstAndAll(t *testing.T) {
	// Held: far (5 hops to baseB), near (2 hops to baseA). The chain must
	// nav to baseA first, deliver near, then baseB, deliver far. Both
	// recorded delivered; held set empty at exit; step Proceed.
	deps, store := freightChainDeps(t /* scripted deliver acks for near+far */)
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "near", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "far", DestinationBaseID: "baseB", DeadlineTick: 99999, Status: "in_transit"})
	var navved []string
	nav := func(ctx context.Context, baseID string) error { navved = append(navved, baseID); return nil }
	hops := func(base string) (int, bool) { return map[string]int{"baseA": 2, "baseB": 5}[base], true }
	step, err := freightChainRun(context.Background(), deps, nav, hops, io.Discard)
	if err != nil || step != freightStepProceed {
		t.Fatalf("step=%v err=%v", step, err)
	}
	if len(navved) != 2 || navved[0] != "baseA" || navved[1] != "baseB" {
		t.Fatalf("nav order = %v, want [baseA baseB]", navved)
	}
	if deps.State.heldFreightCount() != 0 {
		t.Fatal("held set not empty after full chain")
	}
	if store.outcomes["near"] != "delivered" || store.outcomes["far"] != "delivered" {
		t.Fatalf("outcomes = %v", store.outcomes)
	}
}

func TestFreightChainRunNavFailureLeavesInFlight(t *testing.T) {
	deps, _ := freightChainDeps(t)
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "x", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})
	nav := func(ctx context.Context, baseID string) error { return fmt.Errorf("no route") }
	hops := func(string) (int, bool) { return 2, true }
	step, err := freightChainRun(context.Background(), deps, nav, hops, io.Discard)
	if err != nil || step != freightStepProceed {
		t.Fatalf("step=%v err=%v; nav failure must leave contract in flight, not error", step, err)
	}
	if deps.State.heldFreightCount() != 1 {
		t.Fatal("contract must remain held for the next pass")
	}
}

func TestFreightChainRunReturnsDegradedContractOnly(t *testing.T) {
	// "dead" can no longer make its deadline from here (bound blown);
	// "fine" can. Chain returns dead (outcome returned_inflight), keeps and
	// delivers fine.
	deps, store := freightChainDeps(t)
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "dead", DestinationBaseID: "baseB", DeadlineTick: 10, Status: "in_transit"})
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "fine", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})
	nav := func(ctx context.Context, baseID string) error { return nil }
	hops := func(base string) (int, bool) { return map[string]int{"baseA": 1, "baseB": 5}[base], true }
	step, _ := freightChainRun(context.Background(), deps, nav, hops, io.Discard)
	if step != freightStepProceed {
		t.Fatalf("step = %v", step)
	}
	if store.outcomes["dead"] != "returned_inflight" || store.outcomes["fine"] != "delivered" {
		t.Fatalf("outcomes = %v", store.outcomes)
	}
}
```

`freightChainDeps(t)` scripts: docked state with the package items in cargo (so `freightLoadPackage` no-ops), `shipping_deliver` acks per contract id, working `shipping_return` — factored from `TestFreightRunTripDeliversAndRecordsPayout`'s setup. NOTE: the chain-run tests intentionally do NOT script a shipping board (`shipping_list` returns empty) so the refill step is a no-op in these tests; refill-through-the-gate is already covered by the Task 3 gate tests.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run TestFreightChainRun -v`
Expected: FAIL — `undefined: freightChainRun`

- [ ] **Step 3: Implement** (add to freight.go)

```go
// freightChainMaxLegs bounds the chain-run loop. Deliveries and returns
// strictly shrink the held set; refills are bounded by headroom — this
// guard exists only to make an unforeseen no-progress cycle exit loudly
// instead of spinning the pass forever.
const freightChainMaxLegs = 25

// freightChainStops prices the held set from the current dock. A held
// contract the router cannot reach right now prices at hops 0 with a log
// line — unroutable is a transient (mobile capitals, KB gaps), not proof of
// infeasibility, and pricing it 0 keeps its deadline check maximally
// conservative for the OTHER stops while its own deliver attempt resolves
// the truth.
func freightChainStops(ctx context.Context, deps MissionDeps, hopsTo func(string) (int, bool), out io.Writer) []chainStop {
	held := deps.State.heldFreightAll()
	stops := make([]chainStop, 0, len(held))
	for _, c := range held {
		hops := 0
		if hopsTo != nil {
			if h, ok := hopsTo(c.DestinationBaseID); ok {
				hops = h
			} else {
				fmt.Fprintf(out, "freight: no route to %s for held %s from here; pricing leg at 0\n", c.DestinationBaseID, c.ID) //nolint:errcheck
			}
		}
		stops = append(stops, chainStop{ContractID: c.ID, DestBaseID: c.DestinationBaseID, Hops: hops, DeadlineTick: c.DeadlineTick})
	}
	return stops
}

// freightLoadPackage pulls a contract's sealed package from station storage
// into the hold, if it is not already aboard (the reconcile-resume path
// re-enters with the package in CARGO — an unconditional withdraw would
// fail there and destroy a healthy contract). An absent, unloadable package
// means "cannot physically carry" and returns THAT contract.
func freightLoadPackage(ctx context.Context, deps MissionDeps, c *serverapi.ShipmentContract, cand *freightCand, out io.Writer) freightStep {
	item := freightPackageItemID(c.PackageID)
	if cargoCount(deps.Client.GetState(), item) >= 1 {
		return freightStepProceed
	}
	if err := deps.Client.WithdrawItems(ctx, item, 1); err != nil {
		fmt.Fprintf(out, "freight: withdraw %s: %v; returning contract\n", item, err) //nolint:errcheck
		return freightReturn(ctx, deps, out, *c, cand, "returned_infeasible", "package would not load: "+err.Error())
	}
	return freightStepProceed
}

// freightDeliverDueHere delivers every held contract whose destination is
// baseID. Per contract this is the v1 runTrip deliver+record sequence; a
// deliver failure leaves that contract in flight (failed++) and the loop
// continues with the rest.
func freightDeliverDueHere(ctx context.Context, deps MissionDeps, baseID string, out io.Writer) (delivered, failed int) {
	for _, c := range deps.State.heldFreightAll() {
		if c.DestinationBaseID != baseID {
			continue
		}
		if err := deps.Client.ShippingDeliver(ctx, c.ID); err != nil {
			fmt.Fprintf(out, "freight: deliver %s: %v\n", c.ID, err) //nolint:errcheck
			failed++
			continue
		}
		var settle serverapi.ShippingSettlementResponse
		settleReason := ""
		raw := deps.Client.GetRawJSON("shipping_deliver")
		switch {
		case len(raw) == 0:
			settleReason = "settlement decode failed: no reply"
			fmt.Fprintf(out, "freight: deliver %s returned no settlement reply\n", c.ID) //nolint:errcheck
		default:
			if err := json.Unmarshal(raw, &settle); err != nil {
				settleReason = "settlement decode failed: " + err.Error()
				fmt.Fprintf(out, "freight: decode deliver %s: %v\n", c.ID, err) //nolint:errcheck
			}
		}
		final := settle.Contract
		if final.ID == "" {
			final = *c
		}
		fmt.Fprintf(out, "freight: delivered %s, payout %d\n", c.ID, settle.CarrierPayout) //nolint:errcheck
		freightRecord(ctx, deps, out, final, nil, float64(settle.CarrierPayout), "delivered", settleReason)
		deps.State.removeHeldFreight(c.ID)
		delivered++
	}
	return delivered, failed
}

// freightChainRun is the sub-project C dock-pass loop: deliver what is due
// here, return what can no longer make its deadline, refill while headroom
// lasts, fly to the nearest held destination, repeat. The chain and the
// refill behavior both emerge from repeating this at every dock; a held set
// of one with FreightMaxPackages 1 walks exactly the v1 trip.
//
// freightStepProceed covers both "set empty, pass done" and "contracts
// intentionally left in flight" (nav/deliver trouble — next pass reconciles
// and resumes). freightStepStuck means a return FAILED (live undischarged
// contract): the caller must park this pass.
func freightChainRun(ctx context.Context, deps MissionDeps, nav func(ctx context.Context, baseID string) error, hopsTo func(string) (int, bool), fuelCostFor func(int) float64, out io.Writer) (freightStep, error) {
	if out == nil {
		out = io.Discard
	}
	attempted := map[string]bool{} // destinations that failed nav/deliver this pass
	for leg := 0; leg < freightChainMaxLegs; leg++ {
		stops := freightChainStops(ctx, deps, hopsTo, out)
		if len(stops) == 0 {
			return freightStepProceed, nil
		}
		maxPk := freightEffectiveMax(deps.FreightMaxPackages)
		fmt.Fprintf(out, "freight: holding %d/%d packages\n", len(stops), maxPk) //nolint:errcheck

		// Return contracts whose deadline no longer clears the chain bound
		// from here — one at a time, re-pricing after each removal, so a
		// single degraded contract never takes healthy ones down with it.
		for {
			ok, reason := chainFeasible(stops, missionTick(deps))
			if ok {
				break
			}
			victim := freightChainWorstStop(stops, missionTick(deps))
			c := freightHeldByID(deps, victim.ContractID)
			if c == nil {
				break // defensive: stop pricing a contract we no longer hold
			}
			fmt.Fprintf(out, "freight: in-flight buffer collapsed for %s (%s); returning\n", c.ID, reason) //nolint:errcheck
			if freightReturn(ctx, deps, out, *c, nil, "returned_inflight", reason) == freightStepStuck {
				return freightStepStuck, nil
			}
			stops = freightChainStops(ctx, deps, hopsTo, out)
			if len(stops) == 0 {
				return freightStepProceed, nil
			}
		}

		// Nav to the nearest held destination not already attempted this pass.
		target := ""
		for _, s := range chainOrder(stops) {
			if !attempted[s.DestBaseID] {
				target = s.DestBaseID
				break
			}
		}
		if target == "" {
			// Every remaining destination already failed once this pass;
			// leave the rest in flight for the next pass.
			return freightStepProceed, nil
		}
		if err := nav(ctx, target); err != nil {
			fmt.Fprintf(out, "freight: navigate to %s: %v; leaving in flight\n", target, err) //nolint:errcheck
			attempted[target] = true
			continue
		}
		if err := freightSettleDock(ctx, deps, out); err != nil {
			fmt.Fprintf(out, "freight: dock settle at %s: %v; leaving in flight\n", target, err) //nolint:errcheck
			attempted[target] = true
			continue
		}
		_, failed := freightDeliverDueHere(ctx, deps, target, out)
		if failed > 0 {
			// Headroom cannot be trusted mid-failure: no refill at this dock
			// (spec, Error handling). Try the next destination instead.
			attempted[target] = true
			continue
		}

		// Refill: accept while the gate still clears (headroom + chain
		// feasibility are all inside freightCandidate/freightAccept).
		freightChainRefill(ctx, deps, hopsTo, fuelCostFor, out)
	}
	fmt.Fprintf(out, "freight: chain-run leg guard (%d) hit; leaving remainder in flight\n", freightChainMaxLegs) //nolint:errcheck
	return freightStepProceed, nil
}

// freightChainWorstStop picks the stop to sacrifice when the chain bound
// fails: the one with the least deadline slack at its chain position.
func freightChainWorstStop(stops []chainStop, nowTick int64) chainStop {
	ordered := chainOrder(stops)
	cum := chainCumulative(ordered)
	worst, worstSlack := ordered[0], math.Inf(1)
	for i, s := range ordered {
		if s.DeadlineTick <= 0 {
			continue
		}
		slack := float64(s.DeadlineTick-nowTick) - float64(cum[i])*freightTicksPerHop*freightDeadlineSlack
		if slack < worstSlack {
			worst, worstSlack = s, slack
		}
	}
	return worst
}

// freightHeldByID fetches one held contract, nil when absent.
func freightHeldByID(deps MissionDeps, id string) *serverapi.ShipmentContract {
	for _, c := range deps.State.heldFreightAll() {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// freightChainRefill accepts additional contracts at the current dock while
// the gate clears. Each accept immediately loads its package so hold-space
// headroom self-updates for the next iteration; a load failure has already
// returned that contract inside freightLoadPackage.
func freightChainRefill(ctx context.Context, deps MissionDeps, hopsTo func(string) (int, bool), fuelCostFor func(int) float64, out io.Writer) {
	for {
		heldStops := freightChainStops(ctx, deps, hopsTo, out)
		in := freightInputs{
			CargoFree:   float64(cargoFreeSpace(deps.Client.GetState())),
			FuelCostFor: fuelCostFor,
			HopsTo:      hopsTo,
			Held:        heldStops,
			NowTick:     missionTick(deps),
		}
		cand, skip := freightCandidate(ctx, deps, in, out)
		if cand == nil {
			fmt.Fprintf(out, "freight: refill done (%s)\n", skip) //nolint:errcheck
			return
		}
		accepted, step := freightAccept(ctx, deps, cand, heldStops, out)
		if step != freightStepProceed {
			return // released or stuck; stuck is re-detected by the caller's next return
		}
		if freightLoadPackage(ctx, deps, accepted, cand, out) != freightStepProceed {
			continue // that contract was returned; headroom unchanged, try again
		}
	}
}
```

The `fuelCostFor` parameter threads the pass's memoized fuel model into refill pricing; Task 5's own tests may pass `nil` (fuel-free pricing), Task 6 supplies `ensureFuelModel()`.

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/worker/ -run 'TestFreightChainRun|TestFreight' -v`
Expected: PASS. `freightRunTrip` still exists and its tests still pass (deleted next task).

- [ ] **Step 5: Discrimination proof, lint, commit**

Neuter: swap `chainOrder(stops)` for `stops` in the nav-target pick → `TestFreightChainRunDeliversNearestFirstAndAll` must fail on nav order (map iteration is randomized — run with `-count=5`); restore.

```bash
go build ./... && golangci-lint run pkg/worker/...
git add pkg/worker/freight.go pkg/worker/freight_test.go
git commit --no-verify -m "feat(freight): freightChainRun dock-pass loop (deliver due, return degraded, refill, nav nearest)"
```

---

### Task 6: Mission-pass wiring

**Files:**
- Modify: `pkg/worker/mission.go:285-322` (early reconcile block), `:447-472` (candidate inputs), `:575-600` (ranking take-freight)
- Modify: `pkg/worker/freight.go` — `missionTakeFreight` enters the chain; DELETE `freightRunTrip`, the transitional `freightReconcile` wrapper, `freightDeadlineOK` + `freightInFlightCheck` if now uncalled
- Test: `pkg/worker/mission_test.go` / `freight_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: `missionTakeFreight(ctx, deps, cand *freightCand, held []chainStop, hopsTo func(string) (int, bool), fuelCostFor func(int) float64, out io.Writer) (freightStep, error)`; `missionFreightOrDry` gains and forwards the same three new params.

- [ ] **Step 1: Write the failing test**

```go
func TestMissionsResumesHeldChainBeforeBoardWork(t *testing.T) {
	// A held in_transit contract must own the pass: Missions() delivers it
	// via the chain run and never reads the mission board. Build on the
	// fixture from the existing reconcile-resume test for the v1 path
	// (mission_test.go's Missions-level freight tests): scripted client with
	// shipping_get -> in_transit, deliver ack, and a mission board call that
	// FAILS the test if invoked.
	deps := missionsFreightFixture(t /* get: in_transit to baseA; deliver ok; board: t.Fatal */)
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "x", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if deps.State.heldFreightCount() != 0 {
		t.Fatal("held contract not delivered by the pass")
	}
}
```

Discrimination proof is MANDATORY here (this is the exact shape that shipped three vacuous tests): neuter by removing the `addHeldFreight` line and confirm the test then fails on the board-read fatal (or adjust the fixture until it discriminates), then restore.

- [ ] **Step 2: Run to verify it fails** — `go test ./pkg/worker/ -run TestMissionsResumesHeldChain -v` → FAIL (fixture/compile).

- [ ] **Step 3: Rewire mission.go**

3a. Early reconcile block (replace :294-322):

```go
	if deps.EnableFreight {
		hopsTo := func(destBaseID string) (int, bool) { return missionHopsToBase(ctx, deps, destBaseID) }
		if held := freightReconcileSet(ctx, deps, out); len(held) > 0 {
			// Carrying packages legitimately owns the pass. The chain run
			// re-prices every deadline from here with fresh hops (v1's
			// RouteHops-is-total-hops conservatism note carries over: rows
			// with outcome returned_inflight can be this bound, not a real
			// deadline problem).
			publishActivity(deps.SetActivity, fmt.Sprintf("Freight chain: %d package(s)", len(held)))
			step, ferr := freightChainRun(ctx, deps, func(ctx context.Context, baseID string) error {
				return missionNavToBase(ctx, deps, baseID)
			}, hopsTo, ensureFuelModel(), out)
			if step == freightStepStuck {
				return nil // live undischarged contract: park the pass
			}
			return ferr
		}
	}
```

CAREFUL: `ensureFuelModel` is declared LOWER in the function (line ~410) — move the `ensureFuelModel` closure definition (mission.go:409-428) ABOVE this block (it is self-deferring, so hoisting costs nothing; its comment already explains the deferred-probe contract). The routing `graph` it captures must move with it (mission.go:380-384, the `deps.KB.GetConnections` + `JumpGraphFromConnections` lines) — hoist both, delete the originals, keep every downstream use compiling (`dist := navigation.BFSJumps(graph, current, targets)` at :489 still sees `graph`).

3b. Candidate inputs (at :453-472): add the chain fields:

```go
	var freightBest *freightCand
	var freightHeldStops []chainStop
	if deps.EnableFreight {
		hopsTo := func(destBaseID string) (int, bool) { return missionHopsToBase(ctx, deps, destBaseID) }
		freightHeldStops = freightChainStops(ctx, deps, hopsTo, out)
		in := freightInputs{
			CargoFree:   float64(cargoFreeSpace(deps.Client.GetState())),
			FuelCostFor: ensureFuelModel(),
			HopsTo:      hopsTo,
			Held:        freightHeldStops,
			NowTick:     missionTick(deps),
		}
		cand, skip := freightCandidate(ctx, deps, in, out)
		if cand == nil {
			fmt.Fprintf(out, "freight: no candidate (%s)\n", skip) //nolint:errcheck
		}
		freightBest = cand
	}
```

(The held set is empty whenever this point is reached — a non-empty set owned the pass above — but passing `freightHeldStops` keeps the code honest if that invariant ever changes.)

3c. Take-freight call sites (`:479`, `:585`, `:699`): pass the new arguments — `missionFreightOrDry(ctx, deps, freightBest, freightHeldStops, hopsTo, ensureFuelModel(), out)` and same for `missionTakeFreight` (hoist one shared `hopsTo` closure to the top of the pass instead of redeclaring; it is pure plumbing over ctx+deps).

3d. `missionTakeFreight` / `missionFreightOrDry` in freight.go — accept and thread the new params; after a successful accept, load the package and enter the chain:

```go
func missionTakeFreight(ctx context.Context, deps MissionDeps, cand *freightCand, held []chainStop, hopsTo func(string) (int, bool), fuelCostFor func(int) float64, out io.Writer) (freightStep, error) {
	if cand == nil {
		return freightStepReleased, nil
	}
	accepted, step := freightAccept(ctx, deps, cand, held, out)
	if step != freightStepProceed {
		return step, nil
	}
	if s := freightLoadPackage(ctx, deps, accepted, cand, out); s != freightStepProceed {
		return s, nil
	}
	publishActivity(deps.SetActivity, "Freight "+accepted.ID+" to "+accepted.DestinationBaseID)
	// The chain run's refill step bundles any further contracts on this
	// board that clear the gate, then walks the chain — with
	// FreightMaxPackages 1 this is exactly the v1 accept->trip sequence.
	return freightChainRun(ctx, deps, func(ctx context.Context, baseID string) error {
		return missionNavToBase(ctx, deps, baseID)
	}, hopsTo, fuelCostFor, out)
}
```

3e. Delete `freightRunTrip`, the transitional `freightReconcile` wrapper, and — if `grep -n 'freightDeadlineOK\|freightInFlightCheck' pkg/worker/*.go` shows no non-test callers — those two too, migrating their tests' scenarios onto `chainFeasible` (single-stop cases; keep the missing-deadline case against freightAccept's explicit check).

- [ ] **Step 4: Full test run** — `go build ./... && go test ./pkg/worker/ -v` → PASS; then `go test ./...` (mocks). Migrate any `freightRunTrip` tests to `freightChainRun` (single-contract scenarios must pass unchanged — that IS the v1-equivalence proof).

- [ ] **Step 5: Discrimination proof (Step 1's mandate), lint, commit**

```bash
golangci-lint run pkg/worker/...
git add pkg/worker/mission.go pkg/worker/freight.go pkg/worker/mission_test.go pkg/worker/freight_test.go
git commit --no-verify -m "feat(freight): wire multi-package chain into the mission pass; retire single-trip path"
```

---

### Task 7: Flag plumbing + canary config

**Files:**
- Modify: `cmd/worker/main.go:57` (beside enable-freight), `:318` (dispatch assignment)
- Modify: `pkg/worker/dispatch.go:38` (field), `:231` (MissionDeps assignment)
- Modify: `pkg/overmind/supervisor/config.go:25` (WorkerSpec field), `pkg/overmind/supervisor/supervisor.go:39-41` (args)
- Modify: `data/overmind/mission-learn-fleet.yaml` (fighter-4 line)
- Test: `pkg/overmind/supervisor` existing config/args tests (extend)

- [ ] **Step 1: Write the failing test** (extend the supervisor package's existing WorkerSpec parse/args tests — find them via `grep -rn "enable_freight\|enable-freight" pkg/overmind/supervisor/*_test.go` and add cases in the same tables)

```go
// parse case: yaml `{ agent_id: x, role: missionrunner, enable_freight: true, freight_max_packages: 3 }`
// -> spec.FreightMaxPackages == 3; omitted -> 0.
// args case: spec{EnableFreight: true, FreightMaxPackages: 3}
// -> args contain "--enable-freight" and "--freight-max-packages" "3";
// FreightMaxPackages 0 or 1 -> flag absent (worker default already 1).
```

- [ ] **Step 2: Run to verify failure** — `go test ./pkg/overmind/supervisor/ -v` → FAIL (unknown field).

- [ ] **Step 3: Implement**

config.go (after EnableFreight):

```go
	// FreightMaxPackages forwards --freight-max-packages, capping concurrent
	// freight contracts (sub-project C multi-package trips). 0/1 = the v1
	// single-contract behavior; canary fighter-4 runs 3. Layers UNDER the
	// server/cargo headroom gates.
	FreightMaxPackages int `yaml:"freight_max_packages,omitempty"`
```

supervisor.go (after the EnableFreight append):

```go
		if spec.FreightMaxPackages > 1 {
			args = append(args, "--freight-max-packages", strconv.Itoa(spec.FreightMaxPackages))
		}
```

(add `"strconv"` import if absent)

cmd/worker/main.go:

```go
	freightMaxPackages := flag.Int("freight-max-packages", 1, "Max concurrent freight contracts for the missions role (sub-project C multi-package trips; 1 = single-contract v1 behavior)")
	// … beside line 318:
	dispatch.FreightMaxPackages = *freightMaxPackages
```

dispatch.go — field after EnableFreight (:38) and assignment after `EnableFreight: d.EnableFreight` (:231):

```go
	// FreightMaxPackages caps concurrent freight contracts; see
	// MissionDeps.FreightMaxPackages. 0/1 = single-contract v1 behavior.
	FreightMaxPackages int
	// …
			FreightMaxPackages: d.FreightMaxPackages,
```

fleet yaml — fighter-4's line becomes:

```yaml
  - { agent_id: fighter-4, role: missionrunner, station: "", mission_categories: [delivery, exploration], enable_freight: true, freight_max_packages: 3 }
```

- [ ] **Step 4: Test** — `go build ./... && go test ./pkg/overmind/supervisor/ ./pkg/worker/ ./cmd/... -count=1` → PASS.

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run ./...
git add cmd/worker/main.go pkg/worker/dispatch.go pkg/overmind/supervisor/config.go pkg/overmind/supervisor/supervisor.go data/overmind/mission-learn-fleet.yaml
git commit --no-verify -m "feat(freight): --freight-max-packages plumbing; fighter-4 multi-package canary at 3"
```

---

## Rollout (after all tasks land — operator steps, not plan tasks)

1. `go build -o bin/worker ./cmd/worker && go build -o bin/overmind ./cmd/overmind` (binaries into bin/, never the repo root).
2. Restart ONLY the mission-learn fleet on the new binaries (drain → TERM → relaunch per `reference_overmind_launch_commands`, `--stagger 10s`). Every non-canary worker behaves exactly as before (default cap 1).
3. Watch fighter-4: `freight: holding N/3 packages` lines; `sqlite3 data/market.db "SELECT outcome, COUNT(*) FROM freight_results WHERE agent_id='fighter-4' GROUP BY outcome;"` — stop conditions same as sub-project B (`return_failed` rows, or `breached`).
4. Expect `returned_inflight` rows from bound conservatism (the round-trip leg bound + RouteHops-total-hops both over-estimate); they are cheap. If they dominate, that is tuning data, not a defect.
5. Green over ~a day → raise fighter-4 to `freight_max_packages: 7`, consider engineers 1/3/5 (355/400/400 cargo) at 3.

## Self-Review Notes

- Spec coverage: dock-pass steps 1-4 → Task 5/6; chain bound → Task 1; headroom mins → Task 3; set state/reconcile → Tasks 2/4; per-contract isolation → Tasks 4/5; flag/canary → Task 7; telemetry (one row per contract, holding-count log) → Tasks 5 (`freightDeliverDueHere`, `holding N/M` line). Deliver-failure-blocks-refill → Task 5 loop. Out-of-scope items untouched.
- Type consistency: `chainStop`/`chainFeasible(stops, nowTick)`/`chainMarginalHops(held, cand)` used identically in Tasks 1/3/5/6; `freightAccept(ctx, deps, cand, held, out)` consistent in 3/5/6; `freightChainRun(ctx, deps, nav, hopsTo, fuelCostFor, out)` consistent across Tasks 5/6 (Task 6 supplies `ensureFuelModel()`; Task 5 tests pass nil = fuel-free pricing).
- Known accepted conservatisms (do NOT "fix" during implementation): RouteHops-as-total on in_transit contracts; round-trip leg bound; unroutable held stops priced at 0.
