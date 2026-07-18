package market

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// wantTables are the tables that runMigrations must create.
var wantTables = []string{
	"items",
	"stations",
	"market_orders",
	"market_ohlcv",
	"arbitrage_opportunities",
}

func TestRunMigrations_CreatesAllTables(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	got, err := tableNames(db)
	if err != nil {
		t.Fatalf("tableNames: %v", err)
	}
	for _, want := range wantTables {
		if _, ok := got[want]; !ok {
			t.Errorf("missing table %q; got %v", want, got)
		}
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	for range 2 {
		if err := runMigrations(db); err != nil {
			t.Fatalf("runMigrations: %v", err)
		}
	}
}

func TestRunMigrations_IndexesAndChecks(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	// CHECK constraint on side must reject bad values.
	_, execErr := db.Exec(`INSERT INTO market_orders (station_id, item_id, side, price_each, quantity, captured_at, bucket_utc)
		VALUES ('s1','i1','invalid',1.0,1.0,'t','b')`)
	if execErr == nil {
		t.Fatal("expected CHECK constraint to reject side='invalid'")
	} else if !strings.Contains(execErr.Error(), "CHECK") && !strings.Contains(strings.ToLower(execErr.Error()), "constraint") {
		t.Errorf("unexpected error type for CHECK violation: %v", execErr)
	}

	// OHLCV composite primary key must enforce uniqueness.
	ins := `INSERT INTO market_ohlcv (station_id, item_id, side, bucket_utc, open_price, high_price, low_price, close_price, volume, trade_count, vwap)
		VALUES ('s1','i1','buy','b1',1,1,1,1,1,1,1)`
	if _, err := db.Exec(ins); err != nil {
		t.Fatalf("first ohlcv insert: %v", err)
	}
	if _, err := db.Exec(ins); err == nil {
		t.Fatal("expected duplicate PK error on second OHLCV insert")
	}

	// Indexes should exist.
	idx := []string{"idx_orders_station_item", "idx_orders_item_time", "idx_orders_bucket", "idx_arbitrage_status", "idx_arbitrage_item"}
	got, err := indexNames(db)
	if err != nil {
		t.Fatalf("indexNames: %v", err)
	}
	for _, w := range idx {
		if _, ok := got[w]; !ok {
			t.Errorf("missing index %q; got %v", w, got)
		}
	}
}

func tableNames(db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]struct{})
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = struct{}{}
	}
	return out, rows.Err()
}

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

func indexNames(db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='index'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]struct{})
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = struct{}{}
	}
	return out, rows.Err()
}
