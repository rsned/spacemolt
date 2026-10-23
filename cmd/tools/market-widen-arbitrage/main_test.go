package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// oldTable mirrors the live pre-consignment table: the narrow CHECK, columns
// added later by ALTER TABLE, AUTOINCREMENT ids and the two indexes.
const oldTable = `CREATE TABLE arbitrage_opportunities (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    from_station_id     TEXT NOT NULL,
    item_id             TEXT NOT NULL,
    action_type         TEXT NOT NULL CHECK (action_type IN ('buy_then_sell', 'sell_then_buy')),
    status              TEXT DEFAULT 'available' CHECK (status IN ('available', 'claimed', 'completed', 'expired')),
    FOREIGN KEY (item_id) REFERENCES items(item_id)
)`

func openOld(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "market.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, s := range []string{
		oldTable,
		`ALTER TABLE arbitrage_opportunities ADD COLUMN cycles_seen INTEGER DEFAULT 1`,
		`CREATE INDEX idx_arbitrage_status ON arbitrage_opportunities(status)`,
		`CREATE INDEX idx_arbitrage_item ON arbitrage_opportunities(item_id, status)`,
		`INSERT INTO arbitrage_opportunities (id, from_station_id, item_id, action_type, status, cycles_seen)
		 VALUES (5, 'a', 'ore', 'buy_then_sell', 'expired', 3), (900, 'b', 'ingot', 'buy_then_sell', 'claimed', 1)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed %.40q: %v", s, err)
		}
	}
	return db
}

func TestWidenRebuildsPreservingRowsIndexesAndSequence(t *testing.T) {
	ctx := context.Background()
	db := openOld(t)

	dry, derr := widen(ctx, db, true)
	if derr != nil || !strings.HasPrefix(dry, "would rebuild") {
		t.Fatalf("dry run = %q, %v", dry, derr)
	}
	if _, err := db.Exec(`INSERT INTO arbitrage_opportunities (from_station_id, item_id, action_type) VALUES ('v','ore','withdraw_then_sell')`); err == nil {
		t.Fatal("the dry run changed the table: withdraw_then_sell was accepted")
	}

	res, werr := widen(ctx, db, false)
	if werr != nil || !strings.HasPrefix(res, "rebuilt") {
		t.Fatalf("widen = %q, %v", res, werr)
	}

	var n int
	var cycles int
	if err := db.QueryRow(`SELECT COUNT(*), SUM(cycles_seen) FROM arbitrage_opportunities WHERE id IN (5, 900)`).Scan(&n, &cycles); err != nil || n != 2 || cycles != 4 {
		t.Fatalf("rows after rebuild: n=%d cycles=%d err=%v, want both rows with their data", n, cycles, err)
	}
	var idx int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND tbl_name='arbitrage_opportunities' AND name LIKE 'idx_arbitrage_%'`).Scan(&idx); err != nil || idx != 2 {
		t.Fatalf("indexes after rebuild = %d, %v; want 2", idx, err)
	}
	var leftovers int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='arbitrage_opportunities_new'`).Scan(&leftovers); err != nil || leftovers != 0 {
		t.Fatalf("temporary table left behind: %d, %v", leftovers, err)
	}

	// The new type is admitted and AUTOINCREMENT carries on past the old max.
	r, ierr := db.Exec(`INSERT INTO arbitrage_opportunities (from_station_id, item_id, action_type) VALUES ('v','ore','withdraw_then_sell')`)
	if ierr != nil {
		t.Fatalf("withdraw_then_sell rejected after rebuild: %v", ierr)
	}
	if id, _ := r.LastInsertId(); id <= 900 {
		t.Fatalf("new id = %d, want > 900 (sequence carried across the rename)", id)
	}
	// The other CHECK survives.
	if _, err := db.Exec(`INSERT INTO arbitrage_opportunities (from_station_id, item_id, action_type, status) VALUES ('v','ore','buy_then_sell','bogus')`); err == nil {
		t.Fatal("status CHECK lost in the rebuild")
	}

	again, aerr := widen(ctx, db, false)
	if aerr != nil || !strings.HasPrefix(again, "already migrated") {
		t.Fatalf("second run = %q, %v; want already migrated", again, aerr)
	}
}

func TestWidenRefusesUnexpectedSchema(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "market.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close() //nolint:errcheck
	if _, err := db.Exec(`CREATE TABLE arbitrage_opportunities (id INTEGER PRIMARY KEY, action_type TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := widen(context.Background(), db, false); err == nil {
		t.Fatal("rebuilt a table whose CHECK it did not recognise")
	}
}
