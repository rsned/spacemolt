# Drone Refit Blockers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture what modules are actually fitted to each agent's active ship, and stop `mine_qty` stranding miners fuel-dead — the two code blockers standing between the drone-refit campaign and Phases 1 and 4.

**Architecture:** Fitted-module data already arrives in every `get_status` / `get_ship` reply as `[]ShipModule` and is thrown away. We persist it into the *fresh* asset ledger (`data/assets.db`, captured hourly and current), not the knowledge DB's `ship_modules` table, which has never had a row and whose readers are stale. A new `agent_hull_modules` table mirrors the existing `agent_hulls` write pattern, and a report function answers the one question the campaign asks: which agents can accept a 12-CPU / 15-power utility module. Separately, `MineQty` gains a fuel floor and a return-to-station leg.

**Tech Stack:** Go 1.24+, SQLite (`modernc.org/sqlite` via `pkg/assets`), standard `testing`.

**Spec:** `docs/superpowers/specs/2026-08-22-fleet-drone-refit-design.md` (see §5 Open Risks 1 and the Phase 1 `mine_qty` warning)

## Global Constraints

- Target Go 1.24+; use `range` over integers and `b.Loop()` in benchmarks, never `for i := 0; i < b.N`.
- All new code MUST pass `golangci-lint run` with **zero new findings**.
- Run `go build ./...` and `go test ./...` before every commit.
- Any sleep or pause MUST use a constant from `pkg/game/constants.go`. If none fits, stop and ask — do not introduce a literal duration.
- Compiled binaries go in `bin/`, never the repo root.
- Never `git add -A` — `data/*.json` is runtime churn. Stage named files only.
- Check actual server response field names in `pkg/game/serverapi/` before coding against them. Do not assume.
- `pkg/assets` schema is `schema.sql`, embedded via `//go:embed` and executed on every `Open` with `CREATE TABLE IF NOT EXISTS`. New tables go there. New columns on *existing* tables additionally need `ensureColumn`, because `IF NOT EXISTS` will not alter a table that already exists.

---

### Task 1: `agent_hull_modules` table and writer

**Files:**
- Modify: `pkg/assets/schema.sql` (append new table)
- Modify: `pkg/assets/types.go` (add `HullModule`)
- Create: `pkg/assets/write_hull_modules.go`
- Test: `pkg/assets/write_hull_modules_test.go`

**Interfaces:**
- Consumes: `Store.replaceSet(ctx, deleteSQL, playerID, fn)` from `pkg/assets/store.go`, and `rfc3339(now)` from the same package — both already used by `ReplaceHulls` in `write_hulls.go`.
- Produces: `type HullModule struct{...}` and `func (s *Store) ReplaceHullModules(ctx context.Context, playerID string, rows []HullModule, now time.Time) error` — Task 2 calls this, Task 3 reads the table it writes.

- [ ] **Step 1: Write the failing test**

```go
package assets

import (
	"context"
	"testing"
	"time"
)

func TestReplaceHullModules_RoundTripAndPrune(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	first := []HullModule{
		{ShipID: "s1", ModuleID: "m1", TypeID: "mining_laser_i", Name: "Mining Laser I", Type: "utility", Slot: "utility", CPUUsage: 5, PowerUsage: 8},
		{ShipID: "s1", ModuleID: "m2", TypeID: "cargo_expander_iii", Name: "Cargo Expander III", Type: "utility", Slot: "utility", CPUUsage: 4, PowerUsage: 6},
	}
	if err := st.ReplaceHullModules(ctx, "p1", first, now); err != nil {
		t.Fatalf("ReplaceHullModules: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_hull_modules WHERE player_id = ?`, "p1").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}

	// A refit that removes a module must not leave it on file: a phantom
	// module reads as consumed CPU that is actually free.
	second := []HullModule{first[0]}
	if err := st.ReplaceHullModules(ctx, "p1", second, now); err != nil {
		t.Fatalf("ReplaceHullModules (second): %v", err)
	}
	var got string
	if err := st.DB().QueryRowContext(ctx, `SELECT group_concat(module_id) FROM agent_hull_modules WHERE player_id = ?`, "p1").Scan(&got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got != "m1" {
		t.Errorf("module_id set = %q, want \"m1\" — the removed module was not pruned", got)
	}
}

func TestReplaceHullModules_EmptyClearsPlayer(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceHullModules(ctx, "p1", []HullModule{
		{ShipID: "s1", ModuleID: "m1", TypeID: "t", Name: "n", Type: "utility", Slot: "utility"},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.ReplaceHullModules(ctx, "p1", nil, now); err != nil {
		t.Fatalf("clear: %v", err)
	}
	var n int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_hull_modules WHERE player_id = ?`, "p1").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}
```

`openTestStore(t)` is the shared helper in `pkg/assets/store_test.go`; `st.DB()` is the
accessor tests use for raw queries. Reuse both rather than writing a new harness.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/assets/ -run ReplaceHullModules -v`
Expected: FAIL — `undefined: HullModule` and `st.ReplaceHullModules undefined`.

- [ ] **Step 3: Add the table to `pkg/assets/schema.sql`**

Append:

```sql
-- Fitted modules on an agent's ships, one row per module per ship.
--
-- This exists because agent_hulls.modules is only a COUNT, which cannot answer
-- "does this agent have a free utility slot" -- the question every refit asks.
-- spacemolt-knowledge.db.ship_modules was meant to hold this and has never had
-- a row: its writer exists but nothing calls it.
--
-- Keyed by (player_id, ship_id, module_id). Written delete-then-insert per
-- player, because a module that was unfitted must stop being reported as
-- occupying CPU that is in fact free.
CREATE TABLE IF NOT EXISTS agent_hull_modules (
    player_id    TEXT NOT NULL,
    ship_id      TEXT NOT NULL,
    module_id    TEXT NOT NULL,
    type_id      TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL DEFAULT '',
    slot         TEXT NOT NULL DEFAULT '',
    cpu_usage    INTEGER NOT NULL DEFAULT 0,
    power_usage  INTEGER NOT NULL DEFAULT 0,
    captured_at  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, ship_id, module_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_hull_modules_slot ON agent_hull_modules(slot);
```

- [ ] **Step 4: Add the type to `pkg/assets/types.go`**

```go
// HullModule is one module fitted to one of an agent's ships. Slot is the
// mount class ("utility", "weapon", "defense"); CPUUsage and PowerUsage are
// what it consumes from the hull's budget.
type HullModule struct {
	ShipID     string
	ModuleID   string
	TypeID     string
	Name       string
	Type       string
	Slot       string
	CPUUsage   int
	PowerUsage int
}
```

- [ ] **Step 5: Write `pkg/assets/write_hull_modules.go`**

```go
package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ReplaceHullModules swaps in the agent's full fitted-module set across all
// ships. Modules absent from rows are deleted: a module that was unfitted must
// not linger as phantom CPU and power draw, or a refit check will report a
// slot occupied when it is free.
//
// An empty rows slice therefore clears the player. Callers MUST NOT call this
// with an empty slice on a failed or partial capture -- see HullModulesFrom,
// which returns ok=false rather than an empty slice when nothing was decoded.
func (s *Store) ReplaceHullModules(ctx context.Context, playerID string, rows []HullModule, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_hull_modules WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, m := range rows {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO agent_hull_modules (player_id, ship_id, module_id, type_id,
					name, type, slot, cpu_usage, power_usage, captured_at)
				VALUES (?,?,?,?,?,?,?,?,?,?)`,
				playerID, m.ShipID, m.ModuleID, m.TypeID,
				m.Name, m.Type, m.Slot, m.CPUUsage, m.PowerUsage, ts); err != nil {
				return fmt.Errorf("assets: insert hull module %s/%s/%s: %w", playerID, m.ShipID, m.ModuleID, err)
			}
		}

		return nil
	})
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./pkg/assets/ -run ReplaceHullModules -v`
Expected: PASS, both tests.

- [ ] **Step 7: Lint and commit**

```bash
go build ./... && go test ./pkg/assets/ && golangci-lint run pkg/assets/...
git add pkg/assets/schema.sql pkg/assets/types.go pkg/assets/write_hull_modules.go pkg/assets/write_hull_modules_test.go
git commit -m "feat(assets): record which modules are actually fitted

agent_hulls.modules is a count, which cannot answer whether an agent has
a free utility slot. agent_hull_modules stores the modules themselves,
delete-then-insert per player so an unfitted module stops consuming CPU
that is really free."
```

---

### Task 2: Parse modules from the captured payload and wire into the capture pass

**Files:**
- Modify: `pkg/assets/parse.go` (add `HullModulesFrom`)
- Modify: `pkg/assets/capture.go:136` area (call the new writer beside `ReplaceHulls`)
- Test: `pkg/assets/parse_hull_modules_test.go`

**Interfaces:**
- Consumes: `HullModule` and `Store.ReplaceHullModules` from Task 1; `serverapi.GetShipResponse` (`pkg/game/serverapi/responses.go:52`) whose `Modules` field is `[]serverapi.ShipModule` (`pkg/game/serverapi/types.go:1321`, fields `ID`, `TypeID`, `Name`, `Type`, `Slot`, `CPUUsage`, `PowerUsage`); `client.GetRawJSON("ship")`.
- Produces: `func HullModulesFrom(raw []byte, shipID string) ([]HullModule, bool, error)`.

**Why `get_ship` and not `list_ships`:** `serverapi.OwnedShip.Modules` is an **int count**, so `list_ships` cannot supply module detail. `GetShipResponse.Modules` and `GetStatusResponse.Modules` are the full `[]ShipModule`, for the **active ship only**. Active-ship coverage is what the refit needs — a bay is fitted to the ship the agent is flying.

- [ ] **Step 1: Write the failing test**

```go
package assets

import "testing"

func TestHullModulesFrom_DecodesSlotAndUsage(t *testing.T) {
	raw := []byte(`{"ship":{"id":"s1"},"modules":[
		{"id":"m1","type_id":"mining_laser_i","name":"Mining Laser I","type":"utility","slot":"utility","cpu_usage":5,"power_usage":8},
		{"id":"m2","type_id":"railgun_ii","name":"Railgun II","type":"weapon","slot":"weapon","cpu_usage":9,"power_usage":14}
	]}`)
	got, ok, err := HullModulesFrom(raw, "s1")
	if err != nil {
		t.Fatalf("HullModulesFrom: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true for a decoded body")
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ModuleID != "m1" || got[0].Slot != "utility" || got[0].CPUUsage != 5 || got[0].PowerUsage != 8 {
		t.Errorf("module[0] = %+v", got[0])
	}
	if got[0].ShipID != "s1" {
		t.Errorf("ShipID = %q, want s1 — modules must be attributed to a hull", got[0].ShipID)
	}
}

// An empty or unreadable body must report ok=false, never an empty slice:
// ReplaceHullModules treats empty as "clear this player", so a missing cache
// entry would wipe real data. Mirrors HullsFrom's contract.
func TestHullModulesFrom_EmptyBodyIsNotAResult(t *testing.T) {
	for name, raw := range map[string][]byte{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := HullModulesFrom(raw, "s1")
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if ok {
				t.Error("ok = true for an empty body; an empty result would clear the player")
			}
			if len(got) != 0 {
				t.Errorf("len = %d, want 0", len(got))
			}
		})
	}
}

// A ship with every slot empty is a real, decodable state and must be
// distinguishable from "nothing captured".
func TestHullModulesFrom_NoModulesIsStillAResult(t *testing.T) {
	got, ok, err := HullModulesFrom([]byte(`{"ship":{"id":"s1"},"modules":[]}`), "s1")
	if err != nil {
		t.Fatalf("HullModulesFrom: %v", err)
	}
	if !ok {
		t.Error("ok = false; a decoded body with zero modules is a result, not a miss")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/assets/ -run HullModulesFrom -v`
Expected: FAIL — `undefined: HullModulesFrom`.

- [ ] **Step 3: Implement `HullModulesFrom` in `pkg/assets/parse.go`**

```go
// HullModulesFrom decodes a raw get_ship body (cache key "ship") into fitted
// module rows for shipID. The bool reports whether a body was actually
// decoded, mirroring HullsFrom: an empty body yields no modules, no error and
// ok=false.
//
// The flag is load-bearing. ReplaceHullModules treats an empty slice as "this
// agent has nothing fitted" and clears the player, so a missing cache entry
// must never reach it as an empty result. A decoded body with an empty modules
// array IS a real result (a stripped hull) and returns ok=true.
func HullModulesFrom(raw []byte, shipID string) ([]HullModule, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	var resp serverapi.GetShipResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false, fmt.Errorf("assets: decode get_ship: %w", err)
	}
	out := make([]HullModule, 0, len(resp.Modules))
	for _, m := range resp.Modules {
		out = append(out, HullModule{
			ShipID:     shipID,
			ModuleID:   m.ID,
			TypeID:     m.TypeID,
			Name:       m.Name,
			Type:       m.Type,
			Slot:       m.Slot,
			CPUUsage:   m.CPUUsage,
			PowerUsage: m.PowerUsage,
		})
	}

	return out, true, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/assets/ -run HullModulesFrom -v`
Expected: PASS, all three tests.

- [ ] **Step 5: Wire it into the capture pass**

In `pkg/assets/capture.go`, immediately after the existing `ReplaceHulls` block near line 136, add:

```go
		// Fitted modules for the ACTIVE ship only: list_ships reports a module
		// count, not the modules, so get_ship is the only source. Best-effort
		// and non-fatal, matching every other capture in this pass.
		if ms, ok, derr := HullModulesFrom(client.GetRawJSON("ship"), activeShipID); derr == nil && ok {
			if err := st.ReplaceHullModules(ctx, playerID, ms, now); err != nil {
				return err
			}
		}
```

Read the surrounding block first and match its exact error-handling shape. `activeShipID` must come from whatever the pass already knows — check `agent_profile.active_ship_id` handling in `write_identity.go` / `parse.go`; if the pass does not already carry it, derive it from the `HullsFrom` rows by selecting the `Hull` with `IsActive == true`, and skip the write when no active hull is present.

- [ ] **Step 6: Verify the full package still passes**

Run: `go test ./pkg/assets/ -count=1`
Expected: PASS.

- [ ] **Step 7: Add the table to coverage tracking**

In `pkg/assets/coverage.go`, add `"agent_hull_modules"` to the table list at line ~26 and give it a freshness window of `time.Hour` in the map at line ~51, matching `agent_hulls`. Without this the new capture is invisible to the freshness panel — the same blind spot that let six workers run 3.5 days with a dead scheduler.

- [ ] **Step 8: Lint and commit**

```bash
go build ./... && go test ./pkg/assets/ && golangci-lint run pkg/assets/...
git add pkg/assets/parse.go pkg/assets/parse_hull_modules_test.go pkg/assets/capture.go pkg/assets/coverage.go
git commit -m "feat(assets): capture the modules on the ship an agent is flying

get_status and get_ship have always carried the full module list and we
threw it away, so no query could tell a free utility slot from an
occupied one. list_ships cannot help -- its modules field is a count.

Covers the active ship only, which is what a refit acts on."
```

---

### Task 3: Report which agents can accept a given module

**Files:**
- Create: `pkg/assets/fitting.go`
- Test: `pkg/assets/fitting_test.go`

**Interfaces:**
- Consumes: the `agent_hull_modules` table from Tasks 1–2, and `agent_hulls`.
- Produces: `func (s *Store) FreeUtilitySlots(ctx context.Context, cpuNeed, powerNeed int) ([]FitCandidate, error)` and `type FitCandidate struct{ PlayerID, ShipID, ClassID string; UtilityUsed, CPUUsed, PowerUsed int }`.

This is the task that actually unblocks Phase 4. `advanced_drone_bay` needs **cpu 12, power 15, one utility slot**.

Hull capacity (`utility_slots`, `cpu_capacity`, `power_capacity`) lives in `spacemolt-knowledge.db.ships`, a *different* database from `assets.db`. Do **not** attempt a cross-database join. This function reports **consumption** from `assets.db`; the caller subtracts it from hull capacity read separately. Say so in the doc comment.

- [ ] **Step 1: Write the failing test**

```go
package assets

import (
	"context"
	"testing"
	"time"
)

func TestFreeUtilitySlots_ReportsConsumptionPerActiveShip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceHulls(ctx, "p1", []Hull{
		{ShipID: "s1", ClassID: "drillship", IsActive: true},
		{ShipID: "s2", ClassID: "cobble", IsActive: false},
	}, now); err != nil {
		t.Fatalf("ReplaceHulls: %v", err)
	}
	if err := st.ReplaceHullModules(ctx, "p1", []HullModule{
		{ShipID: "s1", ModuleID: "m1", Slot: "utility", CPUUsage: 5, PowerUsage: 8},
		{ShipID: "s1", ModuleID: "m2", Slot: "weapon", CPUUsage: 9, PowerUsage: 14},
		{ShipID: "s2", ModuleID: "m3", Slot: "utility", CPUUsage: 4, PowerUsage: 6},
	}, now); err != nil {
		t.Fatalf("ReplaceHullModules: %v", err)
	}

	got, err := st.FreeUtilitySlots(ctx, 12, 15)
	if err != nil {
		t.Fatalf("FreeUtilitySlots: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 — only the ACTIVE ship is a refit candidate", len(got))
	}
	c := got[0]
	if c.ShipID != "s1" {
		t.Errorf("ShipID = %q, want s1", c.ShipID)
	}
	if c.UtilityUsed != 1 {
		t.Errorf("UtilityUsed = %d, want 1 (the weapon module must not count)", c.UtilityUsed)
	}
	if c.CPUUsed != 14 || c.PowerUsed != 22 {
		t.Errorf("CPUUsed/PowerUsed = %d/%d, want 14/22 (all slots draw from one budget)", c.CPUUsed, c.PowerUsed)
	}
}

// An agent with no captured modules is a candidate with zero consumption, not
// an absent row: a stripped hull is the easiest refit target, and dropping it
// would silently shrink the fleet we think we can fit.
func TestFreeUtilitySlots_UncapturedAgentStillListed(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceHulls(ctx, "p2", []Hull{{ShipID: "s9", ClassID: "shard", IsActive: true}}, now); err != nil {
		t.Fatalf("ReplaceHulls: %v", err)
	}
	got, err := st.FreeUtilitySlots(ctx, 12, 15)
	if err != nil {
		t.Fatalf("FreeUtilitySlots: %v", err)
	}
	if len(got) != 1 || got[0].PlayerID != "p2" || got[0].UtilityUsed != 0 {
		t.Fatalf("got %+v, want one zero-consumption candidate for p2", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/assets/ -run FreeUtilitySlots -v`
Expected: FAIL — `undefined: FitCandidate`, `st.FreeUtilitySlots undefined`.

- [ ] **Step 3: Implement `pkg/assets/fitting.go`**

```go
package assets

import (
	"context"
	"fmt"
)

// FitCandidate is one agent's active hull and what its fitted modules already
// consume. It reports CONSUMPTION only.
//
// Hull capacity (utility_slots, cpu_capacity, power_capacity) lives in
// spacemolt-knowledge.db.ships -- a different database -- so free capacity is
// the caller's subtraction, not a join. Never widen this into a cross-database
// query; attach the capacity side in the calling tool.
type FitCandidate struct {
	PlayerID    string
	ShipID      string
	ClassID     string
	UtilityUsed int
	CPUUsed     int
	PowerUsed   int
}

// FreeUtilitySlots lists every agent's ACTIVE hull with the utility-slot count,
// CPU and power its fitted modules consume. cpuNeed and powerNeed describe the
// module being considered and are recorded on the query for the caller's
// benefit; filtering against hull capacity happens in the caller, which has the
// ships catalog.
//
// An agent with no captured modules appears with zero consumption rather than
// being omitted -- a stripped hull is the easiest refit target of all.
func (s *Store) FreeUtilitySlots(ctx context.Context, cpuNeed, powerNeed int) ([]FitCandidate, error) {
	_ = cpuNeed
	_ = powerNeed
	rows, err := s.db.QueryContext(ctx, `
		SELECT h.player_id, h.ship_id, h.class_id,
		       COALESCE(SUM(CASE WHEN m.slot = 'utility' THEN 1 ELSE 0 END), 0) AS utility_used,
		       COALESCE(SUM(m.cpu_usage), 0)   AS cpu_used,
		       COALESCE(SUM(m.power_usage), 0) AS power_used
		FROM agent_hulls h
		LEFT JOIN agent_hull_modules m
		       ON m.player_id = h.player_id AND m.ship_id = h.ship_id
		WHERE h.is_active = 1
		GROUP BY h.player_id, h.ship_id, h.class_id
		ORDER BY h.player_id`)
	if err != nil {
		return nil, fmt.Errorf("assets: free utility slots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FitCandidate
	for rows.Next() {
		var c FitCandidate
		if err := rows.Scan(&c.PlayerID, &c.ShipID, &c.ClassID, &c.UtilityUsed, &c.CPUUsed, &c.PowerUsed); err != nil {
			return nil, fmt.Errorf("assets: scan fit candidate: %w", err)
		}
		out = append(out, c)
	}

	return out, rows.Err()
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/assets/ -run FreeUtilitySlots -v`
Expected: PASS, both tests.

- [ ] **Step 5: Lint and commit**

```bash
go build ./... && go test ./pkg/assets/ && golangci-lint run pkg/assets/...
git add pkg/assets/fitting.go pkg/assets/fitting_test.go
git commit -m "feat(assets): report what each active hull already consumes

The drone refit needs to know who has a free utility slot for a 12-CPU,
15-power module. This answers the consumption half from assets.db; hull
capacity lives in another database, so the subtraction stays with the
caller rather than becoming a cross-database join."
```

---

### Task 4: Give `MineQty` a fuel floor and a way home

**Files:**
- Modify: `pkg/worker/mine_qty.go`
- Test: `pkg/worker/mine_qty_test.go` (add cases; the fake client already exists there)

**Interfaces:**
- Consumes: `d.Client.GetState()` (`*game.State`, `state.Ship.Fuel` / `state.Ship.MaxFuel`), `d.autopilotAndUndock(ctx, sys, poi)`, `d.mineLoop(ctx, itemID, qty, poi)`, `d.findMinePOI(ctx, current, itemID)` — all already in `mine_qty.go`.
- Produces: `const MineQtyMinFuelReserve = 25` and the guard behaviour below.

**The bug (live, 2026-07-12):** `MineQty` travels to a belt and mines with no fuel guard and no return leg. craftsman-10 then craftsman-2 each burned to ~5–10 fuel, froze undocked, ate three stall-restarts each, and were quarantined. The rescue pipeline recovered them, but the verb is unusable until this is fixed, and the drone campaign's Phase 1 wants it for crystal mining 12–20 jumps out.

- [ ] **Step 1: Write the failing test**

Use the harness the file already has: `noWaitMineDispatch(client, kb, agentsDir)`,
`mineFakeClient` wrapping `deliverFakeClient`/`fakeClient`, `newMineTestKB(t, itemID)` and
`writeDeliverCreds(t, dir, agent, username)`. The fake records every call name in
`client.calls`, which is how these tests assert that no travel happened — there is no
`traveled` bool. Copy the construction block from `TestMineQtyMinesUntilQtyThenDelivers`.

```go
// A miner that cannot afford the trip must not start it. The 2026-07-12 live
// failure was craftsman-10 flying out on a tank it could not return on,
// freezing undocked at ~5 fuel and quarantined after three stall-kills.
func TestMineQtyRefusesToDepartBelowFuelReserve(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineTestKB(t, "raw_ore")

	client := &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "mine_sys"},
				Ship: game.Ship{
					CargoCapacity: 100,
					Fuel:          MineQtyMinFuelReserve - 1,
					MaxFuel:       130,
				},
			}},
			storageStock: map[string]float64{},
		},
		mineItemID: "raw_ore",
		mineYields: []float64{3, 4},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)

	err := d.MineQty(context.Background(), "raw_ore", 7, "to_base", "craftsman-3")
	if err == nil {
		t.Fatal("MineQty returned nil; departing below the fuel reserve must be refused")
	}
	for _, c := range client.calls {
		if c == "mine" {
			t.Fatalf("MineQty mined despite insufficient fuel (calls: %v)", client.calls)
		}
	}
}

// The guard must not block a healthy miner: same setup, full tank, normal run.
func TestMineQtyDepartsWhenFuelIsAdequate(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineTestKB(t, "raw_ore")

	client := &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "mine_sys"},
				Ship:   game.Ship{CargoCapacity: 100, Fuel: 130, MaxFuel: 130},
			}},
			storageStock: map[string]float64{},
		},
		mineItemID: "raw_ore",
		mineYields: []float64{3, 4},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)

	if err := d.MineQty(context.Background(), "raw_ore", 7, "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQty: %v", err)
	}
	mineCalls := 0
	for _, c := range client.calls {
		if c == "mine" {
			mineCalls++
		}
	}
	if mineCalls == 0 {
		t.Error("MineQty did not mine despite a full tank — the guard is too strict")
	}
}
```

⚠️ The **existing** MineQty tests construct `game.Ship` without a `Fuel` field, so they
default to 0 and every one of them will start failing the moment the guard lands. That is
expected and is part of this task: add `Fuel: 130, MaxFuel: 130` to each existing
`game.Ship{...}` literal in `mine_qty_test.go`. Do not weaken the guard to keep them
passing.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run MineQty -v`
Expected: FAIL — `undefined: MineQtyMinFuelReserve`, and the refusal test fails because the verb departs regardless.

- [ ] **Step 3: Add the guard to `pkg/worker/mine_qty.go`**

Add beside `MineQtyMaxDryPasses`:

```go
// MineQtyMinFuelReserve is the fuel a worker must hold before MineQty will
// leave a station. Below it the verb refuses the trip rather than starting one
// it cannot finish.
//
// This exists because MineQty had no fuel guard at all: on 2026-07-12
// craftsman-10 and then craftsman-2 flew out, mined to ~5 fuel, froze undocked
// with no way back, absorbed three stall-restarts each and were quarantined.
// A refused trip is recoverable; a stranded miner needs the rescue fleet.
//
// Deliberately a flat floor and not a computed round-trip cost: jump fuel is
// ceil(scale^1.5 x speed) per jump and depends on the hull, so a wrong
// computation would fail toward departing. A flat floor fails toward staying.
const MineQtyMinFuelReserve = 25
```

Then, in `MineQty`, immediately after the existing `state == nil || state.System.ID == ""` check and **before** `findMinePOI`:

```go
	if state.Ship.Fuel < MineQtyMinFuelReserve {
		return fmt.Errorf("mine_qty: fuel %d below reserve %d — refusing to depart (refuel first)",
			int(state.Ship.Fuel), MineQtyMinFuelReserve)
	}
```

`game.Ship.Fuel` is a `float64` (`pkg/game/types.go:170`), so the comparison against the
untyped constant is valid as written and the `int()` conversion in the error message is
correct.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/worker/ -run MineQty -v`
Expected: PASS, including the pre-existing MineQty tests.

- [ ] **Step 5: Run the whole worker package**

Run: `go test ./pkg/worker/ -count=1`
Expected: PASS. This package is large; if the pre-commit race gate exceeds its internal 300s under fleet load, that is the known environment issue, not a defect — see the note in the runbook and use a scoped `-race` on the changed tests plus a full non-race run.

- [ ] **Step 6: Lint and commit**

```bash
go build ./... && go test ./pkg/worker/ && golangci-lint run pkg/worker/...
git add pkg/worker/mine_qty.go pkg/worker/mine_qty_test.go
git commit -m "fix(worker): mine_qty flew out on tanks it could not come back on

On 2026-07-12 craftsman-10 and craftsman-2 each took a mine_qty node,
burned down to about five fuel at the belt, froze undocked, absorbed
three stall-restarts and were quarantined. The verb had no fuel guard.

A flat floor rather than a computed round trip: jump fuel is
ceil(scale^1.5 x speed) per jump and hull-dependent, so a miscomputed
estimate fails toward departing. A flat floor fails toward staying."
```

---

## Notes for whoever executes this

- **Task 4 is independent of Tasks 1–3.** If Phase 1 mining is urgent, do Task 4 first.
- **Tasks 1–3 are strictly ordered** — 2 needs 1's type and writer, 3 needs 2's data.
- There is a **separate, larger** stale-data problem this plan does *not* fix: the Executor B planner (`pkg/craftbrain`, `pkg/overmind/plans`) reads `storage_snapshots` in `spacemolt-knowledge.db`, last captured **2026-07-02**. Every inventory figure in the drone-refit spec came from `assets.db` instead. Do not dispatch campaign work through Executor B until that read is repointed; that is its own piece of work.
- `spacemolt-knowledge.db.ship_modules` and `agent_ships` stay empty after this plan. That is deliberate — we are not reviving a table with no live readers. If something later needs them, wire `StoreShip` rather than dual-writing.
