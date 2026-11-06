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

func TestGetActiveBookClaim(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	book := BookKey{ItemID: "widget", FromStationID: "src"}
	id := seedAvailableOpp(t, c, "widget", "src", "dst_a", 900)
	r, err := c.AdmitBookClaim(ctx, book, []BookCandidate{{id, "dst_a"}}, "h1", 5, time.Hour)
	if err != nil || !r.OK {
		t.Fatalf("admit: %v ok=%v", err, r.OK)
	}

	// The owning agent recovers its active claim id.
	got, ok, err := c.GetActiveBookClaim(ctx, "widget", "src", "h1")
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if !ok || got != r.ClaimID {
		t.Fatalf("want (%d,true), got (%d,%v)", r.ClaimID, got, ok)
	}

	// A different agent has no active claim on this book.
	if got, ok, err := c.GetActiveBookClaim(ctx, "widget", "src", "h2"); err != nil || ok || got != 0 {
		t.Fatalf("other agent: want (0,false,nil), got (%d,%v,%v)", got, ok, err)
	}
	// A different book has none for h1.
	if got, ok, err := c.GetActiveBookClaim(ctx, "widget", "elsewhere", "h1"); err != nil || ok || got != 0 {
		t.Fatalf("other book: want (0,false,nil), got (%d,%v,%v)", got, ok, err)
	}

	// After completion the claim is 'done' — no longer active.
	if err := c.CompleteBookClaim(ctx, r.ClaimID, "h1"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got, ok, err := c.GetActiveBookClaim(ctx, "widget", "src", "h1"); err != nil || ok || got != 0 {
		t.Fatalf("after complete: want (0,false,nil), got (%d,%v,%v)", got, ok, err)
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

func seedConsignRoute(t *testing.T, c *Collector, to string, qty float64) int {
	t.Helper()
	id := seedAvailableOpp(t, c, "adamantite_ore", "voss_redoubt", to, qty)
	if _, err := c.db.Exec(`UPDATE arbitrage_opportunities SET action_type=?, notes=?, sell_price=1000 WHERE id=?`,
		ActionWithdrawThenSell, consignNotes("marketbot_alhena", "voss_redoubt_station"), id); err != nil {
		t.Fatalf("mark consigned: %v", err)
	}
	return id
}

// 2026-11-05: 2,144 units were claimed against about 1,700 in stock, because a
// claim only counted haulers. Admission now reserves units, whatever the hold.
func TestAdmitConsignClaim_ReservesUnits(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	if err := c.UpsertConsignStock(ctx, alhenaStock(1200, time.Now().UTC().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	a := seedConsignRoute(t, c, "dst_a", 1144)
	b := seedConsignRoute(t, c, "dst_b", 1000)
	d := seedConsignRoute(t, c, "dst_c", 700)
	book := BookKey{"adamantite_ore", "voss_redoubt"}
	cands := []BookCandidate{{a, "dst_a"}, {b, "dst_b"}, {d, "dst_c"}}

	r1, err := c.AdmitConsignClaim(ctx, book, cands, "h1", 1900, time.Hour)
	if err != nil || !r1.OK || r1.Units != 1144 {
		t.Fatalf("h1: %+v %v, want 1144 reserved", r1, err)
	}
	r2, err := c.AdmitConsignClaim(ctx, book, cands, "h2", 100, time.Hour)
	if err != nil || !r2.OK || r2.Units != 56 {
		t.Fatalf("h2: %+v %v, want the 56 left", r2, err)
	}
	r3, err := c.AdmitConsignClaim(ctx, book, cands, "h3", 1900, time.Hour)
	if err != nil || r3.OK {
		t.Fatalf("h3: %+v %v, want no stock left to claim", r3, err)
	}
}

func TestAdmitConsignClaim_RowCarriesReservedQuantity(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	if err := c.UpsertConsignStock(ctx, alhenaStock(500, time.Now().UTC().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	id := seedConsignRoute(t, c, "dst_a", 500)
	r, err := c.AdmitConsignClaim(ctx, BookKey{"adamantite_ore", "voss_redoubt"}, []BookCandidate{{id, "dst_a"}}, "h1", 100, time.Hour)
	if err != nil || !r.OK {
		t.Fatalf("admit: %+v %v", r, err)
	}
	var qty, gross float64
	if err := c.db.QueryRow(`SELECT quantity, gross_profit FROM arbitrage_opportunities WHERE id=?`, id).Scan(&qty, &gross); err != nil {
		t.Fatal(err)
	}
	if qty != 100 || gross != 100000 {
		t.Fatalf("row quantity %.0f gross %.0f, want 100 and 100000", qty, gross)
	}
}

// A remainder too small to be worth the trip is left for the next report.
func TestAdmitConsignClaim_RefusesATrivialRemainder(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	if err := c.UpsertConsignStock(ctx, alhenaStock(3, time.Now().UTC().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	id := seedConsignRoute(t, c, "dst_a", 3)
	r, err := c.AdmitConsignClaim(ctx, BookKey{"adamantite_ore", "voss_redoubt"}, []BookCandidate{{id, "dst_a"}}, "h1", 1900, time.Hour)
	if err != nil || r.OK {
		t.Fatalf("got %+v %v, want 3,000 of value refused", r, err)
	}
}

func TestAdmitConsignClaim_StaleStockAdmitsNothing(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	if err := c.UpsertConsignStock(ctx, alhenaStock(500, time.Now().UTC().Add(-ConsignStockMaxAge-time.Minute))); err != nil {
		t.Fatal(err)
	}
	id := seedConsignRoute(t, c, "dst_a", 500)
	r, err := c.AdmitConsignClaim(ctx, BookKey{"adamantite_ore", "voss_redoubt"}, []BookCandidate{{id, "dst_a"}}, "h1", 100, time.Hour)
	if err != nil || r.OK {
		t.Fatalf("got %+v %v, want stale stock refused", r, err)
	}
}

// Units already reflected in a report are not subtracted twice: a claim from
// before the report no longer counts against it.
func TestAdmitConsignClaim_OlderClaimAlreadyInReport(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	if err := c.UpsertConsignStock(ctx, alhenaStock(500, time.Now().UTC().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	book := BookKey{"adamantite_ore", "voss_redoubt"}
	a := seedConsignRoute(t, c, "dst_a", 400)
	r1, err := c.AdmitConsignClaim(ctx, book, []BookCandidate{{a, "dst_a"}}, "h1", 400, time.Hour)
	if err != nil || !r1.OK {
		t.Fatalf("h1: %+v %v", r1, err)
	}
	if uerr := c.UpsertConsignStock(ctx, alhenaStock(100, time.Now().UTC().Add(time.Minute))); uerr != nil {
		t.Fatal(uerr)
	}
	b := seedConsignRoute(t, c, "dst_b", 300)
	r2, err := c.AdmitConsignClaim(ctx, book, []BookCandidate{{b, "dst_b"}}, "h2", 300, time.Hour)
	if err != nil || !r2.OK || r2.Units != 100 {
		t.Fatalf("h2: %+v %v, want the 100 in the new report", r2, err)
	}
}
