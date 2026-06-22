package market

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestOpen(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		DBPath:       dbPath,
		WAL:          true,
		MaxOpenConns: 5,
		MaxIdleConns: 2,
		BusyTimeout:  time.Second,
	}

	collector, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() {
		if cerr := collector.Close(); cerr != nil {
			t.Errorf("Close failed: %v", cerr)
		}
	})

	if collector.db == nil {
		t.Fatal("db is nil")
	}

	// Verify tables exist.
	var tableName string
	err = collector.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='items'").Scan(&tableName)
	if err != nil {
		t.Fatalf("items table not found: %v", err)
	}
}

func TestWriteRetry_CommitsTransaction(t *testing.T) {
	collector := newTestCollector(t)

	ctx := context.Background()
	err := collector.writeRetry(ctx, func(tx *sql.Tx) error {
		res, execErr := tx.Exec(
			"INSERT INTO items (item_id, item_name, category, first_seen_utc, last_updated_utc) VALUES (?, ?, ?, ?, ?)",
			"iron_ore", "Iron Ore", "ore", "2026-06-20T00:00:00Z", "2026-06-20T00:00:00Z",
		)
		if execErr != nil {
			return execErr
		}
		count, scanErr := res.LastInsertId()
		if scanErr != nil {
			return scanErr
		}
		if count == 0 {
			return errors.New("expected non-zero last insert id")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("writeRetry failed: %v", err)
	}

	var name string
	err = collector.db.QueryRow("SELECT item_name FROM items WHERE item_id = ?", "iron_ore").Scan(&name)
	if err != nil {
		t.Fatalf("query row failed: %v", err)
	}
	if name != "Iron Ore" {
		t.Fatalf("expected 'Iron Ore', got %q", name)
	}
}

func TestWriteRetry_PropagatesNonBusyError(t *testing.T) {
	collector := newTestCollector(t)

	sentinel := errors.New("sentinel")
	err := collector.writeRetry(context.Background(), func(_ *sql.Tx) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestWriteSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	collector, err := Open(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() {
		if cerr := collector.Close(); cerr != nil {
			t.Errorf("Close failed: %v", cerr)
		}
	})

	ctx := context.Background()
	now := time.Now().UTC()
	snapshot := MarketSnapshot{
		StationID:   "station_test",
		StationName: "Test Station",
		SystemID:    "system_test",
		SystemName:  "Test System",
		CapturedAt:  now,
		Orders: []Order{
			{
				StationID:  "station_test",
				ItemID:     "iron",
				ItemName:   "Iron Ore",
				Side:       "buy",
				PriceEach:  100.0,
				Quantity:   500.0,
				MyQuantity: 0,
				Source:     "player",
				CapturedAt: now,
			},
			{
				StationID:  "station_test",
				ItemID:     "copper",
				ItemName:   "Copper Ore",
				Side:       "sell",
				PriceEach:  200.0,
				Quantity:   300.0,
				MyQuantity: 0,
				Source:     "station",
				CapturedAt: now,
			},
		},
	}

	if err := collector.WriteSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("WriteSnapshot failed: %v", err)
	}

	// Verify station was written
	var stationName string
	err = collector.db.QueryRow("SELECT station_name FROM stations WHERE station_id = ?", "station_test").Scan(&stationName)
	if err != nil {
		t.Fatalf("Query station failed: %v", err)
	}
	if stationName != "Test Station" {
		t.Errorf("station_name = %q, want %q", stationName, "Test Station")
	}

	// Verify items were written
	var itemCount int
	err = collector.db.QueryRow("SELECT COUNT(*) FROM items").Scan(&itemCount)
	if err != nil {
		t.Fatalf("Count items failed: %v", err)
	}
	if itemCount != 2 {
		t.Errorf("item_count = %d, want 2", itemCount)
	}

	// Verify orders were written
	var orderCount int
	err = collector.db.QueryRow("SELECT COUNT(*) FROM market_orders WHERE station_id = ?", "station_test").Scan(&orderCount)
	if err != nil {
		t.Fatalf("Count orders failed: %v", err)
	}
	if orderCount != 2 {
		t.Errorf("order_count = %d, want 2", orderCount)
	}

	// Verify OHLCV was written (one per (station, item, side) — here 2 rows:
	// (station_test, iron, buy) and (station_test, copper, sell))
	var ohlcvCount int
	err = collector.db.QueryRow("SELECT COUNT(*) FROM market_ohlcv WHERE station_id = ?", "station_test").Scan(&ohlcvCount)
	if err != nil {
		t.Fatalf("Count ohlcv failed: %v", err)
	}
	if ohlcvCount != 2 {
		t.Errorf("ohlcv_count = %d, want 2", ohlcvCount)
	}
}

func TestComputeOHLCV(t *testing.T) {
	orders := []Order{
		{StationID: "stn1", ItemID: "iron", Side: "buy", PriceEach: 100, Quantity: 10},
		{StationID: "stn1", ItemID: "iron", Side: "buy", PriceEach: 110, Quantity: 5},
		{StationID: "stn1", ItemID: "iron", Side: "buy", PriceEach: 90, Quantity: 8},
	}

	result := computeOHLCV(orders, "2026-06-20T12:00:00Z")

	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}

	o := result[0]
	if o.OpenPrice != 100 {
		t.Errorf("open = %f, want 100", o.OpenPrice)
	}
	if o.HighPrice != 110 {
		t.Errorf("high = %f, want 110", o.HighPrice)
	}
	if o.LowPrice != 90 {
		t.Errorf("low = %f, want 90", o.LowPrice)
	}
	if o.ClosePrice != 90 {
		t.Errorf("close = %f, want 90", o.ClosePrice)
	}
	if o.Volume != 23 {
		t.Errorf("volume = %f, want 23", o.Volume)
	}
	// VWAP = (100*10 + 110*5 + 90*8) / 23 = 2270/23
	expectedVWAP := float64(100*10+110*5+90*8) / 23
	if o.VWAP != expectedVWAP {
		t.Errorf("vwap = %f, want %f", o.VWAP, expectedVWAP)
	}
	if o.TradeCount != 3 {
		t.Errorf("trade_count = %d, want 3", o.TradeCount)
	}
}

func TestIsBusyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"database is locked", errors.New("SQLITE_BUSY: database is locked"), true},
		{"database is busy", errors.New("database is busy"), true},
		{"sqlite busy code", errors.New("SQLITE_BUSY (5)"), true},
		{"unrelated error", errors.New("no such table: items"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBusyError(tc.err); got != tc.want {
				t.Errorf("isBusyError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// newTestCollector opens a Collector against an isolated temp-dir database.
func newTestCollector(t *testing.T) *Collector {
	t.Helper()
	collector, err := Open(Config{
		DBPath:       filepath.Join(t.TempDir(), "test.db"),
		WAL:          false,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
		BusyTimeout:  time.Second,
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
