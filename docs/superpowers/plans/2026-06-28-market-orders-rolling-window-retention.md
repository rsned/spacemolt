# market_orders Rolling-Window Retention — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace append-only `market_orders` growth with a per-station rolling window of the last N captures (default 3), so the table self-prunes on every capture and stops growing.

**Architecture:** Add a `RetainCaptures` knob to `market.Config` (defaulted to 3 in `Open()`). Each `WriteSnapshot` — already one station per call — prunes that station to its N most recent captures inside its existing transaction. One new index supports the prune. No reader changes; `market_ohlcv` (the real price history) is untouched.

**Tech Stack:** Go 1.25, `modernc.org/sqlite`, table-driven tests, `golangci-lint`.

## Global Constraints

- Target Go 1.25 (`go.mod`); use range-over-int (`for i := range n`) where natural.
- All new/changed code must pass `golangci-lint` with no new findings (run after each task).
- SQLite writes go through `Collector.writeRetry` (WAL + `busy_timeout` via DSN); no raw `db.Exec` for writes.
- Schema changes are idempotent (`CREATE ... IF NOT EXISTS`) and live in the embedded `schema.sql` so existing DBs migrate on next `Open()`.
- Sleeps: none introduced by this plan.
- Spec: `docs/superpowers/specs/2026-06-27-market-orders-rolling-window-retention-design.md`.

## File Structure

- **Modify `pkg/market/collector.go`** — `Config` (add `RetainCaptures`), `DefaultConfig` (set 3), `Open` (default ≤0 → 3, store on `Collector`), `Collector` struct (add `RetainCaptures`), new `pruneStationCaptures` helper, `WriteSnapshot` (call the prune + update doc comment).
- **Modify `pkg/market/schema.sql`** — add `idx_orders_station_time`.
- **Create `pkg/market/retention_test.go`** — rolling-window behavior tests.
- **Modify `pkg/market/collector_test.go`** — config-default + index-exists tests.
- **Modify `cmd/tools/market-prune/main.go`** — doc comment: now a manual backstop.

No reader files change (verified: every `market_orders` reader anchors on latest-capture-per-station). No call-site changes to the four `WriteSnapshot` callers (`worker`, `overmind`, `auto-explorer`, `play_as`) — they get rolling-window retention for free via the `Open()` default.

---

### Task 1: RetainCaptures config knob, defaulted to 3

**Files:**
- Modify: `pkg/market/collector.go` (Config struct, DefaultConfig, Open, Collector struct)
- Test: `pkg/market/collector_test.go` (append a test)

**Interfaces:**
- Produces: `Config.RetainCaptures int`, `Collector.RetainCaptures int`. `Open(Config{...})` leaves a Collector whose `RetainCaptures` is the explicit value, or 3 when omitted/≤0. Task 3 reads `c.RetainCaptures`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/market/collector_test.go` (e.g. after `TestDefaultConfigPathIsRelative`):

```go
func TestRetainCapturesDefault(t *testing.T) {
	if got := DefaultConfig().RetainCaptures; got != 3 {
		t.Errorf("DefaultConfig().RetainCaptures = %d, want 3", got)
	}
	// Omitted ⇒ defaulted to 3.
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if c.RetainCaptures != 3 {
		t.Errorf("omitted RetainCaptures defaulted to %d, want 3", c.RetainCaptures)
	}
	// Explicit value is preserved.
	c2, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "t2.db"), RetainCaptures: 5})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c2.Close() })
	if c2.RetainCaptures != 5 {
		t.Errorf("explicit RetainCaptures = %d, want 5", c2.RetainCaptures)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/market/ -run TestRetainCapturesDefault -v`
Expected: FAIL / compile error — `Config.RetainCaptures undefined` and `c.RetainCaptures undefined`.

- [ ] **Step 3: Implement the config knob**

In `pkg/market/collector.go`, add the field to `Config` (gofmt-aligned):

```go
// Config holds configuration for the market database.
type Config struct {
	DBPath         string
	WAL            bool
	MaxOpenConns   int
	MaxIdleConns   int
	BusyTimeout    time.Duration
	RetainCaptures int
}
```

Set the default in `DefaultConfig`:

```go
// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		DBPath:         filepath.Join("data", "market.db"),
		WAL:            true,
		MaxOpenConns:   25,
		MaxIdleConns:   5,
		BusyTimeout:    5 * time.Second,
		RetainCaptures: 3,
	}
}
```

Add the field to `Collector`:

```go
// Collector handles market data collection.
type Collector struct {
	db             *sql.DB
	RetainCaptures int
}
```

In `Open`, default `RetainCaptures` alongside the other fields (right after the `BusyTimeout` default block), then store it on the returned Collector:

```go
	if cfg.BusyTimeout == 0 {
		cfg.BusyTimeout = DefaultConfig().BusyTimeout
	}
	if cfg.RetainCaptures <= 0 {
		cfg.RetainCaptures = DefaultConfig().RetainCaptures
	}
```

…and change the final return from `&Collector{db: db}` to:

```go
	return &Collector{db: db, RetainCaptures: cfg.RetainCaptures}, nil
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/market/ -run TestRetainCapturesDefault -v`
Expected: PASS.

- [ ] **Step 5: Lint + build**

Run: `golangci-lint run ./pkg/market/... && go build ./...`
Expected: no findings, clean build.

- [ ] **Step 6: Commit**

```bash
git add pkg/market/collector.go pkg/market/collector_test.go
git commit -m "feat(market): add RetainCaptures config, defaulted to 3 in Open"
```

---

### Task 2: idx_orders_station_time index

The per-station prune (Task 3) and several readers (`FindBestPrices`, `GetItemStationPrices`, `GetLatestSnapshot`) resolve "latest `captured_at` per station"; the current indexes don't cover `(station_id, captured_at)`.

**Files:**
- Modify: `pkg/market/schema.sql` (add index after `idx_orders_bucket`)
- Test: `pkg/market/collector_test.go` (append a test)

**Interfaces:**
- Produces: index `idx_orders_station_time` on `market_orders(station_id, captured_at)`, created idempotently by `runMigrations` (which re-runs the embedded `schema.sql`).

- [ ] **Step 1: Write the failing test**

Append to `pkg/market/collector_test.go`:

```go
func TestStationTimeIndexExists(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	var name string
	err = c.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='index' AND name='idx_orders_station_time'").Scan(&name)
	if err != nil {
		t.Fatalf("idx_orders_station_time missing after Open: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/market/ -run TestStationTimeIndexExists -v`
Expected: FAIL — `idx_orders_station_time missing after Open`.

- [ ] **Step 3: Add the index**

In `pkg/market/schema.sql`, immediately after the line
`CREATE INDEX IF NOT EXISTS idx_orders_bucket ON market_orders(bucket_utc);`
add:

```sql
CREATE INDEX IF NOT EXISTS idx_orders_station_time ON market_orders(station_id, captured_at);
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/market/ -run TestStationTimeIndexExists -v`
Expected: PASS.

- [ ] **Step 5: Lint + build**

Run: `golangci-lint run ./pkg/market/... && go build ./...`
Expected: no findings, clean build.

- [ ] **Step 6: Commit**

```bash
git add pkg/market/schema.sql pkg/market/collector_test.go
git commit -m "feat(market): add idx_orders_station_time for per-station latest-capture lookups"
```

---

### Task 3: Rolling-window self-prune in WriteSnapshot

**Files:**
- Modify: `pkg/market/collector.go` (new `pruneStationCaptures` helper; call it in `WriteSnapshot`; update `WriteSnapshot` doc comment)
- Test: Create `pkg/market/retention_test.go`

**Interfaces:**
- Consumes: `Collector.RetainCaptures` (Task 1), `idx_orders_station_time` (Task 2).
- Produces: `WriteSnapshot` now leaves at most `RetainCaptures` distinct `captured_at` per station. `pruneStationCaptures(tx, stationID, retain)` is package-private.

- [ ] **Step 1: Write the failing tests**

Create `pkg/market/retention_test.go`:

```go
package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// writeCaptureAt writes a single-item sell snapshot for stn captured at at with
// the given price, so captures are distinguishable by price.
func writeCaptureAt(t *testing.T, c *Collector, stn string, at time.Time, price float64) {
	t.Helper()
	if err := c.WriteSnapshot(context.Background(), MarketSnapshot{
		StationID: stn, StationName: stn, SystemID: "sys", SystemName: "S",
		CapturedAt: at,
		Orders: []Order{{
			StationID: stn, ItemID: "iron", ItemName: "Iron", Side: "sell",
			PriceEach: price, Quantity: 1, CapturedAt: at,
		}},
	}); err != nil {
		t.Fatalf("WriteSnapshot %s @ %v: %v", stn, at, err)
	}
}

// distinctCaptures counts distinct captured_at values for a station.
func distinctCaptures(t *testing.T, c *Collector, stn string) int {
	t.Helper()
	var n int
	if err := c.db.QueryRow(
		`SELECT COUNT(DISTINCT captured_at) FROM market_orders WHERE station_id = ?`, stn).Scan(&n); err != nil {
		t.Fatalf("count distinct captures %s: %v", stn, err)
	}
	return n
}

func TestRollingWindow_KeepsLastN(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")}) // default N=3
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	base := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	// Write N+1 = 4 captures; the oldest (price 10) must be pruned, leaving 3.
	for i := range 4 {
		writeCaptureAt(t, c, "stn1", base.Add(time.Duration(i)*time.Hour), float64(10+i))
	}

	if got := distinctCaptures(t, c, "stn1"); got != 3 {
		t.Fatalf("distinct captures = %d, want 3", got)
	}
	var minPrice, maxPrice float64
	if err := c.db.QueryRow(
		`SELECT MIN(price_each), MAX(price_each) FROM market_orders WHERE station_id = ?`, "stn1").
		Scan(&minPrice, &maxPrice); err != nil {
		t.Fatalf("price range: %v", err)
	}
	if minPrice == 10 {
		t.Errorf("oldest capture (price 10) should be pruned, but a price-10 row remains")
	}
	if maxPrice != 13 {
		t.Errorf("newest price = %v, want 13", maxPrice)
	}
}

func TestRollingWindow_CrossStationIsolation(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db"), RetainCaptures: 1})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	base := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	writeCaptureAt(t, c, "stnA", base, 1)
	writeCaptureAt(t, c, "stnB", base, 2)
	writeCaptureAt(t, c, "stnA", base.Add(time.Hour), 3)
	writeCaptureAt(t, c, "stnA", base.Add(2*time.Hour), 4)

	if got := distinctCaptures(t, c, "stnA"); got != 1 {
		t.Errorf("stnA captures = %d, want 1 (N=1 keeps latest only)", got)
	}
	if got := distinctCaptures(t, c, "stnB"); got != 1 {
		t.Errorf("stnB captures = %d, want 1 (untouched by stnA's captures)", got)
	}
}

func TestRollingWindow_OHLCVNotPruned(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db"), RetainCaptures: 1})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	t1 := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 21, 11, 0, 0, 0, time.UTC)
	// Two captures in distinct hours. With N=1 the older market_orders capture is
	// pruned, but OHLCV must still hold both hourly buckets (it is never pruned).
	writeCaptureAt(t, c, "stn1", t1, 5)
	writeCaptureAt(t, c, "stn1", t2, 9)

	var ohlcvBuckets int
	if err := c.db.QueryRow(
		`SELECT COUNT(*) FROM market_ohlcv WHERE station_id = ?`, "stn1").Scan(&ohlcvBuckets); err != nil {
		t.Fatalf("count ohlcv: %v", err)
	}
	if ohlcvBuckets != 2 {
		t.Errorf("ohlcv buckets = %d, want 2 (OHLCV survives the rolling window)", ohlcvBuckets)
	}
	if got := distinctCaptures(t, c, "stn1"); got != 1 {
		t.Errorf("market_orders captures = %d, want 1 (N=1 pruned the older capture)", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/market/ -run TestRollingWindow -v`
Expected: FAIL — `TestRollingWindow_KeepsLastN` sees 4 distinct captures (no pruning yet), want 3. The isolation/OHLCV tests likewise fail because nothing prunes.

- [ ] **Step 3: Add the prune helper**

In `pkg/market/collector.go`, add this method immediately after `insertOrders`:

```go
// pruneStationCaptures deletes all but the retain most-recent captures for a
// station. A "capture" is the set of market_orders rows sharing one captured_at.
// Called inside WriteSnapshot's transaction so each capture self-cleans its
// station — this is what keeps market_orders bounded without an external prune.
func (c *Collector) pruneStationCaptures(tx *sql.Tx, stationID string, retain int) error {
	if retain <= 0 {
		return nil
	}
	if _, err := tx.Exec(`
		DELETE FROM market_orders
		WHERE station_id = ?
		  AND captured_at NOT IN (
		      SELECT DISTINCT captured_at FROM market_orders
		      WHERE station_id = ?
		      ORDER BY captured_at DESC
		      LIMIT ?
		  )`, stationID, stationID, retain); err != nil {
		return fmt.Errorf("prune station captures: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Wire the prune into WriteSnapshot and update its doc comment**

In `WriteSnapshot`, immediately after the `// Insert orders` block (the `insertOrders` call and its error return), add:

```go
		// Rolling-window retention: keep only the RetainCaptures most recent
		// captures for this station. Self-cleans so no external prune is needed.
		if err := c.pruneStationCaptures(tx, snapshot.StationID, c.RetainCaptures); err != nil {
			return err
		}
```

Replace the **entire** `WriteSnapshot` doc comment — the whole comment block sitting immediately above `func (c *Collector) WriteSnapshot(...)` (it currently starts "// WriteSnapshot persists a market snapshot atomically" and ends just before the `func` line) — with:

```go
// WriteSnapshot persists a market snapshot atomically: it upserts the station
// and items, appends every order to market_orders, prunes the station to its
// last RetainCaptures captures, and upserts the hourly OHLCV bucket for each
// (station, item, side).
//
// market_orders is a rolling window, not an append-only log: each capture
// deletes that station's older captures beyond RetainCaptures, so the table
// holds only the most recent few snapshots per station. Long-term price history
// lives in market_ohlcv (never pruned). OHLCV is computed from this snapshot's
// orders and keyed by the truncated UTC hour; a capture in the SAME hour as a
// previous one overwrites that hour's OHLCV row, so distinct OHLCV points come
// from captures in distinct UTC hours.
```

- [ ] **Step 5: Run the new tests to verify they pass**

Run: `go test ./pkg/market/ -run TestRollingWindow -v`
Expected: PASS (all three).

- [ ] **Step 6: Run the full package suite — confirm no regressions**

Run: `go test ./pkg/market/ -v`
Expected: PASS — every existing test still passes (multi-capture tests write ≤3 captures; `GetStats` OrderCount uses the AUTOINCREMENT high-water mark, unaffected by deletes).

- [ ] **Step 7: Lint + build**

Run: `golangci-lint run ./pkg/market/... && go build ./...`
Expected: no findings, clean build.

- [ ] **Step 8: Commit**

```bash
git add pkg/market/collector.go pkg/market/retention_test.go
git commit -m "feat(market): rolling-window retention — prune station to last N captures on each WriteSnapshot"
```

---

### Task 4: Mark market-prune as a manual backstop

With retention self-managed per capture (Task 3), the scheduled use of `cmd/tools/market-prune` is obsolete. The CLI stays as a manual/one-off backstop. This is a doc-only change.

**Files:**
- Modify: `cmd/tools/market-prune/main.go` (package doc comment)

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update the package doc comment**

In `cmd/tools/market-prune/main.go`, replace the existing package doc comment (lines 1–9, beginning "// Command market-prune enforces a retention window") with:

```go
// Command market-prune is a MANUAL backstop that enforces a time-based retention
// window on the market_orders table.
//
// As of rolling-window retention, WriteSnapshot self-prunes each station to its
// last few captures, so this command is NO LONGER run on a schedule. Keep it for
// one-off sweeps (e.g. a generous --retain to clear stale stations) or --vacuum
// to shrink the file during a maintenance window with no writers active.
//
// --retain deletes orders whose capture bucket is older than the duration.
// --vacuum rebuilds the file (exclusive lock; only when no writers — overminds,
// scanner, play_as — are touching the database). Freed pages are reused by later
// inserts, so without --vacuum the file stabilizes rather than shrinking.
package main
```

- [ ] **Step 2: Build to confirm the package still compiles**

Run: `go build ./cmd/tools/market-prune/`
Expected: clean build (doc-only change).

- [ ] **Step 3: Lint**

Run: `golangci-lint run ./cmd/tools/market-prune/...`
Expected: no findings.

- [ ] **Step 4: Commit**

```bash
git add cmd/tools/market-prune/main.go
git commit -m "docs(market-prune): mark as manual backstop now that retention is self-managed"
```

---

## Operational follow-ups (after merge, not code tasks)

- **Stop the scheduled market-prune trigger.** Recon could not find it in code, crontab, systemd, Makefile, or scripts — it runs from somewhere external. Leaving it running is harmless (it only deletes rows older than its `--retain`, which the per-station prune also subsumes), but it should be retired to avoid confusion.
- **No VACUUM at deploy** (per locked decision). The 13 GB file stabilizes (freed pages reused, growth stops). A future maintenance-window `market-prune --vacuum` can reclaim the ~12 GB of free space when desired.
- **Confirm convergence:** within ~1 hour of deploy, active stations drop from ≤12h-history to ≤3 captures (visible via the dashboard's capture-health view, now showing up to 3 captures per station).
