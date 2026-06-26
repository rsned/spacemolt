# Hauler Dashboard — Phase 1 Recorders Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the durable recorders that capture real per-haul outcomes (with per-leg timing + jumps) and quarter-hourly fleet credit/fuel/cargo snapshots into `market.db`, so the Phase 2 dashboard generator has time-series to render.

**Architecture:** Two new `market.db` tables (`haul_results`, `fleet_timeseries`) with `Collector` writer methods. The worker (`pkg/worker/haul.go`) threads a metrics accumulator through a full fresh haul, stamping wall-time + game-tick at each leg boundary, and writes one `haul_results` row on completion. The overmind (`cmd/overmind/main.go`) opens a `market.Collector` and writes one `fleet_timeseries` row per hauler on quarter-hour boundaries, sourcing cargo from new `cargo_used`/`cargo_capacity` fields plumbed worker→status→fleet-status.json.

**Tech Stack:** Go 1.24, `modernc.org/sqlite` (via existing `market.Collector`), existing overmind control/supervisor/balances packages.

## Global Constraints

- Go 1.24+; use `b.Loop()` in any benchmark (none expected here).
- All new code passes `golangci-lint` with zero new findings.
- `market.db` migrations use the existing pattern: tables via `CREATE TABLE IF NOT EXISTS` in `pkg/market/schema.sql` (run by `runMigrations`); added columns via `ensureColumn`.
- Run `go build ./...` and `go test ./...` green before each commit.
- Any compiled binary goes in `bin/`, never the repo root.
- Sleep/pauses use constants from `pkg/game/constants.go`.
- Phase 2 (the `haul-dashboard` generator) is OUT OF SCOPE for this plan.

---

### Task 1: `haul_results` table + `HaulResult` type + `RecordHaulResult`

**Files:**
- Modify: `pkg/market/schema.sql` (append table)
- Modify: `pkg/market/types.go` (add `HaulResult` struct)
- Create: `pkg/market/haul_results.go` (writer + reader)
- Test: `pkg/market/haul_results_test.go`

**Interfaces:**
- Produces: `market.HaulResult` struct; `func (c *Collector) RecordHaulResult(ctx context.Context, r HaulResult) error`; `func (c *Collector) GetHaulResults(ctx context.Context, agentID string, limit int) ([]HaulResult, error)`.

- [ ] **Step 1: Add the table to `pkg/market/schema.sql`** (append at end)

```sql
-- Per-haul real outcomes + per-leg timing (dashboard Component A).
CREATE TABLE IF NOT EXISTS haul_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    opp_id          INTEGER NOT NULL,
    agent_id        TEXT NOT NULL,
    item_id         TEXT NOT NULL,
    qty             REAL NOT NULL,
    buy_price_paid  REAL NOT NULL,
    sell_price_got  REAL NOT NULL,
    realized_profit REAL NOT NULL,
    jumps_traveled  INTEGER NOT NULL,
    claimed_at      TEXT NOT NULL,
    arrived_src_at  TEXT NOT NULL,
    bought_at       TEXT NOT NULL,
    arrived_dst_at  TEXT NOT NULL,
    sold_at         TEXT NOT NULL,
    claimed_tick    INTEGER NOT NULL,
    arrived_src_tick INTEGER NOT NULL,
    bought_tick     INTEGER NOT NULL,
    arrived_dst_tick INTEGER NOT NULL,
    sold_tick       INTEGER NOT NULL,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_haul_results_agent_time ON haul_results(agent_id, sold_at);
```

- [ ] **Step 2: Add `HaulResult` to `pkg/market/types.go`**

```go
// HaulResult is the real, cargo-capped outcome of one completed haul, with per-leg
// timing in wall-time (RFC3339) and game ticks. Drives the dashboard's response-time
// and credits/jump charts and the true realized-profit column.
type HaulResult struct {
	ID             int64   `json:"id"`
	OppID          int     `json:"opp_id"`
	AgentID        string  `json:"agent_id"`
	ItemID         string  `json:"item_id"`
	Qty            float64 `json:"qty"`
	BuyPricePaid   float64 `json:"buy_price_paid"`
	SellPriceGot   float64 `json:"sell_price_got"`
	RealizedProfit float64 `json:"realized_profit"`
	JumpsTraveled  int     `json:"jumps_traveled"`
	ClaimedAt      string  `json:"claimed_at"`
	ArrivedSrcAt   string  `json:"arrived_src_at"`
	BoughtAt       string  `json:"bought_at"`
	ArrivedDstAt   string  `json:"arrived_dst_at"`
	SoldAt         string  `json:"sold_at"`
	ClaimedTick    int64   `json:"claimed_tick"`
	ArrivedSrcTick int64   `json:"arrived_src_tick"`
	BoughtTick     int64   `json:"bought_tick"`
	ArrivedDstTick int64   `json:"arrived_dst_tick"`
	SoldTick       int64   `json:"sold_tick"`
	CreatedAt      string  `json:"created_at"`
}
```

- [ ] **Step 3: Write the failing test `pkg/market/haul_results_test.go`**

```go
package market

import (
	"context"
	"testing"
	"time"
)

func TestRecordAndGetHaulResult(t *testing.T) {
	c := newTestCollector(t) // existing helper in pkg/market tests; opens an in-memory/temp db with runMigrations
	ctx := context.Background()
	r := HaulResult{
		OppID: 42, AgentID: "trader-1", ItemID: "silicon_ore", Qty: 50,
		BuyPricePaid: 100, SellPriceGot: 130, RealizedProfit: (130 - 100) * 50,
		JumpsTraveled: 3,
		ClaimedAt:    "2026-06-26T10:00:00Z",
		ArrivedSrcAt: "2026-06-26T10:01:00Z",
		BoughtAt:     "2026-06-26T10:01:30Z",
		ArrivedDstAt: "2026-06-26T10:03:00Z",
		SoldAt:       "2026-06-26T10:03:30Z",
		ClaimedTick: 1000, ArrivedSrcTick: 1006, BoughtTick: 1007,
		ArrivedDstTick: 1015, SoldTick: 1016,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := c.RecordHaulResult(ctx, r); err != nil {
		t.Fatalf("RecordHaulResult: %v", err)
	}
	got, err := c.GetHaulResults(ctx, "trader-1", 10)
	if err != nil {
		t.Fatalf("GetHaulResults: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].RealizedProfit != 1500 || got[0].JumpsTraveled != 3 || got[0].ItemID != "silicon_ore" {
		t.Fatalf("round-trip mismatch: %+v", got[0])
	}
	if got[0].SoldTick != 1016 || got[0].ClaimedAt != "2026-06-26T10:00:00Z" {
		t.Fatalf("leg-stamp round-trip mismatch: %+v", got[0])
	}
}
```

(If `newTestCollector` is not the existing helper name, use whatever the other `pkg/market/*_test.go` files use to construct a migrated `*Collector` — match `arbitrage_test.go`.)

- [ ] **Step 4: Run test, verify it fails** — `go test ./pkg/market/ -run TestRecordAndGetHaulResult` → FAIL (RecordHaulResult undefined).

- [ ] **Step 5: Implement `pkg/market/haul_results.go`**

```go
package market

import (
	"context"
	"database/sql"
	"fmt"
)

// RecordHaulResult writes one completed-haul outcome row.
func (c *Collector) RecordHaulResult(ctx context.Context, r HaulResult) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO haul_results
 (opp_id, agent_id, item_id, qty, buy_price_paid, sell_price_got, realized_profit,
  jumps_traveled, claimed_at, arrived_src_at, bought_at, arrived_dst_at, sold_at,
  claimed_tick, arrived_src_tick, bought_tick, arrived_dst_tick, sold_tick, created_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.OppID, r.AgentID, r.ItemID, r.Qty, r.BuyPricePaid, r.SellPriceGot, r.RealizedProfit,
			r.JumpsTraveled, r.ClaimedAt, r.ArrivedSrcAt, r.BoughtAt, r.ArrivedDstAt, r.SoldAt,
			r.ClaimedTick, r.ArrivedSrcTick, r.BoughtTick, r.ArrivedDstTick, r.SoldTick, r.CreatedAt)
		return err
	})
}

// GetHaulResults returns the most recent haul results for agentID (all agents if empty),
// newest sold first, capped at limit (<=0 -> 500).
func (c *Collector) GetHaulResults(ctx context.Context, agentID string, limit int) ([]HaulResult, error) {
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT id, opp_id, agent_id, item_id, qty, buy_price_paid, sell_price_got,
 realized_profit, jumps_traveled, claimed_at, arrived_src_at, bought_at, arrived_dst_at,
 sold_at, claimed_tick, arrived_src_tick, bought_tick, arrived_dst_tick, sold_tick, created_at
 FROM haul_results`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id = ?`
		args = append(args, agentID)
	}
	q += ` ORDER BY sold_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get haul results: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []HaulResult
	for rows.Next() {
		var r HaulResult
		if err := rows.Scan(&r.ID, &r.OppID, &r.AgentID, &r.ItemID, &r.Qty, &r.BuyPricePaid,
			&r.SellPriceGot, &r.RealizedProfit, &r.JumpsTraveled, &r.ClaimedAt, &r.ArrivedSrcAt,
			&r.BoughtAt, &r.ArrivedDstAt, &r.SoldAt, &r.ClaimedTick, &r.ArrivedSrcTick,
			&r.BoughtTick, &r.ArrivedDstTick, &r.SoldTick, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan haul result: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 6: Run test, verify PASS** — `go test ./pkg/market/ -run TestRecordAndGetHaulResult`
- [ ] **Step 7: Commit** — `git add pkg/market/schema.sql pkg/market/types.go pkg/market/haul_results.go pkg/market/haul_results_test.go && git commit -m "feat(market): haul_results table + RecordHaulResult/GetHaulResults"`

---

### Task 2: Worker leg-stamping + jumps + RecordHaulResult on completion

**Files:**
- Modify: `pkg/worker/haul.go` (add `haulMetrics`, thread through `Haul`→`runClaimedHaul`→`haulSellLeg`; add `RecordHaulResult` + `Now`/`Tick` to deps)
- Test: `pkg/worker/haul_test.go` (extend the fake store + a recording test)

**Interfaces:**
- Consumes: `market.HaulResult`, `(*market.Collector).RecordHaulResult` (Task 1).
- Produces: `OpportunityStore.RecordHaulResult(ctx, market.HaulResult) error` (new interface method); `HaulDeps.Now func() time.Time` (nil → `time.Now`).

- [ ] **Step 1: Add `RecordHaulResult` to the `OpportunityStore` interface** (`pkg/worker/haul.go` ~line 291)

```go
	RecordHaulResult(ctx context.Context, r market.HaulResult) error
```

The compile-time assertion `var _ OpportunityStore = (*market.Collector)(nil)` (line 341) now also verifies the collector satisfies it — no extra code.

- [ ] **Step 2: Add a clock to `HaulDeps`** (after `RecaptureBuyMarket`)

```go
	// Now returns the current wall-clock time (nil -> time.Now). Injected for deterministic tests.
	Now func() time.Time
```

- [ ] **Step 3: Add the metrics accumulator + helper near `runClaimedHaul`**

```go
// haulMetrics accumulates per-leg stamps (wall + game tick) and pricing across one fresh
// haul, for a haul_results row written on completion. A nil *haulMetrics (resumed hauls,
// where earlier legs happened in a prior process) disables recording.
type haulMetrics struct {
	jumps                                          int
	buyPrice, sellPrice, qty                       float64
	claimedAt, arrivedSrcAt, boughtAt              time.Time
	arrivedDstAt, soldAt                           time.Time
	claimedTick, arrivedSrcTick, boughtTick        int64
	arrivedDstTick, soldTick                       int64
}

func haulNow(deps HaulDeps) time.Time {
	if deps.Now != nil {
		return deps.Now()
	}
	return time.Now()
}

func haulTick(deps HaulDeps) int64 {
	if st := deps.Client.GetState(); st != nil {
		return st.GetTick()
	}
	return 0
}
```

- [ ] **Step 4: Stamp `claimed` + compute jumps in `Haul` fresh-claim path** (after `claimBest` returns `ok`, before `runClaimedHaul` at line 448)

```go
	m := &haulMetrics{claimedAt: haulNow(deps), claimedTick: haulTick(deps)}
	if buySys := nameToID[opp.FromSystemName]; buySys != "" {
		d := navigation.BFSJumps(graph, current, []string{buySys})
		if sellSys := nameToID[opp.ToSystemName]; sellSys != "" {
			d2 := navigation.BFSJumps(graph, buySys, []string{sellSys})
			m.jumps = d[buySys] + d2[sellSys]
		}
	}
	return runClaimedHaul(ctx, deps, out, opp, nameToID, m)
```

The resume path (line 414) passes `nil`:

```go
		return runClaimedHaul(ctx, deps, out, held[0], nameToID, nil)
```

- [ ] **Step 5: Thread `m` through `runClaimedHaul`** — change signature to `(..., nameToID map[string]string, m *haulMetrics) error`. The early resume branch (line 485) calls `haulSellLeg(ctx, deps, out, opp, sellSys, nil)`. After `Dock` succeeds (line 499) stamp arrival; capture gate prices; after `Buy` succeeds (line 524) stamp bought; then `haulSellLeg(..., m)`:

```go
	// (after successful Dock, before recapture)
	if m != nil {
		m.arrivedSrcAt, m.arrivedSrcTick = haulNow(deps), haulTick(deps)
	}
	...
	qty, liveAsk, sellBid, pass, reason := haulGate(opp, prices, cargoFree, state.Ship.CargoCapacity, state.GetCredits())
	if !pass {
		return abandonClaim(ctx, deps, out, opp, reason)
	}
	if err := deps.Client.Buy(ctx, opp.ItemID, qty); err != nil {
		return abandonClaim(ctx, deps, out, opp, fmt.Sprintf("buy failed: %v", err))
	}
	if m != nil {
		m.boughtAt, m.boughtTick = haulNow(deps), haulTick(deps)
		m.buyPrice, m.sellPrice, m.qty = liveAsk, sellBid, qty
	}
	return haulSellLeg(ctx, deps, out, opp, sellSys, m)
```

- [ ] **Step 6: Thread `m` through `haulSellLeg` + write the row on completion** — change signature to `(..., sellSys string, m *haulMetrics) error`. After the sell autopilot succeeds stamp `arrivedDst`; after `Sell` succeeds stamp `sold`; after `CompleteOpportunity` succeeds, record:

```go
	if m != nil {
		m.arrivedDstAt, m.arrivedDstTick = haulNow(deps), haulTick(deps)
	}
	...
	if err := deps.Client.Sell(ctx, opp.ItemID, held); err != nil { ... }
	if m != nil {
		m.soldAt, m.soldTick = haulNow(deps), haulTick(deps)
	}
	if _, err := deps.Market.CompleteOpportunity(ctx, opp.ID, deps.AgentID); err != nil { ... }
	if m != nil {
		rec := market.HaulResult{
			OppID: opp.ID, AgentID: deps.AgentID, ItemID: opp.ItemID, Qty: m.qty,
			BuyPricePaid: m.buyPrice, SellPriceGot: m.sellPrice,
			RealizedProfit: (m.sellPrice - m.buyPrice) * m.qty, JumpsTraveled: m.jumps,
			ClaimedAt: rfc(m.claimedAt), ArrivedSrcAt: rfc(m.arrivedSrcAt), BoughtAt: rfc(m.boughtAt),
			ArrivedDstAt: rfc(m.arrivedDstAt), SoldAt: rfc(m.soldAt),
			ClaimedTick: m.claimedTick, ArrivedSrcTick: m.arrivedSrcTick, BoughtTick: m.boughtTick,
			ArrivedDstTick: m.arrivedDstTick, SoldTick: m.soldTick, CreatedAt: rfc(haulNow(deps)),
		}
		if rerr := deps.Market.RecordHaulResult(ctx, rec); rerr != nil {
			fmt.Fprintf(out, "haul: opp %d record result failed: %v\n", opp.ID, rerr) //nolint:errcheck
		}
	}
	fmt.Fprintf(out, "haul: opp %d complete (sold %.0f %s)\n", opp.ID, held, opp.ItemID) //nolint:errcheck
	return nil
```

Add helper at file scope: `func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }`. Ensure `time` is imported.

- [ ] **Step 7: Update the fake store in `pkg/worker/haul_test.go`** — add a no-op (or capturing) `RecordHaulResult` method to the existing fake `OpportunityStore`, and a captured-results slice. Write a test that runs a full fresh `Haul` (fake client with a known buy/sell route) and asserts one recorded result with `RealizedProfit == (sellBid-liveAsk)*qty` and `JumpsTraveled` set. Reuse the existing fake-client harness in that test file; if a full end-to-end fake is too heavy, instead unit-test the recording by calling `haulSellLeg` with a populated `*haulMetrics` and a fake client holding cargo, asserting the captured result.

```go
func (f *fakeStore) RecordHaulResult(_ context.Context, r market.HaulResult) error {
	f.recorded = append(f.recorded, r)
	return nil
}
```

- [ ] **Step 8: Run tests** — `go test ./pkg/worker/ -run Haul` → PASS.
- [ ] **Step 9: Commit** — `git add pkg/worker/haul.go pkg/worker/haul_test.go && git commit -m "feat(worker): record real haul_results with per-leg timing + jumps on completion"`

---

### Task 3: Plumb `cargo_used`/`cargo_capacity` worker → status → fleet-status.json

**Files:**
- Modify: `pkg/overmind/control/messages.go` (`Status` struct + test)
- Modify: `cmd/worker/main.go:376` (populate cargo in the built `control.Status`)
- Modify: `pkg/overmind/balances/balances.go` (`LiveRecord` struct)
- Modify: `cmd/overmind/main.go:150` (map cargo WorkerInfo→LiveRecord)
- Test: `pkg/overmind/control/messages_test.go`

**Interfaces:**
- Produces: `control.Status.CargoUsed`, `control.Status.CargoCapacity` (`float64`, json `cargo_used`/`cargo_capacity`); same two fields on `balances.LiveRecord`.

- [ ] **Step 1: Add fields to `control.Status`** (after `Credits`, `pkg/overmind/control/messages.go:46`)

```go
	CargoUsed        float64 `json:"cargo_used"`
	CargoCapacity    float64 `json:"cargo_capacity"`
```

- [ ] **Step 2: Extend the round-trip test** (`pkg/overmind/control/messages_test.go`) — set `CargoCapacity: 80` in the `Status` literal and assert it survives encode/decode.
- [ ] **Step 3: Run test, verify PASS** — `go test ./pkg/overmind/control/`
- [ ] **Step 4: Populate cargo in the worker** (`cmd/worker/main.go:376`, the `return control.Status{...}`) — add `CargoUsed: <state>.Ship.CargoUsed, CargoCapacity: <state>.Ship.CargoCapacity,` reading from the same state value the function already uses for `Fuel`/`Credits` (match the existing field-access expression there).
- [ ] **Step 5: Add the two fields to `balances.LiveRecord`** (after `MaxFuel`, `pkg/overmind/balances/balances.go:37`)

```go
	CargoUsed     float64 `json:"cargo_used"`
	CargoCapacity float64 `json:"cargo_capacity"`
```

- [ ] **Step 6: Map them in the overmind** (`cmd/overmind/main.go:150`, the `balances.LiveRecord{...}` literal) — add `CargoUsed: st.CargoUsed, CargoCapacity: st.CargoCapacity,` (where `st` is `w.LastStatus`).
- [ ] **Step 7: Build + test** — `go build ./... && go test ./pkg/overmind/... ./cmd/...`
- [ ] **Step 8: Commit** — `git add pkg/overmind/control/messages.go pkg/overmind/control/messages_test.go cmd/worker/main.go pkg/overmind/balances/balances.go cmd/overmind/main.go && git commit -m "feat(overmind): plumb cargo_used/cargo_capacity worker->status->fleet-status.json"`

---

### Task 4: `fleet_timeseries` table + `FleetSnapshot` type + `RecordFleetSnapshot`

**Files:**
- Modify: `pkg/market/schema.sql` (append table)
- Modify: `pkg/market/types.go` (add `FleetSnapshot` struct)
- Modify: `pkg/market/haul_results.go` (add writer — co-located fleet/dashboard recorders)
- Test: `pkg/market/haul_results_test.go` (add a snapshot round-trip test)

**Interfaces:**
- Produces: `market.FleetSnapshot` struct; `func (c *Collector) RecordFleetSnapshot(ctx context.Context, rows []FleetSnapshot) error`; `func (c *Collector) GetFleetSnapshots(ctx context.Context, agentID string, limit int) ([]FleetSnapshot, error)`.

- [ ] **Step 1: Append the table to `pkg/market/schema.sql`**

```sql
-- Periodic per-hauler balance/fuel/cargo snapshots (dashboard Component B).
CREATE TABLE IF NOT EXISTS fleet_timeseries (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    ts             TEXT NOT NULL,
    agent_id       TEXT NOT NULL,
    role           TEXT,
    system         TEXT,
    docked         INTEGER,
    credits        REAL,
    fuel           REAL,
    max_fuel       REAL,
    cargo_used     REAL,
    cargo_capacity REAL
);

CREATE INDEX IF NOT EXISTS idx_fleet_timeseries_agent_time ON fleet_timeseries(agent_id, ts);
```

- [ ] **Step 2: Add `FleetSnapshot` to `pkg/market/types.go`**

```go
// FleetSnapshot is one hauler's balance/fuel/cargo at a point in time (quarter-hourly).
type FleetSnapshot struct {
	ID            int64   `json:"id"`
	TS            string  `json:"ts"`
	AgentID       string  `json:"agent_id"`
	Role          string  `json:"role"`
	System        string  `json:"system"`
	Docked        bool    `json:"docked"`
	Credits       float64 `json:"credits"`
	Fuel          float64 `json:"fuel"`
	MaxFuel       float64 `json:"max_fuel"`
	CargoUsed     float64 `json:"cargo_used"`
	CargoCapacity float64 `json:"cargo_capacity"`
}
```

- [ ] **Step 3: Write the failing test** (append to `pkg/market/haul_results_test.go`)

```go
func TestRecordAndGetFleetSnapshot(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	rows := []FleetSnapshot{
		{TS: "2026-06-26T10:00:00Z", AgentID: "trader-1", Role: "hauler", System: "Sol",
			Docked: true, Credits: 1000, Fuel: 50, MaxFuel: 100, CargoUsed: 10, CargoCapacity: 80},
		{TS: "2026-06-26T10:00:00Z", AgentID: "trader-2", Role: "hauler", System: "Sirius",
			Credits: 2000, Fuel: 90, MaxFuel: 100, CargoCapacity: 80},
	}
	if err := c.RecordFleetSnapshot(ctx, rows); err != nil {
		t.Fatalf("RecordFleetSnapshot: %v", err)
	}
	got, err := c.GetFleetSnapshots(ctx, "trader-1", 10)
	if err != nil {
		t.Fatalf("GetFleetSnapshots: %v", err)
	}
	if len(got) != 1 || got[0].Credits != 1000 || got[0].CargoCapacity != 80 || !got[0].Docked {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}
```

- [ ] **Step 4: Run test, verify it fails** — `go test ./pkg/market/ -run TestRecordAndGetFleetSnapshot`
- [ ] **Step 5: Implement in `pkg/market/haul_results.go`**

```go
// RecordFleetSnapshot writes a batch of per-hauler snapshots in one transaction.
func (c *Collector) RecordFleetSnapshot(ctx context.Context, rows []FleetSnapshot) error {
	if len(rows) == 0 {
		return nil
	}
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO fleet_timeseries
 (ts, agent_id, role, system, docked, credits, fuel, max_fuel, cargo_used, cargo_capacity)
 VALUES (?,?,?,?,?,?,?,?,?,?)`,
				r.TS, r.AgentID, r.Role, r.System, boolToInt(r.Docked), r.Credits, r.Fuel,
				r.MaxFuel, r.CargoUsed, r.CargoCapacity); err != nil {
				return err
			}
		}
		return nil
	})
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetFleetSnapshots returns snapshots for agentID (all if empty), newest first, capped at limit (<=0 -> 5000).
func (c *Collector) GetFleetSnapshots(ctx context.Context, agentID string, limit int) ([]FleetSnapshot, error) {
	if limit <= 0 {
		limit = 5000
	}
	q := `SELECT id, ts, agent_id, role, system, docked, credits, fuel, max_fuel, cargo_used, cargo_capacity FROM fleet_timeseries`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id = ?`
		args = append(args, agentID)
	}
	q += ` ORDER BY ts DESC LIMIT ?`
	args = append(args, limit)
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get fleet snapshots: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []FleetSnapshot
	for rows.Next() {
		var r FleetSnapshot
		var docked int
		if err := rows.Scan(&r.ID, &r.TS, &r.AgentID, &r.Role, &r.System, &docked, &r.Credits,
			&r.Fuel, &r.MaxFuel, &r.CargoUsed, &r.CargoCapacity); err != nil {
			return nil, fmt.Errorf("scan fleet snapshot: %w", err)
		}
		r.Docked = docked != 0
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 6: Run test, verify PASS** — `go test ./pkg/market/ -run TestRecordAndGetFleetSnapshot`
- [ ] **Step 7: Commit** — `git add pkg/market/schema.sql pkg/market/types.go pkg/market/haul_results.go pkg/market/haul_results_test.go && git commit -m "feat(market): fleet_timeseries table + RecordFleetSnapshot/GetFleetSnapshots"`

---

### Task 5: Quarter-hourly `fleet_timeseries` write from the overmind

**Files:**
- Modify: `cmd/overmind/main.go` (open a `market.Collector`; write a snapshot on quarter-hour boundary crossings using the same `live []balances.LiveRecord` already built in `recordBalances`)
- Test: a small boundary-crossing unit test (`cmd/overmind/snapshot_test.go`) for the "should I snapshot this tick" predicate.

**Interfaces:**
- Consumes: `market.NewCollector`/equivalent constructor (match how `cmd/arbitrage-scanner` or the marketbot opens `data/market.db`), `(*market.Collector).RecordFleetSnapshot`, `market.FleetSnapshot`; `balances.LiveRecord` cargo fields (Task 3).

- [ ] **Step 1: Find the collector constructor** — grep `cmd/` for how the scanner opens `market.db` (e.g. `market.NewCollector(ctx, "data/market.db")`); reuse that exact call. Add a `--market-db-path` flag defaulting to `data/market.db`; open the collector at overmind startup next to `balances.NewRecorder` (`cmd/overmind/main.go:72`), and `defer col.Close()`. If opening fails, log a warning and run with `col == nil` (snapshots disabled) — the overmind must not fail to start because the market db is unavailable.

- [ ] **Step 2: Write the failing boundary test** (`cmd/overmind/snapshot_test.go`)

```go
package main

import (
	"testing"
	"time"
)

func TestCrossedQuarterBoundary(t *testing.T) {
	q := func(s string) time.Time { tm, _ := time.Parse(time.RFC3339, s); return tm }
	// last snapshot 10:14:59, now 10:15:01 -> crossed the :15 boundary
	if !crossedQuarterBoundary(q("2026-06-26T10:14:59Z"), q("2026-06-26T10:15:01Z")) {
		t.Fatal("want crossed at :15")
	}
	// same quarter -> not crossed
	if crossedQuarterBoundary(q("2026-06-26T10:15:01Z"), q("2026-06-26T10:16:00Z")) {
		t.Fatal("want not crossed within the same quarter")
	}
	// zero last (startup) -> snapshot once
	if !crossedQuarterBoundary(time.Time{}, q("2026-06-26T10:16:00Z")) {
		t.Fatal("want crossed on startup (zero last)")
	}
}
```

- [ ] **Step 3: Run test, verify it fails** — `go test ./cmd/overmind/ -run TestCrossedQuarterBoundary`
- [ ] **Step 4: Implement the predicate + wiring in `cmd/overmind/main.go`**

```go
// crossedQuarterBoundary reports whether now is in a later 15-minute bucket than last
// (a zero last always returns true, so startup snapshots once).
func crossedQuarterBoundary(last, now time.Time) bool {
	if last.IsZero() {
		return true
	}
	q := func(t time.Time) int64 { return t.UTC().Unix() / 900 }
	return q(now) > q(last)
}
```

In `recordBalances` (or its caller — wherever `live` and `now` are in scope), after `WriteStatus`, add a package-level `lastSnapshot time.Time` guarded by the existing loop (single-goroutine; no mutex needed if `recordBalances` is only called from the main loop — verify):

```go
	if col != nil && crossedQuarterBoundary(lastSnapshot, now) {
		rows := make([]market.FleetSnapshot, 0, len(live))
		for _, lr := range live {
			if lr.Role != "hauler" {
				continue
			}
			rows = append(rows, market.FleetSnapshot{
				TS: now.UTC().Format(time.RFC3339), AgentID: lr.AgentID, Role: lr.Role,
				System: lr.System, Docked: lr.Docked, Credits: lr.Credits, Fuel: lr.Fuel,
				MaxFuel: lr.MaxFuel, CargoUsed: lr.CargoUsed, CargoCapacity: lr.CargoCapacity,
			})
		}
		if err := col.RecordFleetSnapshot(ctx, rows); err != nil {
			logger.Printf("fleet_timeseries snapshot failed: %v", err)
		} else {
			lastSnapshot = now
		}
	}
```

Thread `col` and `ctx` into `recordBalances` (add params) and pass them from the call site. If `recordBalances` is called concurrently, guard `lastSnapshot` with a mutex; otherwise a plain package var is fine.

- [ ] **Step 5: Run test + full build/test** — `go test ./cmd/overmind/ -run TestCrossedQuarterBoundary && go build ./... && go test ./...`
- [ ] **Step 6: Rebuild fleet binaries to `bin/`** — `go build -o bin/worker ./cmd/worker && go build -o bin/overmind ./cmd/overmind`
- [ ] **Step 7: Commit** — `git add cmd/overmind/main.go cmd/overmind/snapshot_test.go && git commit -m "feat(overmind): quarter-hourly fleet_timeseries snapshot of haulers"`

---

## Deploy (after all tasks green)

The new worker + overmind binaries take effect only on a fleet restart. Fold into the
scheduled maintenance window: stop the haul overmind, relaunch with the salvager-only
roster (traders pulled for manual cargo upgrades) and the rebuilt `bin/worker`/`bin/overmind`.
Hauls + total-time backfill from existing `completed_at`; legs/jumps/realized + balances +
cargo accrue forward only.

## Self-Review notes

- Spec coverage: A (Task 1+2), B table (Task 4), B recorder (Task 5), WorkerInfo cargo (Task 3) — all covered. Phase 2 generator intentionally excluded.
- Type consistency: `HaulResult`/`FleetSnapshot` field names identical across types.go, writer, reader, and worker call site; `RecordHaulResult` signature identical in interface + collector + fake.
- Open verify-at-implementation items (flagged, not placeholders): exact name of the `pkg/market` test-collector helper; the exact `market` collector constructor used elsewhere in `cmd/`; the state expression at `cmd/worker/main.go:376`; whether `recordBalances` is single-goroutine. Each is a "match the existing pattern" lookup the implementer resolves by reading the cited file.
