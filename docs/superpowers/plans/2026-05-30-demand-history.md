# Demand History & Freshness Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Accumulate an hourly time series of buy-order demand (with Station-Manager split) per station, add a shared freshness primitive so many agents don't re-capture the same station minutes apart, and expose a `demand history` command.

**Architecture:** Mirror the existing `poi_resources` (current) + `resource_history` (time series) pattern. A new append/upsert table `market_demand_history` holds one row per (station, item, hourly bucket). `cmd/tools/play_as`'s `captureDemand` gains a freshness gate that consults `LatestDemandCapture` before writing both the live ledger and a new history sample. A `demand history <item>` command renders the per-station series with a trend arrow.

**Tech Stack:** Go 1.24+, modernc.org/sqlite, the `pkg/knowledge` SQLite KB, the `cmd/tools/play_as` REPL tool.

**Spec:** `docs/superpowers/specs/2026-05-30-demand-history-design.md`

---

## Conventions for every task

- Module path is `github.com/rsned/spacemolt/...` (NOT `spacemolt/...`).
- After each task: `go build ./...`, `go test ./...`, and the `golangci-lint` tool must all be clean. The pre-commit hook re-runs build/test/lint on staged Go files and rejects a red tree, so each commit must stand on its own.
- The working tree has pre-existing uncommitted changes in `server_docs/api.md`, `server_docs/openapi.json`, `server_docs/skill.md`. These are unrelated — do NOT `git add -A`. Stage only the specific files each step names. Leave the `server_docs/*` changes uncommitted.
- KB demand methods live on `*SQLiteKB` only (the faction pattern); reuse the helpers in `pkg/knowledge/faction_tx.go`: `inTx(ctx, func(tx txer) error)`, `utc(time.Time) string` (RFC3339 UTC), `parseUTC(string) time.Time`. Tests use `newTestKB(t)` from `pkg/knowledge/seen_players_test.go` (in-memory DB) — do not redeclare it.

## File Structure

| File | Responsibility |
|------|----------------|
| `pkg/knowledge/sqlite_migrations.go` | Add migration 38 creating `market_demand_history` |
| `scripts/sql/initialize_database.sql` | Regenerated snapshot of the full schema (tooling) |
| `pkg/knowledge/demand.go` | Add `DemandHistorySample` struct |
| `pkg/knowledge/demand_store.go` | Add `RecordDemandHistory` (upsert) |
| `pkg/knowledge/demand_load.go` | Add `LoadDemandHistory`, `capPerStation`, `LatestDemandCapture` |
| `pkg/knowledge/demand_history_test.go` (new) | KB tests for the three methods |
| `cmd/tools/play_as/demand_capture.go` | Add `demandHistoryBucket`/`demandFreshness` consts, `aggregateDemandHistory`, `isFresh`; wire dual write + gate into `captureDemand` |
| `cmd/tools/play_as/demand_history.go` (new) | `parseDemandHistoryOptions`, `runDemandHistory`, renderers, `trendDirection` |
| `cmd/tools/play_as/main.go` | Route `demand history` to `runDemandHistory` |
| `cmd/tools/play_as/demand_capture_test.go` | Add `aggregateDemandHistory` + `isFresh` tests |
| `cmd/tools/play_as/demand_history_test.go` (new) | `parseDemandHistoryOptions` + `trendDirection` tests |

---

### Task 1: Migration 38 — `market_demand_history` table

**Files:**
- Modify: `pkg/knowledge/sqlite_migrations.go` (the `migrations()` slice; last entry is `version: 37`, around line 340-344)
- Create test: `pkg/knowledge/demand_history_test.go`
- Regenerate: `scripts/sql/initialize_database.sql`

- [ ] **Step 1: Write the failing test**

Create `pkg/knowledge/demand_history_test.go`:

```go
package knowledge

import (
	"context"
	"testing"
)

func TestMigration38CreatesDemandHistoryTable(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	var name string
	if err := kb.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, "market_demand_history").Scan(&name); err != nil {
		t.Fatalf("table market_demand_history not found: %v", err)
	}
}
```

(Task 2 adds a `"time"` import to this file when it appends its first time-using test — that keeps each committed task's imports lint-clean.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestMigration38CreatesDemandHistoryTable -v`
Expected: FAIL — `table market_demand_history not found`.

- [ ] **Step 3: Add the migration**

In `pkg/knowledge/sqlite_migrations.go`, insert this entry immediately after the `version: 37` entry (the `drop_market_buy_demand` one) and before the closing `}` of the returned slice:

```go
		{
			version: 38,
			name:    "market_demand_history",
			sql: `
				CREATE TABLE market_demand_history (
					station_id     TEXT NOT NULL,
					system_id      TEXT,
					item_id        TEXT NOT NULL,
					item_name      TEXT,
					bucket_utc     TEXT NOT NULL,
					captured_utc   TEXT NOT NULL,
					best_price     REAL NOT NULL DEFAULT 0,
					total_qty      REAL NOT NULL DEFAULT 0,
					sm_best_price  REAL NOT NULL DEFAULT 0,
					sm_qty         REAL NOT NULL DEFAULT 0,
					order_count    INTEGER NOT NULL DEFAULT 0,
					PRIMARY KEY (station_id, item_id, bucket_utc)
				);
				CREATE INDEX market_demand_history_item ON market_demand_history(item_id, bucket_utc);
			`,
		},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestMigration38CreatesDemandHistoryTable -v`
Expected: PASS.

- [ ] **Step 5: Regenerate the schema snapshot**

Run: `bash scripts/sql/regenerate_initialize_database.sh`
Then verify the in-sync guard: `go test ./pkg/knowledge/ -run TestInitializeDatabaseSQLInSync -v`
Expected: PASS. `git status` should show `scripts/sql/initialize_database.sql` modified to include the new table.

- [ ] **Step 6: Commit**

```bash
git add pkg/knowledge/sqlite_migrations.go pkg/knowledge/demand_history_test.go scripts/sql/initialize_database.sql
git commit -m "feat(knowledge): migration 38 — market_demand_history table"
```

---

### Task 2: `DemandHistorySample` + `RecordDemandHistory` (upsert)

**Files:**
- Modify: `pkg/knowledge/demand.go`
- Modify: `pkg/knowledge/demand_store.go`
- Modify test: `pkg/knowledge/demand_history_test.go`

- [ ] **Step 1: Write the failing test**

Add `"time"` to the import block of `pkg/knowledge/demand_history_test.go` so it reads:

```go
import (
	"context"
	"testing"
	"time"
)
```

Then append to the file:

```go
func TestRecordDemandHistoryUpsertWithinBucket(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	bucket := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	rec := func(best, total float64, count int, capAt time.Time) {
		if err := kb.RecordDemandHistory(ctx, []DemandHistorySample{
			{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore",
				BucketAt: bucket, CapturedAt: capAt,
				BestPrice: best, TotalQty: total, SMBestPrice: best, SMQty: total, OrderCount: count},
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	rec(10, 50, 2, bucket.Add(5*time.Minute))
	rec(12, 60, 3, bucket.Add(40*time.Minute)) // same bucket -> upsert in place

	var rows, count int
	var best, total float64
	if err := kb.db.QueryRowContext(ctx,
		`SELECT COUNT(*), MAX(best_price), MAX(total_qty), MAX(order_count)
		   FROM market_demand_history WHERE station_id=? AND item_id=? AND bucket_utc=?`,
		"stn1", "iron_ore", bucket.UTC().Format(time.RFC3339)).Scan(&rows, &best, &total, &count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if rows != 1 {
		t.Fatalf("want 1 row after same-bucket upsert, got %d", rows)
	}
	if best != 12 || total != 60 || count != 3 {
		t.Errorf("upsert did not take latest values: best=%v total=%v count=%v", best, total, count)
	}

	// A new bucket appends a second row rather than replacing.
	bucket2 := bucket.Add(time.Hour)
	if err := kb.RecordDemandHistory(ctx, []DemandHistorySample{
		{StationID: "stn1", ItemID: "iron_ore", BucketAt: bucket2, CapturedAt: bucket2, BestPrice: 9, TotalQty: 30, OrderCount: 1},
	}); err != nil {
		t.Fatalf("record new bucket: %v", err)
	}
	if err := kb.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM market_demand_history WHERE station_id=? AND item_id=?`,
		"stn1", "iron_ore").Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 2 {
		t.Errorf("want 2 rows across two buckets, got %d", rows)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestRecordDemandHistoryUpsertWithinBucket -v`
Expected: FAIL to compile — `undefined: DemandHistorySample` / `kb.RecordDemandHistory`.

- [ ] **Step 3: Add the struct**

Append to `pkg/knowledge/demand.go` (the file already imports `time`):

```go
// DemandHistorySample is one (station, item, hourly bucket) aggregate of
// buy-order demand, persisted to market_demand_history to build a time series.
// BucketAt is the capture time truncated to the bucket size (the upsert key);
// CapturedAt is the actual last observation time within that bucket. SMBestPrice
// and SMQty are the Station-Manager (source=="station") slice of the demand.
type DemandHistorySample struct {
	StationID   string    `json:"station_id"`
	SystemID    string    `json:"system_id"`
	ItemID      string    `json:"item_id"`
	ItemName    string    `json:"item_name"`
	BucketAt    time.Time `json:"bucket_at"`
	CapturedAt  time.Time `json:"captured_at"`
	BestPrice   float64   `json:"best_price"`
	TotalQty    float64   `json:"total_qty"`
	SMBestPrice float64   `json:"sm_best_price"`
	SMQty       float64   `json:"sm_qty"`
	OrderCount  int       `json:"order_count"`
}
```

- [ ] **Step 4: Add `RecordDemandHistory`**

Append to `pkg/knowledge/demand_store.go` (the file already imports `context`):

```go
// RecordDemandHistory upserts one row per (station, item, bucket) into
// market_demand_history. Re-reading a station within the same bucket updates that
// row in place (last observation in the bucket wins); a new bucket appends a new
// row. Runs in one transaction so a station's samples are all-or-nothing.
func (kb *SQLiteKB) RecordDemandHistory(ctx context.Context, samples []DemandHistorySample) error {
	return kb.inTx(ctx, func(tx txer) error {
		for _, s := range samples {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_demand_history
					(station_id, system_id, item_id, item_name, bucket_utc, captured_utc,
					 best_price, total_qty, sm_best_price, sm_qty, order_count)
				VALUES (?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(station_id, item_id, bucket_utc) DO UPDATE SET
					system_id     = excluded.system_id,
					item_name     = excluded.item_name,
					captured_utc  = excluded.captured_utc,
					best_price    = excluded.best_price,
					total_qty     = excluded.total_qty,
					sm_best_price = excluded.sm_best_price,
					sm_qty        = excluded.sm_qty,
					order_count   = excluded.order_count`,
				s.StationID, s.SystemID, s.ItemID, s.ItemName, utc(s.BucketAt), utc(s.CapturedAt),
				s.BestPrice, s.TotalQty, s.SMBestPrice, s.SMQty, s.OrderCount); err != nil {
				return err
			}
		}
		return nil
	})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestRecordDemandHistoryUpsertWithinBucket -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/knowledge/demand.go pkg/knowledge/demand_store.go pkg/knowledge/demand_history_test.go
git commit -m "feat(knowledge): DemandHistorySample + RecordDemandHistory upsert"
```

---

### Task 3: `LoadDemandHistory` + per-station cap

**Files:**
- Modify: `pkg/knowledge/demand_load.go`
- Modify test: `pkg/knowledge/demand_history_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/knowledge/demand_history_test.go`:

```go
func TestLoadDemandHistoryFilterAndCap(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC)

	var samples []DemandHistorySample
	for h := 0; h < 4; h++ {
		b := base.Add(time.Duration(h) * time.Hour)
		samples = append(samples,
			DemandHistorySample{StationID: "stnA", ItemID: "iron_ore", BucketAt: b, CapturedAt: b, BestPrice: float64(10 + h), TotalQty: 50, OrderCount: 1},
			DemandHistorySample{StationID: "stnB", ItemID: "iron_ore", BucketAt: b, CapturedAt: b, BestPrice: float64(20 + h), TotalQty: 30, OrderCount: 1},
			DemandHistorySample{StationID: "stnA", ItemID: "copper", BucketAt: b, CapturedAt: b, BestPrice: 5, TotalQty: 99, OrderCount: 1},
		)
	}
	if err := kb.RecordDemandHistory(ctx, samples); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Filter by item only: iron_ore across both stations = 8 rows.
	got, err := kb.LoadDemandHistory(ctx, "iron_ore", "", 0)
	if err != nil {
		t.Fatalf("load all: %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("iron_ore all stations: want 8, got %d", len(got))
	}

	// Narrow to one station.
	got, err = kb.LoadDemandHistory(ctx, "iron_ore", "stnB", 0)
	if err != nil {
		t.Fatalf("load station: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("iron_ore stnB: want 4, got %d", len(got))
	}
	for _, s := range got {
		if s.StationID != "stnB" {
			t.Errorf("station filter leaked: %+v", s)
		}
	}

	// Per-station cap = 2 keeps the 2 most recent buckets of each station, ascending.
	got, err = kb.LoadDemandHistory(ctx, "iron_ore", "", 2)
	if err != nil {
		t.Fatalf("load cap: %v", err)
	}
	if len(got) != 4 { // 2 per station x 2 stations
		t.Fatalf("cap=2: want 4, got %d", len(got))
	}
	// stnA sorts first; its two most recent buckets are hours 2 and 3 (prices 12,13), ascending.
	if got[0].StationID != "stnA" || got[0].BestPrice != 12 || got[1].BestPrice != 13 {
		t.Errorf("stnA cap window wrong: %+v %+v", got[0], got[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestLoadDemandHistoryFilterAndCap -v`
Expected: FAIL to compile — `undefined: kb.LoadDemandHistory`.

- [ ] **Step 3: Add `LoadDemandHistory` and `capPerStation`**

`pkg/knowledge/demand_load.go` currently imports only `context`. Change the import block to:

```go
import (
	"context"
	"database/sql"
	"time"
)
```

(`database/sql` is used by `LatestDemandCapture` in Task 4; `time` by the return type. Adding both now keeps the file's imports stable across tasks.)

Append:

```go
// LoadDemandHistory returns demand-history samples for itemID, optionally
// narrowed to a single stationID (""=all stations). Rows are ordered
// chronologically (oldest first) within each station. When limit>0, only the
// most recent `limit` buckets of each station are returned (0=no cap).
func (kb *SQLiteKB) LoadDemandHistory(ctx context.Context, itemID, stationID string, limit int) ([]DemandHistorySample, error) {
	query := `
		SELECT station_id, system_id, item_id, item_name, bucket_utc, captured_utc,
		       best_price, total_qty, sm_best_price, sm_qty, order_count
		FROM market_demand_history
		WHERE item_id=?`
	args := []any{itemID}
	if stationID != "" {
		query += ` AND station_id=?`
		args = append(args, stationID)
	}
	query += ` ORDER BY station_id ASC, bucket_utc ASC`

	rows, err := kb.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var all []DemandHistorySample
	for rows.Next() {
		var s DemandHistorySample
		var bucketStr, capStr string
		if err := rows.Scan(&s.StationID, &s.SystemID, &s.ItemID, &s.ItemName,
			&bucketStr, &capStr, &s.BestPrice, &s.TotalQty, &s.SMBestPrice, &s.SMQty, &s.OrderCount); err != nil {
			return nil, err
		}
		s.BucketAt = parseUTC(bucketStr)
		s.CapturedAt = parseUTC(capStr)
		all = append(all, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if limit > 0 {
		all = capPerStation(all, limit)
	}
	return all, nil
}

// capPerStation keeps only the most recent `limit` samples of each station from
// a slice already ordered by (station_id ASC, bucket_utc ASC).
func capPerStation(samples []DemandHistorySample, limit int) []DemandHistorySample {
	var out []DemandHistorySample
	for i := 0; i < len(samples); {
		j := i
		for j < len(samples) && samples[j].StationID == samples[i].StationID {
			j++
		}
		group := samples[i:j]
		if len(group) > limit {
			group = group[len(group)-limit:]
		}
		out = append(out, group...)
		i = j
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestLoadDemandHistoryFilterAndCap -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/knowledge/demand_load.go pkg/knowledge/demand_history_test.go
git commit -m "feat(knowledge): LoadDemandHistory with per-station cap"
```

---

### Task 4: `LatestDemandCapture` (freshness primitive)

**Files:**
- Modify: `pkg/knowledge/demand_load.go`
- Modify test: `pkg/knowledge/demand_history_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/knowledge/demand_history_test.go`:

```go
func TestLatestDemandCapture(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	// No data yet -> (zero, false, nil).
	if last, ok, err := kb.LatestDemandCapture(ctx, "stn1"); err != nil || ok || !last.IsZero() {
		t.Fatalf("empty: want (zero, false, nil), got last=%v ok=%v err=%v", last, ok, err)
	}

	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(10 * time.Minute)
	if err := kb.ReplaceStationBuyOrders(ctx, "stn1", []MarketBuyOrderRow{
		{StationID: "stn1", ItemID: "iron_ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: t0},
		{StationID: "stn1", ItemID: "copper", PriceEach: 8, Quantity: 20, Source: "station", CapturedAt: t1},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	last, ok, err := kb.LatestDemandCapture(ctx, "stn1")
	if err != nil || !ok {
		t.Fatalf("want (_, true, nil), got ok=%v err=%v", ok, err)
	}
	if !last.Equal(t1) {
		t.Errorf("want latest %v, got %v", t1, last)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestLatestDemandCapture -v`
Expected: FAIL to compile — `undefined: kb.LatestDemandCapture`.

- [ ] **Step 3: Add `LatestDemandCapture`**

Append to `pkg/knowledge/demand_load.go` (imports `database/sql` and `time` were added in Task 3):

```go
// LatestDemandCapture returns the most recent captured_utc recorded for a station
// in the live buy-order ledger (market_buy_orders), and whether any rows exist.
// It is the shared freshness primitive: callers use it to skip re-capturing a
// station whose demand was read recently (possibly by another agent). RFC3339
// timestamps sort lexically in chronological order, so MAX() gives the latest.
func (kb *SQLiteKB) LatestDemandCapture(ctx context.Context, stationID string) (time.Time, bool, error) {
	var capStr sql.NullString
	if err := kb.db.QueryRowContext(ctx,
		`SELECT MAX(captured_utc) FROM market_buy_orders WHERE station_id=?`, stationID).Scan(&capStr); err != nil {
		return time.Time{}, false, err
	}
	if !capStr.Valid || capStr.String == "" {
		return time.Time{}, false, nil
	}
	return parseUTC(capStr.String), true, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestLatestDemandCapture -v`
Expected: PASS.

- [ ] **Step 5: Full KB package check**

Run: `go test ./pkg/knowledge/`
Expected: PASS (all demand + migration + existing tests).

- [ ] **Step 6: Commit**

```bash
git add pkg/knowledge/demand_load.go pkg/knowledge/demand_history_test.go
git commit -m "feat(knowledge): LatestDemandCapture freshness primitive"
```

---

### Task 5: `aggregateDemandHistory` + bucket constant (play_as)

**Files:**
- Modify: `cmd/tools/play_as/demand_capture.go`
- Modify test: `cmd/tools/play_as/demand_capture_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/tools/play_as/demand_capture_test.go` (it already imports `testing` and `time`; add the `knowledge` import):

```go
func TestAggregateDemandHistory(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 34, 56, 0, time.UTC)
	orders := []knowledge.MarketBuyOrderRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 50, Source: "station"},
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 12, Quantity: 20, Source: ""},
		{StationID: "stn1", SystemID: "sys1", ItemID: "copper", ItemName: "Copper", PriceEach: 8, Quantity: 100, Source: "station"},
		{StationID: "stn1", SystemID: "sys1", ItemID: "copper", ItemName: "Copper", PriceEach: 0, Quantity: 5, Source: "station"}, // skipped
	}
	got := aggregateDemandHistory(orders, now, time.Hour)
	if len(got) != 2 {
		t.Fatalf("want 2 samples (iron, copper), got %d", len(got))
	}
	iron := got[0]
	if iron.ItemID != "iron_ore" {
		t.Fatalf("expected iron first (insertion order), got %s", iron.ItemID)
	}
	if iron.BestPrice != 12 || iron.TotalQty != 70 {
		t.Errorf("iron aggregate wrong: best=%v total=%v", iron.BestPrice, iron.TotalQty)
	}
	if iron.SMBestPrice != 10 || iron.SMQty != 50 {
		t.Errorf("iron SM split wrong: smBest=%v smQty=%v", iron.SMBestPrice, iron.SMQty)
	}
	if iron.OrderCount != 2 {
		t.Errorf("iron order count: want 2, got %d", iron.OrderCount)
	}
	want := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if !iron.BucketAt.Equal(want) {
		t.Errorf("bucket not hour-truncated: want %v got %v", want, iron.BucketAt)
	}
	if !iron.CapturedAt.Equal(now) {
		t.Errorf("captured should be now: %v", iron.CapturedAt)
	}
	copper := got[1]
	if copper.OrderCount != 1 || copper.TotalQty != 100 {
		t.Errorf("copper aggregate wrong (zero-price order must be skipped): %+v", copper)
	}
}
```

The import block of `demand_capture_test.go` becomes:

```go
import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestAggregateDemandHistory -v`
Expected: FAIL to compile — `undefined: aggregateDemandHistory`.

- [ ] **Step 3: Add the constant and function**

In `cmd/tools/play_as/demand_capture.go`, add after the imports (the file already imports `time` and `knowledge`):

```go
// demandHistoryBucket is the time-bucket granularity for demand-history samples.
// Captures within the same bucket upsert the same row (last observation wins).
const demandHistoryBucket = time.Hour

// aggregateDemandHistory collapses per-order buy demand into one
// DemandHistorySample per (station, item): best price and total quantity across
// all orders, plus the Station-Manager split (source=="station"). BucketAt is
// `now` truncated to the bucket size; CapturedAt is `now`. Output preserves
// first-seen order for deterministic rendering and tests. Orders with
// non-positive price or quantity are skipped.
func aggregateDemandHistory(orders []knowledge.MarketBuyOrderRow, now time.Time, bucket time.Duration) []knowledge.DemandHistorySample {
	type acc struct {
		stationID, systemID, itemID, itemName string
		best, total, smBest, smQty            float64
		count                                 int
	}
	key := func(s, i string) string { return s + "\x00" + i }
	order := []string{}
	m := map[string]*acc{}
	for _, o := range orders {
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		k := key(o.StationID, o.ItemID)
		a, ok := m[k]
		if !ok {
			a = &acc{stationID: o.StationID, systemID: o.SystemID, itemID: o.ItemID, itemName: o.ItemName}
			m[k] = a
			order = append(order, k)
		}
		a.total += o.Quantity
		a.count++
		if o.PriceEach > a.best {
			a.best = o.PriceEach
		}
		if o.Source == "station" {
			a.smQty += o.Quantity
			if o.PriceEach > a.smBest {
				a.smBest = o.PriceEach
			}
		}
	}
	bucketAt := now.UTC().Truncate(bucket)
	out := make([]knowledge.DemandHistorySample, 0, len(order))
	for _, k := range order {
		a := m[k]
		out = append(out, knowledge.DemandHistorySample{
			StationID:   a.stationID,
			SystemID:    a.systemID,
			ItemID:      a.itemID,
			ItemName:    a.itemName,
			BucketAt:    bucketAt,
			CapturedAt:  now,
			BestPrice:   a.best,
			TotalQty:    a.total,
			SMBestPrice: a.smBest,
			SMQty:       a.smQty,
			OrderCount:  a.count,
		})
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestAggregateDemandHistory -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/play_as/demand_capture.go cmd/tools/play_as/demand_capture_test.go
git commit -m "feat(play_as): aggregateDemandHistory + hourly bucket"
```

---

### Task 6: Freshness gate + dual write in `captureDemand`

**Files:**
- Modify: `cmd/tools/play_as/demand_capture.go`
- Modify test: `cmd/tools/play_as/demand_capture_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/tools/play_as/demand_capture_test.go`:

```go
func TestIsFresh(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if !isFresh(now.Add(-2*time.Minute), now, 5*time.Minute) {
		t.Error("2 min ago should be fresh within a 5 min window")
	}
	if isFresh(now.Add(-10*time.Minute), now, 5*time.Minute) {
		t.Error("10 min ago should be stale within a 5 min window")
	}
	if isFresh(now.Add(-5*time.Minute), now, 5*time.Minute) {
		t.Error("exactly 5 min should be stale (strictly-less window)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestIsFresh -v`
Expected: FAIL to compile — `undefined: isFresh`.

- [ ] **Step 3: Add the constant, predicate, and wire the gate**

In `cmd/tools/play_as/demand_capture.go`, add near `demandHistoryBucket`:

```go
// demandFreshness is how recently a station's demand must have been captured for
// a new capture to be skipped. Prevents many agents sharing a station from
// re-writing the same data minutes apart.
const demandFreshness = 5 * time.Minute

// isFresh reports whether a capture at `last` is recent enough (strictly within
// `window` of `now`) that re-capturing should be skipped.
func isFresh(last, now time.Time, window time.Duration) bool {
	return now.Sub(last) < window
}
```

Replace the body of `captureDemand` (currently ending with the single `ReplaceStationBuyOrders` call) so it gates on freshness and writes both tables:

```go
func captureDemand(client game.GameClient, ctx context.Context) {
	if globalKB == nil {
		return
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return
	}
	state := client.GetState()
	if state == nil {
		return
	}
	now := time.Now()
	orders := parseStationBuyOrders(client.GetRawJSON("market"), state.CurrentPOI, state.CurrentSystem, now)
	if len(orders) == 0 {
		return
	}
	// Skip both writes if this station's demand was captured recently (possibly by
	// another agent) — avoids redundant work across many agents sharing a station.
	if last, ok, err := sqlite.LatestDemandCapture(ctx, state.CurrentPOI); err == nil && ok && isFresh(last, now, demandFreshness) {
		return
	}
	_ = sqlite.ReplaceStationBuyOrders(ctx, state.CurrentPOI, orders)
	_ = sqlite.RecordDemandHistory(ctx, aggregateDemandHistory(orders, now, demandHistoryBucket))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestIsFresh -v`
Expected: PASS.

- [ ] **Step 5: Build + full package test**

Run: `go build ./...` then `go test ./cmd/tools/play_as/`
Expected: both succeed.

- [ ] **Step 6: Commit**

```bash
git add cmd/tools/play_as/demand_capture.go cmd/tools/play_as/demand_capture_test.go
git commit -m "feat(play_as): freshness gate + history dual-write in captureDemand"
```

---

### Task 7: `demand history` command

**Files:**
- Create: `cmd/tools/play_as/demand_history.go`
- Create test: `cmd/tools/play_as/demand_history_test.go`
- Modify: `cmd/tools/play_as/main.go` (the `case "demand":` block, around line 6344)

- [ ] **Step 1: Write the failing test**

Create `cmd/tools/play_as/demand_history_test.go`:

```go
package main

import "testing"

func TestTrendDirection(t *testing.T) {
	if got := trendDirection(10, 12); got != "↑" {
		t.Errorf("rising: want ↑, got %s", got)
	}
	if got := trendDirection(12, 10); got != "↓" {
		t.Errorf("falling: want ↓, got %s", got)
	}
	if got := trendDirection(10, 10); got != "→" {
		t.Errorf("flat: want →, got %s", got)
	}
}

func TestParseDemandHistoryOptions(t *testing.T) {
	opts, err := parseDemandHistoryOptions([]string{"iron_ore", "--station", "stn1", "--limit", "10"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.item != "iron_ore" || opts.station != "stn1" || opts.limit != 10 {
		t.Errorf("parsed wrong: %+v", opts)
	}

	// Default limit when omitted.
	opts, err = parseDemandHistoryOptions([]string{"copper"})
	if err != nil {
		t.Fatalf("parse default: %v", err)
	}
	if opts.limit != demandHistoryDefaultLimit {
		t.Errorf("default limit: want %d, got %d", demandHistoryDefaultLimit, opts.limit)
	}

	// Missing item is an error.
	if _, err := parseDemandHistoryOptions([]string{"--station", "stn1"}); err == nil {
		t.Error("missing item should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run 'TestTrendDirection|TestParseDemandHistoryOptions' -v`
Expected: FAIL to compile — `undefined: trendDirection` / `parseDemandHistoryOptions`.

- [ ] **Step 3: Create the command file**

Create `cmd/tools/play_as/demand_history.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// demandHistoryDefaultLimit caps the visible buckets per station (one day of
// hourly samples) when --limit is not supplied.
const demandHistoryDefaultLimit = 24

// demandHistoryOptions are the parsed flags for `demand history`.
type demandHistoryOptions struct {
	item    string
	station string
	limit   int
}

// parseDemandHistoryOptions parses `demand history <item> [--station id] [--limit N]`.
// The first non-flag argument is the required (lower-cased) item id.
func parseDemandHistoryOptions(args []string) (demandHistoryOptions, error) {
	opts := demandHistoryOptions{limit: demandHistoryDefaultLimit}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			if opts.item == "" {
				opts.item = strings.ToLower(arg)
				continue
			}
			return opts, fmt.Errorf("demand history: unexpected argument %q", arg)
		}
		key, val, hasEq := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		next := func() (string, error) {
			if hasEq {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("demand history: %s requires a value", arg)
			}
			i++
			return args[i], nil
		}
		switch key {
		case "station":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.station = v
		case "limit":
			v, err := next()
			if err != nil {
				return opts, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return opts, fmt.Errorf("demand history: --limit: %w", err)
			}
			opts.limit = n
		default:
			return opts, fmt.Errorf("demand history: unknown flag %q", arg)
		}
	}
	if opts.item == "" {
		return opts, fmt.Errorf("usage: demand history <item> [--station <id>] [--limit N]")
	}
	return opts, nil
}

// runDemandHistory loads the demand-history time series for an item from the
// ledger and renders it grouped per station with a trend-direction summary. It
// reads purely from the KB (no live game state needed).
func runDemandHistory(ctx context.Context, args []string, format outputFormat) error {
	if globalKB == nil {
		return fmt.Errorf("demand history: no knowledge DB configured (start play_as with --db)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("demand history: knowledge DB is not SQLite-backed")
	}
	opts, err := parseDemandHistoryOptions(args)
	if err != nil {
		return err
	}
	samples, err := sqlite.LoadDemandHistory(ctx, opts.item, opts.station, opts.limit)
	if err != nil {
		return fmt.Errorf("demand history: load: %w", err)
	}
	switch format {
	case formatStyled:
		fmt.Print(renderDemandHistoryStyled(opts.item, samples))
	default:
		fmt.Print(renderDemandHistoryJSON(samples))
	}
	return nil
}

// trendDirection compares the first and last value in a window and returns an
// arrow: ↑ rising, ↓ falling, → flat.
func trendDirection(first, last float64) string {
	switch {
	case last > first:
		return "↑"
	case last < first:
		return "↓"
	default:
		return "→"
	}
}

func renderDemandHistoryJSON(samples []knowledge.DemandHistorySample) string {
	b, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\":%q}\n", err.Error())
	}
	return string(b) + "\n"
}

func renderDemandHistoryStyled(item string, samples []knowledge.DemandHistorySample) string {
	var sb strings.Builder
	if len(samples) == 0 {
		fmt.Fprintf(&sb, "No demand history for %q. Visit stations and run view_market to accumulate samples.\n", item)
		return sb.String()
	}
	fmt.Fprintf(&sb, "Demand history — %s\n", item)
	// Samples arrive ordered by (station, bucket asc); group consecutive stations.
	for i := 0; i < len(samples); {
		j := i
		for j < len(samples) && samples[j].StationID == samples[i].StationID {
			j++
		}
		group := samples[i:j]
		priceDir := trendDirection(group[0].BestPrice, group[len(group)-1].BestPrice)
		qtyDir := trendDirection(group[0].TotalQty, group[len(group)-1].TotalQty)
		fmt.Fprintf(&sb, "\nStation %s  (price %s, demand %s)\n", group[0].StationID, priceDir, qtyDir)
		fmt.Fprintf(&sb, "%-16s %10s %10s %10s %10s\n", "BUCKET", "PRICE", "DEMAND", "SM PRICE", "SM QTY")
		for _, s := range group {
			fmt.Fprintf(&sb, "%-16s %10.0f %10.0f %10.0f %10.0f\n",
				s.BucketAt.Format("2006-01-02 15h"), s.BestPrice, s.TotalQty, s.SMBestPrice, s.SMQty)
		}
		i = j
	}
	return sb.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run 'TestTrendDirection|TestParseDemandHistoryOptions' -v`
Expected: PASS.

- [ ] **Step 5: Wire the dispatch**

In `cmd/tools/play_as/main.go`, replace the `case "demand":` block:

```go
	case "demand":
		opts, err := parseDemandOptions(parts[1:])
		if err != nil {
			return err
		}
		return runDemand(client, ctx, opts, format)
```

with:

```go
	case "demand":
		if len(parts) >= 2 && parts[1] == "history" {
			return runDemandHistory(ctx, parts[2:], format)
		}
		opts, err := parseDemandOptions(parts[1:])
		if err != nil {
			return err
		}
		return runDemand(client, ctx, opts, format)
```

- [ ] **Step 6: Build and verify the whole tool compiles**

Run: `go build ./...` then `go test ./cmd/tools/play_as/`
Expected: both succeed.

- [ ] **Step 7: Commit**

```bash
git add cmd/tools/play_as/demand_history.go cmd/tools/play_as/demand_history_test.go cmd/tools/play_as/main.go
git commit -m "feat(play_as): demand history command with per-station trends"
```

---

### Task 8: Final verification

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 2: Full test suite**

Run: `go test ./...`
Expected: all pass.

- [ ] **Step 3: Lint**

Run the `golangci-lint` tool over the changed packages (`pkg/knowledge/...`, `cmd/tools/play_as/...`).
Expected: no new findings.

- [ ] **Step 4: Confirm working tree**

Run: `git status --short`
Expected: only the pre-existing `server_docs/*` modifications remain unstaged. No stray debug files, no `aa.json`/`a.json` blobs. All feature commits are present in `git log`.

---

## Self-Review (controller — completed during planning)

**Spec coverage:**
- History table (migration 38, columns, PK, index) → Task 1. ✓
- `RecordDemandHistory` upsert → Task 2. ✓
- `LoadDemandHistory` (item/station filter, chronological, per-station limit) → Task 3. ✓
- `LatestDemandCapture` shared freshness primitive → Task 4. ✓
- `aggregateDemandHistory` pure func + `demandHistoryBucket` → Task 5. ✓
- Freshness gate (`demandFreshness`, `isFresh`) + dual write in `captureDemand` → Task 6. ✓
- `demand history` command + `history` dispatch sub-branch + `trendDirection` → Task 7. ✓
- Best-effort writes (errors swallowed) → preserved in Task 6's `captureDemand` body. ✓
- Testing matrix (upsert-within-bucket, load round-trip/filter, latest empty/latest, aggregate SM-split + truncation, isFresh, trendDirection) → Tasks 2–7. ✓
- Retention deferred / out-of-scope items → not built. ✓

**Type consistency:** `DemandHistorySample` field names (`BucketAt`, `CapturedAt`, `BestPrice`, `TotalQty`, `SMBestPrice`, `SMQty`, `OrderCount`) are used identically in `RecordDemandHistory`, `LoadDemandHistory`, `aggregateDemandHistory`, and the renderers. Method signatures match between definition and call sites (`LoadDemandHistory(ctx, item, station, limit)`, `LatestDemandCapture(ctx, station) (time.Time, bool, error)`, `runDemandHistory(ctx, args, format)`).

**No placeholders:** every code step contains complete code; every run step has an exact command and expected outcome.
