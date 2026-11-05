package market

import (
	"path/filepath"
	"testing"
)

// idx_orders_buy_station_item_cap cost a write on every buy order (most of the
// table) to serve one operator command, the unfiltered `play_as demand` report,
// which runs in 3-5 s without it on a 4h-retained table (1.3 s with it). It was
// also the index that corrupted on 2026-11-04 and stalled every prune. A fresh
// database must never get it, and an existing one must lose it on open.
func TestMigrations_DropBuySideIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	c, err := Open(Config{DBPath: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if indexExists(t, c, "idx_orders_buy_station_item_cap") {
		t.Fatal("fresh database created the buy-side index")
	}
	// An older database still carries it.
	if _, err = c.db.Exec(`CREATE INDEX idx_orders_buy_station_item_cap ON market_orders(station_id, item_id, captured_at) WHERE side='buy'`); err != nil {
		t.Fatalf("seed old index: %v", err)
	}
	_ = c.Close()

	c, err = Open(Config{DBPath: path})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = c.Close() }()
	if indexExists(t, c, "idx_orders_buy_station_item_cap") {
		t.Fatal("migrations kept the buy-side index on an existing database")
	}
	// The indexes the hot paths need are untouched.
	for _, keep := range []string{"idx_orders_item_side_station_time", "idx_orders_station_item", "idx_orders_bucket", "idx_orders_item_time"} {
		if !indexExists(t, c, keep) {
			t.Errorf("index %s missing", keep)
		}
	}
}

func indexExists(t *testing.T, c *Collector, name string) bool {
	t.Helper()
	var n int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&n); err != nil {
		t.Fatalf("sqlite_master: %v", err)
	}
	return n > 0
}
