# Fuel-Price Feed (Arbitrage Sub-project A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Marketbots capture each station's live all-in fuel price into market.db, one upserted row per station, so any hauler can later price fuel at a station it isn't docked at.

**Architecture:** A new `station_fuel_prices` table in market.db (one row per station, replace-on-capture — never grows). A worker command `capture_fuel` reads the cached `get_base` response, extracts `fuel_price_all_in` (+ base + tax), and upserts via a new `market.Collector.UpsertStationFuel`. A read method `GetStationFuelPrice` is the single consumption point for Sub-project B. Wired onto the resident marketbot hourly schedule.

**Tech Stack:** Go 1.24, `pkg/market` (SQLite collector), `pkg/game/serverapi`, `pkg/worker` (dispatch + roles), `data/overmind/roles.yaml`.

## Global Constraints

- Target Go 1.24+; modern features where applicable.
- New code must pass `golangci-lint` with no new findings.
- Run `go build ./...` and `go test ./...` before every commit.
- Do NOT assume server field names. Verified facts: `get_base` responses are cached under the raw-JSON key `"base"` (`pkg/game/client.go:4232`); the response carries top-level `fuel_price`, `fuel_tax_per_unit`, `fuel_price_all_in` (from a live `get_base`); the docked station id is `State.CurrentPOI`.
- `fuel_price_all_in` is the per-unit cost to the hauler — the value Sub-project B consumes.
- There is a pre-commit hook that runs the race detector (~140s); use `timeout: 240000` on `git commit` Bash calls.
- Stage ONLY the files each task names. The working tree has unrelated modified runtime files under `data/` — never stage them. `data/overmind/roles.yaml` is an intended file in Task 3; stage it by explicit path.

---

### Task 1: `station_fuel_prices` table + storage layer

**Files:**
- Modify: `pkg/market/schema.sql` (add the table)
- Modify: `pkg/market/types.go` (add `StationFuel` struct)
- Modify: `pkg/market/collector.go` (add `UpsertStationFuel`, `GetStationFuelPrice`)
- Test: `pkg/market/station_fuel_test.go` (new)

**Interfaces:**
- Produces: `market.StationFuel{ StationID string; FuelPrice int; FuelTaxPerUnit int; FuelPriceAllIn int; CapturedAt string; CapturedBy string }`; `(*Collector).UpsertStationFuel(ctx, StationFuel) error`; `(*Collector).GetStationFuelPrice(ctx, stationID string) (allIn int, capturedAt time.Time, ok bool, err error)`.

- [ ] **Step 1: Write the failing test**

Create `pkg/market/station_fuel_test.go`:

```go
package market

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStationFuelRoundTrip(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close() //nolint:errcheck
	ctx := context.Background()

	// Unknown station -> ok=false, no error.
	if _, _, ok, err := c.GetStationFuelPrice(ctx, "sol_central"); err != nil || ok {
		t.Fatalf("unknown station: want ok=false err=nil, got ok=%v err=%v", ok, err)
	}

	// Insert.
	if err := c.UpsertStationFuel(ctx, StationFuel{
		StationID: "sol_central", FuelPrice: 2, FuelTaxPerUnit: 5, FuelPriceAllIn: 7,
		CapturedAt: "2026-07-15T00:00:00Z", CapturedBy: "marketbot_sol",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	allIn, at, ok, err := c.GetStationFuelPrice(ctx, "sol_central")
	if err != nil || !ok || allIn != 7 {
		t.Fatalf("after insert: allIn=%d ok=%v err=%v", allIn, ok, err)
	}
	if at.IsZero() {
		t.Fatalf("expected a parsed captured_at, got zero time")
	}

	// Re-upsert the same station -> still ONE row, values replaced.
	if err := c.UpsertStationFuel(ctx, StationFuel{
		StationID: "sol_central", FuelPrice: 3, FuelTaxPerUnit: 9, FuelPriceAllIn: 12,
		CapturedAt: "2026-07-15T01:00:00Z", CapturedBy: "marketbot_sol",
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	var n int
	if err := c.db.QueryRow(`SELECT count(*) FROM station_fuel_prices`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row after re-upsert, got %d", n)
	}
	if allIn, _, _, _ := c.GetStationFuelPrice(ctx, "sol_central"); allIn != 12 {
		t.Fatalf("expected updated allIn=12, got %d", allIn)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/market/ -run TestStationFuelRoundTrip -v`
Expected: FAIL — compile error (`StationFuel`, `UpsertStationFuel`, `GetStationFuelPrice` undefined).

- [ ] **Step 3: Add the table to `schema.sql`**

Append to `pkg/market/schema.sql`:

```sql

-- One row per station, upserted in place (replace-on-capture — never grows).
-- fuel_price_all_in is the per-unit cost a hauler pays; captured_at is a
-- freshness stamp on the single row (not history).
CREATE TABLE IF NOT EXISTS station_fuel_prices (
    station_id        TEXT PRIMARY KEY,
    fuel_price        INTEGER NOT NULL,
    fuel_tax_per_unit INTEGER NOT NULL,
    fuel_price_all_in INTEGER NOT NULL,
    captured_at       TEXT NOT NULL,
    captured_by       TEXT
);
```

- [ ] **Step 4: Add the `StationFuel` struct**

In `pkg/market/types.go`, near the `Station` struct, add:

```go
// StationFuel is the latest captured fuel price at a station. One row per
// station (upserted, never grows). FuelPriceAllIn is the per-unit cost a hauler
// actually pays; Sub-project B consumes it. CapturedAt is RFC3339.
type StationFuel struct {
	StationID      string
	FuelPrice      int
	FuelTaxPerUnit int
	FuelPriceAllIn int
	CapturedAt     string
	CapturedBy     string
}
```

- [ ] **Step 5: Add the collector methods**

In `pkg/market/collector.go` (near `upsertStation`), add. Ensure `time` and `database/sql` are imported (they are used elsewhere in the package; confirm the file's own import block includes `time`):

```go
// UpsertStationFuel writes the latest fuel price for a station, replacing any
// existing row (one row per station — no time growth).
func (c *Collector) UpsertStationFuel(ctx context.Context, s StationFuel) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO station_fuel_prices
			  (station_id, fuel_price, fuel_tax_per_unit, fuel_price_all_in, captured_at, captured_by)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(station_id) DO UPDATE SET
				fuel_price = excluded.fuel_price,
				fuel_tax_per_unit = excluded.fuel_tax_per_unit,
				fuel_price_all_in = excluded.fuel_price_all_in,
				captured_at = excluded.captured_at,
				captured_by = excluded.captured_by
		`, s.StationID, s.FuelPrice, s.FuelTaxPerUnit, s.FuelPriceAllIn, s.CapturedAt, s.CapturedBy)
		return err
	})
}

// GetStationFuelPrice returns the latest captured all-in fuel price for a
// station. ok is false (no error) when no price has been captured yet.
func (c *Collector) GetStationFuelPrice(ctx context.Context, stationID string) (allIn int, capturedAt time.Time, ok bool, err error) {
	var at string
	scanErr := c.db.QueryRowContext(ctx,
		`SELECT fuel_price_all_in, captured_at FROM station_fuel_prices WHERE station_id = ?`,
		stationID).Scan(&allIn, &at)
	if errors.Is(scanErr, sql.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if scanErr != nil {
		return 0, time.Time{}, false, scanErr
	}
	t, perr := time.Parse(time.RFC3339, at)
	if perr != nil {
		return allIn, time.Time{}, true, nil // price is valid even if the stamp won't parse
	}
	return allIn, t, true, nil
}
```

`collector.go` currently imports `context`, `database/sql`, `fmt`, `os`, `path/filepath`, `strconv`, `strings`, `time` — it does NOT import `errors`. Add `errors` to the import block (used by `errors.Is` above; a bare `== sql.ErrNoRows` would trip the `errorlint` linter).

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./pkg/market/ -run TestStationFuelRoundTrip -v`
Expected: PASS.

- [ ] **Step 7: Build, package test, lint**

Run: `go build ./... && go test ./pkg/market/ && golangci-lint run ./pkg/market/...`
Expected: build ok; tests pass; no new lint findings.

- [ ] **Step 8: Commit**

```bash
git add pkg/market/schema.sql pkg/market/types.go pkg/market/collector.go pkg/market/station_fuel_test.go
git commit -m "feat(market): station_fuel_prices table + upsert/lookup"
```

---

### Task 2: `get_base` fuel fields + fuel capture

**Files:**
- Modify: `pkg/game/serverapi/responses.go` (add two fields to `GetBaseResponse`)
- Modify: `pkg/market/capture.go` (add `parseGetBaseFuel`, `CaptureFuelFromClient`)
- Test: `pkg/market/capture_test.go` (add `TestParseGetBaseFuel`; create the file if absent)

**Interfaces:**
- Consumes: `market.StationFuel`, `(*Collector).UpsertStationFuel` (Task 1); `serverapi.GetBaseResponse`; `game.GameClient.GetRawJSON("base")`, `game.GameClient.GetState()`.
- Produces: `serverapi.GetBaseResponse.FuelPriceAllIn int64`, `.FuelTaxPerUnit int64`; `market.parseGetBaseFuel(raw []byte, stationID, capturedBy string, capturedAt time.Time) (StationFuel, bool, error)`; `market.CaptureFuelFromClient(ctx, client game.GameClient, collector *Collector, capturedBy string) error`.

- [ ] **Step 1: Write the failing test**

Append to the existing `pkg/market/capture_test.go` (it is already `package market`; ensure its import block includes `testing` and `time` — add either if missing):

```go
func TestParseGetBaseFuel(t *testing.T) {
	at := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

	raw := []byte(`{"action":"get_base","fuel_price":2,"fuel_tax_per_unit":5,"fuel_price_all_in":7}`)
	sf, ok, err := parseGetBaseFuel(raw, "sol_central", "marketbot_sol", at)
	if err != nil || !ok {
		t.Fatalf("valid payload: ok=%v err=%v", ok, err)
	}
	if sf.FuelPrice != 2 || sf.FuelTaxPerUnit != 5 || sf.FuelPriceAllIn != 7 {
		t.Fatalf("wrong fuel values: %+v", sf)
	}
	if sf.StationID != "sol_central" || sf.CapturedBy != "marketbot_sol" {
		t.Fatalf("wrong identity: %+v", sf)
	}
	if sf.CapturedAt != "2026-07-15T00:00:00Z" {
		t.Fatalf("capturedAt=%q", sf.CapturedAt)
	}

	// No fuel price in payload -> ok=false (station without a pump).
	if _, ok, _ := parseGetBaseFuel([]byte(`{"action":"get_base"}`), "x", "y", at); ok {
		t.Fatal("no fuel price: expected ok=false")
	}
	// Empty payload -> ok=false.
	if _, ok, _ := parseGetBaseFuel(nil, "x", "y", at); ok {
		t.Fatal("empty payload: expected ok=false")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/market/ -run TestParseGetBaseFuel -v`
Expected: FAIL — `parseGetBaseFuel` undefined.

- [ ] **Step 3: Add the fuel fields to `GetBaseResponse`**

In `pkg/game/serverapi/responses.go`, in `GetBaseResponse`, directly below the existing `FuelPrice int64 \`json:"fuel_price,omitempty"\`` line, add:

```go
	FuelPriceAllIn int64 `json:"fuel_price_all_in,omitempty"`
	FuelTaxPerUnit int64 `json:"fuel_tax_per_unit,omitempty"`
```

- [ ] **Step 4: Add the parse + capture functions**

In `pkg/market/capture.go`, add:

```go
// parseGetBaseFuel extracts a station's fuel price from a raw get_base JSON
// response. ok is false when the payload is empty or carries no usable fuel
// price (all-in <= 0), so a station without a fuel pump is skipped.
func parseGetBaseFuel(raw []byte, stationID, capturedBy string, capturedAt time.Time) (StationFuel, bool, error) {
	if len(raw) == 0 {
		return StationFuel{}, false, nil
	}
	var resp serverapi.GetBaseResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return StationFuel{}, false, err
	}
	if resp.FuelPriceAllIn <= 0 {
		return StationFuel{}, false, nil
	}
	return StationFuel{
		StationID:      stationID,
		FuelPrice:      int(resp.FuelPrice),
		FuelTaxPerUnit: int(resp.FuelTaxPerUnit),
		FuelPriceAllIn: int(resp.FuelPriceAllIn),
		CapturedAt:     capturedAt.UTC().Format(time.RFC3339),
		CapturedBy:     capturedBy,
	}, true, nil
}

// CaptureFuelFromClient reads the get_base response the client last cached and
// upserts the docked station's fuel price. No-op (nil) when the collector is
// nil, the ship is not at a station, or the payload has no fuel price.
func CaptureFuelFromClient(ctx context.Context, client game.GameClient, collector *Collector, capturedBy string) error {
	if collector == nil {
		return nil
	}
	state := client.GetState()
	if state == nil || state.CurrentPOI == "" {
		return nil
	}
	sf, ok, err := parseGetBaseFuel(client.GetRawJSON("base"), state.CurrentPOI, capturedBy, time.Now())
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return collector.UpsertStationFuel(ctx, sf)
}
```

`capture.go` already imports `context`, `encoding/json`, `time`, `pkg/game`, and `pkg/game/serverapi` — no new imports needed.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/market/ -run TestParseGetBaseFuel -v`
Expected: PASS. (Note: `CaptureFuelFromClient` is a thin client-glue wrapper; its end-to-end behavior is covered by Task 3's dispatch test with the worker `fakeClient`, which already stubs `GetRawJSON`/`GetState`.)

- [ ] **Step 6: Build, package tests, lint**

Run: `go build ./... && go test ./pkg/market/ ./pkg/game/... && golangci-lint run ./pkg/market/... ./pkg/game/...`
Expected: build ok; tests pass; no new lint findings.

- [ ] **Step 7: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/market/capture.go pkg/market/capture_test.go
git commit -m "feat(market): parse get_base fuel price and capture it per station"
```

---

### Task 3: `capture_fuel` worker command + resident schedule

**Files:**
- Modify: `pkg/worker/dispatch.go` (add `capture_fuel` to `supported` and a dispatch case)
- Modify: `data/overmind/roles.yaml` (add `capture_fuel` to the three resident role schedules)
- Test: `pkg/worker/dispatch_test.go` (add `TestCaptureFuelWritesRow`)

**Interfaces:**
- Consumes: `market.CaptureFuelFromClient` (Task 2); `WorkerDispatch.Client`, `.Market`, `.AgentID`; `game.GameClient.GetBase(ctx)`.
- Produces: dispatchable command token `capture_fuel`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/worker/dispatch_test.go` (the file already imports `context`, `io`, `path/filepath`, `slices`, `testing`, `pkg/game`, `pkg/market`):

```go
func TestCaptureFuelWritesRow(t *testing.T) {
	c, err := market.Open(market.Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open collector: %v", err)
	}
	defer c.Close() //nolint:errcheck

	fc := &fakeClient{
		state: &game.State{CurrentPOI: "sol_central"},
		raw:   map[string][]byte{"base": []byte(`{"action":"get_base","fuel_price":2,"fuel_tax_per_unit":5,"fuel_price_all_in":7}`)},
	}
	d := NewWorkerDispatch(fc, nil, c, io.Discard)
	d.AgentID = "marketbot_sol"

	if err := d.Run(context.Background(), []string{"capture_fuel"}); err != nil {
		t.Fatalf("capture_fuel: %v", err)
	}
	allIn, _, ok, err := c.GetStationFuelPrice(context.Background(), "sol_central")
	if err != nil || !ok || allIn != 7 {
		t.Fatalf("row not written: allIn=%d ok=%v err=%v", allIn, ok, err)
	}
	if !slices.Contains(fc.calls, "get_base") {
		t.Errorf("get_base not issued; calls=%v", fc.calls)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run TestCaptureFuelWritesRow -v`
Expected: FAIL — `capture_fuel` is an unsupported command (`Run` returns `unsupported command "capture_fuel"`).

- [ ] **Step 3: Register `capture_fuel` in `supported`**

In `pkg/worker/dispatch.go`, the `supported` map currently reads (around line 95):

```go
	"view_market": true, "facilities": true, "kb_update": true,
	"update_market": true,
```

Add `capture_fuel` to it — put it next to `update_market`:

```go
	"view_market": true, "facilities": true, "kb_update": true,
	"update_market": true, "capture_fuel": true,
```

- [ ] **Step 4: Add the dispatch case**

In `pkg/worker/dispatch.go`, add a case near `case "update_market":`:

```go
	case "capture_fuel":
		if d.Market == nil {
			return fmt.Errorf("capture_fuel: market collector not configured (use --market-db-path)")
		}
		if err := d.Client.GetBase(ctx); err != nil {
			return err
		}
		return market.CaptureFuelFromClient(ctx, d.Client, d.Market, d.AgentID)
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/worker/ -run TestCaptureFuelWritesRow -v`
Expected: PASS.

- [ ] **Step 6: Add `capture_fuel` to the resident schedules**

In `data/overmind/roles.yaml`, add one schedule line to each of `resident`, `resident_gas`, and `resident_ice` (leave `craftsman` unchanged). For `resident`:

```yaml
  resident:
    schedule:
      - { every: hourly, command: "kb_update" }
      - { every: hourly, command: "view_market" }
      - { every: hourly, command: "update_market" }
      - { every: hourly, command: "facilities" }
      - { every: hourly, command: "capture_fuel" }
    idle: resident_market
    idle_params:
      N: "20"
```

Add the same `- { every: hourly, command: "capture_fuel" }` line to the `resident_gas` and `resident_ice` schedule blocks.

- [ ] **Step 7: Run the coverage + full suite**

Run: `go test ./pkg/worker/ -run 'TestCaptureFuelWritesRow|TestSeededCommandsAreDispatchable' -v`
Expected: PASS. `TestSeededCommandsAreDispatchable` now sees the scheduled `capture_fuel` and confirms it is in `supported`.

- [ ] **Step 8: Build, full test, lint**

Run: `go build ./... && go test ./... && golangci-lint run ./...`
Expected: build ok; all tests pass; no new lint findings.

- [ ] **Step 9: Commit**

```bash
git add pkg/worker/dispatch.go pkg/worker/dispatch_test.go data/overmind/roles.yaml
git commit -m "feat(worker): capture_fuel command on resident marketbot schedule"
```

---

## Deploy (operator-gated, not a task)

Activation is a live redeploy handled separately: rebuild `bin/worker` (`go build -o bin/worker ./cmd/worker`) and restart the mb fleet (staggered). Until then the feed is inert — no consumer reads it yet (Sub-project B). Can ride the same restart as the resident home-station-enforcement activation. On the live 2 GB `data/market.db`, the `station_fuel_prices` table is created automatically on next `Open` (schema.sql is `CREATE TABLE IF NOT EXISTS`).

## Notes

- Design spec: `docs/superpowers/specs/2026-07-15-fuel-price-feed-design.md`.
- Sub-projects B (net-of-fuel hauler economics) and C (fuel-arbitrage chained routing) are sequenced follow-ons; grand_exchange's free-pump (cost 0) is a B concern, not captured here.
