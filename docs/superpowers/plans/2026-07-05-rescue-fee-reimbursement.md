# Rescue-Fee Reimbursement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After a real fuel rescue, the rescued worker reimburses its rescuer with a flat credit gift on its next docked pass, so assist-fleet rescuers stop going broke.

**Architecture:** The overmind records a debt (`{recipient, credits}`) to `data/agents/<strandee>/rescue-debts.json` when a rescue record goes `done` and actually spent an assister's fuel. The rescued worker, on each idle standing-loop pass, pays the head debt via `send_gift` when docked. Three units: pure `pkg/rescue` file helpers, the overmind write hook, and the worker pay hook.

**Tech Stack:** Go 1.24+; existing `pkg/rescue`, `pkg/worker`, `cmd/overmind`, `cmd/worker`. `send_gift` server command via `game.GameClient.SendGift`.

## Global Constraints

- Go 1.24+; use modern features where natural.
- New code must pass `golangci-lint` (repo config) with no new findings.
- Run `go build ./...` and `go test ./...` before committing.
- **Never run `git add -A`** — live fleet runtime data churns in the working tree (many modified `data/**` files that are NOT yours). Stage only the exact files each task names.
- A `pkg/actionspace` test failure (`TestLoadFromOpenAPIContainsAllHardcoded`, server_docs drift) is KNOWN PRE-EXISTING and unrelated — ignore it.
- `send_gift` with `credits` requires the **sender** docked at a base; recipient is async (no co-location, no online). Gate payment on `state.Doc`.
- Fee is a flat int, default **1000**, configurable via the overmind `--rescue-fee` flag; `0` disables.
- Only real assister rescues owe a fee: `ClaimedBy != "" && RescueFuel > 0` (excludes tows, operator-manual done-flips, skip-and-release).

---

### Task 1: `pkg/rescue` debt file helpers

**Files:**
- Create: `pkg/rescue/debt.go`
- Test: `pkg/rescue/debt_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type Debt struct { Recipient string `json:"recipient"`; Credits int `json:"credits"` }`
  - `func LoadDebts(agentsDir, strandeeID string) ([]Debt, error)` — missing file → `(nil, nil)`.
  - `func AppendDebt(agentsDir, strandeeID string, d Debt) error` — load, append, write.
  - `func RemoveHead(agentsDir, strandeeID string) error` — drop `[0]` and rewrite; remove the file when it empties.
  - (Tasks 2 and 3 call these.)

- [ ] **Step 1: Write the failing test**

Create `pkg/rescue/debt_test.go`:

```go
package rescue

import (
	"path/filepath"
	"testing"
)

func TestDebtRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Missing file -> empty, no error.
	got, err := LoadDebts(dir, "salvager-10")
	if err != nil || len(got) != 0 {
		t.Fatalf("LoadDebts on missing = (%v, %v), want (nil, nil)", got, err)
	}
	// Append accumulates in order.
	if err := AppendDebt(dir, "salvager-10", Debt{Recipient: "shipside_assist_haven", Credits: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := AppendDebt(dir, "salvager-10", Debt{Recipient: "shipside_assist_sol", Credits: 1000}); err != nil {
		t.Fatal(err)
	}
	got, err = LoadDebts(dir, "salvager-10")
	if err != nil || len(got) != 2 || got[0].Recipient != "shipside_assist_haven" || got[1].Recipient != "shipside_assist_sol" {
		t.Fatalf("after 2 appends = %+v (err %v)", got, err)
	}
	// RemoveHead pops the first.
	if err := RemoveHead(dir, "salvager-10"); err != nil {
		t.Fatal(err)
	}
	got, _ = LoadDebts(dir, "salvager-10")
	if len(got) != 1 || got[0].Recipient != "shipside_assist_sol" {
		t.Fatalf("after RemoveHead = %+v, want [sol]", got)
	}
	// RemoveHead on the last entry removes the file (LoadDebts -> empty).
	if err := RemoveHead(dir, "salvager-10"); err != nil {
		t.Fatal(err)
	}
	if got, _ = LoadDebts(dir, "salvager-10"); len(got) != 0 {
		t.Fatalf("after draining = %+v, want empty", got)
	}
	// RemoveHead on an already-empty/missing file is a no-op, no error.
	if err := RemoveHead(dir, "salvager-10"); err != nil {
		t.Fatalf("RemoveHead on missing = %v, want nil", err)
	}
	_ = filepath.Join // keep import if unused after edits
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/rescue/ -run TestDebtRoundTrip -v`
Expected: FAIL — `undefined: LoadDebts` / `Debt` / `AppendDebt` / `RemoveHead`.

- [ ] **Step 3: Write the implementation**

Create `pkg/rescue/debt.go`:

```go
package rescue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Debt is one owed rescue-fee reimbursement: the strandee gifts Credits to the
// rescuer's in-game username Recipient once it is next docked.
type Debt struct {
	Recipient string `json:"recipient"`
	Credits   int    `json:"credits"`
}

// debtPath is the strandee's outstanding-debt file.
func debtPath(agentsDir, strandeeID string) string {
	return filepath.Join(agentsDir, strandeeID, "rescue-debts.json")
}

// LoadDebts reads a strandee's outstanding rescue debts. A missing file is not
// an error — it means no debts.
func LoadDebts(agentsDir, strandeeID string) ([]Debt, error) {
	b, err := os.ReadFile(debtPath(agentsDir, strandeeID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rescue: read debts %s: %w", strandeeID, err)
	}
	var debts []Debt
	if err := json.Unmarshal(b, &debts); err != nil {
		return nil, fmt.Errorf("rescue: parse debts %s: %w", strandeeID, err)
	}
	return debts, nil
}

// writeDebts writes the list, or removes the file when the list is empty.
func writeDebts(agentsDir, strandeeID string, debts []Debt) error {
	p := debtPath(agentsDir, strandeeID)
	if len(debts) == 0 {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rescue: clear debts %s: %w", strandeeID, err)
		}
		return nil
	}
	b, err := json.Marshal(debts)
	if err != nil {
		return fmt.Errorf("rescue: marshal debts %s: %w", strandeeID, err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return fmt.Errorf("rescue: write debts %s: %w", strandeeID, err)
	}
	return nil
}

// AppendDebt adds one debt to the strandee's list, creating the file if absent.
func AppendDebt(agentsDir, strandeeID string, d Debt) error {
	debts, err := LoadDebts(agentsDir, strandeeID)
	if err != nil {
		return err
	}
	return writeDebts(agentsDir, strandeeID, append(debts, d))
}

// RemoveHead drops the first debt and rewrites the file (removing it when the
// list empties). A missing/empty file is a no-op.
func RemoveHead(agentsDir, strandeeID string) error {
	debts, err := LoadDebts(agentsDir, strandeeID)
	if err != nil {
		return err
	}
	if len(debts) == 0 {
		return nil
	}
	return writeDebts(agentsDir, strandeeID, debts[1:])
}
```

Remove the trailing `_ = filepath.Join` line from the test if the import is unused (it is — delete the `path/filepath` import and that line).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/rescue/ -run TestDebtRoundTrip -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/rescue/debt.go pkg/rescue/debt_test.go
git commit -m "feat(rescue): per-strandee rescue-debt file helpers"
```

---

### Task 2: Overmind records the debt on rescue completion

**Files:**
- Modify: `cmd/overmind/rescueops.go` — `pollRescues` gains `agentsDir string, fee int`; write the debt in the done-branch.
- Modify: `cmd/overmind/main.go` — add `--rescue-fee` flag; pass `agentsDir` + fee to `pollRescues`.
- Modify: `cmd/overmind/rescueops_test.go` — update 4 `pollRescues` call sites; add debt-gate tests.

**Interfaces:**
- Consumes: `rescue.Debt`, `rescue.AppendDebt` (Task 1); existing `rescue.ResolveUsername(agentsDir, agentID) (string, error)`.
- Produces: new `pollRescues` signature `func pollRescues(logger *log.Logger, sup *supervisor.Supervisor, queue *rescue.Queue, histPath, fleetName, agentsDir string, fee int, snap []supervisor.WorkerInfo)`.

- [ ] **Step 1: Write the failing tests**

First update the 4 existing call sites in `cmd/overmind/rescueops_test.go` — each currently reads:
```go
pollRescues(logger, sup, queue, histPath, "haul", fleet.Snapshot())
```
Change each to (fee 0 disables the fee, preserving existing behavior):
```go
pollRescues(logger, sup, queue, histPath, "haul", t.TempDir(), 0, fleet.Snapshot())
```

Then append two debt-gate tests plus a small local credentials helper. These follow this file's existing construction pattern exactly (`writeQueueRecords` + `rescueTestSpawn` already exist in the file; `supervisor.NewFleet`/`ApplyHello`/`ApplyStatus`/`Quarantine`/`NewSupervisor` as in `TestPollRescuesArchivesDoneAndReleases`). Required imports already present in the file: `os`, `path/filepath`, `log`, `io`, `sync/atomic`, `time`, `context`, `github.com/rsned/spacemolt/pkg/rescue`, `.../pkg/overmind/supervisor`, `.../pkg/overmind/control`.

```go
// writeTestCredentials writes the minimal credentials.json ResolveUsername reads.
func writeTestCredentials(t *testing.T, agentsDir, agentID, username string) {
	t.Helper()
	d := filepath.Join(agentsDir, agentID)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "credentials.json"),
		[]byte(`{"username":"`+username+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPollRescuesWritesDebtOnRealRescue: a done record that actually spent an
// assister's fuel records a fee debt naming the rescuer's in-game username.
func TestPollRescuesWritesDebtOnRealRescue(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "q.json")
	queue := rescue.NewQueue(queuePath)
	histPath := filepath.Join(dir, "history.jsonl")
	agentsDir := filepath.Join(dir, "agents")
	writeTestCredentials(t, agentsDir, "assist-haven", "shipside_assist_haven")
	writeQueueRecords(t, queuePath, []rescue.Record{
		{AgentID: "salvager-10", Fleet: "haul", Status: rescue.StatusDone, RescueFuel: 110, ClaimedBy: "assist-haven"},
	})

	fleet := supervisor.NewFleet()
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "salvager-10", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("salvager-10", control.Status{}, now)
	fleet.Quarantine("salvager-10", "fuel-dead: stalled")

	var spawned atomic.Int32
	specs := []supervisor.WorkerSpec{{AgentID: "salvager-10"}}
	sup := supervisor.NewSupervisor(nil, fleet, specs, rescueTestSpawn(&spawned), log.New(io.Discard, "", 0))
	logger := log.New(io.Discard, "", 0)

	pollRescues(logger, sup, queue, histPath, "haul", agentsDir, 1000, fleet.Snapshot())

	debts, err := rescue.LoadDebts(agentsDir, "salvager-10")
	if err != nil || len(debts) != 1 || debts[0].Recipient != "shipside_assist_haven" || debts[0].Credits != 1000 {
		t.Fatalf("debts = %+v (err %v), want one 1000cr debt to shipside_assist_haven", debts, err)
	}
}

// TestPollRescuesNoDebtForTowOrManual: a done record with no rescuer (server
// tow / operator flip) or zero fuel owes no fee.
func TestPollRescuesNoDebtForTowOrManual(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "q.json")
	queue := rescue.NewQueue(queuePath)
	histPath := filepath.Join(dir, "history.jsonl")
	agentsDir := filepath.Join(dir, "agents")
	writeQueueRecords(t, queuePath, []rescue.Record{
		{AgentID: "trader-1", Fleet: "haul", Status: rescue.StatusDone, ClaimedBy: "", RescueFuel: 0},
	})

	fleet := supervisor.NewFleet()
	now := time.Now()
	fleet.ApplyHello(control.Hello{AgentID: "trader-1", Role: "hauler"}, 1, now)
	fleet.ApplyStatus("trader-1", control.Status{}, now)
	fleet.Quarantine("trader-1", "fuel-dead: stalled")

	var spawned atomic.Int32
	specs := []supervisor.WorkerSpec{{AgentID: "trader-1"}}
	sup := supervisor.NewSupervisor(nil, fleet, specs, rescueTestSpawn(&spawned), log.New(io.Discard, "", 0))
	logger := log.New(io.Discard, "", 0)

	pollRescues(logger, sup, queue, histPath, "haul", agentsDir, 1000, fleet.Snapshot())

	if debts, _ := rescue.LoadDebts(agentsDir, "trader-1"); len(debts) != 0 {
		t.Fatalf("no-rescuer record must owe no fee, got %+v", debts)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/overmind/ -run 'TestPollRescues' -v`
Expected: FAIL — `pollRescues` still has the old signature / does not write debts (compile error on the new call arity is the first failure; fix the call sites in Step 1, then the behavior assertions fail).

- [ ] **Step 3: Change the `pollRescues` signature and write the debt**

In `cmd/overmind/rescueops.go`, change the signature:
```go
func pollRescues(logger *log.Logger, sup *supervisor.Supervisor, queue *rescue.Queue, histPath, fleetName, agentsDir string, fee int, snap []supervisor.WorkerInfo) {
```

In the done-branch, insert the debt write **before** `archiveRescue`. Replace:
```go
		if rec.Fleet == fleetName && rec.Status == rescue.StatusDone {
			archiveRescue(logger, queue, histPath, w.AgentID)
			sup.ReleaseQuarantine(w.AgentID)
			logger.Printf("rescue: %s rescued (+%d fuel by %s); rejoining fleet", w.AgentID, rec.RescueFuel, rec.ClaimedBy)
		}
```
with:
```go
		if rec.Fleet == fleetName && rec.Status == rescue.StatusDone {
			// Reimburse the rescuer, but only when an assister actually spent
			// fuel: tows, operator-manual done-flips, and skip-and-release
			// (RescueFuel 0 / no ClaimedBy) owe nothing.
			if fee > 0 && rec.ClaimedBy != "" && rec.RescueFuel > 0 {
				if recipient, err := rescue.ResolveUsername(agentsDir, rec.ClaimedBy); err != nil {
					logger.Printf("rescue: fee recipient for %s (rescuer %s): %v; skipping fee", w.AgentID, rec.ClaimedBy, err)
				} else if err := rescue.AppendDebt(agentsDir, w.AgentID, rescue.Debt{Recipient: recipient, Credits: fee}); err != nil {
					logger.Printf("rescue: record fee debt for %s: %v", w.AgentID, err)
				} else {
					logger.Printf("rescue: %s owes %d cr fee to %s (%s)", w.AgentID, fee, recipient, rec.ClaimedBy)
				}
			}
			archiveRescue(logger, queue, histPath, w.AgentID)
			sup.ReleaseQuarantine(w.AgentID)
			logger.Printf("rescue: %s rescued (+%d fuel by %s); rejoining fleet", w.AgentID, rec.RescueFuel, rec.ClaimedBy)
		}
```

- [ ] **Step 4: Add the flag and update the call in `cmd/overmind/main.go`**

Add the flag alongside the other rescue flags (near `rescueHistPath`, line ~42):
```go
	rescueFee := flag.Int("rescue-fee", 1000, "Flat credit fee a rescued worker gifts its rescuer on its next dock (0 disables)")
```
Update the `pollRescues` call (line ~184) to:
```go
			pollRescues(logger, sup, queue, *rescueHistPath, *fleetName, "data/agents", *rescueFee, snap)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./cmd/overmind/ -run 'TestPollRescues|Rescue' -v`
Expected: PASS — new debt-gate tests pass; all existing rescueops tests still pass (they pass fee 0).

- [ ] **Step 6: Build + lint**

Run: `go build ./... && golangci-lint run cmd/overmind/... pkg/rescue/...`
Expected: clean, no new findings.

- [ ] **Step 7: Commit**

```bash
git add cmd/overmind/rescueops.go cmd/overmind/main.go cmd/overmind/rescueops_test.go
git commit -m "feat(overmind): record a rescue-fee debt when an assister spends fuel"
```

---

### Task 3: Worker pays the debt on its next docked pass

**Files:**
- Create: `pkg/worker/rescue_fee.go`
- Test: `pkg/worker/rescue_fee_test.go`
- Modify: `pkg/worker/standing.go` — call the pay hook each idle pass.
- Modify: `cmd/worker/main.go` — bind the hook to the live client.

**Interfaces:**
- Consumes: `rescue.LoadDebts`, `rescue.RemoveHead` (Task 1); `game.State` (`Doc bool`); `game.GameClient.SendGift`.
- Produces:
  - `type GiftClient interface { GetState() *game.State; SendGift(ctx context.Context, payload map[string]any) error }`
  - `func PayRescueDebt(ctx context.Context, c GiftClient, out io.Writer, agentsDir, agentID string)`
  - New `StandingDeps` field `PayDebts func(context.Context)` (nil-safe; called each non-drained idle pass under `ExecMu`).

- [ ] **Step 1: Write the failing test**

Create `pkg/worker/rescue_fee_test.go`:

```go
package worker

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/rescue"
)

type fakeGiftClient struct {
	state *game.State
	gifts []map[string]any
	err   error
}

func (f *fakeGiftClient) GetState() *game.State { return f.state }
func (f *fakeGiftClient) SendGift(_ context.Context, payload map[string]any) error {
	if f.err != nil {
		return f.err
	}
	f.gifts = append(f.gifts, payload)
	return nil
}

func TestPayRescueDebt(t *testing.T) {
	ctx := context.Background()

	// Docked + one debt -> gift sent with recipient+credits, debt removed.
	t.Run("docked pays and clears", func(t *testing.T) {
		dir := t.TempDir()
		if err := rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000}); err != nil {
			t.Fatal(err)
		}
		c := &fakeGiftClient{state: &game.State{Doc: true}}
		PayRescueDebt(ctx, c, io.Discard, dir, "salvager-10")
		if len(c.gifts) != 1 || c.gifts[0]["recipient"] != "shipside_assist_haven" || c.gifts[0]["credits"] != 1000 {
			t.Fatalf("gifts = %+v, want one gift of 1000 to shipside_assist_haven", c.gifts)
		}
		if debts, _ := rescue.LoadDebts(dir, "salvager-10"); len(debts) != 0 {
			t.Fatalf("debt must be cleared, got %+v", debts)
		}
	})

	// Undocked -> no gift, debt retained (send_gift credits needs a station).
	t.Run("undocked skips", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		c := &fakeGiftClient{state: &game.State{Doc: false}}
		PayRescueDebt(ctx, c, io.Discard, dir, "salvager-10")
		if len(c.gifts) != 0 {
			t.Fatalf("undocked must not gift, got %+v", c.gifts)
		}
		if debts, _ := rescue.LoadDebts(dir, "salvager-10"); len(debts) != 1 {
			t.Fatalf("undocked must retain debt, got %+v", debts)
		}
	})

	// No debts -> no-op.
	t.Run("no debts noop", func(t *testing.T) {
		dir := t.TempDir()
		c := &fakeGiftClient{state: &game.State{Doc: true}}
		PayRescueDebt(ctx, c, io.Discard, dir, "salvager-10")
		if len(c.gifts) != 0 {
			t.Fatalf("no debts must not gift, got %+v", c.gifts)
		}
	})

	// Gift error -> debt retained for next pass.
	t.Run("gift error retains debt", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		c := &fakeGiftClient{state: &game.State{Doc: true}, err: errors.New("not docked at storage")}
		PayRescueDebt(ctx, c, io.Discard, dir, "salvager-10")
		if debts, _ := rescue.LoadDebts(dir, "salvager-10"); len(debts) != 1 {
			t.Fatalf("failed gift must retain debt, got %+v", debts)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run TestPayRescueDebt -v`
Expected: FAIL — `undefined: PayRescueDebt` / `GiftClient`.

- [ ] **Step 3: Write `PayRescueDebt`**

Create `pkg/worker/rescue_fee.go`:

```go
package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/rescue"
)

// GiftClient is the slice of game.GameClient the rescue-fee payment needs.
type GiftClient interface {
	GetState() *game.State
	SendGift(ctx context.Context, payload map[string]any) error
}

// PayRescueDebt pays the head outstanding rescue debt for agentID when the
// worker is docked (send_gift credits requires a base with storage). One debt
// per call respects the 1-gift-per-tick rate limit; the rest wait for the next
// docked pass. Best-effort: every failure logs and leaves the debt in place.
func PayRescueDebt(ctx context.Context, c GiftClient, out io.Writer, agentsDir, agentID string) {
	debts, err := rescue.LoadDebts(agentsDir, agentID)
	if err != nil {
		fmt.Fprintf(out, "rescue-fee: load debts: %v\n", err) //nolint:errcheck
		return
	}
	if len(debts) == 0 {
		return
	}
	st := c.GetState()
	if st == nil || !st.Doc {
		return // pay on a later pass once docked
	}
	d := debts[0]
	payload := map[string]any{
		"recipient": d.Recipient,
		"credits":   d.Credits,
		"message":   "rescue fuel reimbursement",
	}
	if err := c.SendGift(ctx, payload); err != nil {
		fmt.Fprintf(out, "rescue-fee: gift %d to %s: %v (retrying next pass)\n", d.Credits, d.Recipient, err) //nolint:errcheck
		return
	}
	if err := rescue.RemoveHead(agentsDir, agentID); err != nil {
		fmt.Fprintf(out, "rescue-fee: clear paid debt: %v\n", err) //nolint:errcheck
	}
	fmt.Fprintf(out, "rescue-fee: paid %d cr to %s\n", d.Credits, d.Recipient) //nolint:errcheck
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/worker/ -run TestPayRescueDebt -v`
Expected: PASS

- [ ] **Step 5: Wire the hook into the standing loop**

In `pkg/worker/standing.go`, add a field to the `StandingDeps` struct (near the task-hook fields around line 43):
```go
	// PayDebts, when set, runs once per non-drained idle pass under ExecMu to
	// pay any outstanding rescue-fee debt. nil for workers with no fee wiring.
	PayDebts func(context.Context)
```

In the idle loop, call it immediately after acquiring `ExecMu` (it uses the game conn, so it must be serialized like other work). Change:
```go
		deps.ExecMu.Lock()
		if task := deps.nextTask(); task != nil {
```
to:
```go
		deps.ExecMu.Lock()
		if deps.PayDebts != nil {
			deps.PayDebts(ctx)
		}
		if task := deps.nextTask(); task != nil {
```

- [ ] **Step 6: Bind the hook in `cmd/worker/main.go`**

Where `StandingDeps` is constructed (around line 279-289), add the binding using the live `client` and `*agentID`:
```go
					PayDebts: func(c context.Context) {
						worker.PayRescueDebt(c, client, os.Stdout, filepath.Join("data", "agents"), *agentID)
					},
```
(`client` is the full `*game.Client` already in scope; `filepath` and `os` are already imported in this file.)

- [ ] **Step 7: Build, full test, lint**

Run: `go build ./... && go test ./pkg/rescue/ ./pkg/worker/ ./cmd/overmind/ ./cmd/worker/ && golangci-lint run pkg/worker/... cmd/worker/...`
Expected: build clean, tests pass, no new lint findings.

- [ ] **Step 8: Commit**

```bash
git add pkg/worker/rescue_fee.go pkg/worker/rescue_fee_test.go pkg/worker/standing.go cmd/worker/main.go
git commit -m "feat(worker): pay the rescue-fee debt via send_gift on the next docked pass"
```

---

## Notes for the implementer

- The hook is role-agnostic on purpose: `RunStanding` is the universal standing loop, so any rescued worker (hauler, assister, shuttle) pays. No per-role wiring.
- `send_gift` recipient is async — the assister need not be online or co-located; only the paying worker must be docked (the `state.Doc` gate).
- Do not touch the fuel-sizing logic shipped earlier today, and do not change the rescue queue's state machine — the debt is a separate sidecar file.
