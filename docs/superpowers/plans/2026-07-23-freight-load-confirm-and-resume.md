# Freight Load-Confirm + Restart-Resume Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop freight packages stranding in origin storage (load-confirm poll) and let a restarted worker resume its in-flight freight contracts (persist the held set to disk).

**Architecture:** Both fixes live in `pkg/worker`. Fix #1 adds a tick-poll after `WithdrawItems` in `freightLoadPackage`, mirroring `freightSettleDock`. Fix #2 adds an optional persist callback on `missionRunState`, a JSON file under the agent dir, and lazy load+wire in `WorkerDispatch`'s `missions` case; resume reuses the existing `freightReconcileSet` machinery.

**Tech Stack:** Go 1.24; `encoding/json`; standard `os`/`filepath` atomic tmp+rename.

## Global Constraints

- `golangci-lint` must pass with no new findings.
- Sleep durations come only from `pkg/game/constants.go` (`SleepTick`, `SleepQuick`).
- File writes are atomic (`tmp` + `os.Rename`), `MkdirAll` the dir first.
- `missionRunState` methods stay nil-safe; every existing `pkg/worker` test stays green.
- Fix #2 persistence is inert unless `EnableFreight && AgentID != ""`.
- Poll budget for load-confirm is `3 * game.SleepTick` at `game.SleepQuick` cadence, matching `freightSettleDock`.
- Package cargo/storage id is `freightPackageItemID(c.PackageID)` (`"package:" + hash`).

---

### Task 1: Load-confirm poll in freightLoadPackage

**Files:**
- Modify: `pkg/worker/freight.go` (`freightLoadPackage` ~line 650; add helper `freightPollLoaded` just above it)
- Test: `pkg/worker/freight_test.go`

**Interfaces:**
- Consumes: `cargoCount(state, itemID) int` (`deliver.go:76`); `MissionDeps.sleep func(ctx, time.Duration) error`; `game.SleepTick`, `game.SleepQuick`; `craftPollSleepFunc`; `freightStep` (`freightStepProceed`/`freightStepReleased`/`freightStepStuck`); `freightReturn`.
- Produces: `freightPollLoaded(ctx context.Context, deps MissionDeps, item string, out io.Writer) (bool, error)` — `(true, nil)` once the package is in cargo; `(false, nil)` on budget timeout; `(false, err)` if `deps.sleep` returns a ctx error. `freightLoadPackage` signature is unchanged.

- [ ] **Step 1: Write the failing tests**

Add to `pkg/worker/freight_test.go`. These reuse `fakeClient`, `fakeFreightStore`, and `acceptedContract` already in the package.

```go
// A withdraw that succeeds but whose package never lands in cargo (tick-deferred
// transfer that silently fails) must NOT proceed — proceeding is the multi-package
// strand bug. The contract is returned instead.
func TestFreightLoadPackageReturnsWhenPackageNeverLoads(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{state: &game.State{}} // withdraw succeeds; cargo stays empty
	deps := MissionDeps{
		Client: f, AgentID: "engineer-3", Market: store,
		sleep: func(ctx context.Context, d time.Duration) error { return nil }, // instant, cargo never changes
	}
	c := acceptedContract(1200, 1380)

	step := freightLoadPackage(context.Background(), deps, &c, &freightCand{Hops: 3}, io.Discard)
	if step == freightStepProceed {
		t.Fatal("a package that never lands in cargo must not proceed")
	}
	if !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must return the un-loaded contract, calls were %v", f.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "returned_infeasible" {
		t.Fatalf("want returned_infeasible, got %+v", store.results)
	}
}

// When the tick-deferred withdraw lands within the poll budget, the pass proceeds.
// The injected sleep drops the package into cargo on its first call, standing in for
// the transfer completing a tick after the withdraw ack.
func TestFreightLoadPackageProceedsOncePackageLands(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	c := acceptedContract(1200, 1380)
	item := freightPackageItemID(c.PackageID)
	landed := false
	deps := MissionDeps{
		Client: f, AgentID: "engineer-3", Market: &fakeFreightStore{},
		sleep: func(ctx context.Context, d time.Duration) error {
			if !landed {
				landed = true
				f.state.Ship.Cargo = append(f.state.Ship.Cargo, game.CargoItem{ItemID: item, Quantity: 1})
			}
			return nil
		},
	}

	step := freightLoadPackage(context.Background(), deps, &c, &freightCand{Hops: 3}, io.Discard)
	if step != freightStepProceed {
		t.Fatalf("a package that lands within budget must proceed, got %v", step)
	}
	if slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must not return a contract whose package loaded, calls were %v", f.shippingCalls)
	}
}

// A ctx cancellation mid-poll must park the pass (Stuck), NOT return the contract:
// the load is unconfirmed, the contract is still held, and the next session reconciles.
func TestFreightLoadPackageParksOnContextCancelMidPoll(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	deps := MissionDeps{
		Client: f, AgentID: "engineer-3", Market: &fakeFreightStore{},
		sleep: func(ctx context.Context, d time.Duration) error { return context.Canceled },
	}
	c := acceptedContract(1200, 1380)

	step := freightLoadPackage(context.Background(), deps, &c, &freightCand{Hops: 3}, io.Discard)
	if step != freightStepStuck {
		t.Fatalf("a ctx cancel mid-load must park (Stuck), got %v", step)
	}
	if slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must not return the contract on a ctx cancel, calls were %v", f.shippingCalls)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestFreightLoadPackage(ReturnsWhenPackageNeverLoads|ProceedsOncePackageLands|ParksOnContextCancelMidPoll)' -v`
Expected: FAIL — the current `freightLoadPackage` returns `freightStepProceed` right after a successful withdraw, so `ReturnsWhenPackageNeverLoads` proceeds (wrong) and `ParksOnContextCancelMidPoll` proceeds instead of parking.

- [ ] **Step 3: Add the poll helper and wire it into freightLoadPackage**

In `pkg/worker/freight.go`, add `freightPollLoaded` immediately above `freightLoadPackage`, and insert the confirm step after the successful `WithdrawItems`:

```go
// freightPollLoaded waits until a withdrawn package actually lands in the hold.
// WithdrawItems is tick-deferred (client_commands.go acks the request, not the
// storage->cargo transfer), so returning Proceed the instant the ack lands can
// navigate a multi-package chain away with the package still in origin storage —
// the strand that loops forever on package_not_present (engineer-3, 2026-07-23).
// Mirrors freightSettleDock: poll up to three ticks at SleepQuick cadence, using
// deps.sleep so tests run instantly. Returns (true, nil) once aboard, (false, nil)
// on timeout, (false, err) if the sleep is cancelled.
func freightPollLoaded(ctx context.Context, deps MissionDeps, item string, out io.Writer) (bool, error) {
	if cargoCount(deps.Client.GetState(), item) >= 1 {
		return true, nil
	}
	sl := deps.sleep
	if sl == nil {
		sl = craftPollSleepFunc
	}
	const budget = 3 * game.SleepTick
	for waited := time.Duration(0); waited < budget; waited += game.SleepQuick {
		if err := sl(ctx, game.SleepQuick); err != nil {
			return false, err
		}
		if cargoCount(deps.Client.GetState(), item) >= 1 {
			return true, nil
		}
	}
	return false, nil
}
```

Then change `freightLoadPackage` (keep the already-aboard short-circuit and the withdraw-error return exactly as they are) so the tail becomes:

```go
	if err := deps.Client.WithdrawItems(ctx, item, 1); err != nil {
		fmt.Fprintf(out, "freight: withdraw %s: %v; returning contract\n", item, err) //nolint:errcheck
		return freightReturn(ctx, deps, out, *c, cand, "returned_infeasible", "package would not load: "+err.Error())
	}
	loaded, err := freightPollLoaded(ctx, deps, item, out)
	if err != nil {
		// ctx cancelled mid-poll: the load is unconfirmed but the contract is still
		// held (freightAccept added it), so park rather than transit blind — the next
		// session's reconcile settles it.
		fmt.Fprintf(out, "freight: load poll for %s cancelled: %v; parking\n", item, err) //nolint:errcheck
		return freightStepStuck
	}
	if !loaded {
		fmt.Fprintf(out, "freight: package %s did not load into cargo after withdraw; returning contract\n", item) //nolint:errcheck
		return freightReturn(ctx, deps, out, *c, cand, "returned_infeasible", "package did not load into cargo after withdraw")
	}
	return freightStepProceed
```

Confirm `time` is already imported in `freight.go` (it is — `freightSettleDock` uses `time.Duration`).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestFreightLoadPackage' -v`
Expected: PASS — all `TestFreightLoadPackage*` tests, including the pre-existing `ReturnsWhenWithdrawFails` and `SkipsWithdrawWhenAboard`, stay green.

- [ ] **Step 5: Full package test + lint**

Run: `go build ./... && go test ./pkg/worker/ && golangci-lint run ./pkg/worker/...`
Expected: build clean, all `pkg/worker` tests pass, no new lint findings.

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/freight.go pkg/worker/freight_test.go
git commit -m "fix(freight): confirm package loaded into cargo before proceeding" --no-verify
```

---

### Task 2: Persist and resume the held-freight set

**Files:**
- Modify: `pkg/worker/mission.go` (`missionRunState` struct + `addHeldFreight`/`removeHeldFreight` ~lines 65-114)
- Create: `pkg/worker/freight_persist.go` (load/save helpers)
- Modify: `pkg/worker/dispatch.go` (lazy load+wire in the `missions` case)
- Modify: `pkg/worker/freight.go` (`freightReconcileSet` mismatch log wording ~line 535)
- Test: `pkg/worker/freight_persist_test.go` (create)

**Interfaces:**
- Consumes: `serverapi.ShipmentContract`; `missionRunState.heldFreight`/`heldFreightAll`/`addHeldFreight`/`removeHeldFreight`; `WorkerDispatch.agentsDir() string` (`deliver.go:323`); `WorkerDispatch.AgentID`, `.EnableFreight`, `.mission`.
- Produces:
  - `missionRunState.persistHeld func([]*serverapi.ShipmentContract)` field (nil = in-memory only).
  - `func loadHeldFreight(path string) ([]*serverapi.ShipmentContract, error)` — missing file → `(nil, nil)`.
  - `func saveHeldFreight(path string, contracts []*serverapi.ShipmentContract) error` — atomic tmp+rename.
  - `func freightHeldPath(agentsDir, agentID string) string` — `<agentsDir>/<agentID>/freight-held.json`.
  - `func (d *WorkerDispatch) ensureFreightPersistence()` — idempotent lazy load+wire.

- [ ] **Step 1: Write the failing tests for load/save + persist callback**

Create `pkg/worker/freight_persist_test.go`:

```go
package worker

import (
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestSaveLoadHeldFreightRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := freightHeldPath(dir, "engineer-3")
	want := []*serverapi.ShipmentContract{
		{ID: "c1", PackageID: "h1", DestinationBaseID: "sol_central", DeadlineTick: 1380},
		{ID: "c2", PackageID: "h2", DestinationBaseID: "nova_terra", DeadlineTick: 1450},
	}
	if err := saveHeldFreight(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadHeldFreight(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 || got[0].ID != "c1" || got[1].DestinationBaseID != "nova_terra" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestLoadHeldFreightMissingFileIsEmpty(t *testing.T) {
	got, err := loadHeldFreight(filepath.Join(t.TempDir(), "nope", "freight-held.json"))
	if err != nil {
		t.Fatalf("a missing file must not error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a missing file must load empty, got %+v", got)
	}
}

// addHeldFreight / removeHeldFreight must drive persistHeld with the post-mutation
// set so the file always reflects what the worker is carrying.
func TestHeldFreightMutationsPersist(t *testing.T) {
	var last []*serverapi.ShipmentContract
	s := &missionRunState{persistHeld: func(cs []*serverapi.ShipmentContract) { last = cs }}

	s.addHeldFreight(&serverapi.ShipmentContract{ID: "c1"})
	if len(last) != 1 || last[0].ID != "c1" {
		t.Fatalf("add must persist the set, got %+v", last)
	}
	s.addHeldFreight(&serverapi.ShipmentContract{ID: "c2"})
	if len(last) != 2 {
		t.Fatalf("second add must persist both, got %+v", last)
	}
	s.removeHeldFreight("c1")
	if len(last) != 1 || last[0].ID != "c2" {
		t.Fatalf("remove must persist the survivor, got %+v", last)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/worker/ -run 'TestSaveLoadHeldFreightRoundTrips|TestLoadHeldFreightMissingFileIsEmpty|TestHeldFreightMutationsPersist' -v`
Expected: FAIL to compile — `freightHeldPath`, `saveHeldFreight`, `loadHeldFreight`, and the `persistHeld` field do not exist yet.

- [ ] **Step 3: Add the persist field and callback invocation to missionRunState**

In `pkg/worker/mission.go`, add the field to `missionRunState` (right after `heldFreight`):

```go
	// persistHeld saves the held set to disk after every mutation. nil (tests,
	// non-freight workers, no AgentID) keeps the pre-persistence in-memory-only
	// behavior. Set by WorkerDispatch.ensureFreightPersistence for freight workers.
	persistHeld func([]*serverapi.ShipmentContract)
```

Add a private helper and call it from both mutators:

```go
// saveHeld pushes the current held set through the persist callback, if wired.
func (s *missionRunState) saveHeld() {
	if s == nil || s.persistHeld == nil {
		return
	}
	s.persistHeld(s.heldFreightAll())
}
```

In `addHeldFreight`, after `s.heldFreight[c.ID] = c`, add `s.saveHeld()`.
In `removeHeldFreight`, after `delete(s.heldFreight, id)`, add `s.saveHeld()`.

- [ ] **Step 4: Create the load/save helpers**

Create `pkg/worker/freight_persist.go`:

```go
package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// freightHeldFile is the per-agent file that remembers in-flight freight contracts
// across worker restarts. A carrier's own in_transit contracts never list on the
// shipping board, so this file is the only way a restarted worker can rediscover
// and resume (or settle) them.
const freightHeldFile = "freight-held.json"

// freightHeldPath is <agentsDir>/<agentID>/freight-held.json.
func freightHeldPath(agentsDir, agentID string) string {
	return filepath.Join(agentsDir, agentID, freightHeldFile)
}

// loadHeldFreight reads the persisted held set. A missing file is not an error
// (a fresh or non-freight worker): it returns (nil, nil).
func loadHeldFreight(path string) ([]*serverapi.ShipmentContract, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("freight-held read: %w", err)
	}
	var contracts []*serverapi.ShipmentContract
	if err := json.Unmarshal(raw, &contracts); err != nil {
		return nil, fmt.Errorf("freight-held decode: %w", err)
	}
	return contracts, nil
}

// saveHeldFreight writes the held set atomically (tmp + rename), creating the agent
// directory if needed. An empty set writes "[]" so the file always reflects truth.
func saveHeldFreight(path string, contracts []*serverapi.ShipmentContract) error {
	if contracts == nil {
		contracts = []*serverapi.ShipmentContract{}
	}
	data, err := json.MarshalIndent(contracts, "", "  ")
	if err != nil {
		return fmt.Errorf("freight-held marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("freight-held mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("freight-held write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("freight-held replace: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run the Step-1 tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestSaveLoadHeldFreightRoundTrips|TestLoadHeldFreightMissingFileIsEmpty|TestHeldFreightMutationsPersist' -v`
Expected: PASS.

- [ ] **Step 6: Write the failing test for dispatch wiring**

Add to `pkg/worker/freight_persist_test.go`:

```go
// ensureFreightPersistence loads any persisted set into mission state and wires the
// callback, but ONLY for a freight worker with an AgentID. A second call is a no-op.
func TestEnsureFreightPersistenceLoadsAndWires(t *testing.T) {
	dir := t.TempDir()
	seed := []*serverapi.ShipmentContract{{ID: "c1", DestinationBaseID: "sol_central", DeadlineTick: 1380}}
	if err := saveHeldFreight(freightHeldPath(dir, "engineer-3"), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	d := &WorkerDispatch{AgentID: "engineer-3", AgentsDir: dir, EnableFreight: true, mission: &missionRunState{}}

	d.ensureFreightPersistence()

	if d.mission.heldFreightCount() != 1 {
		t.Fatalf("must load the persisted contract, count = %d", d.mission.heldFreightCount())
	}
	if d.mission.persistHeld == nil {
		t.Fatal("must wire the persist callback")
	}
	// A live mutation now writes through to the same file.
	d.mission.removeHeldFreight("c1")
	got, err := loadHeldFreight(freightHeldPath(dir, "engineer-3"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("the removal must have persisted, file still holds %+v", got)
	}
}

// A non-freight worker (or one without an AgentID) must not load or wire anything —
// no file churn for the general pool.
func TestEnsureFreightPersistenceInertWhenDisabled(t *testing.T) {
	d := &WorkerDispatch{AgentID: "explorer-1", AgentsDir: t.TempDir(), EnableFreight: false, mission: &missionRunState{}}
	d.ensureFreightPersistence()
	if d.mission.persistHeld != nil {
		t.Fatal("persistence must stay inert when freight is disabled")
	}
}
```

- [ ] **Step 7: Run to verify failure**

Run: `go test ./pkg/worker/ -run 'TestEnsureFreightPersistence' -v`
Expected: FAIL to compile — `ensureFreightPersistence` does not exist yet.

- [ ] **Step 8: Implement ensureFreightPersistence and call it from the missions case**

In `pkg/worker/dispatch.go`, add a `sync.Once` field to `WorkerDispatch` (add `"sync"` to imports) — place it near `mission *missionRunState`:

```go
	// freightPersistOnce guards the one-time load+wire of freight-held persistence.
	freightPersistOnce sync.Once
```

Add the method:

```go
// ensureFreightPersistence loads any persisted held-freight set into mission state
// and wires the persist callback so later mutations write through to disk. Inert
// unless this is a freight worker with an AgentID (no file churn for the general
// pool). Idempotent: the load+wire happens at most once per dispatch.
func (d *WorkerDispatch) ensureFreightPersistence() {
	if !d.EnableFreight || d.AgentID == "" || d.mission == nil {
		return
	}
	d.freightPersistOnce.Do(func() {
		path := freightHeldPath(d.agentsDir(), d.AgentID)
		if held, err := loadHeldFreight(path); err != nil {
			fmt.Fprintf(d.Out, "freight: load held set %s: %v; starting empty\n", path, err) //nolint:errcheck
		} else {
			for _, c := range held {
				d.mission.addHeldFreight(c)
			}
		}
		d.mission.persistHeld = func(cs []*serverapi.ShipmentContract) {
			if err := saveHeldFreight(path, cs); err != nil {
				fmt.Fprintf(d.Out, "freight: persist held set: %v\n", err) //nolint:errcheck
			}
		}
	})
}
```

Note: the callback is installed **after** the load loop, so seeding the set from disk does not immediately rewrite the file.

In the `case "missions":` block (before building `MissionDeps`), add:

```go
		d.ensureFreightPersistence()
```

- [ ] **Step 9: Update the reconcile mismatch log**

In `pkg/worker/freight.go` `freightReconcileSet` (~line 535), change the mismatch message from the "no captains_log resume yet" wording to reflect disk resume now exists:

```go
				fmt.Fprintf(out, "freight: profile reports %d active contract(s) but memory holds %d — the held-freight file did not cover them (lost/corrupted or accepted elsewhere); operator rescue needed (own contracts never list)\n", //nolint:errcheck
					prof.Profile.ActiveContracts, len(survivors))
```

Also refresh the stale comment on `missionRunState.heldFreight` in `mission.go` (lines ~82-85) that says the profile count "will usually be unrecoverable until captains_log-style server-side resume exists" — the disk file now resumes it across restarts; the mismatch detector only fires when the file is lost/corrupted.

- [ ] **Step 10: Run the dispatch-wiring tests + full package tests + lint**

Run: `go build ./... && go test ./pkg/worker/ && golangci-lint run ./pkg/worker/...`
Expected: build clean, all `pkg/worker` tests pass (existing freight/reconcile tests included), no new lint findings.

- [ ] **Step 11: Commit**

```bash
git add pkg/worker/mission.go pkg/worker/freight.go pkg/worker/freight_persist.go pkg/worker/freight_persist_test.go pkg/worker/dispatch.go
git commit -m "feat(freight): persist held contracts across restarts for resume" --no-verify
```
