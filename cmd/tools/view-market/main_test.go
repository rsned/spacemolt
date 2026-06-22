package main

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// it wrote. The view-market cmd funcs print results directly to os.Stdout, so
// this lets tests assert on rendered content, not just the returned error.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = orig
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(out), runErr
}

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

	// By system ID (station omitted).
	out, err := captureStdout(t, func() error { return cmdLatest(ctx, db, cfg, []string{"sys-1"}) })
	if err != nil {
		t.Errorf("cmdLatest by system: %v", err)
	}
	for _, want := range []string{"Alpha Station", "iron_ore", "Iron Ore"} {
		if !strings.Contains(out, want) {
			t.Errorf("cmdLatest by system output missing %q; got:\n%s", want, out)
		}
	}

	// By system + explicit station.
	out, err = captureStdout(t, func() error { return cmdLatest(ctx, db, cfg, []string{"sys-1", "station-alpha"}) })
	if err != nil {
		t.Errorf("cmdLatest by station: %v", err)
	}
	if !strings.Contains(out, "Alpha Station") {
		t.Errorf("cmdLatest by station output missing Alpha Station; got:\n%s", out)
	}
}

func TestCmdHistory(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	cfg := Config{Format: formatJSON, Limit: 20}
	ctx := context.Background()

	out, err := captureStdout(t, func() error { return cmdHistory(ctx, db, cfg, []string{"sys-1"}) })
	if err != nil {
		t.Errorf("cmdHistory: %v", err)
	}
	// One capture was seeded; the captured_at timestamp should appear.
	if !strings.Contains(out, "captured_at") && !strings.Contains(out, "20") {
		t.Errorf("cmdHistory output looks empty; got:\n%s", out)
	}
}

func TestCmdItems(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	cfg := Config{Format: formatJSON, Limit: 20}
	ctx := context.Background()

	out, err := captureStdout(t, func() error { return cmdItems(ctx, db, cfg, nil) })
	if err != nil {
		t.Errorf("cmdItems: %v", err)
	}
	if !strings.Contains(out, "iron_ore") {
		t.Errorf("cmdItems output missing iron_ore; got:\n%s", out)
	}
}

func TestCmdPrices(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	cfg := Config{Format: formatJSON, Limit: 20}
	ctx := context.Background()

	// No OHLCV aggregation is run in this seed, so the result is legitimately
	// empty — assert the command runs and produces output without error.
	out, err := captureStdout(t, func() error { return cmdPrices(ctx, db, cfg, []string{"iron_ore"}) })
	if err != nil {
		t.Errorf("cmdPrices: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("cmdPrices produced no output")
	}
}

func TestCmdArbitrage(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	cfg := Config{Format: formatJSON, Limit: 20}
	ctx := context.Background()

	// Cheapest sell across stations is beta@45; highest buy is beta@60 →
	// profit 15 on iron_ore. Assert the computed opportunity surfaces.
	out, err := captureStdout(t, func() error { return cmdArbitrage(ctx, db, cfg, nil) })
	if err != nil {
		t.Errorf("cmdArbitrage: %v", err)
	}
	for _, want := range []string{"iron_ore", "\"min_sell_price\": 45", "\"max_buy_price\": 60", "\"profit\": 15"} {
		if !strings.Contains(out, want) {
			t.Errorf("cmdArbitrage output missing %q; got:\n%s", want, out)
		}
	}
}
