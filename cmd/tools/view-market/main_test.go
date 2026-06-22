package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
)

// openTestDB creates a market.db in t.TempDir(), seeds it with two stations
// and buy+sell orders for one item, then returns an open *sql.DB for that file.
func openTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "m.db")

	col, err := market.Open(market.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("market.Open: %v", err)
	}

	now := time.Now().UTC()
	snap1 := market.MarketSnapshot{
		StationID:   "station-alpha",
		StationName: "Alpha Station",
		SystemID:    "sys-1",
		SystemName:  "System One",
		CapturedAt:  now,
		Orders: []market.Order{
			{StationID: "station-alpha", ItemID: "iron_ore", ItemName: "Iron Ore", Side: "sell", PriceEach: 50, Quantity: 100, CapturedAt: now},
			{StationID: "station-alpha", ItemID: "iron_ore", ItemName: "Iron Ore", Side: "buy", PriceEach: 40, Quantity: 50, CapturedAt: now},
		},
	}
	snap2 := market.MarketSnapshot{
		StationID:   "station-beta",
		StationName: "Beta Station",
		SystemID:    "sys-2",
		SystemName:  "System Two",
		CapturedAt:  now,
		Orders: []market.Order{
			{StationID: "station-beta", ItemID: "iron_ore", ItemName: "Iron Ore", Side: "sell", PriceEach: 45, Quantity: 80, CapturedAt: now},
			{StationID: "station-beta", ItemID: "iron_ore", ItemName: "Iron Ore", Side: "buy", PriceEach: 60, Quantity: 30, CapturedAt: now},
		},
	}

	ctx := context.Background()
	if err := col.WriteSnapshot(ctx, snap1); err != nil {
		t.Fatalf("WriteSnapshot snap1: %v", err)
	}
	if err := col.WriteSnapshot(ctx, snap2); err != nil {
		t.Fatalf("WriteSnapshot snap2: %v", err)
	}
	if err := col.Close(); err != nil {
		t.Fatalf("col.Close: %v", err)
	}

	db, err := openDatabase(dbPath)
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	return db, func() { _ = db.Close() }
}

func TestCmdLatest(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	cfg := Config{Format: formatJSON, Limit: 20}
	ctx := context.Background()

	// By system ID (station omitted)
	if err := cmdLatest(ctx, db, cfg, []string{"sys-1"}); err != nil {
		t.Errorf("cmdLatest by system: %v", err)
	}

	// By system + explicit station
	if err := cmdLatest(ctx, db, cfg, []string{"sys-1", "station-alpha"}); err != nil {
		t.Errorf("cmdLatest by station: %v", err)
	}
}

func TestCmdHistory(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	cfg := Config{Format: formatJSON, Limit: 20}
	ctx := context.Background()

	if err := cmdHistory(ctx, db, cfg, []string{"sys-1"}); err != nil {
		t.Errorf("cmdHistory: %v", err)
	}
}

func TestCmdItems(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	cfg := Config{Format: formatJSON, Limit: 20}
	ctx := context.Background()

	if err := cmdItems(ctx, db, cfg, nil); err != nil {
		t.Errorf("cmdItems: %v", err)
	}
}

func TestCmdPrices(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	cfg := Config{Format: formatJSON, Limit: 20}
	ctx := context.Background()

	if err := cmdPrices(ctx, db, cfg, []string{"iron_ore"}); err != nil {
		t.Errorf("cmdPrices: %v", err)
	}
}

func TestCmdArbitrage(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	cfg := Config{Format: formatJSON, Limit: 20}
	ctx := context.Background()

	// beta station has buy@60 and alpha has sell@50 → should detect arb opportunity
	if err := cmdArbitrage(ctx, db, cfg, nil); err != nil {
		t.Errorf("cmdArbitrage: %v", err)
	}
}
