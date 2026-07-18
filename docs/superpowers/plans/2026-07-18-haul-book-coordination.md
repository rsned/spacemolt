# Haul Book Coordination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the *book* `(item_id, from_station_id)` the unit of haul coordination so a deep order book is shared across haulers (fanned out over destinations), a thin book admits only as many haulers as it can supply, and a collapsed book is cleared instead of endlessly republished.

**Architecture:** Keep `arbitrage_opportunities` rows as the destination enumerator + per-row destination lock. Add a `source_units` column (the book's source supply) and a `haul_book_claims` roster table. Admission runs in one SQLite `IMMEDIATE` transaction (count-under-cap → pick unassigned destination → claim opp row → insert roster row); the book size sets the concurrency cap `K = ceil(source_units / ship cargo)`. A hauler subtracts its *actual* bought quantity into `bought_units` at the buy phase (settle-only). On a collapse-confirmed abandon it invalidates the book's available rows so no new hauler is drawn in.

**Tech Stack:** Go 1.24+, `modernc.org/sqlite` (via `database/sql`), existing `pkg/market.Collector` write-retry pattern, `pkg/worker` haul engine.

## Global Constraints

- Go 1.24+; use `max()`/`min()` builtins and `range over int` where natural.
- All new code passes `golangci-lint` with no new findings; gate every task with `go build ./... && go test ./...`.
- `market.db` is a shared multi-process SQLite database (~40 agents). Every write goes through `Collector.writeRetry` (retries `SQLITE_BUSY`). Never `db.Exec("PRAGMA ...")` for connection settings — use the DSN (see Task 1).
- Timestamps stored as RFC3339 UTC (`time.Now().UTC().Format(time.RFC3339)`); freshness compared with `julianday(...)`, never string compare. Reuse the existing `notExpiredSQL` constant (`pkg/market/arbitrage.go:343`).
- Do NOT add a numbered migration for the new column — use the `ensureColumn` pattern in `pkg/market/migrations.go` and add the table to `pkg/market/schema.sql` (reference: `reference_ships_table_migration_trap`).
- Commit staging is explicit per file (never `git add -A` — `data/*.json` is dirty runtime churn).
- End commit messages with the `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` trailer.

---

### Task 1: IMMEDIATE transactions on the market DB

**Why:** Deadlock-freedom of admission depends on transactions taking the write lock at `BEGIN` (no read→write upgrade). `modernc.org/sqlite` issues deferred `BEGIN` by default; `_txlock=immediate` on the DSN makes every `db.Begin()` a `BEGIN IMMEDIATE`. All `writeRetry` transactions are writes, so immediate is correct for all of them.

**Files:**
- Modify: `pkg/market/collector.go:87-93` (`sqliteDSN`)
- Test: `pkg/market/collector_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `sqliteDSN(cfg)` now appends `&_txlock=immediate`. No signature change.

- [ ] **Step 1: Write the failing test**

Add to `pkg/market/collector_test.go`:

```go
func TestSqliteDSNUsesImmediateTxLock(t *testing.T) {
	dsn := sqliteDSN(Config{DBPath: "data/market.db", WAL: true, BusyTimeout: 5 * time.Second})
	if !strings.Contains(dsn, "_txlock=immediate") {
		t.Fatalf("DSN must request immediate txlock for deadlock-free admission; got %q", dsn)
	}
}
```

If `strings` / `time` are not yet imported in the test file, add them to its import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestSqliteDSNUsesImmediateTxLock -v`
Expected: FAIL (`_txlock=immediate` not present).

- [ ] **Step 3: Implement**

In `pkg/market/collector.go`, change `sqliteDSN`:

```go
func sqliteDSN(cfg Config) string {
	dsn := cfg.DBPath + "?_pragma=busy_timeout(" + strconv.Itoa(int(cfg.BusyTimeout.Milliseconds())) + ")"
	if cfg.WAL {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	// Take the write lock at BEGIN (never upgrade read->write): this is what makes
	// the book-admission transaction deadlock-free across the ~40 worker processes
	// that share this database. Every writeRetry transaction is a write, so
	// IMMEDIATE is correct for all of them.
	dsn += "&_txlock=immediate"
	return dsn
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestSqliteDSNUsesImmediateTxLock -v && go test ./pkg/market/`
Expected: PASS, and the existing market suite still passes.

- [ ] **Step 5: Commit**

```bash
git add pkg/market/collector.go pkg/market/collector_test.go
git commit -m "perf(market): IMMEDIATE txlock on market.db for deadlock-free writes

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `source_units` column populated by the scanner

**Why:** The book size (`src.AskQty`) is currently lost — each row stores only `quantity = min(AskQty, BidQty)`. Haulers need the true source depth to compute the cap `K` and to render an honest status label.

**Files:**
- Modify: `pkg/market/types.go:67-95` (`ArbitrageOpportunity` struct)
- Modify: `pkg/market/schema.sql:63-89` (add column to the `CREATE TABLE`)
- Modify: `pkg/market/migrations.go:13-35` (`ensureColumn` call)
- Modify: `pkg/market/arbitrage.go` (`arbCandidate`, `ScanArbitrage` insert, `arbitrageSelectJoin`, `scanOpportunityRows`)
- Test: `pkg/market/arbitrage_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `ArbitrageOpportunity.SourceUnits float64` (json `source_units`), populated on scan and hydrated on every read via `GetOpportunities`/`GetClaimedByAgent`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/market/arbitrage_test.go` (follow the file's existing seeding helpers for `market_orders`/`stations`; seed one source station with a 40-unit best-ask sell book for `widget`, and two destination stations each with a buy book):

```go
func TestScanArbitragePopulatesSourceUnits(t *testing.T) {
	c := newTestCollector(t) // existing helper in this package's tests
	seedSellBook(t, c, "src_station", "widget", 10.0, 40.0)  // best ask 10, qty 40
	seedBuyBook(t, c, "dst_a", "widget", 100.0, 15.0)        // best bid 100, qty 15
	seedBuyBook(t, c, "dst_b", "widget", 90.0, 30.0)         // best bid 90, qty 30

	if _, err := c.ScanArbitrage(context.Background(), ScanOptions{}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	opps, err := c.GetOpportunities(context.Background(), "available", 50)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(opps) < 2 {
		t.Fatalf("want >=2 opps off one source book, got %d", len(opps))
	}
	for _, o := range opps {
		if o.FromStationID == "src_station" && o.SourceUnits != 40 {
			t.Fatalf("opp to %s: SourceUnits = %v, want 40 (the source best-ask depth)", o.ToStationID, o.SourceUnits)
		}
	}
}
```

If `seedSellBook`/`seedBuyBook`/`newTestCollector` do not exist under these names, use the equivalent seeding already present in `arbitrage_test.go` (match the existing tests' setup — do not invent a new harness).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestScanArbitragePopulatesSourceUnits -v`
Expected: FAIL (`SourceUnits` field/column absent → compile error or 0 value).

- [ ] **Step 3: Implement — struct field**

In `pkg/market/types.go`, add to `ArbitrageOpportunity` (after `Quantity`):

```go
	Quantity        float64 `json:"quantity"`
	SourceUnits     float64 `json:"source_units"` // book's source best-ask depth (src.AskQty); shared across a book's dest rows
```

- [ ] **Step 4: Implement — schema + migration**

In `pkg/market/schema.sql`, add the column inside `CREATE TABLE ... arbitrage_opportunities` (after `quantity REAL NOT NULL,`):

```sql
    quantity            REAL NOT NULL,
    source_units        REAL NOT NULL DEFAULT 0,
```

In `pkg/market/migrations.go`, add after the `cycles_seen` ensureColumn block (before `mission_results`):

```go
	// source_units: the book's (item, from_station) source best-ask depth, shared
	// across that book's destination rows. Drives the hauler's per-book concurrency
	// cap and status label.
	if err := ensureColumn(db, "arbitrage_opportunities", "source_units", "REAL NOT NULL DEFAULT 0"); err != nil {
		return err
	}
```

- [ ] **Step 5: Implement — scanner populates it**

In `pkg/market/arbitrage.go`, add to `arbCandidate` (line ~83):

```go
type arbCandidate struct {
	fromStation, toStation, itemID       string
	buyPrice, sellPrice, qty, gross      float64
	sourceUnits                          float64
}
```

In the candidate-building loop (line ~144), set it:

```go
				candidates = append(candidates, arbCandidate{
					fromStation: src.StationID,
					toStation:   dst.StationID,
					itemID:      itemID,
					buyPrice:    src.BestAsk,
					sellPrice:   dst.BestBid,
					qty:         qty,
					gross:       gross,
					sourceUnits: src.AskQty,
				})
```

In the INSERT (line ~185), add the column + value:

```go
			_, err := tx.ExecContext(ctx, `
				INSERT INTO arbitrage_opportunities
				  (from_station_id, to_station_id, item_id, action_type, buy_price, sell_price,
				   quantity, source_units, gross_profit, fuel_cost, travel_ticks, cargo_required, cycles_seen, status,
				   expires_at, discovered_at, discovered_by, notes)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				cand.fromStation, cand.toStation, cand.itemID, "buy_then_sell",
				cand.buyPrice, cand.sellPrice, cand.qty, cand.sourceUnits, cand.gross,
				0.0, 0, cand.qty, cyclesSeen, "available", expiresAt, discoveredAt, "arbitrage_scanner", "logistics:deferred")
```

(Note: one extra `?` and `cand.sourceUnits` inserted right after `cand.qty`.)

- [ ] **Step 6: Implement — hydrate on read**

In `pkg/market/arbitrage.go`, add `ao.source_units` to `arbitrageSelectJoin` (right after `ao.quantity`):

```go
			ao.buy_price, ao.sell_price, ao.quantity, ao.source_units, ao.gross_profit,
```

In `scanOpportunityRows`, add the scan target in the matching position (after `&o.Quantity`):

```go
			&o.BuyPrice, &o.SellPrice, &o.Quantity, &o.SourceUnits, &o.GrossProfit,
```

- [ ] **Step 7: Run tests**

Run: `go test ./pkg/market/ -run TestScanArbitragePopulatesSourceUnits -v && go test ./pkg/market/`
Expected: PASS (and the full market suite still green — the column-order edits in `arbitrageSelectJoin`/`scanOpportunityRows` must stay in lockstep, so any mismatch surfaces here).

- [ ] **Step 8: Commit**

```bash
git add pkg/market/types.go pkg/market/schema.sql pkg/market/migrations.go pkg/market/arbitrage.go pkg/market/arbitrage_test.go
git commit -m "feat(market): persist per-book source_units on arbitrage opportunities

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `haul_book_claims` table

**Why:** The roster of active claims per book — the substrate for the cap count, destination fan-out, settle, and release.

**Files:**
- Modify: `pkg/market/schema.sql` (add table + indexes near the arbitrage section, ~line 93)
- Test: `pkg/market/migrations_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: table `haul_book_claims` with columns
  `claim_id, item_id, from_station_id, opp_id, to_station_id, agent_id, phase, bought_units, claimed_at, updated_at, expires_at`;
  a partial unique index enforcing one *active* claim per `(item, from, agent)`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/market/migrations_test.go` (mirror the existing in-memory migration tests in that file):

```go
func TestHaulBookClaimsTableCreated(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := runMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var name string
	err = db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='haul_book_claims'`).Scan(&name)
	if err != nil {
		t.Fatalf("haul_book_claims table not created: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestHaulBookClaimsTableCreated -v`
Expected: FAIL (`sql: no rows`).

- [ ] **Step 3: Implement — add table to schema.sql**

In `pkg/market/schema.sql`, after the arbitrage indexes (line ~92), add:

```sql
-- Haul book coordination: one row per active claim on a book (item + source
-- station). Concurrency cap and destination fan-out are derived from this roster.
CREATE TABLE IF NOT EXISTS haul_book_claims (
    claim_id        INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id         TEXT NOT NULL,
    from_station_id TEXT NOT NULL,
    opp_id          INTEGER NOT NULL,
    to_station_id   TEXT NOT NULL,
    agent_id        TEXT NOT NULL,
    phase           TEXT NOT NULL DEFAULT 'claimed'
                       CHECK (phase IN ('claimed','bought','released','done')),
    bought_units    REAL NOT NULL DEFAULT 0,
    claimed_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_bookclaims_book
    ON haul_book_claims(item_id, from_station_id, phase, expires_at);
-- One ACTIVE claim per agent per book (released/done history rows are exempt).
CREATE UNIQUE INDEX IF NOT EXISTS idx_bookclaims_active_agent
    ON haul_book_claims(item_id, from_station_id, agent_id)
    WHERE phase IN ('claimed','bought');
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestHaulBookClaimsTableCreated -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/market/schema.sql pkg/market/migrations_test.go
git commit -m "feat(market): haul_book_claims roster table + active-claim unique index

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Book admission store method

**Why:** The atomic, serialized admission that enforces the cap, fans out destinations, and takes both the roster row and the per-row destination lock in one `IMMEDIATE` transaction.

**Files:**
- Create: `pkg/market/arbitrage_book.go`
- Test: `pkg/market/arbitrage_book_test.go`

**Interfaces:**
- Consumes: `writeRetry`, `notExpiredSQL` (both `pkg/market`), the `arbitrage_opportunities` + `haul_book_claims` tables.
- Produces:
  ```go
  type BookKey struct { ItemID, FromStationID string }
  type BookCandidate struct { OppID int; ToStationID string } // best-first
  type AdmitResult struct { OK bool; ClaimID int64; OppID int; ToStationID string }
  func (c *Collector) AdmitBookClaim(ctx context.Context, book BookKey, candidates []BookCandidate, agentID string, capN int, expiresIn time.Duration) (AdmitResult, error)
  ```

- [ ] **Step 1: Write the failing test**

Create `pkg/market/arbitrage_book_test.go`:

```go
package market

import (
	"context"
	"sync"
	"testing"
	"time"
)

// seedAvailableOpp inserts one available arbitrage row and returns its id.
func seedAvailableOpp(t *testing.T, c *Collector, item, from, to string, sourceUnits float64) int {
	t.Helper()
	now := time.Now().UTC()
	res, err := c.db.Exec(`
		INSERT INTO arbitrage_opportunities
		  (from_station_id, to_station_id, item_id, action_type, buy_price, sell_price,
		   quantity, source_units, gross_profit, fuel_cost, travel_ticks, cargo_required,
		   cycles_seen, status, expires_at, discovered_at, discovered_by, notes)
		VALUES (?,?,?, 'buy_then_sell', 10, 100, ?, ?, 9000, 0, 0, ?, 1, 'available', ?, ?, 'test', '')`,
		from, to, item, sourceUnits, sourceUnits, sourceUnits,
		now.Add(time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed opp: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func TestAdmitBookClaim_CapAndFanout(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	book := BookKey{ItemID: "widget", FromStationID: "src"}
	// One book, three destination rows, source depth 900.
	idA := seedAvailableOpp(t, c, "widget", "src", "dst_a", 900)
	idB := seedAvailableOpp(t, c, "widget", "src", "dst_b", 900)
	idC := seedAvailableOpp(t, c, "widget", "src", "dst_c", 900)
	cands := []BookCandidate{{idA, "dst_a"}, {idB, "dst_b"}, {idC, "dst_c"}}

	// cap 2 -> two admits succeed on distinct dests, third is rejected.
	r1, err := c.AdmitBookClaim(ctx, book, cands, "h1", 2, time.Hour)
	if err != nil || !r1.OK {
		t.Fatalf("admit 1: ok=%v err=%v", r1.OK, err)
	}
	r2, err := c.AdmitBookClaim(ctx, book, cands, "h2", 2, time.Hour)
	if err != nil || !r2.OK {
		t.Fatalf("admit 2: ok=%v err=%v", r2.OK, err)
	}
	if r1.ToStationID == r2.ToStationID {
		t.Fatalf("fan-out failed: both admits took %s", r1.ToStationID)
	}
	r3, err := c.AdmitBookClaim(ctx, book, cands, "h3", 2, time.Hour)
	if err != nil {
		t.Fatalf("admit 3 err: %v", err)
	}
	if r3.OK {
		t.Fatalf("admit 3 should be rejected: book at capacity 2")
	}
}

func TestAdmitBookClaim_ThinBookCapOne(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	book := BookKey{ItemID: "core", FromStationID: "starfall"}
	idA := seedAvailableOpp(t, c, "core", "starfall", "dst_a", 42)
	idB := seedAvailableOpp(t, c, "core", "starfall", "dst_b", 42)
	cands := []BookCandidate{{idA, "dst_a"}, {idB, "dst_b"}}

	r1, _ := c.AdmitBookClaim(ctx, book, cands, "h1", 1, time.Hour)
	r2, _ := c.AdmitBookClaim(ctx, book, cands, "h2", 1, time.Hour)
	if !r1.OK || r2.OK {
		t.Fatalf("cap 1 must admit exactly one: r1=%v r2=%v", r1.OK, r2.OK)
	}
}

func TestAdmitBookClaim_ConcurrentNoDeadlockNoDoubleAdmit(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	book := BookKey{ItemID: "widget", FromStationID: "src"}
	idA := seedAvailableOpp(t, c, "widget", "src", "dst_a", 42)
	cands := []BookCandidate{{idA, "dst_a"}}

	var wg sync.WaitGroup
	results := make([]bool, 8)
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := c.AdmitBookClaim(ctx, book, cands, "h"+string(rune('0'+i)), 1, time.Hour)
			if err != nil {
				t.Errorf("admit %d: %v", i, err)
			}
			results[i] = r.OK
		}(i)
	}
	wg.Wait()
	won := 0
	for _, ok := range results {
		if ok {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("cap 1 book under 8 concurrent admits: want exactly 1 winner, got %d", won)
	}
}
```

If `newTestCollector` is not the helper name in this package, use whatever the other `pkg/market` tests use to open a temp-file collector (a real file, not `:memory:`, so the concurrency test exercises WAL + `_txlock`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestAdmitBookClaim -v`
Expected: FAIL to compile (`AdmitBookClaim`/`BookKey`/`BookCandidate`/`AdmitResult` undefined).

- [ ] **Step 3: Implement**

Create `pkg/market/arbitrage_book.go`:

```go
package market

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// BookKey identifies a haul "book": one item at one source station.
type BookKey struct {
	ItemID        string
	FromStationID string
}

// BookCandidate is one destination row of a book, offered to AdmitBookClaim in
// ranked (best-first) order. OppID is the arbitrage_opportunities row id.
type BookCandidate struct {
	OppID       int
	ToStationID string
}

// AdmitResult reports the outcome of AdmitBookClaim.
type AdmitResult struct {
	OK          bool   // false = book at capacity, or no candidate still claimable
	ClaimID     int64  // haul_book_claims.claim_id (valid when OK)
	OppID       int    // admitted opportunity row (valid when OK)
	ToStationID string // its destination (valid when OK)
}

// AdmitBookClaim atomically admits agentID onto a book if it is under capacity,
// assigning the best still-available destination not already held by another active
// claimant (fan-out; degrades to reusing the best destination when all are taken).
// The whole decision — cap count, taken-destination read, per-row claim, and roster
// insert — runs in one IMMEDIATE transaction, so a losing racer observes the winner's
// committed state and never re-collides. Returns OK=false (no error) when the book is
// at capacity or every candidate row was claimed by someone else first.
func (c *Collector) AdmitBookClaim(ctx context.Context, book BookKey, candidates []BookCandidate, agentID string, capN int, expiresIn time.Duration) (AdmitResult, error) {
	var res AdmitResult
	if capN < 1 || len(candidates) == 0 {
		return res, nil
	}
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	expiresStr := now.Add(expiresIn).Format(time.RFC3339)

	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		// 1. Capacity: how many haulers are already active on this book?
		var active int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM haul_book_claims
			 WHERE item_id=? AND from_station_id=? AND phase IN ('claimed','bought')
			   AND `+notExpiredSQL, book.ItemID, book.FromStationID).Scan(&active); err != nil {
			return fmt.Errorf("count book claimants: %w", err)
		}
		if active >= capN {
			return nil // res.OK stays false
		}

		// 2. Destinations already taken by active claimants (for fan-out).
		taken := map[string]bool{}
		rows, err := tx.QueryContext(ctx,
			`SELECT to_station_id FROM haul_book_claims
			 WHERE item_id=? AND from_station_id=? AND phase IN ('claimed','bought')
			   AND `+notExpiredSQL, book.ItemID, book.FromStationID)
		if err != nil {
			return fmt.Errorf("read taken dests: %w", err)
		}
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan taken dest: %w", err)
			}
			taken[d] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate taken dests: %w", err)
		}
		_ = rows.Close()

		// 3. Prefer unassigned destinations (preserving rank), then fall back to taken
		//    ones so a fat book with fewer distinct dests than K is not stranded.
		ordered := make([]BookCandidate, 0, len(candidates))
		for _, cand := range candidates {
			if !taken[cand.ToStationID] {
				ordered = append(ordered, cand)
			}
		}
		for _, cand := range candidates {
			if taken[cand.ToStationID] {
				ordered = append(ordered, cand)
			}
		}

		// 4. Claim the first candidate whose opp row is still available (the per-row
		//    destination lock), then record the roster row.
		for _, cand := range ordered {
			r, err := tx.ExecContext(ctx,
				`UPDATE arbitrage_opportunities SET status='claimed', claimed_by=?, claimed_at=?
				 WHERE id=? AND status='available' AND `+notExpiredSQL,
				agentID, nowStr, cand.OppID)
			if err != nil {
				return fmt.Errorf("claim opp row: %w", err)
			}
			n, _ := r.RowsAffected()
			if n == 0 {
				continue // claimed by another hauler between scan and now; try next
			}
			ins, err := tx.ExecContext(ctx,
				`INSERT INTO haul_book_claims
				   (item_id, from_station_id, opp_id, to_station_id, agent_id, phase,
				    bought_units, claimed_at, updated_at, expires_at)
				 VALUES (?,?,?,?,?, 'claimed', 0, ?,?,?)`,
				book.ItemID, book.FromStationID, cand.OppID, cand.ToStationID, agentID,
				nowStr, nowStr, expiresStr)
			if err != nil {
				return fmt.Errorf("insert book claim: %w", err)
			}
			id, _ := ins.LastInsertId()
			res = AdmitResult{OK: true, ClaimID: id, OppID: cand.OppID, ToStationID: cand.ToStationID}
			return nil
		}
		return nil // nothing claimable
	})
	return res, err
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/market/ -run TestAdmitBookClaim -v`
Expected: PASS (cap, fan-out, thin-book, and concurrency winner-count all green).

- [ ] **Step 5: Commit**

```bash
git add pkg/market/arbitrage_book.go pkg/market/arbitrage_book_test.go
git commit -m "feat(market): AdmitBookClaim — capped, fanned-out, serialized book admission

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Settle / complete / release / invalidate / reap

**Why:** The settle-at-buy write, terminal transitions, the collapse signal-back, and the crashed-claim reaper.

**Files:**
- Modify: `pkg/market/arbitrage_book.go`
- Test: `pkg/market/arbitrage_book_test.go`

**Interfaces:**
- Consumes: Task 4 types + tables.
- Produces:
  ```go
  func (c *Collector) SettleBookClaim(ctx context.Context, claimID int64, agentID string, boughtUnits float64) error
  func (c *Collector) CompleteBookClaim(ctx context.Context, claimID int64, agentID string) error
  func (c *Collector) ReleaseBookClaim(ctx context.Context, claimID int64, agentID string) error
  func (c *Collector) InvalidateBook(ctx context.Context, itemID, fromStation, agentID, reason string) error
  func (c *Collector) ReapExpiredBookClaims(ctx context.Context) (int, error)
  ```

- [ ] **Step 1: Write the failing test**

Add to `pkg/market/arbitrage_book_test.go`:

```go
func bookClaimPhase(t *testing.T, c *Collector, claimID int64) (string, float64) {
	t.Helper()
	var phase string
	var bought float64
	if err := c.db.QueryRow(
		`SELECT phase, bought_units FROM haul_book_claims WHERE claim_id=?`, claimID).Scan(&phase, &bought); err != nil {
		t.Fatalf("read claim %d: %v", claimID, err)
	}
	return phase, bought
}

func TestBookClaimLifecycle(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	book := BookKey{ItemID: "widget", FromStationID: "src"}
	id := seedAvailableOpp(t, c, "widget", "src", "dst_a", 900)
	r, err := c.AdmitBookClaim(ctx, book, []BookCandidate{{id, "dst_a"}}, "h1", 5, time.Hour)
	if err != nil || !r.OK {
		t.Fatalf("admit: %v ok=%v", err, r.OK)
	}
	if err := c.SettleBookClaim(ctx, r.ClaimID, "h1", 480); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if phase, bought := bookClaimPhase(t, c, r.ClaimID); phase != "bought" || bought != 480 {
		t.Fatalf("after settle: phase=%s bought=%v, want bought/480", phase, bought)
	}
	if err := c.CompleteBookClaim(ctx, r.ClaimID, "h1"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if phase, _ := bookClaimPhase(t, c, r.ClaimID); phase != "done" {
		t.Fatalf("after complete: phase=%s, want done", phase)
	}
}

func TestInvalidateBookExpiresAvailableAndOwnClaimedRows(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	// One available row + one row this agent has claimed + one another agent claimed.
	avail := seedAvailableOpp(t, c, "core", "starfall", "dst_a", 42)
	mine := seedAvailableOpp(t, c, "core", "starfall", "dst_b", 42)
	other := seedAvailableOpp(t, c, "core", "starfall", "dst_c", 42)
	_, _ = c.db.Exec(`UPDATE arbitrage_opportunities SET status='claimed', claimed_by='me' WHERE id=?`, mine)
	_, _ = c.db.Exec(`UPDATE arbitrage_opportunities SET status='claimed', claimed_by='other' WHERE id=?`, other)

	if err := c.InvalidateBook(ctx, "core", "starfall", "me", "no live ask"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	status := func(id int) string {
		var s string
		_ = c.db.QueryRow(`SELECT status FROM arbitrage_opportunities WHERE id=?`, id).Scan(&s)
		return s
	}
	if status(avail) != "expired" {
		t.Fatalf("available row not expired: %s", status(avail))
	}
	if status(mine) != "expired" {
		t.Fatalf("own claimed row not expired: %s", status(mine))
	}
	if status(other) != "claimed" {
		t.Fatalf("other agent's claimed row must NOT be yanked: %s", status(other))
	}
	// A fresh available fetch no longer offers this book.
	opps, _ := c.GetOpportunities(ctx, "available", 50)
	for _, o := range opps {
		if o.ItemID == "core" && o.FromStationID == "starfall" {
			t.Fatalf("collapsed book still offered: opp %d", o.ID)
		}
	}
}

func TestReapExpiredBookClaims(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	book := BookKey{ItemID: "widget", FromStationID: "src"}
	id := seedAvailableOpp(t, c, "widget", "src", "dst_a", 900)
	// Admit with an already-past TTL so it is immediately reapable.
	r, _ := c.AdmitBookClaim(ctx, book, []BookCandidate{{id, "dst_a"}}, "h1", 5, -time.Minute)
	n, err := c.ReapExpiredBookClaims(ctx)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 reaped, got %d", n)
	}
	if phase, _ := bookClaimPhase(t, c, r.ClaimID); phase != "released" {
		t.Fatalf("reaped claim phase=%s, want released", phase)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run 'TestBookClaimLifecycle|TestInvalidateBook|TestReapExpiredBookClaims' -v`
Expected: FAIL to compile (methods undefined).

- [ ] **Step 3: Implement**

Append to `pkg/market/arbitrage_book.go`:

```go
// SettleBookClaim records the actual bought quantity on a claim (settle-only: this is
// the "subtract actual amount used" write). Idempotent no-op if the claim is not in
// 'claimed' phase or not owned by agentID.
func (c *Collector) SettleBookClaim(ctx context.Context, claimID int64, agentID string, boughtUnits float64) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE haul_book_claims SET phase='bought', bought_units=?, updated_at=?
			 WHERE claim_id=? AND agent_id=? AND phase='claimed'`,
			boughtUnits, time.Now().UTC().Format(time.RFC3339), claimID, agentID)
		if err != nil {
			return fmt.Errorf("settle book claim: %w", err)
		}
		return nil
	})
}

// CompleteBookClaim marks a claim done after its sell leg completes, freeing its cap
// slot. Owner-scoped; no-op otherwise.
func (c *Collector) CompleteBookClaim(ctx context.Context, claimID int64, agentID string) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE haul_book_claims SET phase='done', updated_at=?
			 WHERE claim_id=? AND agent_id=? AND phase IN ('claimed','bought')`,
			time.Now().UTC().Format(time.RFC3339), claimID, agentID)
		if err != nil {
			return fmt.Errorf("complete book claim: %w", err)
		}
		return nil
	})
}

// ReleaseBookClaim returns a claim's cap slot on a pre-buy abandon. Owner-scoped.
func (c *Collector) ReleaseBookClaim(ctx context.Context, claimID int64, agentID string) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE haul_book_claims SET phase='released', updated_at=?
			 WHERE claim_id=? AND agent_id=? AND phase IN ('claimed','bought')`,
			time.Now().UTC().Format(time.RFC3339), claimID, agentID)
		if err != nil {
			return fmt.Errorf("release book claim: %w", err)
		}
		return nil
	})
}

// InvalidateBook clears a collapsed book so no further hauler is drawn in: it expires
// the book's still-available opportunity rows and the CALLING agent's own claimed row
// (so its abandon does not republish it), while leaving other agents' claimed rows
// untouched — they confirm-and-invalidate on their own arrival. Call only when a live
// recapture confirmed the source is gone.
func (c *Collector) InvalidateBook(ctx context.Context, itemID, fromStation, agentID, reason string) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		note := fmt.Sprintf("source collapsed: %s (by %s)", reason, agentID)
		_, err := tx.ExecContext(ctx,
			`UPDATE arbitrage_opportunities SET status='expired', notes=?
			 WHERE item_id=? AND from_station_id=?
			   AND (status='available' OR (status='claimed' AND claimed_by=?))`,
			note, itemID, fromStation, agentID)
		if err != nil {
			return fmt.Errorf("invalidate book opps: %w", err)
		}
		return nil
	})
}

// ReapExpiredBookClaims releases roster rows whose TTL passed while still active
// (e.g. a hauler killed between claim and settle). The cap count already ignores
// expired rows; this is cleanup so the roster does not grow unbounded. Returns the
// number reaped.
func (c *Collector) ReapExpiredBookClaims(ctx context.Context) (int, error) {
	n := 0
	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE haul_book_claims SET phase='released', updated_at=?
			 WHERE phase IN ('claimed','bought') AND julianday(expires_at) <= julianday('now')`,
			time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("reap book claims: %w", err)
		}
		if k, e := res.RowsAffected(); e == nil {
			n = int(k)
		}
		return nil
	})
	return n, err
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/market/ -run 'TestBookClaimLifecycle|TestInvalidateBook|TestReapExpiredBookClaims' -v && go test ./pkg/market/`
Expected: PASS (and full market suite green).

- [ ] **Step 5: Commit**

```bash
git add pkg/market/arbitrage_book.go pkg/market/arbitrage_book_test.go
git commit -m "feat(market): book-claim settle/complete/release + collapse invalidate + reaper

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Interface + book-aware claim in the hauler

**Why:** Route the hauler through book admission instead of the per-row `claimBest`, compute the cap from `source_units` and ship cargo, fix the status label, and extend the store interface + test fake.

**Files:**
- Modify: `pkg/worker/haul.go` (`OpportunityStore` interface ~376; new `bookCap`/`claimBestBook`; `haulActivityLabel` ~461; `Haul` claim site ~666-685; `runClaimedHaul` signature ~814 + resume caller ~592)
- Modify: `pkg/worker/haul_test.go` (extend `fakeStore`, update the `runClaimedHaul` call at :845 and any `claimBest` test)
- Test: `pkg/worker/haul_test.go`

**Interfaces:**
- Consumes: `market.AdmitBookClaim`, `market.BookKey`, `market.BookCandidate`, `market.AdmitResult`.
- Produces:
  ```go
  func bookCap(sourceUnits, cargoCap float64) int
  func claimBestBook(ctx context.Context, store OpportunityStore, ranked []market.ArbitrageOpportunity, agentID string, cargoCap float64) (opp market.ArbitrageOpportunity, claimID int64, ok bool, err error)
  func haulActivityLabel(opp market.ArbitrageOpportunity, cargoCap float64) string
  ```
  `runClaimedHaul` gains a trailing `bookClaimID int64` parameter.

- [ ] **Step 1: Write the failing test**

Add to `pkg/worker/haul_test.go`:

```go
func TestBookCap(t *testing.T) {
	cases := []struct {
		src, cargo float64
		want       int
	}{
		{42, 300, 1},
		{900, 300, 3},
		{4367, 480, 10},
		{0, 300, 1},
		{300, 0, 1},
	}
	for _, tc := range cases {
		if got := bookCap(tc.src, tc.cargo); got != tc.want {
			t.Errorf("bookCap(%v,%v)=%d want %d", tc.src, tc.cargo, got, tc.want)
		}
	}
}

func TestHaulActivityLabelCapsToShip(t *testing.T) {
	opp := market.ArbitrageOpportunity{ID: 921069, ItemName: "Steel Plate", SourceUnits: 4367,
		FromStationName: "Ironhearth Station", ToStationName: "Ramen's Rest"}
	got := haulActivityLabel(opp, 480)
	if !strings.Contains(got, "up to 480 of 4367") {
		t.Fatalf("label must cap to ship slice; got %q", got)
	}
	if strings.Contains(got, "4847") {
		t.Fatalf("label must not show raw impossible quantity: %q", got)
	}
}
```

Ensure `strings` is imported in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run 'TestBookCap|TestHaulActivityLabelCapsToShip' -v`
Expected: FAIL (`bookCap` undefined; `haulActivityLabel` signature mismatch).

- [ ] **Step 3: Implement — extend the store interface**

In `pkg/worker/haul.go`, add to the `OpportunityStore` interface (after `ScanArbitrage`):

```go
	AdmitBookClaim(ctx context.Context, book market.BookKey, candidates []market.BookCandidate, agentID string, capN int, expiresIn time.Duration) (market.AdmitResult, error)
	SettleBookClaim(ctx context.Context, claimID int64, agentID string, boughtUnits float64) error
	CompleteBookClaim(ctx context.Context, claimID int64, agentID string) error
	ReleaseBookClaim(ctx context.Context, claimID int64, agentID string) error
	InvalidateBook(ctx context.Context, itemID, fromStation, agentID, reason string) error
	ReapExpiredBookClaims(ctx context.Context) (int, error)
```

(The `var _ OpportunityStore = (*market.Collector)(nil)` check at haul.go:429 now also verifies these compile against the real collector.)

- [ ] **Step 4: Implement — cap, admission, label**

In `pkg/worker/haul.go`, add near `sizeBuy`:

```go
// haulBookClaimTTL bounds how long a book claim occupies a cap slot if the hauler
// never settles/releases it (crash mid-run); the reaper frees it after this.
const haulBookClaimTTL = 6 * time.Hour

// bookCap is the number of concurrent haulers a book can supply: one ship-load per
// claimant, ceil(source depth / ship cargo). Always >= 1.
func bookCap(sourceUnits, cargoCap float64) int {
	if cargoCap < 1 || sourceUnits <= 0 {
		return 1
	}
	return max(1, int(math.Ceil(sourceUnits/cargoCap)))
}

// bookKey groups ranked opportunities by their book (item + source station).
type bookKey struct{ item, from string }

// claimBestBook walks ranked opportunities book-by-book (best-first) and admits the
// hauler onto the first book with a free cap slot, letting AdmitBookClaim assign a
// fanned-out destination. Books already attempted this pass are skipped. ok=false means
// every reachable book was at capacity or fully claimed.
func claimBestBook(ctx context.Context, store OpportunityStore, ranked []market.ArbitrageOpportunity, agentID string, cargoCap float64) (market.ArbitrageOpportunity, int64, bool, error) {
	attempted := map[bookKey]bool{}
	for _, top := range ranked {
		bk := bookKey{top.ItemID, top.FromStationID}
		if attempted[bk] {
			continue
		}
		attempted[bk] = true

		var cands []market.BookCandidate
		var srcUnits float64
		for _, o := range ranked {
			if o.ItemID == bk.item && o.FromStationID == bk.from {
				cands = append(cands, market.BookCandidate{OppID: o.ID, ToStationID: o.ToStationID})
				srcUnits = o.SourceUnits // identical across a book's rows
			}
		}
		capN := bookCap(srcUnits, cargoCap)
		res, err := store.AdmitBookClaim(ctx, market.BookKey{ItemID: bk.item, FromStationID: bk.from}, cands, agentID, capN, haulBookClaimTTL)
		if err != nil {
			return market.ArbitrageOpportunity{}, 0, false, fmt.Errorf("haul: admit book %s/%s: %w", bk.item, bk.from, err)
		}
		if !res.OK {
			continue // book full or all rows claimed; try the next book
		}
		for _, o := range ranked {
			if o.ID == res.OppID {
				return o, res.ClaimID, true, nil
			}
		}
	}
	return market.ArbitrageOpportunity{}, 0, false, nil
}
```

Replace `haulActivityLabel` (haul.go:461) with the ship-capped form:

```go
// haulActivityLabel renders a claimed opportunity as the status-page activity line,
// capping the shown quantity to what the ship will actually attempt (min of ship cargo
// and book depth) alongside the book's total source depth, so it never shows a
// physically impossible order-book quantity.
func haulActivityLabel(opp market.ArbitrageOpportunity, cargoCap float64) string {
	item := opp.ItemName
	if item == "" {
		item = opp.ItemID
	}
	from := opp.FromStationName
	if from == "" {
		from = opp.FromStationID
	}
	to := opp.ToStationName
	if to == "" {
		to = opp.ToStationID
	}
	slice := opp.SourceUnits
	if cargoCap > 0 && cargoCap < slice {
		slice = cargoCap
	}
	return fmt.Sprintf("Opportunity #%d · buying up to %.0f of %.0f %s · %s → %s",
		opp.ID, slice, opp.SourceUnits, item, from, to)
}
```

Ensure `math` and `time` are imported in `haul.go` (both are already used elsewhere in the package; add to `haul.go`'s import block if not present there).

- [ ] **Step 5: Implement — wire the claim site**

In `pkg/worker/haul.go` `Haul`, replace the `claimBest` block (lines ~666-685) with:

```go
	cargoCap := 0.0
	if st := deps.Client.GetState(); st != nil {
		cargoCap = st.Ship.CargoCapacity
	}
	opp, bookClaimID, ok, err := claimBestBook(ctx, deps.Market, ranked, deps.AgentID, cargoCap)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "haul: all candidate books at capacity; idling") //nolint:errcheck
		return nil
	}

	publishActivity(deps.SetActivity, haulActivityLabel(opp, cargoCap))

	m := &haulMetrics{claimedAt: haulNow(deps), claimedTick: haulTick(deps)}
	if buySys := nameToID[opp.FromSystemName]; buySys != "" {
		if sellSys := nameToID[opp.ToSystemName]; sellSys != "" {
			toBuy := navigation.BFSJumps(graph, current, []string{buySys})
			toSell := navigation.BFSJumps(graph, buySys, []string{sellSys})
			m.jumps = toBuy[buySys] + toSell[sellSys]
		}
	}
	return runClaimedHaul(ctx, deps, out, opp, nameToID, m, fuel, bookClaimID)
```

- [ ] **Step 6: Implement — thread `bookClaimID` through `runClaimedHaul`**

Change the signature (haul.go:814):

```go
func runClaimedHaul(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, nameToID map[string]string, m *haulMetrics, fuel haulFuel, bookClaimID int64) error {
```

Update the resume caller at haul.go:592 to pass `0` (a resumed haul's book claim, if any, belongs to a prior process and is not settled again here):

```go
		return runClaimedHaul(ctx, deps, out, held[0], nameToID, nil, fuel, 0)
```

(The buy/settle + invalidate use of `bookClaimID` is added in Task 7. For this task it is threaded through unused except by the signature.)

- [ ] **Step 7: Implement — extend the test fake + fix callers**

In `pkg/worker/haul_test.go`, add fields to `fakeStore`:

```go
	admitOK      bool                    // AdmitBookClaim returns this ok
	admitResult  market.AdmitResult      // returned when admitOK (ClaimID/OppID/ToStationID)
	settled      []float64               // bought_units passed to SettleBookClaim
	bookClaimsCompleted []int64          // claimIDs completed
	bookClaimsReleased  []int64          // claimIDs released
	invalidated  []string                // "item/from" strings passed to InvalidateBook
```

Add methods:

```go
func (f *fakeStore) AdmitBookClaim(_ context.Context, _ market.BookKey, cands []market.BookCandidate, _ string, _ int, _ time.Duration) (market.AdmitResult, error) {
	if !f.admitOK {
		return market.AdmitResult{}, nil
	}
	r := f.admitResult
	if r.OppID == 0 && len(cands) > 0 { // default: admit the first candidate
		r = market.AdmitResult{OK: true, ClaimID: 1, OppID: cands[0].OppID, ToStationID: cands[0].ToStationID}
	}
	r.OK = true
	return r, nil
}
func (f *fakeStore) SettleBookClaim(_ context.Context, _ int64, _ string, boughtUnits float64) error {
	f.settled = append(f.settled, boughtUnits)
	return nil
}
func (f *fakeStore) CompleteBookClaim(_ context.Context, claimID int64, _ string) error {
	f.bookClaimsCompleted = append(f.bookClaimsCompleted, claimID)
	return nil
}
func (f *fakeStore) ReleaseBookClaim(_ context.Context, claimID int64, _ string) error {
	f.bookClaimsReleased = append(f.bookClaimsReleased, claimID)
	return nil
}
func (f *fakeStore) InvalidateBook(_ context.Context, itemID, fromStation, _, _ string) error {
	f.invalidated = append(f.invalidated, itemID+"/"+fromStation)
	return nil
}
func (f *fakeStore) ReapExpiredBookClaims(_ context.Context) (int, error) { return 0, nil }
```

Ensure `time` is imported in the test file. Update the direct `runClaimedHaul` call at haul_test.go:845 to pass a trailing `0`:

```go
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, n2id, nil, haulFuel{}, 0); err != nil {
```

If an existing test drives the claim path through `claimBest` and asserts on `f.claims`, update it to set `f.admitOK = true` (and, if it needs a specific admitted row, `f.admitResult`) and assert via the new book path. Keep `ClaimOpportunity`/`ReleaseOpportunity`/`CompleteOpportunity` on the fake — the real collector still exposes them and Task 7 keeps the per-row release for my-problem abandons.

- [ ] **Step 8: Run tests**

Run: `go build ./... && go test ./pkg/worker/ ./pkg/market/`
Expected: PASS. (`go build ./...` also compiles every other `OpportunityStore` implementer, if any, so a missing method surfaces here.)

- [ ] **Step 9: Commit**

```bash
git add pkg/worker/haul.go pkg/worker/haul_test.go
git commit -m "feat(haul): book-aware admission (cap + fan-out) replacing per-row claim; ship-capped label

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Settle at buy, complete on sell, invalidate on collapse

**Why:** Record the actual bought quantity (the settle write), free the cap slot when the haul completes, and — on a collapse-confirmed pre-buy abandon — invalidate the book instead of republishing the row.

**Files:**
- Modify: `pkg/worker/haul.go` (`runClaimedHaul` buy leg ~870-887; the collapse-abandon branches; `haulSellLeg` completion ~962/985; a new `abandonCollapsed` helper)
- Test: `pkg/worker/haul_test.go`

**Interfaces:**
- Consumes: `SettleBookClaim`, `CompleteBookClaim`, `ReleaseBookClaim`, `InvalidateBook` (Task 5/6); `bookClaimID` param (Task 6).
- Produces: `abandonCollapsed(ctx, deps, out, opp, bookClaimID, reason)` — expires the book + releases the book claim + releases the per-row claim.

- [ ] **Step 1: Write the failing test**

These drive the full buy leg (not the resume path). Model the fakeClient on `TestRunClaimedHaulResumesWithGoodsAboard` (haul_test.go:832) but with an **empty** cargo so it proceeds to buy. Build the opportunity **inline** (not via `opp()`, which sets `FromStationID == ToStationID` when both systems match) with two *distinct* stations in the **same** system `"a"`, so autopilot transit is a no-op but the buy/sell legs still run. Seed `f.prices` so `haulGate` passes: a sell (ask) at the buy station and a buy (bid) at the sell station with a wide spread. Match `game.State`/`game.Ship` field names to the resume test's fixture (haul_test.go:834-842) — adjust `Credits`/`CargoCapacity` field placement if they differ.

```go
func buyLegOpp() market.ArbitrageOpportunity {
	return market.ArbitrageOpportunity{
		ID: 7, FromSystemName: "a", ToSystemName: "a",
		FromStationID: "buy-stn", ToStationID: "sell-stn",
		ItemID: "iron_ore", GrossProfit: 9000, Quantity: 10, SourceUnits: 400, BuyPrice: 5,
	}
}

func TestRunClaimedHaulSettlesBoughtQuantity(t *testing.T) {
	o := buyLegOpp()
	fc := &fakeClient{
		state: &game.State{
			System:  game.SystemData{ID: "a", Name: "A"},
			Fuel:    100, MaxFuel: 100, Credits: 1_000_000,
			Ship:    game.Ship{CargoCapacity: 500}, // empty cargo, room to buy
		},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}},
	}
	f := &fakeStore{
		admitOK: true,
		prices: []market.ItemStationPrice{
			{StationID: "buy-stn", HasSell: true, BestAsk: 10, AskQty: 400},
			{StationID: "sell-stn", HasBuy: true, BestBid: 100, BidQty: 400},
		},
	}
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, n2id, nil, haulFuel{}, 7); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(f.settled) != 1 {
		t.Fatalf("expected one settle write after buy, got %d", len(f.settled))
	}
}

func TestRunClaimedHaulInvalidatesBookOnCollapse(t *testing.T) {
	o := buyLegOpp()
	fc := &fakeClient{
		state: &game.State{
			System:  game.SystemData{ID: "a", Name: "A"},
			Fuel:    100, MaxFuel: 100, Credits: 1_000_000,
			Ship:    game.Ship{CargoCapacity: 500},
		},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}},
	}
	// No sell side at the buy station -> haulGate returns "no live ask at buy station"
	// (collapse-confirmed after the live recapture, which is nil here).
	f := &fakeStore{
		admitOK: true,
		prices:  []market.ItemStationPrice{{StationID: "sell-stn", HasBuy: true, BestBid: 100, BidQty: 400}},
	}
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, n2id, nil, haulFuel{}, 7); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(f.invalidated) != 1 {
		t.Fatalf("collapse must invalidate the book, got %d invalidations", len(f.invalidated))
	}
	if len(f.bookClaimsReleased) == 0 {
		t.Fatalf("collapse abandon must still release the book claim slot")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run 'TestRunClaimedHaulSettles|TestRunClaimedHaulInvalidates' -v`
Expected: FAIL (no settle write; no invalidation — collapse currently calls `abandonClaim`, which republishes).

- [ ] **Step 3: Implement — collapse-aware abandon helper**

In `pkg/worker/haul.go`, add near `abandonClaim` (haul.go:762):

```go
// abandonCollapsed handles a pre-buy abandon caused by the source book being gone at
// arrival (confirmed by the live recapture): it invalidates the book so no further
// hauler is drawn in (expiring the book's available rows and this hauler's own claimed
// row), releases this hauler's book-claim cap slot, and logs. Unlike abandonClaim it
// does NOT return the row to the available pool.
func abandonCollapsed(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, bookClaimID int64, reason string) error {
	if err := deps.Market.InvalidateBook(ctx, opp.ItemID, opp.FromStationID, deps.AgentID, reason); err != nil {
		fmt.Fprintf(out, "haul: opp %d collapse (%s); invalidate failed: %v\n", opp.ID, reason, err) //nolint:errcheck
	}
	if bookClaimID > 0 {
		if err := deps.Market.ReleaseBookClaim(ctx, bookClaimID, deps.AgentID); err != nil {
			fmt.Fprintf(out, "haul: opp %d release book claim failed: %v\n", opp.ID, err) //nolint:errcheck
		}
	}
	fmt.Fprintf(out, "haul: opp %d source collapsed (%s); book invalidated\n", opp.ID, reason) //nolint:errcheck
	return nil
}

// releaseBookAnd releases the hauler's book-claim slot (best-effort) then delegates to
// abandonClaim for a "my problem" pre-buy abandon (unroutable/dock/credits), which
// returns the per-row claim to the pool for other haulers.
func releaseBookAnd(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, bookClaimID int64, reason string) error {
	if bookClaimID > 0 {
		if err := deps.Market.ReleaseBookClaim(ctx, bookClaimID, deps.AgentID); err != nil {
			fmt.Fprintf(out, "haul: opp %d release book claim failed: %v\n", opp.ID, err) //nolint:errcheck
		}
	}
	return abandonClaim(ctx, deps, out, opp, reason)
}
```

- [ ] **Step 4: Implement — route abandons by reason + settle after buy**

In `runClaimedHaul`, the pre-buy abandons split by cause. Replace the relevant `abandonClaim(...)` calls in `runClaimedHaul` (buy-system/sell-system unresolved, transit failed, dock failed, no state, price-check failed) with `releaseBookAnd(ctx, deps, out, opp, bookClaimID, ...)` — these are the hauler's problem, so the row returns to the pool.

Then replace the gate-fail branch (haul.go:871-873) with a collapse-vs-thin split:

```go
	qty, liveAsk, sellBid, pass, reason := haulGate(opp, prices, cargoFree, state.Ship.CargoCapacity, state.GetCredits(), haulLegCost)
	if !pass {
		// A missing live ask / vanished spread means the source book is gone — invalidate
		// it so no other hauler is drawn in. An affordability/cargo reason is this
		// hauler's problem, so return the row to the pool for others.
		if strings.HasPrefix(reason, "no live ask") || strings.HasPrefix(reason, "spread too thin") {
			return abandonCollapsed(ctx, deps, out, opp, bookClaimID, reason)
		}
		return releaseBookAnd(ctx, deps, out, opp, bookClaimID, reason)
	}
	if err := deps.Client.Buy(ctx, opp.ItemID, qty); err != nil {
		return releaseBookAnd(ctx, deps, out, opp, bookClaimID, fmt.Sprintf("buy failed: %v", err))
	}
	// Settle: record the actual bought quantity so other haulers see the reduced book
	// remainder (source_units - SUM(bought_units)).
	if bookClaimID > 0 {
		if serr := deps.Market.SettleBookClaim(ctx, bookClaimID, deps.AgentID, qty); serr != nil {
			fmt.Fprintf(out, "haul: opp %d settle book claim failed: %v\n", opp.ID, serr) //nolint:errcheck
		}
	}
	if m != nil {
		m.boughtAt, m.boughtTick = haulNow(deps), haulTick(deps)
		m.buyPrice, m.sellPrice, m.qty = liveAsk, sellBid, qty
		if px, ok := actualUnitPrice(deps.Client.GetRawJSON("buy"), "total_cost", "quantity"); ok {
			m.buyPrice = px
		}
	}

	return haulSellLeg(ctx, deps, out, opp, sellSys, m, bookClaimID)
```

- [ ] **Step 5: Implement — complete the book claim on sell**

Change `haulSellLeg`'s signature to accept `bookClaimID int64` (add as the trailing parameter), update its recursive/normal callers, and after each successful `CompleteOpportunity` call (haul.go:962 and :985) add:

```go
		if bookClaimID > 0 {
			if err := deps.Market.CompleteBookClaim(ctx, bookClaimID, deps.AgentID); err != nil {
				fmt.Fprintf(out, "haul: opp %d complete book claim failed: %v\n", opp.ID, err) //nolint:errcheck
			}
		}
```

Update the resume caller of `haulSellLeg` (haul.go:828, the goods-aboard resume) to pass `0`:

```go
		return haulSellLeg(ctx, deps, out, opp, sellSys, nil, 0)
```

Also update every direct `haulSellLeg(...)` caller in `pkg/worker/haul_test.go` to pass a trailing `0` — there are several isolated sell-leg tests (`TestHaulSellLegReroutesOnThinDemandMidRoute`, `...PostsCostOrderOnThinDemand`, `...PostsCostOrderOnEmptyButCapturedBook`, `...SellsWhenNoCaptureData`, `...SellsOnHealthyDemand`, `TestHaulSellLegRecordsResult`, `...RecordsActualSellFill`, `...NilMetricsRecordsNothing`). `go build ./...` / the test compile will flag any missed caller.

Confirm `strings` is imported in `haul.go` (it is — used at :635).

- [ ] **Step 6: Run tests**

Run: `go build ./... && go test ./pkg/worker/ ./pkg/market/`
Expected: PASS (settle write recorded on the happy path; book invalidated + per-row released on collapse).

- [ ] **Step 7: Commit**

```bash
git add pkg/worker/haul.go pkg/worker/haul_test.go
git commit -m "feat(haul): settle bought qty at buy, complete on sell, invalidate collapsed book

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Reaper hook + full verification

**Why:** Run the crashed-claim reaper once per haul pass (cheap, best-effort) so stale cap slots free automatically, then verify the whole feature builds, tests, and lints clean.

**Files:**
- Modify: `pkg/worker/haul.go` (`loadAvailable` ~393, best-effort reap)
- Test: `pkg/worker/haul_test.go` (assert reap is invoked)

**Interfaces:**
- Consumes: `ReapExpiredBookClaims` (Task 5/6).
- Produces: no new exported API.

- [ ] **Step 1: Write the failing test**

Add a counter to `fakeStore` and assert it is called. First extend the fake's `ReapExpiredBookClaims` (from Task 6) to count:

```go
	reaped int
```

Change the method body to `f.reaped++; return 0, nil`. Then:

```go
func TestLoadAvailableReapsExpiredBookClaims(t *testing.T) {
	f := &fakeStore{available: []market.ArbitrageOpportunity{{ID: 1, ItemID: "x", FromStationID: "s", ToStationID: "d", SourceUnits: 10}}}
	if _, err := loadAvailable(context.Background(), f, 50); err != nil {
		t.Fatalf("loadAvailable: %v", err)
	}
	if f.reaped != 1 {
		t.Fatalf("loadAvailable must reap expired book claims once per pass, got %d", f.reaped)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run TestLoadAvailableReapsExpiredBookClaims -v`
Expected: FAIL (`reaped` == 0).

- [ ] **Step 3: Implement**

In `pkg/worker/haul.go` `loadAvailable`, add a best-effort reap at the top of the function (before the first `GetOpportunities`):

```go
func loadAvailable(ctx context.Context, store OpportunityStore, limit int) ([]market.ArbitrageOpportunity, error) {
	// Free cap slots held by book claims whose hauler died mid-run (best-effort; the
	// cap count already ignores expired rows, so a reap failure is non-fatal).
	if _, err := store.ReapExpiredBookClaims(ctx); err != nil {
		_ = err // non-fatal: stale rows still age out of the cap count via expires_at
	}
	opps, err := store.GetOpportunities(ctx, "available", limit)
	// ... unchanged remainder ...
```

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/worker/ -run TestLoadAvailableReapsExpiredBookClaims -v`
Expected: PASS.

- [ ] **Step 5: Full gate**

Run:
```bash
go build ./...
go test ./...
golangci-lint run ./pkg/market/... ./pkg/worker/...
```
Expected: build OK; all tests pass; golangci-lint reports 0 new findings.

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/haul.go pkg/worker/haul_test.go
git commit -m "feat(haul): reap expired book claims once per pass

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Post-implementation verification (live)

After all tasks merge and workers are rebuilt (`bin/worker`), confirm on a live fleet:

1. The status page no longer shows impossible quantities — a fat book renders `buying up to <cargo> of <sourceUnits>`.
2. Query `market.db`: for a thin book (e.g. a 42-unit source), `SELECT COUNT(*) FROM haul_book_claims WHERE item_id=? AND from_station_id=? AND phase IN ('claimed','bought')` stays `<= ceil(42/cargo)` (≈1), not a dozen.
3. A fat book (e.g. Steel Plate from Ironhearth) shows multiple active claimants across *distinct* `to_station_id`s.
4. After a source collapses, its `arbitrage_opportunities` rows go `status='expired'` and are not re-served until a later scan on returned supply.

## Future work (out of scope — see design §Future work)

- Destination reroute mid-flight when the sell-side bid is eaten before arrival.
- Demand-side reservation across our own haulers.
- Per-unit item volume in `sizeBuy`.
