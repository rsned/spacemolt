# Mission-Runner Fleet Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A new `missions` worker task that earns credits from deliver-shaped mission-board missions, with realized-outcome telemetry, wired into the overmind worker stack and canaried on 4 idle agents.

**Architecture:** Autonomous runners + telemetry (spec: `docs/superpowers/specs/2026-07-16-mission-runner-fleet-design.md`). Each worker reads the live board where it is docked, selects a stackable deliver-mission set through expiry/cost/cargo gates, executes (acquire cargo → autopilot → complete), and records a `mission_results` row per mission in market.db. No central brain.

**Tech Stack:** Go 1.24+, SQLite via `pkg/market` Collector, existing `pkg/worker` task pattern (haul.go is the model), `pkg/navigation` BFS, `pkg/game` WebSocket client.

## Global Constraints

- Go 1.24+ idioms: range-over-int, `b.Loop()` in benchmarks (user global CLAUDE.md).
- All new code passes `golangci-lint run <changed dirs>` with no new findings; run it after each task.
- Run `go build ./...` and `go test ./...` before every commit.
- Sleeps only via `pkg/game/constants.go` constants (`game.SleepQuick` etc.) — never raw durations.
- Never assume server field names — the structs referenced below were verified in `pkg/game/serverapi/types.go` and `responses.go`.
- Do NOT `git add -A` — the working tree has dirty runtime `data/agents/*/schedule.json` churn that must never be committed. Stage files explicitly by path.
- Compiled binaries go in `bin/`, never the repo root.
- New `game.GameClient` methods are NOT being added (all mission commands already exist), so pkg/agent / pkg/skills mocks are unaffected.

---

### Task 1: `mission_results` telemetry (pkg/market)

**Files:**
- Modify: `pkg/market/types.go` (append after `HaulResult`, ~line 300)
- Modify: `pkg/market/schema.sql` (append after the `fleet_timeseries` block)
- Create: `pkg/market/mission_results.go`
- Test: `pkg/market/mission_results_test.go`

**Interfaces:**
- Consumes: existing `Collector`, `writeRetry`, `Open(Config{DBPath})` test pattern from `haul_results_test.go`.
- Produces: `type MissionResult struct` (fields below), `func (c *Collector) RecordMissionResult(ctx context.Context, r MissionResult) error`, `func (c *Collector) GetMissionResults(ctx context.Context, agentID string, limit int) ([]MissionResult, error)`. Task 3's `MissionStore` interface names `RecordMissionResult` with exactly this signature.

`schema.sql` is `//go:embed`-ed and executed with `CREATE TABLE IF NOT EXISTS` on every `Open` (`pkg/market/migrations.go:9`), so existing market.db files pick the table up automatically — no numbered migration.

- [ ] **Step 1: Write the failing test**

Create `pkg/market/mission_results_test.go`:

```go
package market

import (
	"context"
	"path/filepath"
	"testing"
)

func newMissionTestCollector(t *testing.T) *Collector {
	t.Helper()
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open collector: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRecordAndGetMissionResult(t *testing.T) {
	c := newMissionTestCollector(t)
	ctx := context.Background()
	r := MissionResult{
		AgentID: "engineer-1", MissionID: "m-123", TemplateID: "",
		MissionType: "delivery", Title: "Supply Run: Steel",
		FromBaseID: "haven_station", ToBaseID: "sol_station",
		ItemID: "steel", Qty: 20,
		ExpectedReward: 2500, CreditsEarned: 2500, ItemCost: 400, FuelCost: 60,
		Jumps: 2, Outcome: "completed",
		AcceptedAt: "2026-07-16T10:00:00Z", FinishedAt: "2026-07-16T10:20:00Z",
		AcceptedTick: 1000, FinishedTick: 1120,
		CreatedAt: "2026-07-16T10:20:01Z",
	}
	if err := c.RecordMissionResult(ctx, r); err != nil {
		t.Fatalf("record: %v", err)
	}
	// Second agent's row must not leak into the first agent's query.
	r2 := r
	r2.AgentID, r2.MissionID, r2.Outcome = "engineer-2", "m-456", "abandoned"
	if err := c.RecordMissionResult(ctx, r2); err != nil {
		t.Fatalf("record 2: %v", err)
	}

	got, err := c.GetMissionResults(ctx, "engineer-1", 0)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].MissionID != "m-123" || got[0].CreditsEarned != 2500 || got[0].Outcome != "completed" {
		t.Fatalf("row mismatch: %+v", got[0])
	}

	all, err := c.GetMissionResults(ctx, "", 0)
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d rows for all agents, want 2", len(all))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestRecordAndGetMissionResult -v`
Expected: FAIL to compile — `undefined: MissionResult`, `c.RecordMissionResult undefined`.

- [ ] **Step 3: Add the type, schema, and recorder**

Append to `pkg/market/types.go` (after the `HaulResult` struct, before `FleetSnapshot`):

```go
// MissionResult is one finished (completed or abandoned) mission's real outcome,
// the mission-fleet analogue of HaulResult. CreditsEarned is the observed wallet
// delta around complete_mission (0 for abandons); ExpectedReward is what the
// board advertised, so the dashboard can compare promised vs realized.
type MissionResult struct {
	ID             int64   `json:"id"`
	AgentID        string  `json:"agent_id"`
	MissionID      string  `json:"mission_id"`
	TemplateID     string  `json:"template_id"`
	MissionType    string  `json:"mission_type"`
	Title          string  `json:"title"`
	FromBaseID     string  `json:"from_base_id"`
	ToBaseID       string  `json:"to_base_id"`
	ItemID         string  `json:"item_id"`
	Qty            float64 `json:"qty"`
	ExpectedReward float64 `json:"expected_reward"`
	CreditsEarned  float64 `json:"credits_earned"`
	ItemCost       float64 `json:"item_cost"`
	FuelCost       float64 `json:"fuel_cost"`
	Jumps          int     `json:"jumps"`
	Outcome        string  `json:"outcome"` // completed | abandoned
	AcceptedAt     string  `json:"accepted_at"`
	FinishedAt     string  `json:"finished_at"`
	AcceptedTick   int64   `json:"accepted_tick"`
	FinishedTick   int64   `json:"finished_tick"`
	CreatedAt      string  `json:"created_at"`
}
```

Append to `pkg/market/schema.sql` (after the `fleet_timeseries` table + its index):

```sql
-- Per-mission real outcomes for the mission-runner fleet (spec 2026-07-16).
CREATE TABLE IF NOT EXISTS mission_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id        TEXT NOT NULL,
    mission_id      TEXT NOT NULL,
    template_id     TEXT,
    mission_type    TEXT,
    title           TEXT,
    from_base_id    TEXT,
    to_base_id      TEXT,
    item_id         TEXT,
    qty             REAL,
    expected_reward REAL NOT NULL,
    credits_earned  REAL NOT NULL,
    item_cost       REAL NOT NULL,
    fuel_cost       REAL NOT NULL,
    jumps           INTEGER NOT NULL,
    outcome         TEXT NOT NULL,
    accepted_at     TEXT NOT NULL,
    finished_at     TEXT NOT NULL,
    accepted_tick   INTEGER,
    finished_tick   INTEGER,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mission_results_agent_time ON mission_results(agent_id, finished_at);
```

Create `pkg/market/mission_results.go`:

```go
package market

import (
	"context"
	"database/sql"
	"fmt"
)

// RecordMissionResult writes one finished-mission outcome row (completed or
// abandoned), the mission-fleet analogue of RecordHaulResult.
func (c *Collector) RecordMissionResult(ctx context.Context, r MissionResult) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO mission_results
 (agent_id, mission_id, template_id, mission_type, title, from_base_id, to_base_id,
  item_id, qty, expected_reward, credits_earned, item_cost, fuel_cost, jumps, outcome,
  accepted_at, finished_at, accepted_tick, finished_tick, created_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.AgentID, r.MissionID, r.TemplateID, r.MissionType, r.Title, r.FromBaseID,
			r.ToBaseID, r.ItemID, r.Qty, r.ExpectedReward, r.CreditsEarned, r.ItemCost,
			r.FuelCost, r.Jumps, r.Outcome, r.AcceptedAt, r.FinishedAt, r.AcceptedTick,
			r.FinishedTick, r.CreatedAt)
		return err
	})
}

// GetMissionResults returns the most recent mission results for agentID (all
// agents if empty), newest finished first, capped at limit (<=0 -> 500).
func (c *Collector) GetMissionResults(ctx context.Context, agentID string, limit int) ([]MissionResult, error) {
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT id, agent_id, mission_id, template_id, mission_type, title, from_base_id,
 to_base_id, item_id, qty, expected_reward, credits_earned, item_cost, fuel_cost, jumps,
 outcome, accepted_at, finished_at, accepted_tick, finished_tick, created_at
 FROM mission_results`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id = ?`
		args = append(args, agentID)
	}
	q += ` ORDER BY finished_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get mission results: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []MissionResult
	for rows.Next() {
		var r MissionResult
		if err := rows.Scan(&r.ID, &r.AgentID, &r.MissionID, &r.TemplateID, &r.MissionType,
			&r.Title, &r.FromBaseID, &r.ToBaseID, &r.ItemID, &r.Qty, &r.ExpectedReward,
			&r.CreditsEarned, &r.ItemCost, &r.FuelCost, &r.Jumps, &r.Outcome,
			&r.AcceptedAt, &r.FinishedAt, &r.AcceptedTick, &r.FinishedTick, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan mission result: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

Note: `template_id`, `mission_type`, `title`, `from_base_id`, `to_base_id`, `item_id`, `qty`, `accepted_tick`, `finished_tick` are nullable in SQL but always written non-null by the recorder; scanning into plain Go types is safe because every row comes from `RecordMissionResult`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestRecordAndGetMissionResult -v`
Expected: PASS

- [ ] **Step 5: Lint, build, full package test**

Run: `go build ./... && go test ./pkg/market/ && golangci-lint run pkg/market/`
Expected: build OK, tests PASS, no new lint findings.

- [ ] **Step 6: Commit**

```bash
git add pkg/market/types.go pkg/market/schema.sql pkg/market/mission_results.go pkg/market/mission_results_test.go
git commit -m "feat(market): mission_results table + recorder for mission-runner telemetry"
```

---

### Task 2: Mission selector (pure functions)

**Files:**
- Create: `pkg/worker/mission_select.go`
- Test: `pkg/worker/mission_select_test.go`

**Interfaces:**
- Consumes: `serverapi.MissionBoardEntry` (fields verified: `MissionID`, `TemplateID`, `Type`, `Title`, `ExpiresInTicks`, `ProvidedItems map[string]int`, `RequiredModules []string`, `Requirements *serverapi.MissionRequirements` with `DeliverItemID/DeliverQuantity/DeliverToBaseID`, `Rewards *serverapi.MissionRewards` with `Credits int`, `Objectives []serverapi.MissionObjective` with `TargetBaseID/SystemID`).
- Produces (Task 3 consumes exactly these):
  - `type missionCandidate struct` (fields below)
  - `func buildMissionCandidate(e serverapi.MissionBoardEntry, dist map[string]int, refAsk func(itemID string) (float64, bool), fuelCostFor func(jumps int) float64) (missionCandidate, string)` — second return is a non-empty rejection reason when the entry is filtered out.
  - `func SelectMissionSet(cands []missionCandidate, credits, cargoFree float64, maxJumps int) []missionCandidate`

- [ ] **Step 1: Write the failing tests**

Create `pkg/worker/mission_select_test.go`:

```go
package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// boardEntry builds a deliver-shaped mission: deliver qty of item to destBase
// (in destSystem), paying reward credits, expiring in expiry ticks (0 = never).
func boardEntry(id, item string, qty int, destBase, destSystem string, reward, expiry int) serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{
		MissionID: id, Type: "delivery", Title: "Deliver " + item,
		ExpiresInTicks: expiry,
		Requirements: &serverapi.MissionRequirements{
			DeliverItemID: item, DeliverQuantity: qty, DeliverToBaseID: destBase,
		},
		Rewards:    &serverapi.MissionRewards{Credits: reward},
		Objectives: []serverapi.MissionObjective{{Type: "deliver", TargetBaseID: destBase, SystemID: destSystem}},
	}
}

func TestBuildMissionCandidate(t *testing.T) {
	dist := map[string]int{"sol": 2, "haven": 0}
	ask := func(item string) (float64, bool) {
		if item == "steel" {
			return 20, true
		}
		return 0, false
	}
	noFuel := func(jumps int) float64 { return 0 }

	t.Run("deliver mission prices and routes", func(t *testing.T) {
		c, reason := buildMissionCandidate(boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0), dist, ask, noFuel)
		if reason != "" {
			t.Fatalf("rejected: %s", reason)
		}
		if c.ItemID != "steel" || c.Qty != 20 || c.DestSystem != "sol" || c.Jumps != 2 {
			t.Fatalf("candidate mismatch: %+v", c)
		}
		if c.ItemCost != 400 || c.Net != 3000-400 {
			t.Fatalf("economics mismatch: cost=%v net=%v", c.ItemCost, c.Net)
		}
	})

	t.Run("provided items reduce buy quantity", func(t *testing.T) {
		e := boardEntry("m2", "steel", 20, "sol_station", "sol", 3000, 0)
		e.ProvidedItems = map[string]int{"steel": 20}
		c, reason := buildMissionCandidate(e, dist, ask, noFuel)
		if reason != "" {
			t.Fatalf("rejected: %s", reason)
		}
		if c.BuyQty != 0 || c.ItemCost != 0 {
			t.Fatalf("provided cargo should zero the buy: %+v", c)
		}
	})

	t.Run("non-deliver mission rejected", func(t *testing.T) {
		e := boardEntry("m3", "", 0, "", "", 5000, 0)
		e.Requirements = &serverapi.MissionRequirements{KillCount: 3}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("kill mission must be rejected")
		}
	})

	t.Run("module-gated mission rejected", func(t *testing.T) {
		e := boardEntry("m4", "steel", 20, "sol_station", "sol", 3000, 0)
		e.RequiredModules = []string{"smuggler_hold"}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("module-gated mission must be rejected")
		}
	})

	t.Run("tight expiry rejected", func(t *testing.T) {
		e := boardEntry("m5", "steel", 20, "sol_station", "sol", 3000, 30)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("30-tick expiry must be rejected (arbitrage-expiry lesson)")
		}
	})

	t.Run("unpriceable item rejected", func(t *testing.T) {
		e := boardEntry("m6", "unobtainium", 5, "sol_station", "sol", 3000, 0)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("no reference ask + buy needed must be rejected")
		}
	})

	t.Run("unreachable destination rejected", func(t *testing.T) {
		e := boardEntry("m7", "steel", 20, "far_station", "far_system", 3000, 0)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("destination missing from dist map must be rejected")
		}
	})

	t.Run("negative net rejected", func(t *testing.T) {
		e := boardEntry("m8", "steel", 100, "sol_station", "sol", 500, 0) // cost 2000 > reward 500
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("negative-net mission must be rejected")
		}
	})
}

func TestSelectMissionSet(t *testing.T) {
	mk := func(id, destSystem string, net float64, jumps, buyQty, qty int) missionCandidate {
		return missionCandidate{
			Entry:  serverapi.MissionBoardEntry{MissionID: id},
			ItemID: "steel", Qty: qty, BuyQty: buyQty,
			ItemCost: float64(buyQty) * 20, DestSystem: destSystem,
			Net: net, Jumps: jumps, Reward: net + float64(buyQty)*20,
		}
	}
	const credits, cargoFree = 10000.0, 100.0

	t.Run("stacks same-destination missions best-net first", func(t *testing.T) {
		got := SelectMissionSet([]missionCandidate{
			mk("low", "sol", 1000, 2, 10, 10),
			mk("best", "sol", 3000, 2, 10, 10),
			mk("elsewhere", "krynn", 2000, 1, 10, 10),
		}, credits, cargoFree, 5)
		if len(got) != 2 || got[0].Entry.MissionID != "best" || got[1].Entry.MissionID != "low" {
			t.Fatalf("want [best low] (same destination as anchor), got %+v", got)
		}
	})

	t.Run("caps at MissionMaxStack", func(t *testing.T) {
		var cands []missionCandidate
		for i := range 8 {
			cands = append(cands, mk(string(rune('a'+i)), "sol", float64(1000+i), 2, 1, 1))
		}
		if got := SelectMissionSet(cands, credits, cargoFree, 5); len(got) != MissionMaxStack {
			t.Fatalf("got %d, want %d", len(got), MissionMaxStack)
		}
	})

	t.Run("respects buy budget", func(t *testing.T) {
		// Each costs 2000; budget = 3000*0.8 = 2400 -> only one fits.
		got := SelectMissionSet([]missionCandidate{
			mk("a", "sol", 3000, 2, 100, 100),
			mk("b", "sol", 2900, 2, 100, 100),
		}, 3000, 1000, 5)
		if len(got) != 1 {
			t.Fatalf("budget must cap the stack: got %d", len(got))
		}
	})

	t.Run("respects cargo space", func(t *testing.T) {
		got := SelectMissionSet([]missionCandidate{
			mk("a", "sol", 3000, 2, 60, 60),
			mk("b", "sol", 2900, 2, 60, 60),
		}, 100000, 100, 5)
		if len(got) != 1 {
			t.Fatalf("cargo must cap the stack: got %d", len(got))
		}
	})

	t.Run("drops anchors beyond maxJumps", func(t *testing.T) {
		got := SelectMissionSet([]missionCandidate{mk("far", "sol", 9000, 9, 1, 1)}, credits, cargoFree, 5)
		if len(got) != 0 {
			t.Fatalf("9-jump destination with maxJumps=5 must be dropped: %+v", got)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestBuildMissionCandidate|TestSelectMissionSet' -v`
Expected: FAIL to compile — `undefined: buildMissionCandidate`, `undefined: missionCandidate`, `undefined: SelectMissionSet`, `undefined: MissionMaxStack`.

- [ ] **Step 3: Implement the selector**

Create `pkg/worker/mission_select.go`:

```go
package worker

import (
	"fmt"
	"sort"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

const (
	// MissionMaxStack is the server's concurrent-mission cap per player (the
	// Mission Runner guide's "accept 5 simultaneous missions" stacking play).
	MissionMaxStack = 5
	// DefaultMissionMaxJumps caps how far (jumps) a mission destination may be,
	// matching the haul fleet's reposition philosophy (DefaultHaulMaxJumps):
	// several nearby runs net more than one distant payday.
	DefaultMissionMaxJumps = 5
	// missionMinNet is the minimum estimated profit (reward - item cost - fuel)
	// a mission must clear. Below this the accept isn't worth the slot.
	missionMinNet = 500.0
	// Expiry gate (the arbitrage-expiry lesson: never accept work that can time
	// out mid-route). A finite expiry must cover a base margin plus a per-jump
	// travel allowance. Ticks are ~10s wall.
	missionMinExpiryTicks  = 180 // 30 min base margin
	missionTicksPerJump    = 12  // ~2 min/jump allowance (jump + transit + dock)
	// missionBuyBudgetFraction of current credits may be spent acquiring
	// mission cargo across the whole stacked set — never bet the full wallet.
	missionBuyBudgetFraction = 0.8
)

// missionCandidate is a deliver-shaped board entry with derived routing and
// economics, ready for stacking.
type missionCandidate struct {
	Entry      serverapi.MissionBoardEntry
	ItemID     string
	Qty        int     // units to deliver
	BuyQty     int     // units we must acquire (Qty minus provided)
	DestBaseID string
	DestSystem string
	Reward     float64
	ItemCost   float64 // BuyQty x reference ask
	FuelCost   float64
	Net        float64 // Reward - ItemCost - FuelCost
	Jumps      int     // current system -> DestSystem
}

// deliverShape extracts the deliver-mission core of e. ok=false when e is not a
// pure deliver mission v1 can run: kill/mine/visit components, module gates,
// and entries with no resolvable destination system are all skipped.
func deliverShape(e serverapi.MissionBoardEntry) (item string, qty int, destBase, destSystem string, ok bool) {
	r := e.Requirements
	if r == nil || r.DeliverItemID == "" || r.DeliverQuantity <= 0 || r.DeliverToBaseID == "" {
		return "", 0, "", "", false
	}
	// Any non-deliver component makes this a compound mission v1 skips.
	if r.KillCount > 0 || r.MineQuantity > 0 || r.VisitSystemCount > 0 || r.TargetPlayerID != "" {
		return "", 0, "", "", false
	}
	if len(e.RequiredModules) > 0 {
		return "", 0, "", "", false
	}
	// Destination system comes from the matching deliver objective; entries
	// without one cannot be routed and are skipped.
	for _, o := range e.Objectives {
		if o.TargetBaseID == r.DeliverToBaseID && o.SystemID != "" {
			return r.DeliverItemID, r.DeliverQuantity, r.DeliverToBaseID, o.SystemID, true
		}
	}
	return "", 0, "", "", false
}

// buildMissionCandidate prices and routes one board entry. dist maps system id
// -> jumps from the worker's current system (navigation.BFSJumps output); refAsk
// returns the sentinel-filtered best ask for an item (ok=false -> unpriceable);
// fuelCostFor prices the fuel for a jump count. A non-empty reason means the
// entry was filtered out (and why, for the worker log).
func buildMissionCandidate(e serverapi.MissionBoardEntry, dist map[string]int, refAsk func(itemID string) (float64, bool), fuelCostFor func(jumps int) float64) (missionCandidate, string) {
	item, qty, destBase, destSystem, ok := deliverShape(e)
	if !ok {
		return missionCandidate{}, "not a plain deliver mission"
	}
	jumps, reachable := dist[destSystem]
	if !reachable {
		return missionCandidate{}, fmt.Sprintf("destination system %s unreachable", destSystem)
	}
	if e.ExpiresInTicks > 0 && e.ExpiresInTicks < missionMinExpiryTicks+jumps*missionTicksPerJump {
		return missionCandidate{}, fmt.Sprintf("expires in %d ticks (< %d needed for %d jumps)",
			e.ExpiresInTicks, missionMinExpiryTicks+jumps*missionTicksPerJump, jumps)
	}
	reward := 0.0
	if e.Rewards != nil {
		reward = float64(e.Rewards.Credits)
	}
	buyQty := qty - e.ProvidedItems[item]
	if buyQty < 0 {
		buyQty = 0
	}
	itemCost := 0.0
	if buyQty > 0 {
		ask, priced := refAsk(item)
		if !priced || ask <= 0 {
			return missionCandidate{}, fmt.Sprintf("no reference ask for %s", item)
		}
		itemCost = float64(buyQty) * ask
	}
	fuelCost := fuelCostFor(jumps)
	net := reward - itemCost - fuelCost
	if net < missionMinNet {
		return missionCandidate{}, fmt.Sprintf("net %.0f below floor %.0f", net, missionMinNet)
	}
	return missionCandidate{
		Entry: e, ItemID: item, Qty: qty, BuyQty: buyQty,
		DestBaseID: destBase, DestSystem: destSystem,
		Reward: reward, ItemCost: itemCost, FuelCost: fuelCost, Net: net, Jumps: jumps,
	}, ""
}

// SelectMissionSet picks up to MissionMaxStack candidates to run as one trip:
// the best-net candidate anchors the trip, and only candidates sharing its
// destination system stack onto it (cross-system banding is a phase-2 upgrade).
// The greedy fill respects the buy budget (missionBuyBudgetFraction of credits)
// and free cargo space; anchors farther than maxJumps are skipped entirely.
func SelectMissionSet(cands []missionCandidate, credits, cargoFree float64, maxJumps int) []missionCandidate {
	if maxJumps <= 0 {
		maxJumps = DefaultMissionMaxJumps
	}
	sorted := make([]missionCandidate, 0, len(cands))
	for _, c := range cands {
		if c.Jumps <= maxJumps {
			sorted = append(sorted, c)
		}
	}
	if len(sorted) == 0 {
		return nil
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Net != sorted[j].Net {
			return sorted[i].Net > sorted[j].Net
		}
		return sorted[i].Entry.MissionID < sorted[j].Entry.MissionID
	})

	anchor := sorted[0]
	budget := credits * missionBuyBudgetFraction
	var picked []missionCandidate
	var spent, cargo float64
	for _, c := range sorted {
		if c.DestSystem != anchor.DestSystem {
			continue
		}
		if len(picked) >= MissionMaxStack {
			break
		}
		if spent+c.ItemCost > budget {
			continue
		}
		// The whole delivery quantity rides in the hold at once (provided items
		// are granted into cargo on accept).
		if cargo+float64(c.Qty) > cargoFree {
			continue
		}
		picked = append(picked, c)
		spent += c.ItemCost
		cargo += float64(c.Qty)
	}
	return picked
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestBuildMissionCandidate|TestSelectMissionSet' -v`
Expected: PASS

- [ ] **Step 5: Lint and commit**

Run: `go build ./... && golangci-lint run pkg/worker/`
Expected: OK, no new findings.

```bash
git add pkg/worker/mission_select.go pkg/worker/mission_select_test.go
git commit -m "feat(worker): mission selector — deliver-shape filter, expiry/cost gates, same-destination stacking"
```

---

### Task 3: Mission execution loop (`Missions` worker task)

**Files:**
- Create: `pkg/worker/mission.go`
- Test: `pkg/worker/mission_test.go`

**Interfaces:**
- Consumes: Task 2's `buildMissionCandidate`/`SelectMissionSet`; Task 1's `market.MissionResult`; existing helpers in the same package: `buildNameToID(systems)`, `navigation.JumpGraphFromConnections(conns)`, `navigation.BFSJumps(graph, from, targets)`, `haulFuelPerJump(ctx, client, probeTarget)`, `buildPriceOf(ctx, src)`, `cargoQty(state, itemID)`, `Autopilot(ctx, AutopilotDeps{Client, Out}, system, poi)`, `(*treasuryRescue).maybe(ctx, client, out, now)`, `game.SleepQuick`.
- Client methods (all existing, signatures verified): `GetMissions(ctx) error`, `GetActiveMissions(ctx) error`, `AcceptMission(ctx, missionID) error`, `CompleteMission(ctx, missionID) error`, `AbandonMission(ctx, missionID) error`, `Buy(ctx, itemID string, quantity float64) error`, `Dock(ctx) error`, `GetRawJSON(key)` with store keys `"missions"` and `"active_missions"` (both routed — verified `pkg/game/client.go:4849-4858`).
- Produces (Task 4 consumes): `type MissionDeps struct` and `func Missions(ctx context.Context, deps MissionDeps) error`, plus `type MissionStore interface` satisfied by `*market.Collector`.

Behavior contract (mirrors `Haul`'s resilience): every mid-run failure logs to `deps.Out` and returns nil so the worker idles and retries next pass; only dependency-missing misconfigurations log-and-skip too. The task never kills the worker loop.

- [ ] **Step 1: Write the failing tests**

Create `pkg/worker/mission_test.go`. The package-shared `fakeClient` (defined in `dispatch_test.go`, embeds `game.GameClient` so unimplemented methods panic) gains mission methods here — methods on a type may live in any file of the same package:

```go
package worker

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
)

// Mission-command fakes for the shared fakeClient (fields it needs are added
// to the struct in dispatch_test.go by this task — see Step 3 note).
func (f *fakeClient) GetMissions(ctx context.Context) error {
	f.calls = append(f.calls, "get_missions")
	return nil
}
func (f *fakeClient) GetActiveMissions(ctx context.Context) error {
	f.calls = append(f.calls, "get_active_missions")
	return nil
}
func (f *fakeClient) AcceptMission(ctx context.Context, id string) error {
	f.calls = append(f.calls, "accept:"+id)
	return f.acceptErr
}
func (f *fakeClient) CompleteMission(ctx context.Context, id string) error {
	f.calls = append(f.calls, "complete:"+id)
	f.state.Credits += f.completeReward // GetCredits() reads State.Credits
	return nil
}
func (f *fakeClient) AbandonMission(ctx context.Context, id string) error {
	f.calls = append(f.calls, "abandon:"+id)
	return nil
}

// fakeMissionStore records results and serves reference asks.
type fakeMissionStore struct {
	asks    map[string]float64
	results []market.MissionResult
}

func (s *fakeMissionStore) RecordMissionResult(ctx context.Context, r market.MissionResult) error {
	s.results = append(s.results, r)
	return nil
}
func (s *fakeMissionStore) GetReferenceAsk(ctx context.Context, itemID string) (market.ReferenceAsk, bool, error) {
	ask, ok := s.asks[itemID]
	return market.ReferenceAsk{ItemID: itemID, BestAsk: ask}, ok, nil
}

// missionKB returns a two-system KB (haven <-> sol) with the worker at haven.
func missionKB() *fakeKB {
	return &fakeKB{
		systems: []knowledge.System{{ID: "haven", Name: "Haven"}, {ID: "sol", Name: "Sol"}},
		conns:   []knowledge.Connection{{FromSystem: "haven", ToSystem: "sol"}},
	}
}

func boardJSON(t *testing.T, entries ...serverapi.MissionBoardEntry) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.GetMissionsResponse{Missions: entries, BaseID: "haven_station"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func activeJSON(t *testing.T, missions ...serverapi.ActiveMission) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.GetActiveMissionsResponse{Missions: missions})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func missionState(docked bool, credits, cargoUsed float64) *game.State {
	return &game.State{
		System:     game.SystemData{ID: "haven", Name: "Haven"},
		CurrentPOI: "haven_station",
		Doc:        docked,
		Credits:    credits, // State.Credits is what GetCredits() returns (types.go:581)
		Ship:       game.Ship{CargoCapacity: 100, CargoUsed: cargoUsed},
	}
}

func missionDeps(fc *fakeClient, store *fakeMissionStore, kb *fakeKB) MissionDeps {
	return MissionDeps{
		Client: fc, KB: kb, Market: store, Out: io.Discard, AgentID: "engineer-1",
		nav:   func(ctx context.Context, system, poi string) error { return nil },
		sleep: func(ctx context.Context, d time.Duration) error { return nil },
	}
}

func TestMissionsHappyPath(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	fc := &fakeClient{
		state:          missionState(true, 5000, 0),
		completeReward: 3000,
		raw: map[string][]byte{
			"missions":        boardJSON(t, entry),
			"active_missions": activeJSON(t),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	for _, want := range []string{"get_active_missions", "get_missions", "accept:m1", "buy:steel", "complete:m1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("calls missing %q: %v", want, fc.calls)
		}
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 result row, got %d", len(store.results))
	}
	r := store.results[0]
	if r.Outcome != "completed" || r.MissionID != "m1" || r.CreditsEarned != 3000 || r.ItemCost != 400 {
		t.Fatalf("result mismatch: %+v", r)
	}
}

func TestMissionsNotDockedSkips(t *testing.T) {
	fc := &fakeClient{state: missionState(false, 5000, 0), raw: map[string][]byte{}}
	fc.state.CurrentPOI = "" // adrift in space, not at a station POI
	store := &fakeMissionStore{}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	for _, c := range fc.calls {
		if strings.HasPrefix(c, "accept:") {
			t.Fatalf("must not accept while not docked: %v", fc.calls)
		}
	}
}

func TestMissionsBuyFailureAbandons(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	fc := &fakeClient{
		state:     missionState(true, 5000, 0),
		buyErr:    context.DeadlineExceeded, // any error: the station ran dry
		raw: map[string][]byte{
			"missions":        boardJSON(t, entry),
			"active_missions": activeJSON(t),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	if !strings.Contains(joined, "abandon:m1") {
		t.Fatalf("buy failure must abandon the accepted mission: %v", fc.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "abandoned" {
		t.Fatalf("want 1 abandoned row, got %+v", store.results)
	}
}

func TestMissionsDryPassesReposition(t *testing.T) {
	// Empty board every pass: the third consecutive dry pass must hop the
	// worker to the next nearby station instead of camping forever.
	fc := &fakeClient{state: missionState(true, 5000, 0), raw: map[string][]byte{
		"missions":        boardJSON(t),
		"active_missions": activeJSON(t),
	}}
	store := &fakeMissionStore{}
	deps := missionDeps(fc, store, missionKB())
	deps.State = &missionRunState{}
	deps.nearbyStations = func(ctx context.Context, limit int) ([]stationHop, error) {
		return []stationHop{{SystemID: "sol", POIID: "sol_station"}}, nil
	}
	var navTo []string
	deps.nav = func(ctx context.Context, system, poi string) error {
		navTo = append(navTo, system+"/"+poi)
		return nil
	}
	for range 3 {
		if err := Missions(context.Background(), deps); err != nil {
			t.Fatalf("Missions: %v", err)
		}
	}
	if len(navTo) != 1 || navTo[0] != "sol/sol_station" {
		t.Fatalf("3rd dry pass must reposition exactly once: %v", navTo)
	}
}

func TestMissionsResumesActiveDeliverable(t *testing.T) {
	active := serverapi.ActiveMission{
		MissionID: "held", Type: "delivery", Title: "Held delivery",
		Requirements: &serverapi.MissionRequirements{
			DeliverItemID: "steel", DeliverQuantity: 10, DeliverToBaseID: "sol_station",
		},
	}
	fc := &fakeClient{
		state:          missionState(true, 5000, 10),
		completeReward: 2000,
		raw: map[string][]byte{
			"missions":        boardJSON(t),
			"active_missions": activeJSON(t, active),
		},
	}
	fc.state.Ship.Cargo = []game.CargoItem{{ItemID: "steel", Quantity: 10}}
	store := &fakeMissionStore{}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if !strings.Contains(strings.Join(fc.calls, " "), "complete:held") {
		t.Fatalf("goods-aboard active mission must be completed: %v", fc.calls)
	}
}
```

Note on struct fields: `fakeClient` (in `dispatch_test.go`) needs three new fields for these tests — `acceptErr error`, `buyErr error`, `completeReward float64` — and its existing `Buy` method changes to `return f.buyErr` while KEEPING its `f.calls = append(f.calls, "buy:"+itemID)` line (the happy-path test asserts on it). `game.State.Player.Credits`, `game.Ship.Cargo []game.CargoItem`, and `cargoQty` already exist. Check the actual `fakeKB` struct fields in `pkg/worker/haul_test.go` before writing `missionKB` — if its systems/connections fields are named differently, adapt `missionKB` to match (do NOT change `fakeKB`). Likewise verify `knowledge.Connection`'s from/to field names before using them.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run 'TestMissions' -v`
Expected: FAIL to compile — `undefined: MissionDeps`, `undefined: Missions`.

- [ ] **Step 3: Implement the execution loop**

Add to `fakeClient` in `pkg/worker/dispatch_test.go`: fields `acceptErr error`, `buyErr error`, `completeReward float64`; change `Buy` to `return f.buyErr`.

Create `pkg/worker/mission.go`:

```go
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

const (
	// missionDryPassLimit: consecutive passes with no acceptable work before
	// the worker repositions to another station (spec: hop, don't camp).
	missionDryPassLimit = 3
	// missionRepositionPool: how many nearby stations the reposition cursor
	// rotates through.
	missionRepositionPool = 5
)

// MissionStore is the subset of *market.Collector the mission runner needs
// (result telemetry + item pricing for the cost gate).
type MissionStore interface {
	RecordMissionResult(ctx context.Context, r market.MissionResult) error
	GetReferenceAsk(ctx context.Context, itemID string) (market.ReferenceAsk, bool, error)
}

var _ MissionStore = (*market.Collector)(nil)

// missionRunState carries cross-pass memory (dry-pass streak + reposition
// cursor), held by WorkerDispatch so it survives between command passes —
// the shuttleState pattern.
type missionRunState struct {
	dry    int
	cursor int
}

// stationHop is one reposition target from the nearest-stations query.
type stationHop struct {
	SystemID string
	POIID    string
}

// MissionDeps are the injected collaborators for one Missions pass.
type MissionDeps struct {
	Client  game.GameClient
	KB      knowledge.Base
	Market  MissionStore
	Out     io.Writer // nil -> io.Discard
	AgentID string
	// MaxJumps caps mission-destination distance (0 -> DefaultMissionMaxJumps).
	MaxJumps int
	// Treasury rate-limits faction-treasury rescue withdrawals (nil disables).
	Treasury *treasuryRescue
	// FuelPrices supplies captured station fuel prices for the fuel-cost model.
	// nil disables fuel accounting (net == reward - item cost).
	FuelPrices FuelPriceSource
	// Now returns wall-clock time (nil -> time.Now); injected for tests.
	Now func() time.Time
	// State carries the cross-pass dry-streak/reposition memory (nil disables
	// repositioning — tests that don't care simply omit it).
	State *missionRunState
	// nav navigates to (system, poi); nil -> real Autopilot. Injected for tests,
	// mirroring WorkerDispatch.ensureHomeNav.
	nav func(ctx context.Context, system, poi string) error
	// nearbyStations lists reposition targets near the current system; nil ->
	// the galaxy-graph default built inside Missions. Injected for tests.
	nearbyStations func(ctx context.Context, limit int) ([]stationHop, error)
	// sleep is the post-fetch settle delay (nil -> craftPollSleepFunc, the
	// ctx-aware real sleep). Tests inject a zero-delay stand-in so the suite
	// doesn't accumulate real SleepQuick waits — the craftPollSleep pattern.
	sleep func(ctx context.Context, d time.Duration) error
}

func missionNow(deps MissionDeps) time.Time {
	if deps.Now != nil {
		return deps.Now()
	}
	return time.Now()
}

func missionTick(deps MissionDeps) int64 {
	if st := deps.Client.GetState(); st != nil {
		return st.GetTick()
	}
	return 0
}

// Missions performs one mission-runner pass: complete any resumable active
// missions, read the local board, accept + provision a stackable deliver set,
// run it to the shared destination system, and record every outcome. Mirrors
// Haul's resilience contract: mid-run failures log and return nil so the
// worker idles and retries; the pass never kills the worker loop.
func Missions(ctx context.Context, deps MissionDeps) error {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	if deps.Market == nil {
		fmt.Fprintln(out, "missions: market collector not configured; skipping") //nolint:errcheck
		return nil
	}
	if deps.KB == nil {
		fmt.Fprintln(out, "missions: no knowledge base; skipping") //nolint:errcheck
		return nil
	}
	if deps.nav == nil {
		deps.nav = func(ctx context.Context, system, poi string) error {
			return Autopilot(ctx, AutopilotDeps{Client: deps.Client, Out: out}, system, poi)
		}
	}
	if deps.sleep == nil {
		deps.sleep = craftPollSleepFunc
	}
	state := deps.Client.GetState()
	if state == nil || state.System.ID == "" {
		fmt.Fprintln(out, "missions: current system unknown; skipping") //nolint:errcheck
		return nil
	}
	// A board only exists at a station. Not at one -> dock if possible, else
	// idle this pass (the role's ensure_home/reposition machinery moves us).
	if state.CurrentPOI == "" {
		fmt.Fprintln(out, "missions: not at a station POI; idling") //nolint:errcheck
		return nil
	}
	if !state.Doc {
		if err := deps.Client.Dock(ctx); err != nil {
			fmt.Fprintf(out, "missions: dock failed: %v; idling\n", err) //nolint:errcheck
			return nil
		}
	}

	// Routing substrate (same shape as Haul).
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("missions: get connections: %w", err)
	}
	graph := navigation.JumpGraphFromConnections(conns)
	current := state.System.ID

	// Default reposition source: nearest accessible stations by the galaxy
	// graph (the same query haul's stranded-recovery uses; it excludes
	// strongholds and non-public stations).
	if deps.nearbyStations == nil {
		deps.nearbyStations = func(ctx context.Context, limit int) ([]stationHop, error) {
			gal := &galaxy.GalaxyGraph{}
			if gerr := gal.BuildFromDB(ctx, deps.KB); gerr != nil {
				return nil, gerr
			}
			near, nerr := galaxy.FindNearestByPOIType(ctx, deps.KB, gal, current, "station", limit)
			if nerr != nil {
				return nil, nerr
			}
			hops := make([]stationHop, 0, len(near))
			for _, n := range near {
				if n.SystemID == current || len(n.POIs) == 0 {
					continue // skip the station we're camping; we want elsewhere
				}
				hops = append(hops, stationHop{SystemID: n.SystemID, POIID: n.POIs[0].ID})
			}
			return hops, nil
		}
	}

	// Resume held missions before accepting new ones: complete what's aboard,
	// abandon what isn't (v1 keeps resume simple; a lost-cargo mission cannot
	// be completed anyway).
	if done := missionResume(ctx, deps, out, current); done {
		return nil
	}

	// Idle and unencumbered: safe point for a treasury top-up before buying.
	deps.Treasury.maybe(ctx, deps.Client, out, missionNow(deps))

	// Read the live board.
	board, baseID, ok := missionReadBoard(ctx, deps, out)
	if !ok || len(board) == 0 {
		fmt.Fprintln(out, "missions: no board entries here") //nolint:errcheck
		return missionDryPass(ctx, deps, out)
	}

	// Distance map to every candidate destination.
	targets := make([]string, 0, len(board))
	for _, e := range board {
		if _, _, _, destSys, shaped := deliverShape(e); shaped {
			targets = append(targets, destSys)
		}
	}
	dist := navigation.BFSJumps(graph, current, targets)
	dist[current] = 0

	// Fuel model (same probe as Haul).
	probeTarget := ""
	for _, nb := range graph[current] {
		probeTarget = nb
		break
	}
	fuelPerJump := haulFuelPerJump(ctx, deps.Client, probeTarget)
	priceOf := buildPriceOf(ctx, deps.FuelPrices)
	fuelCostFor := func(jumps int) float64 {
		if fuelPerJump <= 0 || priceOf == nil {
			return 0
		}
		return float64(jumps*fuelPerJump) * priceOf(state.CurrentPOI)
	}
	refAsk := func(itemID string) (float64, bool) {
		ra, found, aerr := deps.Market.GetReferenceAsk(ctx, itemID)
		if aerr != nil || !found {
			return 0, false
		}
		return ra.BestAsk, true
	}

	// Gate + stack.
	var cands []missionCandidate
	for _, e := range board {
		c, reason := buildMissionCandidate(e, dist, refAsk, fuelCostFor)
		if reason != "" {
			fmt.Fprintf(out, "missions: skip %s (%s): %s\n", e.MissionID, e.Title, reason) //nolint:errcheck
			continue
		}
		cands = append(cands, c)
	}
	st := deps.Client.GetState()
	cargoFree := st.Ship.CargoCapacity - st.Ship.CargoUsed
	set := SelectMissionSet(cands, st.GetCredits(), cargoFree, deps.MaxJumps)
	if len(set) == 0 {
		fmt.Fprintln(out, "missions: no acceptable missions on this board") //nolint:errcheck
		return missionDryPass(ctx, deps, out)
	}
	if deps.State != nil {
		deps.State.dry = 0 // found work: reset the dry streak
	}

	// Accept + provision. A failed accept just drops that mission from the trip;
	// a failed buy abandons the accepted mission (recorded) and drops it.
	acceptedAt, acceptedTick := rfc(missionNow(deps)), missionTick(deps)
	trip := make([]missionCandidate, 0, len(set))
	for _, c := range set {
		if aerr := deps.Client.AcceptMission(ctx, c.Entry.MissionID); aerr != nil {
			fmt.Fprintf(out, "missions: accept %s failed: %v\n", c.Entry.MissionID, aerr) //nolint:errcheck
			continue
		}
		if c.BuyQty > 0 {
			if berr := deps.Client.Buy(ctx, c.ItemID, float64(c.BuyQty)); berr != nil {
				fmt.Fprintf(out, "missions: buy %dx %s for %s failed: %v; abandoning\n", c.BuyQty, c.ItemID, c.Entry.MissionID, berr) //nolint:errcheck
				missionAbandon(ctx, deps, out, c, baseID, acceptedAt, acceptedTick)
				continue
			}
		}
		trip = append(trip, c)
	}
	if len(trip) == 0 {
		return nil
	}

	// One shared destination system (SelectMissionSet guarantees it). Transit,
	// then complete each mission at its own base within that system.
	dest := trip[0].DestSystem
	fmt.Fprintf(out, "missions: running %d mission(s) to %s (%d jumps, est net %.0f)\n",
		len(trip), dest, trip[0].Jumps, tripNet(trip)) //nolint:errcheck
	for i, c := range trip {
		if nerr := deps.nav(ctx, dest, c.DestBaseID); nerr != nil {
			fmt.Fprintf(out, "missions: transit to %s failed: %v; %d mission(s) left held for next pass\n", c.DestBaseID, nerr, len(trip)-i) //nolint:errcheck
			return nil // held missions resume on the next pass
		}
		if derr := deps.Client.Dock(ctx); derr != nil {
			fmt.Fprintf(out, "missions: dock at %s failed: %v; held for next pass\n", c.DestBaseID, derr) //nolint:errcheck
			return nil
		}
		missionComplete(ctx, deps, out, c, baseID, acceptedAt, acceptedTick)
	}
	return nil
}

// missionDryPass counts a no-work pass; on the missionDryPassLimit-th
// consecutive one, the worker repositions to the next nearby station
// (rotating cursor) instead of camping a dry board forever. Nil State (no
// cross-pass memory) just idles.
func missionDryPass(ctx context.Context, deps MissionDeps, out io.Writer) error {
	if deps.State == nil {
		return nil
	}
	deps.State.dry++
	if deps.State.dry < missionDryPassLimit {
		return nil
	}
	hops, err := deps.nearbyStations(ctx, missionRepositionPool)
	if err != nil || len(hops) == 0 {
		fmt.Fprintf(out, "missions: reposition lookup failed (%v, %d targets); idling\n", err, len(hops)) //nolint:errcheck
		return nil
	}
	hop := hops[deps.State.cursor%len(hops)]
	deps.State.cursor++
	deps.State.dry = 0
	fmt.Fprintf(out, "missions: %d dry passes; repositioning to %s/%s\n", missionDryPassLimit, hop.SystemID, hop.POIID) //nolint:errcheck
	if nerr := deps.nav(ctx, hop.SystemID, hop.POIID); nerr != nil {
		fmt.Fprintf(out, "missions: reposition transit failed: %v; retry next pass\n", nerr) //nolint:errcheck
		return nil
	}
	if derr := deps.Client.Dock(ctx); derr != nil {
		fmt.Fprintf(out, "missions: reposition dock failed: %v\n", derr) //nolint:errcheck
	}
	return nil
}

// tripNet sums the estimated net of a selected trip (for the log line).
func tripNet(trip []missionCandidate) float64 {
	total := 0.0
	for _, c := range trip {
		total += c.Net
	}
	return total
}

// missionReadBoard fetches and parses the local mission board. ok=false on any
// fetch/parse problem (logged), so the pass idles rather than erroring out.
func missionReadBoard(ctx context.Context, deps MissionDeps, out io.Writer) ([]serverapi.MissionBoardEntry, string, bool) {
	if err := deps.Client.GetMissions(ctx); err != nil {
		fmt.Fprintf(out, "missions: get_missions: %v\n", err) //nolint:errcheck
		return nil, "", false
	}
	_ = deps.sleep(ctx, game.SleepQuick)
	raw := deps.Client.GetRawJSON("missions")
	if len(raw) == 0 {
		fmt.Fprintln(out, "missions: get_missions returned no data") //nolint:errcheck
		return nil, "", false
	}
	var resp serverapi.GetMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(out, "missions: parse board: %v\n", err) //nolint:errcheck
		return nil, "", false
	}
	return resp.Missions, resp.BaseID, true
}

// missionResume handles missions held from a previous pass/process: deliverable
// ones (goods aboard) are run to completion; the rest are abandoned so their
// slots free up. Returns true when it acted (the pass ends; the board is read
// fresh next pass).
func missionResume(ctx context.Context, deps MissionDeps, out io.Writer, current string) bool {
	if err := deps.Client.GetActiveMissions(ctx); err != nil {
		fmt.Fprintf(out, "missions: get_active_missions: %v\n", err) //nolint:errcheck
		return false
	}
	_ = deps.sleep(ctx, game.SleepQuick)
	raw := deps.Client.GetRawJSON("active_missions")
	if len(raw) == 0 {
		return false
	}
	var resp serverapi.GetActiveMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(out, "missions: parse active missions: %v\n", err) //nolint:errcheck
		return false
	}
	if len(resp.Missions) == 0 {
		return false
	}
	acted := false
	for _, m := range resp.Missions {
		r := m.Requirements
		if r == nil || r.DeliverItemID == "" || r.DeliverToBaseID == "" {
			continue // non-deliver active mission: leave it alone (manual/other origin)
		}
		aboard := cargoQty(deps.Client.GetState(), r.DeliverItemID)
		held := missionCandidate{
			Entry: serverapi.MissionBoardEntry{
				MissionID: m.MissionID, TemplateID: m.TemplateID, Type: m.Type, Title: m.Title,
			},
			ItemID: r.DeliverItemID, Qty: r.DeliverQuantity, DestBaseID: r.DeliverToBaseID,
		}
		if aboard >= float64(r.DeliverQuantity) {
			// Deliverable: resolve the destination system via FindRoute (the
			// active-mission payload has no system id) and run it in.
			route, rerr := deps.Client.FindRoute(ctx, r.DeliverToBaseID)
			destSys := current
			if rerr == nil && len(route) > 0 {
				destSys = route[len(route)-1].SystemID
			}
			fmt.Fprintf(out, "missions: resuming held %s (%s) -> %s\n", m.MissionID, m.Title, r.DeliverToBaseID) //nolint:errcheck
			if nerr := deps.nav(ctx, destSys, r.DeliverToBaseID); nerr != nil {
				fmt.Fprintf(out, "missions: resume transit failed: %v; retry next pass\n", nerr) //nolint:errcheck
				return true
			}
			if derr := deps.Client.Dock(ctx); derr != nil {
				fmt.Fprintf(out, "missions: resume dock failed: %v; retry next pass\n", derr) //nolint:errcheck
				return true
			}
			missionComplete(ctx, deps, out, held, "", rfc(missionNow(deps)), missionTick(deps))
		} else {
			fmt.Fprintf(out, "missions: abandoning held %s (%s): cargo %s %.0f/%d\n",
				m.MissionID, m.Title, r.DeliverItemID, aboard, r.DeliverQuantity) //nolint:errcheck
			missionAbandon(ctx, deps, out, held, "", rfc(missionNow(deps)), missionTick(deps))
		}
		acted = true
	}
	return acted
}

// missionComplete completes c, measuring realized income as the wallet delta
// (the raw router has no complete_mission store key), and records the row.
func missionComplete(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, fromBase, acceptedAt string, acceptedTick int64) {
	before := deps.Client.GetState().GetCredits()
	if err := deps.Client.CompleteMission(ctx, c.Entry.MissionID); err != nil {
		fmt.Fprintf(out, "missions: complete %s failed: %v; held for next pass\n", c.Entry.MissionID, err) //nolint:errcheck
		return
	}
	_ = deps.sleep(ctx, game.SleepQuick) // let the ok response update credits in State
	earned := deps.Client.GetState().GetCredits() - before
	fmt.Fprintf(out, "missions: completed %s (%s): +%.0f cr (expected %.0f)\n", c.Entry.MissionID, c.Entry.Title, earned, c.Reward) //nolint:errcheck
	missionRecord(ctx, deps, out, c, fromBase, acceptedAt, acceptedTick, earned, "completed")
}

// missionAbandon abandons c and records the loss row (item cost already sunk).
func missionAbandon(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, fromBase, acceptedAt string, acceptedTick int64) {
	if err := deps.Client.AbandonMission(ctx, c.Entry.MissionID); err != nil {
		fmt.Fprintf(out, "missions: abandon %s failed: %v\n", c.Entry.MissionID, err) //nolint:errcheck
	}
	missionRecord(ctx, deps, out, c, fromBase, acceptedAt, acceptedTick, 0, "abandoned")
}

func missionRecord(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, fromBase, acceptedAt string, acceptedTick int64, earned float64, outcome string) {
	now := missionNow(deps)
	r := market.MissionResult{
		AgentID:        deps.AgentID,
		MissionID:      c.Entry.MissionID,
		TemplateID:     c.Entry.TemplateID,
		MissionType:    c.Entry.Type,
		Title:          c.Entry.Title,
		FromBaseID:     fromBase,
		ToBaseID:       c.DestBaseID,
		ItemID:         c.ItemID,
		Qty:            float64(c.Qty),
		ExpectedReward: c.Reward,
		CreditsEarned:  earned,
		ItemCost:       c.ItemCost,
		FuelCost:       c.FuelCost,
		Jumps:          c.Jumps,
		Outcome:        outcome,
		AcceptedAt:     acceptedAt,
		FinishedAt:     rfc(now),
		AcceptedTick:   acceptedTick,
		FinishedTick:   missionTick(deps),
		CreatedAt:      rfc(now),
	}
	if err := deps.Market.RecordMissionResult(ctx, r); err != nil {
		fmt.Fprintf(out, "missions: record result %s: %v\n", c.Entry.MissionID, err) //nolint:errcheck
	}
}
```

Implementation notes for the engineer:
- `rfc(t)` (RFC3339 UTC formatter) already exists in `haul.go` — reuse, don't redefine.
- `GetCredits()` reads `State.Credits` (`pkg/game/types.go:581`); the fake's
  `CompleteMission` bumps that field directly.
- The `deps.sleep(ctx, game.SleepQuick)` settle delay after fetch commands mirrors `KBUpdateMissions` (`pkg/worker/capture.go`); tests inject a zero-delay stand-in via `missionDeps` so the suite stays fast. `craftPollSleepFunc` (the real default) already exists in `dispatch.go`.
- If `knowledge` ends up unused after wiring (only `knowledge.Base` in `MissionDeps` uses it), keep the import — the deps struct references it.
- `galaxy.FindNearestByPOIType(ctx, kb, graph, current, "station", limit)` and its result fields (`SystemID`, `Hops`, `POIs[0].ID`) were verified against the call in `haulRecoverIfStranded` (`pkg/worker/haul.go:667`); check the result type's exact name there if the compiler disagrees.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestMissions' -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Full package test + lint**

Run: `go build ./... && go test ./pkg/worker/ && golangci-lint run pkg/worker/`
Expected: PASS, no new findings (the whole package, to catch fakeClient changes breaking other tests).

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/mission.go pkg/worker/mission_test.go pkg/worker/dispatch_test.go
git commit -m "feat(worker): missions task — resume, board read, stacked accept/provision/complete, telemetry"
```

---

### Task 4: Dispatch + role wiring

**Files:**
- Modify: `pkg/worker/dispatch.go` (supported map ~line 90, Run switch ~line 180)
- Modify: `data/overmind/roles.yaml`
- Test: extend `pkg/worker/dispatch_test.go`

**Interfaces:**
- Consumes: Task 3's `Missions(ctx, MissionDeps)`.
- Produces: worker command `"missions"`; role `missionrunner`. `roles_test.go` already enforces that every command named in roles.yaml exists in the supported map — it will fail if the wiring is incomplete.

- [ ] **Step 1: Write the failing test**

Add to `pkg/worker/dispatch_test.go`:

```go
func TestDispatchMissionsCommand(t *testing.T) {
	fc := &fakeClient{state: missionState(true, 5000, 0), raw: map[string][]byte{}}
	d := NewWorkerDispatch(fc, nil, nil, io.Discard)
	if !d.Supports("missions") {
		t.Fatal("missions must be a supported worker command")
	}
	// No market collector configured -> logs and returns nil (degraded no-op),
	// matching the haul command's contract.
	if err := d.Run(context.Background(), []string{"missions"}); err != nil {
		t.Fatalf("missions without market collector must no-op, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run TestDispatchMissionsCommand -v`
Expected: FAIL — `missions must be a supported worker command`.

- [ ] **Step 3: Wire the command**

In `pkg/worker/dispatch.go`:

1. In the `supported` map (~line 90), change the line
   `"explore": true, "scan": true, "haul": true, "shuttle": true, "assist": true,`
   to
   `"explore": true, "scan": true, "haul": true, "shuttle": true, "assist": true, "missions": true,`

2. In the `WorkerDispatch` struct, after the `shuttle *shuttleState` field:

```go
	// mission carries cross-pass mission-runner memory (dry-pass streak +
	// reposition cursor), the shuttleState pattern.
	mission *missionRunState
```

3. In `NewWorkerDispatch`, alongside `shuttle: &shuttleState{}`:

```go
		mission:        &missionRunState{},
```

4. In the `Run` switch, after the `case "haul":` block:

```go
	case "missions":
		if d.Market == nil {
			fmt.Fprintln(d.Out, "missions: market collector not configured (use --market-db-path)") //nolint:errcheck
			return nil
		}
		return Missions(ctx, MissionDeps{
			Client: d.Client, KB: d.KB, Market: d.Market, Out: d.Out, AgentID: d.AgentID,
			Treasury: d.treasury, FuelPrices: d.Market, State: d.mission,
		})
```

In `data/overmind/roles.yaml`, append after the `craftsman` role:

```yaml
  missionrunner:
    schedule:
      - { every: hourly, command: "update_market" }
      - { every: hourly, command: "capture_fuel" }
      - { every: daily, command: "kb_update" }
    idle: missions
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestDispatchMissionsCommand|TestRoles' -v && go test ./pkg/worker/`
Expected: PASS, including the roles_test command-coverage guard.

- [ ] **Step 5: Full build/test + lint, then commit**

Run: `go build ./... && go test ./... && golangci-lint run pkg/worker/`
Expected: all green.

```bash
git add pkg/worker/dispatch.go pkg/worker/dispatch_test.go data/overmind/roles.yaml
git commit -m "feat(worker): wire missions command into dispatch + missionrunner role"
```

---

### Task 5: Canary fleet config + provisioning/launch runbook

**Files:**
- Create: `data/overmind/mission-fleet.yaml`
- Create: `docs/superpowers/plans/2026-07-16-mission-canary-runbook.md`

No Go code — this task is config plus an operator runbook. It is still a reviewable unit: wrong roster or launch steps would sink the canary.

- [ ] **Step 1: Write the fleet roster**

Create `data/overmind/mission-fleet.yaml`:

```yaml
# Mission-runner canary (4): engineer-1/2 + fighter-1/2, all role=missionrunner.
# All four are band 1-2 accounts (same empire) per the agent empire-band
# numbering; verify each agent's actual empire via get_status during
# provisioning before first launch. Spec:
# docs/superpowers/specs/2026-07-16-mission-runner-fleet-design.md
workers:
  - { agent_id: engineer-1, role: missionrunner, station: "" }
  - { agent_id: engineer-2, role: missionrunner, station: "" }
  - { agent_id: fighter-1, role: missionrunner, station: "" }
  - { agent_id: fighter-2, role: missionrunner, station: "" }
```

- [ ] **Step 2: Write the provisioning + launch runbook**

Create `docs/superpowers/plans/2026-07-16-mission-canary-runbook.md`:

```markdown
# Mission-Runner Canary — Provisioning & Launch Runbook

Operator steps (manual, via play_as). Worker-agent manual commands require
the supervisor-freeze + worker-stop protocol ONLY for agents in a running
fleet; these four are idle, so plain play_as sessions are fine.

## 1. Per-agent provisioning (engineer-1, engineer-2, fighter-1, fighter-2)

For each agent, `go run ./cmd/tools/play_as <agent-id>` and:

1. `get_status` — record credits, current ship, system, and EMPIRE. All four
   must be in the same empire; if one isn't, swap it for another idle
   band-1/2 account and update mission-fleet.yaml before launch.
2. Ship check: the target hull is a T2 freighter-class (guide: "a T2
   freighter with a weapon mount covers 90% of boards"; v1 skips the
   weapon). At a shipyard station, list hulls and pick the ~8,000 cr T2
   cargo hull; `buy_ship` it and `switch_ship`.
3. Funding: each agent needs ship cost + ~10,000 cr working capital for
   mission cargo buys. Fund from a treasury-holding agent via send_gift
   (note: `send_gift --source=cargo|storage` is on an unpushed local
   commit; plain credit gifts work on main).
4. `buy_insurance` on the new hull (premiums are trivial; guide
   recommendation).
5. Park docked at a station with a mission board (`get_missions` returns
   entries) so the first pass has work.

## 2. Launch

1. Build: `go build -o bin/overmind ./cmd/overmind` (binaries go in bin/).
2. Construct the launch line by mirroring the RUNNING haul fleet's exact
   flags: `cat /proc/$(pgrep -f overmind | head -1)/cmdline | tr '\0' ' '`
   — swap in `--fleet data/overmind/mission-fleet.yaml` and a distinct
   socket/log (`mission.sock`, `mission-overmind.log`). Remove any stale
   `data/overmind/mission.sock` first (`rm -f`).
3. Stagger startup (login rate limits are per-IP/minute; 4 workers is
   safe, but do not relaunch repeatedly in a tight loop).
4. Add the final launch line to the overmind launch-commands runbook
   (memory: reference_overmind_launch_commands) the SAME DAY — the
   arbitrage scanner was once lost from a relaunch because its line was
   missing there.

## 3. Verify (first hour)

- `tail -f data/overmind/mission-overmind.log` — expect "missions:" lines:
  board reads, skips with reasons, accepts, completes.
- `sqlite3 <market.db> "SELECT agent_id, outcome, expected_reward,
  credits_earned, item_cost, jumps FROM mission_results ORDER BY id DESC
  LIMIT 20;"` — rows appearing with outcome=completed and positive
  credits_earned is the success signal.
- Watch for pathologies: repeated abandon rows (selector gates too loose),
  zero board entries everywhere (parked at boardless stations), buys
  failing (underfunded).

## 4. Measure (before any scale-up)

Run for 48h+, then compare net cr/hour/worker:
`SELECT agent_id, COUNT(*), SUM(credits_earned - item_cost - fuel_cost)
 FROM mission_results WHERE outcome='completed' GROUP BY agent_id;`
against haul-fleet realized economics. Scale-up (more workers, exploration
circuits, dashboard panel) is a separate decision from this data.
```

- [ ] **Step 3: Verify config parses**

Run: `go test ./pkg/worker/ -run TestRoles -v` (roles/fleet parsing guards) and `go build ./...`
Expected: PASS. Also check `.gitignore` does not swallow the new files: `git check-ignore data/overmind/mission-fleet.yaml docs/superpowers/plans/2026-07-16-mission-canary-runbook.md` — expect no output (exit 1).

- [ ] **Step 4: Commit**

```bash
git add data/overmind/mission-fleet.yaml docs/superpowers/plans/2026-07-16-mission-canary-runbook.md
git commit -m "feat(overmind): mission-runner canary fleet config + provisioning/launch runbook"
```

---

### Task 6: Live smoke (operator-gated)

No files. Execute the runbook's provisioning + launch steps with ONE agent
(engineer-1) first: provision, run a single `missions` pass via play_as or a
one-worker fleet, and watch one full accept → transit → complete →
mission_results row cycle before starting the other three. This validates
the assumptions unit tests cannot: real board entry shapes (do procedural
delivery missions carry `objectives[].system_id`? does accept grant
`provided_items` into cargo?), credits-delta measurement, and expiry
behavior. If board entries are shaped differently than `MissionBoardEntry`
suggests, capture the raw JSON (`get_missions` via play_as), fix
`deliverShape`/tests to match reality, and re-run Tasks 2-3's suites.

This task is deliberately last and requires the operator (Robert) to be
present — it spends real credits and occupies real agents.
