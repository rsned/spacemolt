# Dynamic Rescue-Fuel Sizing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Size each rescue fuel transfer at refuel time to fill the strandee's tank, capped by what the rescuer can spare after reserving enough fuel to fly home — replacing the flat ~10-fuel transfer that traps big-tank haulers in a rescue loop.

**Architecture:** A pure helper `rescue.TransferQuantity` holds the MIN/clamp math. `pkg/worker/assist.go`'s `runRescue` computes the quantity live from the rescuer's own fuel and its BFS distance home, falling back to the record's enqueue-time estimate when live data is missing, and releasing the claim when it cannot spare fuel without stranding itself.

**Tech Stack:** Go 1.24+, existing `pkg/rescue`, `pkg/worker`, `pkg/navigation`, `pkg/knowledge` packages. Tests use the in-package `fakeClient` / `fakeRescueQueue` and `knowledge.NewMemoryKB()`.

## Global Constraints

- Go 1.24+; use modern features where natural.
- New code must pass `golangci-lint` with no new findings.
- Run `go build ./...` and `go test ./...` before committing.
- Reuse the existing `FuelPerJump = 5` and `FuelBuffer = 5` constants in `pkg/rescue/enrich.go`; do not introduce new magic numbers.
- `rescue.Record.Fuel` and `.MaxFuel` are `float64`; `.RescueFuel` is `int`; `game.State.Fuel` is `float64`. Convert to `int` at the call site.

---

### Task 1: `TransferQuantity` pure helper

**Files:**
- Modify: `pkg/rescue/enrich.go` (add function below the existing constants/`fuelForHops`)
- Test: `pkg/rescue/enrich_test.go` (create if absent; otherwise append)

**Interfaces:**
- Consumes: existing `FuelPerJump`, `FuelBuffer` constants in the same file.
- Produces: `func TransferQuantity(strandeeMaxFuel, strandeeFuel, rescuerFuel, hopsHome int) int` — returns `min(need, spare)` where `need = max(0, strandeeMaxFuel-strandeeFuel)` and `spare = max(0, rescuerFuel-(FuelPerJump*hopsHome+FuelBuffer))`. Task 2 calls this.

- [ ] **Step 1: Write the failing test**

Create/append `pkg/rescue/enrich_test.go`:

```go
package rescue

import "testing"

func TestTransferQuantity(t *testing.T) {
	cases := []struct {
		name                                        string
		strandeeMax, strandeeFuel, rescuerFuel, hops int
		want                                        int
	}{
		// Big-tank strandee, healthy near rescuer: capped by rescuer spare.
		// spare = 130 - (5*1 + 5) = 120; need = 420 - 0 = 420 -> 120.
		{"capped by rescuer spare", 420, 0, 130, 1, 120},
		// Small strandee, healthy rescuer: capped by need.
		// need = 75 - 5 = 70; spare = 130 - (5*1+5) = 120 -> 70.
		{"capped by strandee need", 75, 5, 130, 1, 70},
		// Far / low-fuel rescuer: spare clamps to 0 -> caller declines.
		// spare = 20 - (5*3 + 5) = 0; need = 420 -> 0.
		{"rescuer cannot spare", 420, 0, 20, 3, 0},
		// Strandee already full: need 0 -> 0.
		{"strandee already full", 120, 120, 130, 0, 0},
		// hops 0 (station in-system): reserve is just the buffer.
		// spare = 100 - (0 + 5) = 95; need = 100 -> 95.
		{"zero hops home reserves buffer only", 100, 0, 100, 0, 95},
	}
	for _, tc := range cases {
		if got := TransferQuantity(tc.strandeeMax, tc.strandeeFuel, tc.rescuerFuel, tc.hops); got != tc.want {
			t.Errorf("%s: TransferQuantity(%d,%d,%d,%d) = %d, want %d",
				tc.name, tc.strandeeMax, tc.strandeeFuel, tc.rescuerFuel, tc.hops, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/rescue/ -run TestTransferQuantity -v`
Expected: FAIL — `undefined: TransferQuantity`

- [ ] **Step 3: Write minimal implementation**

Append to `pkg/rescue/enrich.go` (after `fuelForHops`):

```go
// TransferQuantity sizes a rescue fuel transfer at refuel time: fill the
// strandee's remaining tank capacity, but never give away more than the
// rescuer can spare after reserving fuel to fly home (hopsHome jumps at
// FuelPerJump each, plus FuelBuffer). Both terms clamp at zero, so a rescuer
// that cannot cover its own trip home returns 0 — the caller then declines the
// transfer rather than stranding itself.
func TransferQuantity(strandeeMaxFuel, strandeeFuel, rescuerFuel, hopsHome int) int {
	need := strandeeMaxFuel - strandeeFuel
	if need < 0 {
		need = 0
	}
	spare := rescuerFuel - (FuelPerJump*hopsHome + FuelBuffer)
	if spare < 0 {
		spare = 0
	}
	if need < spare {
		return need
	}
	return spare
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/rescue/ -run TestTransferQuantity -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/rescue/enrich.go pkg/rescue/enrich_test.go
git commit -m "feat(rescue): TransferQuantity sizes fuel by strandee need and rescuer spare"
```

---

### Task 2: Wire dynamic sizing into `runRescue`

**Files:**
- Modify: `pkg/worker/assist.go` — add `rescuerHome`, `rescueFuelQty`; change `runRescue`'s refuel/done block
- Modify: `pkg/worker/assist_test.go` — update `TestAssistClaimsPendingNearMobileCapital` (now exercises the dynamic path); add two new tests

**Interfaces:**
- Consumes: `rescue.TransferQuantity` (Task 1); existing `assistHomes`, `assistMobileHomes`, `assistResolveMobile`, `navigation.JumpGraphFromConnections`, `navigation.BFSJumps`, `navigation.RouteInf`, `deps.KB.GetConnections`.
- Produces: `func rescuerHome(ctx context.Context, deps AssistDeps) (string, bool)` and `func rescueFuelQty(ctx context.Context, deps AssistDeps, rec rescue.Record) int` — internal to `pkg/worker`.

- [ ] **Step 1: Write the failing tests**

In `pkg/worker/assist_test.go`, first update the existing KB-wired test so the strandee and rescuer have fuel (without it the dynamic path now yields 0 and the record would release instead of completing). Replace the record and client lines in `TestAssistClaimsPendingNearMobileCapital`:

Find:
```go
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 15, Status: rescue.StatusPending,
	}}}
	client := &fakeClient{route: []game.RouteStep{{SystemID: "altais"}}, state: &game.State{}}
```
Replace with:
```go
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 15, Fuel: 0, MaxFuel: 200, Status: rescue.StatusPending,
	}}}
	client := &fakeClient{route: []game.RouteStep{{SystemID: "altais"}}, state: &game.State{Fuel: 120, MaxFuel: 120}}
```

Then append two new tests:

```go
// TestAssistDynamicFuelSizing: with live rescuer fuel and a KB graph, the
// transfer is sized by rescue.TransferQuantity, not the record's flat estimate.
// strand-altais is 1 jump; rescuer has 120 fuel; strandee tank 200 empty.
// spare = 120 - (5*1 + 5) = 110; need = 200 -> transfer 110.
func TestAssistDynamicFuelSizing(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:          "altais",
		Connections: []knowledge.SystemConnection{{SystemID: "strand"}},
	}); err != nil {
		t.Fatal(err)
	}
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 10, Fuel: 0, MaxFuel: 200,
		Status: rescue.StatusClaimed, ClaimedBy: "assist-frontier",
	}}}
	client := &fakeClient{route: []game.RouteStep{{SystemID: "altais"}}, state: &game.State{Fuel: 120, MaxFuel: 120}}
	deps := AssistDeps{
		Client: client, KB: kb, Queue: q, Out: io.Discard,
		AgentID: "assist-frontier", HomeStation: "mobile_capital",
		Navigate: func(ctx context.Context, system, poi string) error { return nil },
	}
	if err := Assist(ctx, deps); err != nil {
		t.Fatal(err)
	}
	if got := client.refuelShipCalls; len(got) != 1 || got[0].quantity != 110 {
		t.Fatalf("RefuelShip calls = %+v, want one call of quantity 110", got)
	}
	if q.recs[0].Status != rescue.StatusDone || q.recs[0].RescueFuel != 110 {
		t.Fatalf("record = %+v, want done with RescueFuel recorded as 110", q.recs[0])
	}
}

// TestAssistReleasesWhenCannotSpare: a low-fuel rescuer that cannot give any
// fuel without eating its trip home refuses the transfer and returns the claim
// to pending (ClaimedBy cleared) instead of stranding itself.
func TestAssistReleasesWhenCannotSpare(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:          "altais",
		Connections: []knowledge.SystemConnection{{SystemID: "strand"}},
	}); err != nil {
		t.Fatal(err)
	}
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 10, Fuel: 0, MaxFuel: 200,
		Status: rescue.StatusClaimed, ClaimedBy: "assist-frontier",
	}}}
	// spare = 8 - (5*1 + 5) -> clamps to 0, so no transfer.
	client := &fakeClient{route: []game.RouteStep{{SystemID: "altais"}}, state: &game.State{Fuel: 8, MaxFuel: 120}}
	deps := AssistDeps{
		Client: client, KB: kb, Queue: q, Out: io.Discard,
		AgentID: "assist-frontier", HomeStation: "mobile_capital",
		Navigate: func(ctx context.Context, system, poi string) error { return nil },
	}
	if err := Assist(ctx, deps); err != nil {
		t.Fatal(err)
	}
	if len(client.refuelShipCalls) != 0 {
		t.Fatalf("rescuer that cannot spare must not refuel, got %+v", client.refuelShipCalls)
	}
	if q.recs[0].Status != rescue.StatusPending || q.recs[0].ClaimedBy != "" {
		t.Fatalf("record = %+v, want pending with ClaimedBy cleared", q.recs[0])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestAssistDynamicFuelSizing|TestAssistReleasesWhenCannotSpare|TestAssistClaimsPendingNearMobileCapital' -v`
Expected: FAIL — `TestAssistDynamicFuelSizing` and `TestAssistReleasesWhenCannotSpare` fail because `runRescue` still transfers `rec.RescueFuel` (e.g. quantity 10, record marked done) rather than the dynamic amount / release.

- [ ] **Step 3: Add the sizing helpers**

In `pkg/worker/assist.go`, add these two functions (place after `runRescue`, before `assistEnsureHome`):

```go
// rescuerHome returns this assist agent's home system id: the static capital,
// or the current location of its mobile capital. ok is false when neither is
// configured or the mobile home cannot be resolved this pass.
func rescuerHome(ctx context.Context, deps AssistDeps) (string, bool) {
	if home, ok := assistHomes[deps.AgentID]; ok {
		return home, true
	}
	if poi, mobile := assistMobileHomes[deps.AgentID]; mobile {
		if home, err := assistResolveMobile(ctx, deps, poi); err == nil {
			return home, true
		}
	}
	return "", false
}

// rescueFuelQty computes how much fuel to transfer to the strandee: fill its
// tank, capped by what the rescuer can spare after reserving its trip home
// (rescue.TransferQuantity). Falls back to the record's enqueue-time
// RescueFuel estimate when live state, the KB, or the home route is
// unavailable, so a transfer is never blocked on missing data.
func rescueFuelQty(ctx context.Context, deps AssistDeps, rec rescue.Record) int {
	st := deps.Client.GetState()
	if st == nil || deps.KB == nil {
		return rec.RescueFuel
	}
	home, ok := rescuerHome(ctx, deps)
	if !ok {
		return rec.RescueFuel
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return rec.RescueFuel
	}
	graph := navigation.JumpGraphFromConnections(conns)
	dist := navigation.BFSJumps(graph, rec.SystemID, []string{home})
	hops, ok := dist[home]
	if !ok || hops >= navigation.RouteInf {
		return rec.RescueFuel
	}
	return rescue.TransferQuantity(int(rec.MaxFuel), int(rec.Fuel), int(st.Fuel), hops)
}
```

- [ ] **Step 4: Rewire `runRescue`'s refuel/done block**

In `pkg/worker/assist.go` `runRescue`, find:

```go
	if err := deps.Client.RefuelShip(ctx, rec.TargetUsername, rec.RescueFuel); err != nil {
		return fail("refuel", err)
	}
	if ok, err := deps.Queue.Transition(rec.AgentID, rescue.StatusClaimed, rescue.StatusDone, nil); err != nil || !ok {
		fmt.Fprintf(deps.Out, "assist: mark done %s: ok=%v err=%v\n", rec.AgentID, ok, err) //nolint:errcheck
	}
	fmt.Fprintf(deps.Out, "assist: rescued %s (+%d fuel to %s)\n", rec.AgentID, rec.RescueFuel, rec.TargetUsername) //nolint:errcheck
	return assistEnsureHome(ctx, deps)
```

Replace with:

```go
	qty := rescueFuelQty(ctx, deps, rec)
	if qty <= 0 {
		// The rescuer cannot spare fuel without risking its own trip home (or
		// the strandee is already full). Release the claim so a fuller or
		// nearer rescuer takes it, and head home to re-tank.
		fmt.Fprintf(deps.Out, "assist: rescue %s: nothing to spare after home reserve; releasing claim\n", rec.AgentID) //nolint:errcheck
		if _, err := deps.Queue.Transition(rec.AgentID, rescue.StatusClaimed, rescue.StatusPending,
			func(r *rescue.Record) { r.ClaimedBy = "" }); err != nil {
			fmt.Fprintf(deps.Out, "assist: release %s: %v\n", rec.AgentID, err) //nolint:errcheck
		}
		return assistEnsureHome(ctx, deps)
	}
	if err := deps.Client.RefuelShip(ctx, rec.TargetUsername, qty); err != nil {
		return fail("refuel", err)
	}
	if ok, err := deps.Queue.Transition(rec.AgentID, rescue.StatusClaimed, rescue.StatusDone,
		func(r *rescue.Record) { r.RescueFuel = qty }); err != nil || !ok {
		fmt.Fprintf(deps.Out, "assist: mark done %s: ok=%v err=%v\n", rec.AgentID, ok, err) //nolint:errcheck
	}
	fmt.Fprintf(deps.Out, "assist: rescued %s (+%d fuel to %s)\n", rec.AgentID, qty, rec.TargetUsername) //nolint:errcheck
	return assistEnsureHome(ctx, deps)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestAssist' -v`
Expected: PASS — the new dynamic + release tests pass, and every existing `TestAssist*` (including the no-KB fallback test asserting quantity 15) still passes.

- [ ] **Step 6: Build, full test, lint**

Run: `go build ./... && go test ./pkg/rescue/ ./pkg/worker/ && golangci-lint run pkg/rescue/... pkg/worker/...`
Expected: build clean, tests pass, no new lint findings.

- [ ] **Step 7: Commit**

```bash
git add pkg/worker/assist.go pkg/worker/assist_test.go
git commit -m "feat(worker/assist): size rescue fuel to strandee tank, capped by rescuer spare

Replaces the flat ~10-fuel transfer that looped big-tank haulers.
Falls back to the enqueue estimate when live state/KB is missing;
releases the claim when the rescuer cannot spare fuel for its trip home."
```

---

## Notes for the implementer

- The enqueue-side sizing in `cmd/overmind/rescueops.go` / `rescue.FuelForSystem` is intentionally left unchanged — it now serves as the `rescueFuelQty` fallback value (`rec.RescueFuel`).
- Do not touch the strandee's own refuel behavior — the "why doesn't a stranded hauler self-refuel at a station" question is out of scope for this plan.
- The done-transition now records the actual transferred amount into `RescueFuel`, so `rescue-history.jsonl` will reflect real transfer sizes instead of the flat estimate.
