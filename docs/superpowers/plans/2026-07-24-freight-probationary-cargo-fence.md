# Freight Probationary-Cargo Fence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reserve scarce probationary-band freight for carriers still climbing out of the probationary tier by fencing above-probationary carriers off probationary-band cargo.

**Architecture:** Two pure helpers in `pkg/worker/freight.go` decide, per board listing, whether an above-probationary carrier should skip a probationary-band contract while the bootstrap policy is active. One filter at the top of `freightCandidate`'s existing per-listing loop drops fenced listings before `selectFreightCand` scores them. No new server calls — `ShipmentContract.RiskBand` and `prof.Progression.CurrentTier` are already in the board/profile responses the function fetches. No new struct fields or flags — the fence is gated on the existing `deps.FreightBootstrap`.

**Tech Stack:** Go 1.24+, standard library `testing`. Existing test fakes in `pkg/worker/freight_test.go` (`freightGateDeps`, `listing`, `noFuel`).

## Global Constraints

- `golangci-lint` must pass with no new findings.
- No new struct fields, flags, or server calls; reuse `deps.FreightBootstrap`, `prof.Progression.CurrentTier`, and `l.Contract.RiskBand`.
- All existing `pkg/worker` tests stay green.
- Behavior is inert unless `EnableFreight` AND `FreightBootstrap` are both set.
- `carrierTierProbationary` is the existing constant in `freight.go` with value `"probationary"`; use it, do not hardcode the string.
- Fprintf skip logs in the loop carry a trailing `//nolint:errcheck` comment, matching every other `fmt.Fprintf(out, ...)` in `freightCandidate`.

---

### Task 1: Pure tier helpers

**Files:**
- Modify: `pkg/worker/freight.go` (add two functions immediately after `effectiveFreightFloor`, which ends around line 107)
- Test: `pkg/worker/freight_test.go` (add two table tests near `TestEffectiveFreightFloor`)

**Interfaces:**
- Consumes: the existing package constant `carrierTierProbationary = "probationary"` (already declared in `freight.go`).
- Produces:
  - `func carrierTierAboveProbationary(tier string) bool`
  - `func freightBandExcluded(carrierTier, contractBand string, bootstrapEnabled bool) bool`

- [ ] **Step 1: Write the failing tests**

Add to `pkg/worker/freight_test.go`:

```go
func TestCarrierTierAboveProbationary(t *testing.T) {
	cases := map[string]bool{
		"":             false, // unknown/empty: never treated as advanced
		"probationary": false,
		"licensed":     true,
		"trusted":      true,
		"prime":        true,
	}
	for tier, want := range cases {
		if got := carrierTierAboveProbationary(tier); got != want {
			t.Fatalf("carrierTierAboveProbationary(%q) = %v, want %v", tier, got, want)
		}
	}
}

func TestFreightBandExcluded(t *testing.T) {
	cases := []struct {
		name         string
		carrierTier  string
		contractBand string
		bootstrap    bool
		want         bool
	}{
		{"probationary carrier is never fenced from probationary cargo", "probationary", "probationary", true, false},
		{"licensed carrier is fenced from probationary cargo", "licensed", "probationary", true, true},
		{"trusted carrier is fenced from probationary cargo", "trusted", "probationary", true, true},
		{"licensed carrier keeps its own-tier cargo", "licensed", "licensed", true, false},
		{"bootstrap off disables the fence", "licensed", "probationary", false, false},
		{"unknown carrier tier is never fenced", "", "probationary", true, false},
		{"empty contract band is never fenced", "licensed", "", true, false},
		{"unpriced contract band is never fenced", "licensed", "unpriced", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := freightBandExcluded(tc.carrierTier, tc.contractBand, tc.bootstrap); got != tc.want {
				t.Fatalf("freightBandExcluded(%q, %q, %v) = %v, want %v",
					tc.carrierTier, tc.contractBand, tc.bootstrap, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestCarrierTierAboveProbationary|TestFreightBandExcluded' -v`
Expected: compile failure — `undefined: carrierTierAboveProbationary` and `undefined: freightBandExcluded`.

- [ ] **Step 3: Write the helpers**

In `pkg/worker/freight.go`, immediately after the `effectiveFreightFloor` function (which closes around line 107), add:

```go
// carrierTierAboveProbationary reports whether a KNOWN tier outranks
// probationary. An empty/unknown tier returns false: never fence a carrier that
// might itself be probationary (that would starve it of the hauls it needs).
func carrierTierAboveProbationary(tier string) bool {
	return tier != "" && tier != carrierTierProbationary
}

// freightBandExcluded reports whether to skip a board listing to reserve
// probationary-band cargo for carriers still climbing out of the probationary
// tier. Gated on the bootstrap switch (the fleet-wide "help the probationary
// climb" policy): with bootstrap off, selection is unchanged. Pure — the caller
// supplies the live carrier tier, the contract's required band, and the toggle.
func freightBandExcluded(carrierTier, contractBand string, bootstrapEnabled bool) bool {
	return bootstrapEnabled &&
		contractBand == carrierTierProbationary &&
		carrierTierAboveProbationary(carrierTier)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestCarrierTierAboveProbationary|TestFreightBandExcluded' -v`
Expected: PASS (both tests, all sub-cases).

- [ ] **Step 5: Lint**

Run the `golangci-lint` tool on `pkg/worker/freight.go` and `pkg/worker/freight_test.go`.
Expected: no new findings.

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/freight.go pkg/worker/freight_test.go
git commit -m "feat(freight): pure helpers for probationary-cargo fence"
```

---

### Task 2: Wire the fence into freightCandidate

**Files:**
- Modify: `pkg/worker/freight.go` — inside `freightCandidate`, the `for _, l := range board.Shipments` loop (loop begins around line 262; the floor is computed just above at line 259)
- Test: `pkg/worker/freight_test.go` (add four integration tests near the existing `TestFreightCandidateProbationBootstrapAdmitsSubFloor`)

**Interfaces:**
- Consumes: `freightBandExcluded(carrierTier, contractBand string, bootstrapEnabled bool) bool` from Task 1; the existing `freightGateDeps(t, profileJSON, boardJSON string) MissionDeps`, `noFuel`, and `freightInputs` from the test file.
- Produces: no new exported surface; changes only the internal selection behavior of `freightCandidate`.

**Context:** `freightCandidate` already fetches the profile (into `prof`) and the board (into `board`), computes `floor := effectiveFreightFloor(prof.Progression.CurrentTier, ...)`, then loops `for _, l := range board.Shipments`. The first statement in that loop today is the aggregate-liability check (`if !prof.Capacity.LiabilityUnlimited && ...`). The fence goes ABOVE that check so a fenced contract costs no route lookup. `freightGateDeps` returns deps with `AgentID` set but `State` nil and `FreightBootstrap` false; the existing bootstrap tests set `deps.State = &missionRunState{}` and `deps.FreightBootstrap = true` after calling it — mirror that. `freightInputs` with no `HopsTo` uses each contract's `route_hops`; with `noFuel`, net equals base_reward.

- [ ] **Step 1: Write the failing tests**

Add to `pkg/worker/freight_test.go`. The shared board has an eligible probationary contract with the HIGHER net (800) and an eligible licensed contract with a lower net (600); without the fence the probationary one wins, so each test's assertion discriminates the fence cleanly:

```go
// fenceBoard: two eligible contracts. The probationary-band one has the higher
// net (reward 800, no fuel -> net 800); the licensed-band one is lower (600).
// Without the fence selectFreightCand picks prob800; the fence flips it.
const fenceBoard = `{"shipments":[` +
	`{"eligible":true,"contract":{"id":"prob800","destination_base_id":"sol_central","base_reward":800,"route_hops":2,"service_level":"standard","risk_band":"probationary"}},` +
	`{"eligible":true,"contract":{"id":"lic600","destination_base_id":"sol_central","base_reward":600,"route_hops":2,"service_level":"standard","risk_band":"licensed"}}` +
	`],"total":2}`

// An above-probationary carrier skips the (higher-net) probationary cargo and
// takes its own-tier licensed cargo instead; the skip is logged.
func TestFreightCandidateFencesProbationaryForAdvancedCarrier(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"licensed"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	deps := freightGateDeps(t, profile, fenceBoard)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = true
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}
	var log strings.Builder

	cand, skip := freightCandidate(context.Background(), deps, in, &log)
	if cand == nil {
		t.Fatalf("a licensed carrier must still take licensed cargo, got skip %q", skip)
	}
	if cand.Contract.ID != "lic600" {
		t.Fatalf("want the licensed contract lic600 (probationary fenced), got %q", cand.Contract.ID)
	}
	if !strings.Contains(log.String(), "prob800") || !strings.Contains(log.String(), "reserved for climbing carriers") {
		t.Fatalf("the fence must log why prob800 was skipped, got %q", log.String())
	}
}

// A probationary carrier is never fenced: it takes the highest-net probationary
// contract as before, and nothing is logged as reserved.
func TestFreightCandidateProbationaryCarrierNotFenced(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"probationary"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	deps := freightGateDeps(t, profile, fenceBoard)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = true
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}
	var log strings.Builder

	cand, skip := freightCandidate(context.Background(), deps, in, &log)
	if cand == nil {
		t.Fatalf("a probationary carrier must take probationary cargo, got skip %q", skip)
	}
	if cand.Contract.ID != "prob800" {
		t.Fatalf("want the highest-net probationary contract prob800, got %q", cand.Contract.ID)
	}
	if strings.Contains(log.String(), "reserved for climbing carriers") {
		t.Fatalf("a probationary carrier must never fence itself, log was %q", log.String())
	}
}

// Bootstrap off disables the fence entirely: even an advanced carrier takes the
// highest-net contract regardless of band.
func TestFreightCandidateFenceOffWhenBootstrapDisabled(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"licensed"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	deps := freightGateDeps(t, profile, fenceBoard)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = false
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}

	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand == nil {
		t.Fatalf("bootstrap off must not fence, got skip %q", skip)
	}
	if cand.Contract.ID != "prob800" {
		t.Fatalf("bootstrap off: want the highest-net contract prob800, got %q", cand.Contract.ID)
	}
}

// An above-probationary carrier at a station whose only eligible cargo is
// probationary-band fences everything and falls through (nil candidate).
func TestFreightCandidateAdvancedCarrierProbationaryOnlyBoardFallsThrough(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"licensed"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	board := `{"shipments":[{"eligible":true,"contract":{"id":"prob800","destination_base_id":"sol_central","base_reward":800,"route_hops":2,"service_level":"standard","risk_band":"probationary"}}],"total":1}`
	deps := freightGateDeps(t, profile, board)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = true
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}

	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand != nil {
		t.Fatalf("all cargo fenced must yield no candidate, got %q", cand.Contract.ID)
	}
	if !strings.Contains(skip, "no freight cleared the gate") {
		t.Fatalf("want the board-emptied skip reason, got %q", skip)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestFreightCandidateFencesProbationaryForAdvancedCarrier|TestFreightCandidateProbationaryCarrierNotFenced|TestFreightCandidateFenceOffWhenBootstrapDisabled|TestFreightCandidateAdvancedCarrierProbationaryOnlyBoardFallsThrough' -v`
Expected: `TestFreightCandidateFencesProbationaryForAdvancedCarrier` FAILS (selects `prob800`, no fence log) and `...ProbationaryOnlyBoardFallsThrough` FAILS (returns `prob800` instead of nil). The other two pass even without the fence (they assert the un-fenced outcome), which is expected — they lock in that the fence does NOT fire for those cases.

- [ ] **Step 3: Add the fence to the loop**

In `pkg/worker/freight.go`, inside `freightCandidate`, make the fence the FIRST statement in the `for _, l := range board.Shipments {` loop body — above the `if !prof.Capacity.LiabilityUnlimited && ...` liability check:

```go
	for _, l := range board.Shipments {
		if freightBandExcluded(prof.Progression.CurrentTier, l.Contract.RiskBand, deps.FreightBootstrap) {
			fmt.Fprintf(out, "freight: skip %s: probationary cargo reserved for climbing carriers (carrier tier %s)\n",
				l.Contract.ID, prof.Progression.CurrentTier) //nolint:errcheck
			continue
		}
		if !prof.Capacity.LiabilityUnlimited && prof.Capacity.AggregateLiabilityLimit > 0 &&
			l.Contract.ReservedExposure > prof.Capacity.RemainingAggregateLiability {
			// ... existing body unchanged ...
```

(Only the first `if freightBandExcluded(...) { ... continue }` block is new; leave the rest of the loop exactly as it is.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestFreightCandidateFencesProbationaryForAdvancedCarrier|TestFreightCandidateProbationaryCarrierNotFenced|TestFreightCandidateFenceOffWhenBootstrapDisabled|TestFreightCandidateAdvancedCarrierProbationaryOnlyBoardFallsThrough' -v`
Expected: PASS (all four).

- [ ] **Step 5: Run the full worker suite (guard existing tests)**

Run: `go test ./pkg/worker/`
Expected: PASS. The existing `shippingProfileJSON` sets only `Profile.Tier`, leaving `Progression.CurrentTier` empty, so `carrierTierAboveProbationary("")` is false and no existing test is fenced.

- [ ] **Step 6: Build and lint**

Run: `go build ./...`
Expected: no errors.
Then run the `golangci-lint` tool on the changed files.
Expected: no new findings.

- [ ] **Step 7: Commit**

```bash
git add pkg/worker/freight.go pkg/worker/freight_test.go
git commit -m "feat(freight): fence advanced carriers off probationary cargo"
```

---

## Notes for the final review

- Minor findings roll up here as encountered (none anticipated).
- The change is selection-only; no wire structs, flags, or server calls were added. Confirm `deps.FreightBootstrap` is the sole gate and that an empty `Progression.CurrentTier` never fences (the safe default that keeps every existing test green).
