package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

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
