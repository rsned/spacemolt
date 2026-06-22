//go:build integration

// Integration test placeholder for market capture against a live game server.
// Not compiled into the normal test build. Run with:
//
//	go test -tags=integration -v ./pkg/market/...
package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestCaptureIntegration documents the manual procedure for verifying that a
// real game client can capture market data end-to-end. Skipped by default.
func TestCaptureIntegration(t *testing.T) {
	t.Skip("requires a live game connection — manual verification only")

	// Manual verification procedure:
	//  1. Build play_as and connect an agent docked at a station.
	//  2. Run the `update_market` command (or `schedule_add hourly update_market`).
	//  3. Run `go run ./cmd/tools/market-stats` and confirm station/item/order
	//     counts increase.
	//  4. Query the DB directly, e.g.:
	//       sqlite3 data/market.db "SELECT COUNT(*) FROM market_orders"
	//       sqlite3 data/market.db "SELECT station_id, COUNT(*) FROM market_orders GROUP BY station_id"
	//       sqlite3 data/market.db "SELECT bucket_utc, COUNT(*) FROM market_orders GROUP BY bucket_utc ORDER BY bucket_utc DESC LIMIT 10"
}

// TestMarketRoundTrip proves the single-source read/write path end-to-end.
// Writes a snapshot via WriteSnapshot, reads it back via GetLatestSnapshot,
// runs FindBestPrices, and asserts the cheapest-station result.
func TestMarketRoundTrip(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "rt.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID:   "stn1",
		StationName: "One",
		SystemID:    "sys",
		SystemName:  "S",
		CapturedAt:  now,
		Orders: []Order{{
			StationID:  "stn1",
			ItemID:     "iron",
			ItemName:   "Iron",
			Side:       "sell",
			PriceEach:  5,
			Quantity:   10,
			CapturedAt: now,
		}},
	}); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	snap, err := c.GetLatestSnapshot(ctx, "stn1")
	if err != nil || snap == nil || len(snap.Orders) != 1 {
		t.Fatalf("GetLatestSnapshot = (%+v, %v)", snap, err)
	}
	best, err := c.FindBestPrices(ctx, "iron", "sell", 1)
	if err != nil || len(best) != 1 || best[0].StationID != "stn1" {
		t.Fatalf("FindBestPrices = (%+v, %v)", best, err)
	}
}
