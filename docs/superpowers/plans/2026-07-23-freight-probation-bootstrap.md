# Freight Probation Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a probationary-tier worker carrier accept bounded-loss freight contracts so it accumulates the deliveries + delivered-value needed to advance to the licensed tier (which unlocks ~4×-paying contracts), escaping the deadlock where its only eligible contracts fall under the 500cr net floor.

**Architecture:** A single local gate modifier in the freight decision path. While a worker's carrier is `probationary` and its per-worker in-memory loss budget is not spent, the freight net floor drops from `+500` to `-400`, admitting the small probationary-eligible contracts. Losses (negative-net accepts) accrue against a 3,000cr budget; positive-net contracts are free. The tier is read live from the server each pass, so a worker stops bootstrapping the pass after the server flips it to `licensed`. Nothing else in the fail-closed freight path changes.

**Tech Stack:** Go 1.24+, `pkg/worker` (freight decision path), `pkg/game/serverapi` (already parses `CarrierTierProgress.CurrentTier`).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-23-freight-probation-bootstrap-design.md`.
- Probationary floor = **-400.0**; loss budget = **3000.0**; both are named constants, not literals.
- Bootstrap applies **only** at `current_tier == "probationary"`. Every other tier (licensed/trusted/prime) AND empty/unknown/legacy tier keep the normal `freightMinNet` (500) floor.
- The budget tracks **actual losses only** — accrue `-net` on an accepted contract with `net < 0`; a positive-net accept never touches the budget.
- Per-worker = per-account. The tally lives on the per-worker `missionRunState` (`deps.State`); there is NO cross-worker coordination. All new `missionRunState` methods must be nil-receiver safe (`deps.State` is optional and nil in many tests), matching the existing `addHeldFreight`/`markAttempted` pattern.
- `FreightBootstrap` toggle defaults **on** wherever `--enable-freight` is set; `FreightBootstrap == false` forces the 500 floor (kill switch).
- Freight path only — do NOT touch `missionMinNet` or any mission-board (`get_missions`) logic.
- `selectFreightCand` ranking is unchanged (highest-net-first): profitable contracts are always preferred; a loss is taken only when nothing better is eligible.
- No new server round-trips: the floor is computed from the `ShippingProfileResponse` that `freightCandidate` already fetches.
- Sleeps (if any were needed) use `pkg/game/constants.go` constants — this feature adds none.
- `golangci-lint` must pass with no new findings. Run `go build ./...` and `go test ./pkg/worker/...` before every commit.

---

### Task 1: Constants + `effectiveFreightFloor` pure function

**Files:**
- Modify: `pkg/worker/freight.go` (add constants near `freightMinNet` at line 16-41; add the function after `freightEffectiveMax`, ~line 74)
- Test: `pkg/worker/freight_test.go` (append)

**Interfaces:**
- Consumes: nothing (pure function over primitives).
- Produces:
  - `const carrierTierProbationary = "probationary"`
  - `const freightProbationFloor = -400.0`
  - `const freightProbationBudget = 3000.0`
  - `func effectiveFreightFloor(tier string, bootstrapEnabled bool, spent, budget float64) float64` — returns `freightProbationFloor` when `bootstrapEnabled && tier == carrierTierProbationary && spent < budget`, else `freightMinNet`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/worker/freight_test.go`:

```go
func TestEffectiveFreightFloor(t *testing.T) {
	cases := []struct {
		name      string
		tier      string
		enabled   bool
		spent     float64
		budget    float64
		wantFloor float64
	}{
		{"probationary with budget remaining relaxes", "probationary", true, 0, freightProbationBudget, freightProbationFloor},
		{"probationary partway through budget still relaxes", "probationary", true, 2999, freightProbationBudget, freightProbationFloor},
		{"probationary budget exhausted reverts", "probationary", true, freightProbationBudget, freightProbationBudget, freightMinNet},
		{"probationary over budget reverts", "probationary", true, 3500, freightProbationBudget, freightMinNet},
		{"licensed keeps normal floor", "licensed", true, 0, freightProbationBudget, freightMinNet},
		{"trusted keeps normal floor", "trusted", true, 0, freightProbationBudget, freightMinNet},
		{"prime keeps normal floor", "prime", true, 0, freightProbationBudget, freightMinNet},
		{"empty tier keeps normal floor", "", true, 0, freightProbationBudget, freightMinNet},
		{"unknown legacy tier keeps normal floor", "gold", true, 0, freightProbationBudget, freightMinNet},
		{"bootstrap disabled forces normal floor even when probationary", "probationary", false, 0, freightProbationBudget, freightMinNet},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveFreightFloor(tc.tier, tc.enabled, tc.spent, tc.budget); got != tc.wantFloor {
				t.Fatalf("effectiveFreightFloor(%q, %v, %v, %v) = %v, want %v",
					tc.tier, tc.enabled, tc.spent, tc.budget, got, tc.wantFloor)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run TestEffectiveFreightFloor -v`
Expected: FAIL — `undefined: effectiveFreightFloor` (and undefined constants).

- [ ] **Step 3: Add the constants**

In `pkg/worker/freight.go`, inside the existing `const ( ... )` block that ends with `freightMinNet = 500.0` (lines 16-41), append after `freightMinNet`:

```go
	// carrierTierProbationary is the lowest carrier standing. Bootstrap relaxes
	// the freight net floor ONLY at this tier; every higher tier's cargo already
	// clears the normal floor. Matches ShippingProfileResponse.Progression.CurrentTier.
	carrierTierProbationary = "probationary"

	// freightProbationFloor is the relaxed net floor a probationary carrier
	// accepts down to while bootstrapping out of the tier. Negative: the
	// probationary board is mostly small POSITIVE-net contracts the 500 floor
	// wrongly rejects, plus a few small losses worth eating to reach the
	// ~4x-paying licensed tier. Aggressive-profile value; tunable.
	freightProbationFloor = -400.0

	// freightProbationBudget caps cumulative bootstrap LOSSES per worker
	// (positive-net accepts are free). Once spent, the floor reverts to 500
	// until the server flips the tier. Aggressive-profile value; tunable.
	freightProbationBudget = 3000.0
```

- [ ] **Step 4: Add the function**

In `pkg/worker/freight.go`, after `freightEffectiveMax` (ends ~line 73), add:

```go
// effectiveFreightFloor is the net floor a contract must clear this pass. It
// relaxes from freightMinNet (500) to freightProbationFloor (-400) only while a
// worker is bootstrapping out of the probationary tier: bootstrap enabled, tier
// is exactly probationary, and its per-worker loss budget is not yet spent. Any
// other tier, an unknown/empty tier, a spent budget, or a disabled toggle all
// yield the normal 500 floor. Pure — the caller supplies the live tier and the
// per-worker spend so this needs no I/O and is table-testable.
func effectiveFreightFloor(tier string, bootstrapEnabled bool, spent, budget float64) float64 {
	if bootstrapEnabled && tier == carrierTierProbationary && spent < budget {
		return freightProbationFloor
	}
	return freightMinNet
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/worker/ -run TestEffectiveFreightFloor -v`
Expected: PASS (all subtests).

- [ ] **Step 6: Build and commit**

Run: `go build ./... && go test ./pkg/worker/ -run TestEffectiveFreightFloor` (expect PASS)

```bash
git add pkg/worker/freight.go pkg/worker/freight_test.go
git commit -m "feat(freight): effectiveFreightFloor + probation-bootstrap constants"
```

---

### Task 2: `buildFreightCand` floor parameter (behavior-preserving refactor)

Isolate the risky signature change from the behavior change. `buildFreightCand` currently hard-codes `freightMinNet` in its net check. Add a `floor float64` parameter and pass `freightMinNet` at every existing call site, so behavior is byte-for-byte identical and all existing tests stay green. Task 3 will pass a computed floor.

**Files:**
- Modify: `pkg/worker/freight.go:78` (signature), `:107-108` (the `net < floor` check + log message), `:241` (the sole non-test caller inside `freightCandidate`)
- Modify: `pkg/worker/freight_test.go` (update every `buildFreightCand(...)` call to pass the new floor argument)

**Interfaces:**
- Consumes: `freightMinNet` (existing const).
- Produces: `func buildFreightCand(l serverapi.ShippingListing, hops int, held []chainStop, nowTick int64, fuelCostFor func(jumps int) float64, floor float64) (freightCand, string)` — the trailing `floor` is the net threshold.

- [ ] **Step 1: Change the signature and the check**

In `pkg/worker/freight.go`, change the signature at line 78 to add the trailing `floor float64` parameter:

```go
func buildFreightCand(l serverapi.ShippingListing, hops int, held []chainStop, nowTick int64, fuelCostFor func(jumps int) float64, floor float64) (freightCand, string) {
```

Then change the net-floor check (currently lines 106-109) to use `floor`:

```go
	net := reward - fuel
	if net < floor {
		return freightCand{}, fmt.Sprintf("net %.0f below floor %.0f (reward %.0f, fuel %.0f)", net, floor, reward, fuel)
	}
```

- [ ] **Step 2: Update the sole non-test caller**

In `pkg/worker/freight.go`, the call inside `freightCandidate` (currently line 241). For this task pass `freightMinNet` verbatim — Task 3 replaces it with the computed floor:

```go
		c, reason := buildFreightCand(l, hops, in.Held, in.NowTick, in.FuelCostFor, freightMinNet)
```

- [ ] **Step 3: Update every test call site**

In `pkg/worker/freight_test.go`, append `, freightMinNet` as the final argument to every `buildFreightCand(...)` call. There are exactly these call sites (verify with `grep -n "buildFreightCand(" pkg/worker/freight_test.go` — expect 9):
- Line ~66: `buildFreightCand(listing("a", false, 5000, 2), 2, nil, 0, noFuel, freightMinNet)`
- Line ~70: `buildFreightCand(listing("b", true, 100, 2), 2, nil, 0, noFuel, freightMinNet)`
- Line ~74: `buildFreightCand(listing("c", true, 520, 5), 5, nil, 0, flatFuel, freightMinNet)`
- Line ~80: `buildFreightCand(bad, 2, nil, 0, noFuel, freightMinNet)`
- Line ~86: `buildFreightCand(listing("e", true, 5000, 3), 3, nil, 0, flatFuel, freightMinNet)`
- Line ~99: `buildFreightCand(low, 1, nil, 0, noFuel, freightMinNet)`
- Line ~108: `buildFreightCand(listing("a", true, 1000, 1), 1, nil, 0, noFuel, freightMinNet)`
- Line ~109: `buildFreightCand(listing("b", true, 9000, 1), 1, nil, 0, noFuel, freightMinNet)`
- Line ~110: `buildFreightCand(listing("c", true, 3000, 1), 1, nil, 0, noFuel, freightMinNet)`
- Line ~1454: `buildFreightCand(l, 2, held, 0, func(j int) float64 { return float64(j) * 100 }, freightMinNet)`
- Line ~1474: `buildFreightCand(l, 2, held, 0, nil, freightMinNet)`

(If the grep count differs from the list, update whatever call sites exist — every call must gain the trailing `freightMinNet`.)

- [ ] **Step 4: Run tests to verify green (behavior unchanged)**

Run: `go build ./... && go test ./pkg/worker/`
Expected: PASS — this is a pure refactor; no test assertion changes.

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/freight.go pkg/worker/freight_test.go
git commit -m "refactor(freight): thread net floor into buildFreightCand as a parameter"
```

---

### Task 3: Per-worker loss tally + wire the relaxed floor into the freight pass

Add the `bootstrapSpent` tally to `missionRunState`, the `FreightBootstrap` field to `MissionDeps`, compute the live floor inside `freightCandidate`, and accrue losses in `freightAccept`. This is the behavior change: a probationary bootstrap worker now accepts sub-500 contracts and stops once its loss budget is spent.

**Files:**
- Modify: `pkg/worker/mission.go` (`missionRunState` struct ~line 65-87; add methods after `shouldLogSkip` ~line 174; `MissionDeps` struct ~line 183-230)
- Modify: `pkg/worker/freight.go` (`freightCandidate` — compute floor from `prof` + `deps` + `deps.State`, pass to `buildFreightCand` at ~line 241; `freightAccept` — accrue at the proceed return ~line 377)
- Test: `pkg/worker/freight_test.go` (append)

**Interfaces:**
- Consumes: `effectiveFreightFloor` (Task 1); `buildFreightCand(..., floor)` (Task 2); `deps.State *missionRunState`; `prof.Progression.CurrentTier` (`serverapi.CarrierTierProgress`); `cand.Net`.
- Produces:
  - `MissionDeps.FreightBootstrap bool`
  - `func (s *missionRunState) addBootstrapSpent(loss float64)` — nil-safe; adds `loss` to the tally.
  - `func (s *missionRunState) bootstrapSpent() float64` — nil-safe; returns the tally (0 on nil).

- [ ] **Step 1: Write the failing tests**

Append to `pkg/worker/freight_test.go`. These cover (a) the nil-safe tally, (b) a probationary bootstrap pass admitting a sub-500 contract, (c) accrual on a negative-net accept, and (d) no accrual on a positive-net accept:

```go
func TestBootstrapSpentTally(t *testing.T) {
	var nilState *missionRunState
	if got := nilState.bootstrapSpent(); got != 0 {
		t.Fatalf("nil state bootstrapSpent = %v, want 0", got)
	}
	nilState.addBootstrapSpent(500) // must not panic on nil receiver

	s := &missionRunState{}
	if got := s.bootstrapSpent(); got != 0 {
		t.Fatalf("fresh state bootstrapSpent = %v, want 0", got)
	}
	s.addBootstrapSpent(300)
	s.addBootstrapSpent(200)
	if got := s.bootstrapSpent(); got != 500 {
		t.Fatalf("bootstrapSpent = %v, want 500", got)
	}
}

// A probationary bootstrap worker accepts a contract whose net is below the
// normal 500 floor but above the -400 probation floor. The same board with
// bootstrap OFF selects nothing.
func TestFreightCandidateProbationBootstrapAdmitsSubFloor(t *testing.T) {
	// One eligible contract: reward 100, no fuel -> net +100 (below 500,
	// above -400). Profile reports the probationary tier.
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"probationary"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	board := `{"shipments":[{"eligible":true,"contract":{"id":"p1","destination_base_id":"sol_central","base_reward":100,"route_hops":2,"service_level":"standard"}}]}`

	// Bootstrap ON: the sub-floor contract clears.
	deps := freightGateDeps(t, profile, board)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = true
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}
	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand == nil {
		t.Fatalf("probationary bootstrap must admit the sub-floor contract, got skip %q", skip)
	}
	if cand.Contract.ID != "p1" {
		t.Fatalf("wrong candidate %q, want p1", cand.Contract.ID)
	}

	// Bootstrap OFF: same board, normal 500 floor rejects it.
	depsOff := freightGateDeps(t, profile, board)
	depsOff.State = &missionRunState{}
	depsOff.FreightBootstrap = false
	if cand, _ := freightCandidate(context.Background(), depsOff, in, io.Discard); cand != nil {
		t.Fatalf("bootstrap off must reject the sub-floor contract, got %+v", cand)
	}
}

// The budget caps losses: once bootstrapSpent >= freightProbationBudget the
// floor reverts to 500 even while still probationary, so a sub-500 contract is
// rejected.
func TestFreightCandidateProbationBudgetExhaustedReverts(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"probationary"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	board := `{"shipments":[{"eligible":true,"contract":{"id":"p1","destination_base_id":"sol_central","base_reward":100,"route_hops":2,"service_level":"standard"}}]}`
	deps := freightGateDeps(t, profile, board)
	deps.State = &missionRunState{}
	deps.State.addBootstrapSpent(freightProbationBudget) // budget fully spent
	deps.FreightBootstrap = true
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}
	if cand, _ := freightCandidate(context.Background(), deps, in, io.Discard); cand != nil {
		t.Fatalf("a spent budget must revert to the 500 floor and reject, got %+v", cand)
	}
}

// A negative-net accept accrues -net against the budget; a positive-net accept
// does not touch it.
func TestFreightAcceptAccruesBootstrapLoss(t *testing.T) {
	feasible := shippingContractJSON(t, "accept", acceptedContract(1200, 1380))

	// Negative net -> accrue.
	f := &fakeClient{state: &game.State{CurrentTick: 1200}, raw: map[string][]byte{"shipping_accept": feasible}}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeMissionStore{}, State: &missionRunState{}}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: -300}
	if _, step := freightAccept(context.Background(), deps, cand, nil, io.Discard); step != freightStepProceed {
		t.Fatal("a feasible contract must proceed")
	}
	if got := deps.State.bootstrapSpent(); got != 300 {
		t.Fatalf("bootstrapSpent = %v, want 300 after a -300 accept", got)
	}

	// Positive net -> no accrual.
	f2 := &fakeClient{state: &game.State{CurrentTick: 1200}, raw: map[string][]byte{"shipping_accept": shippingContractJSON(t, "accept", acceptedContract(1200, 1380))}}
	deps2 := MissionDeps{Client: f2, AgentID: "fighter-4", Market: &fakeMissionStore{}, State: &missionRunState{}}
	candPos := &freightCand{Contract: serverapi.ShipmentContract{ID: "high2"}, Hops: 3, Net: 6000}
	if _, step := freightAccept(context.Background(), deps2, candPos, nil, io.Discard); step != freightStepProceed {
		t.Fatal("a feasible positive contract must proceed")
	}
	if got := deps2.State.bootstrapSpent(); got != 0 {
		t.Fatalf("bootstrapSpent = %v, want 0 after a positive-net accept", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestBootstrapSpentTally|TestFreightCandidateProbation|TestFreightAcceptAccruesBootstrapLoss' -v`
Expected: FAIL — `deps.State.bootstrapSpent undefined`, `deps.FreightBootstrap undefined`.

- [ ] **Step 3: Add the tally field + methods to `missionRunState`**

In `pkg/worker/mission.go`, add a field to the `missionRunState` struct (after `heldFreight` at line 86):

```go
	// bootstrapSpent is the cumulative freight LOSS (sum of -net over accepted
	// negative-net contracts) this worker has eaten to climb out of the
	// probationary carrier tier. In-memory and per-worker; resets on restart
	// (bounded — the probationary gate and fast advancement keep it small).
	// Positive-net accepts never touch it.
	bootstrapSpent float64
```

Rename note: the field and the accessor share the name `bootstrapSpent`, which Go forbids on the same type. Name the field `bootstrapLoss` and keep the accessor `bootstrapSpent()`:

```go
	// bootstrapLoss is the cumulative freight LOSS (sum of -net over accepted
	// negative-net contracts) this worker has eaten to climb out of the
	// probationary carrier tier. In-memory and per-worker; resets on restart
	// (bounded — the probationary gate and fast advancement keep it small).
	// Positive-net accepts never touch it. Read via bootstrapSpent().
	bootstrapLoss float64
```

Then add the methods after `shouldLogSkip` (after line 174):

```go
// addBootstrapSpent adds a bootstrap freight loss to the per-worker tally. No-op
// on a nil receiver (State is optional; tests that don't care omit it).
func (s *missionRunState) addBootstrapSpent(loss float64) {
	if s == nil {
		return
	}
	s.bootstrapLoss += loss
}

// bootstrapSpent is the cumulative bootstrap freight loss this session; 0 on a
// nil receiver.
func (s *missionRunState) bootstrapSpent() float64 {
	if s == nil {
		return 0
	}
	return s.bootstrapLoss
}
```

- [ ] **Step 4: Add the `FreightBootstrap` field to `MissionDeps`**

In `pkg/worker/mission.go`, add to the `MissionDeps` struct after `FreightMaxPackages` (after line 229):

```go
	// FreightBootstrap enables the probationary loss-leader floor relaxation
	// (effectiveFreightFloor). Defaults on wherever EnableFreight is set; a
	// false value forces the normal 500 floor — the kill switch. Inert unless
	// EnableFreight is also true (freightCandidate only runs on the freight path).
	FreightBootstrap bool
```

- [ ] **Step 5: Compute the live floor inside `freightCandidate`**

In `pkg/worker/freight.go`, `freightCandidate` already decodes `prof serverapi.ShippingProfileResponse` (line 169-174). After the candidate loop begins, compute the floor once from the live tier and per-worker spend, then pass it to `buildFreightCand`. Change the call at line 241 from `freightMinNet` to a `floor` computed just above the loop (insert right before `cands := make(...)` at line 225):

```go
	// Bootstrap floor: relaxed to freightProbationFloor only while this worker
	// is probationary and its loss budget holds; otherwise the normal 500 floor.
	// Read from the profile just fetched — no extra server call.
	floor := effectiveFreightFloor(prof.Progression.CurrentTier, deps.FreightBootstrap, deps.State.bootstrapSpent(), freightProbationBudget)
```

Then the call at line 241 becomes:

```go
		c, reason := buildFreightCand(l, hops, in.Held, in.NowTick, in.FuelCostFor, floor)
```

- [ ] **Step 6: Accrue the loss in `freightAccept`**

In `pkg/worker/freight.go`, `freightAccept` reaches its success return at line 376-377:

```go
	deps.State.addHeldFreight(&c)
	return &c, freightStepProceed
```

Insert the accrual immediately before the `return`, so a contract accepted at a loss under the relaxed floor charges the budget (a returned/infeasible contract never reaches here, so no loss is charged for one that was handed back):

```go
	deps.State.addHeldFreight(&c)
	// A negative-net accept only happens under the relaxed probation floor;
	// charge the loss against the per-worker budget so it eventually reverts.
	if cand.Net < 0 {
		deps.State.addBootstrapSpent(-cand.Net)
	}
	return &c, freightStepProceed
```

- [ ] **Step 7: Run the new tests**

Run: `go test ./pkg/worker/ -run 'TestBootstrapSpentTally|TestFreightCandidateProbation|TestFreightAcceptAccruesBootstrapLoss' -v`
Expected: PASS.

- [ ] **Step 8: Run the full package suite (existing freight tests must stay green)**

Run: `go build ./... && go test ./pkg/worker/`
Expected: PASS. Existing `freightCandidate` tests use `shippingProfileJSON` with no `current_tier` and default `FreightBootstrap == false`, so `effectiveFreightFloor` returns 500 for them — behavior unchanged.

- [ ] **Step 9: Lint**

Run: `golangci-lint run pkg/worker/...`
Expected: no new findings.

- [ ] **Step 10: Commit**

```bash
git add pkg/worker/mission.go pkg/worker/freight.go pkg/worker/freight_test.go
git commit -m "feat(freight): probation bootstrap floor + per-worker loss budget"
```

---

### Task 4: Wire the `--freight-bootstrap` toggle end-to-end

Make the kill switch reachable from the CLI and the overmind supervisor. The flag defaults **on**; the operator disables it with `--freight-bootstrap=false` (or the supervisor config `disable_freight_bootstrap: true`), which lets an operator keep freight running while turning bootstrap off to observe the deadlock.

**Files:**
- Modify: `pkg/worker/dispatch.go` (`WorkerDispatch` struct ~line 35-41; the `"missions"` dispatch case ~line 230-236)
- Modify: `cmd/worker/main.go` (flag definition ~line 58-59; assignment ~line 330-331)
- Modify: `pkg/overmind/supervisor/config.go` (`WorkerSpec` fields ~line 20-30)
- Modify: `pkg/overmind/supervisor/supervisor.go` (arg building ~line 40-44)
- Test: `pkg/overmind/supervisor/supervisor_test.go` (or the existing arg-building test — verify with `grep -rn "enable-freight\|func Test.*[Aa]rgs" pkg/overmind/supervisor/*_test.go`)

**Interfaces:**
- Consumes: `MissionDeps.FreightBootstrap` (Task 3).
- Produces:
  - `WorkerDispatch.FreightBootstrap bool`
  - `supervisor.WorkerSpec.DisableFreightBootstrap bool` (yaml `disable_freight_bootstrap,omitempty`)
  - CLI flag `--freight-bootstrap` (default `true`).

- [ ] **Step 1: Add the `WorkerDispatch` field and pass it through**

In `pkg/worker/dispatch.go`, add to the `WorkerDispatch` struct after `FreightMaxPackages` (after line 41):

```go
	// FreightBootstrap enables the probationary loss-leader floor relaxation for
	// the missions role. Defaults on when EnableFreight is set; false is the
	// kill switch. See MissionDeps.FreightBootstrap.
	FreightBootstrap bool
```

Then in the `"missions"` case, add the field to the `MissionDeps` literal (after `FreightMaxPackages: d.FreightMaxPackages,` at line 235):

```go
			FreightBootstrap:   d.FreightBootstrap,
```

- [ ] **Step 2: Add the CLI flag and assignment**

In `cmd/worker/main.go`, after the `freightMaxPackages` flag (line 59), add:

```go
	freightBootstrap := flag.Bool("freight-bootstrap", true, "Enable the probationary loss-leader freight floor for the missions role (default on when --enable-freight; set =false to keep freight on but disable bootstrapping)")
```

Then after `dispatch.FreightMaxPackages = *freightMaxPackages` (line 331), add:

```go
			dispatch.FreightBootstrap = *freightBootstrap
```

- [ ] **Step 3: Add the supervisor config field**

In `pkg/overmind/supervisor/config.go`, add to `WorkerSpec` after `FreightMaxPackages` (after line 30):

```go
	// DisableFreightBootstrap forwards --freight-bootstrap=false, turning off the
	// probationary loss-leader floor while leaving freight enabled. Zero value
	// (false) keeps the CLI default (bootstrap on).
	DisableFreightBootstrap bool `yaml:"disable_freight_bootstrap,omitempty"`
```

- [ ] **Step 4: Write the failing supervisor arg test**

First confirm the existing test name/pattern: `grep -rn "enable-freight\|freight-max-packages\|func Test" pkg/overmind/supervisor/supervisor_test.go`. Add a test that a spec with `DisableFreightBootstrap` emits `--freight-bootstrap=false`, and that a spec without it does NOT (default-on convention). Adapt the deps to the file's existing helpers; the shape:

```go
func TestBuildArgsFreightBootstrapDisable(t *testing.T) {
	on := WorkerSpec{ID: "w1", Role: "missions", EnableFreight: true}
	if args := buildWorkerArgs(on); slices.Contains(args, "--freight-bootstrap=false") {
		t.Fatalf("bootstrap on by default must not emit the disable flag: %v", args)
	}
	off := WorkerSpec{ID: "w1", Role: "missions", EnableFreight: true, DisableFreightBootstrap: true}
	if args := buildWorkerArgs(off); !slices.Contains(args, "--freight-bootstrap=false") {
		t.Fatalf("DisableFreightBootstrap must emit --freight-bootstrap=false: %v", args)
	}
}
```

Replace `buildWorkerArgs(spec)` with the actual arg-building function/method name found in `supervisor.go` (the code around line 40 that appends `--enable-freight`). If it is a method like `(s *Supervisor) workerArgs(spec)`, mirror how the existing freight test in this file constructs its call. Add `"slices"` to the test imports if absent.

- [ ] **Step 5: Run the test to verify it fails**

Run: `go test ./pkg/overmind/supervisor/ -run TestBuildArgsFreightBootstrapDisable -v`
Expected: FAIL — the disable arg is never emitted yet.

- [ ] **Step 6: Emit the arg in the supervisor**

In `pkg/overmind/supervisor/supervisor.go`, in the block that appends freight args (after the `FreightMaxPackages` append at line 43-45), add:

```go
		if spec.DisableFreightBootstrap {
			args = append(args, "--freight-bootstrap=false")
		}
```

- [ ] **Step 7: Run the supervisor test**

Run: `go test ./pkg/overmind/supervisor/ -run TestBuildArgsFreightBootstrapDisable -v`
Expected: PASS.

- [ ] **Step 8: Build, full test, lint**

Run: `go build ./... && go test ./pkg/worker/ ./pkg/overmind/supervisor/ && golangci-lint run pkg/worker/... pkg/overmind/supervisor/...`
Expected: PASS, no new lint findings.

- [ ] **Step 9: Commit**

```bash
git add pkg/worker/dispatch.go cmd/worker/main.go pkg/overmind/supervisor/config.go pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/supervisor_test.go
git commit -m "feat(freight): --freight-bootstrap toggle through dispatch/CLI/supervisor"
```

---

## Self-Review

**1. Spec coverage:**
- Component 1 `effectiveFreightFloor` → Task 1. ✔
- Component 2 `buildFreightCand` floor parameter → Task 2. ✔
- Component 3 per-worker loss tally + accrual on net<0 → Task 3 (Steps 3, 6). ✔
- Component 4 constants + `FreightBootstrap` toggle → constants Task 1, toggle Tasks 3-4. ✔
- Decision 1 (both per-contract floor + budget): per-contract floor = `effectiveFreightFloor`/`buildFreightCand`; budget = `bootstrapLoss` vs `freightProbationBudget`. ✔
- Decision 2 (aggressive: -400 / 3000): Task 1 constants. ✔
- Decision 4 (probationary only): `tier == carrierTierProbationary` guard. ✔
- Decision 5 (freight only): no mission-path files touched. ✔
- Decision 6 (positive-net free): accrual guarded by `cand.Net < 0`. ✔
- Error handling — tier empty/legacy → 500 (Task 1 test cases); budget exhausted → 500 (Task 3 test); restart resets in-memory (field on `missionRunState`, no persistence). ✔
- Testing section (effective-floor table, build-cand floor behavior via existing Task 2 tests, loss tally, integration probationary board) → covered across Tasks 1-3. ✔
- Out of scope respected: no floor relaxation above probationary, no persistence, no campaign state machine, no `missionMinNet` change, no `selectFreightCand` change. ✔

**2. Placeholder scan:** No TBD/TODO/"handle edge cases". Every code step shows complete code. Two call-site-count caveats (Task 2 Step 3, Task 4 Step 4) instruct the engineer to verify with grep and adapt — deliberate, because the exact line numbers/helper names drift; the grep command and the transformation are both concrete.

**3. Type consistency:** `effectiveFreightFloor(tier string, bootstrapEnabled bool, spent, budget float64) float64` is used identically in Task 1 (def) and Task 3 Step 5 (call). `buildFreightCand(..., floor float64)` consistent Task 2 → Task 3. `bootstrapSpent()` accessor vs `bootstrapLoss` field — the name collision is explicitly resolved in Task 3 Step 3 (field renamed to `bootstrapLoss`). `MissionDeps.FreightBootstrap` / `WorkerDispatch.FreightBootstrap` / `WorkerSpec.DisableFreightBootstrap` names consistent across Tasks 3-4.
