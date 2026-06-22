//go:build integration

// Integration test placeholder for market capture against a live game server.
// Not compiled into the normal test build. Run with:
//
//	go test -tags=integration -v ./pkg/market/...
package market

import "testing"

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
