package market

import (
	"context"
	"path/filepath"
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

// newConcurrentTestCollector opens a real file-backed collector with WAL and a
// multi-connection pool, so a concurrency test actually exercises SQLite's
// _txlock=immediate locking across goroutines/connections instead of being
// serialized by database/sql's own single-connection queuing (which is what
// newTestCollector's MaxOpenConns:1 setup would do).
func newConcurrentTestCollector(t *testing.T) *Collector {
	t.Helper()
	collector, err := Open(Config{
		DBPath:       filepath.Join(t.TempDir(), "m.db"),
		WAL:          true,
		MaxOpenConns: 8,
		MaxIdleConns: 8,
		BusyTimeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() {
		if cerr := collector.Close(); cerr != nil {
			t.Errorf("Close failed: %v", cerr)
		}
	})
	return collector
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
	c := newConcurrentTestCollector(t)
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
