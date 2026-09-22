package market

import (
	"context"
	"testing"
	"time"
)

// seedOHLCV writes hourly aggregate rows directly: the reference price reads
// market_ohlcv, which survives the market_orders retention window.
func seedOHLCV(t *testing.T, c *Collector, rows []OHLCV) {
	t.Helper()
	for _, o := range rows {
		if _, err := c.db.Exec(`
			INSERT INTO market_ohlcv (station_id, item_id, side, bucket_utc,
				open_price, high_price, low_price, close_price, volume, trade_count, vwap)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			o.StationID, o.ItemID, o.Side, o.BucketUTC,
			o.OpenPrice, o.HighPrice, o.LowPrice, o.ClosePrice, o.Volume, o.TradeCount, o.VWAP); err != nil {
			t.Fatalf("seed ohlcv: %v", err)
		}
	}
}

func hourly(station, item, side string, at time.Time, low float64) OHLCV {
	return OHLCV{
		StationID: station, ItemID: item, Side: side,
		BucketUTC:  at.UTC().Truncate(time.Hour).Format(time.RFC3339),
		OpenPrice:  low, HighPrice: low, LowPrice: low, ClosePrice: low,
		Volume:     1, TradeCount: 1, VWAP: low,
	}
}

// The anti-gouge reference asked for a 24h window while reading
// market_orders, whose retention is 2h. It therefore never saw 24 hours: the
// parameter had been quietly truncated by whatever --retain happened to be,
// and during the 2026-09-15..21 pruner outage it accidentally saw six days.
// market_ohlcv holds two months of hourly per-station lows, so the window the
// caller asks for is the window it gets, independent of order retention.
func TestGetReferencePrice_UsesHourlyAggregatesBeyondOrderRetention(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Cheap history 8-20 hours back: far outside any order-retention window.
	var rows []OHLCV
	for i, p := range []float64{6, 7, 8, 9, 10} {
		rows = append(rows, hourly(string(rune('a'+i)), "iron_ore", "sell", now.Add(-time.Duration(8+i)*time.Hour), p))
	}
	// One gouging station, recent.
	rows = append(rows, hourly("z", "iron_ore", "sell", now.Add(-time.Hour), 2000))
	seedOHLCV(t, c, rows)

	ref, ok, err := c.GetReferencePrice(ctx, "iron_ore", 24*time.Hour)
	if err != nil || !ok {
		t.Fatalf("GetReferencePrice: ok=%v err=%v", ok, err)
	}
	if ref > 12 {
		t.Errorf("reference = %v; the 20th percentile must land in the cheap cluster, not near the gouger", ref)
	}
}

// The window must actually bound the query, or a stale price from days ago
// sets the reference and the gate stops catching anything.
func TestGetReferencePrice_IgnoresDataOlderThanTheLookback(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seedOHLCV(t, c, []OHLCV{
		hourly("ancient", "iron_ore", "sell", now.Add(-72*time.Hour), 1), // 3 days old
		hourly("a", "iron_ore", "sell", now.Add(-2*time.Hour), 500),
		hourly("b", "iron_ore", "sell", now.Add(-3*time.Hour), 520),
	})
	ref, ok, err := c.GetReferencePrice(ctx, "iron_ore", 24*time.Hour)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ref < 400 {
		t.Errorf("reference = %v; the 3-day-old price 1 is outside the 24h window and must not set it", ref)
	}
}

// Buy-side rows are what other players PAY, not what we would be charged.
// Folding them in would drag the reference down and make the gate reject
// legitimate fills.
func TestGetReferencePrice_SellSideOnly(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seedOHLCV(t, c, []OHLCV{
		hourly("a", "iron_ore", "buy", now.Add(-time.Hour), 1),
		hourly("b", "iron_ore", "sell", now.Add(-time.Hour), 300),
	})
	ref, ok, _ := c.GetReferencePrice(ctx, "iron_ore", 24*time.Hour)
	if !ok || ref != 300 {
		t.Errorf("reference = %v (ok=%v), want 300 — the buy row must be ignored", ref, ok)
	}
}

// The not-for-sale sentinel is a listing marker, not a price.
func TestGetReferencePrice_ExcludesTheNotForSaleSentinel(t *testing.T) {
	c := newTestCollector(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seedOHLCV(t, c, []OHLCV{
		hourly("a", "iron_ore", "sell", now.Add(-time.Hour), 999999.0),
		hourly("b", "iron_ore", "sell", now.Add(-time.Hour), 42),
	})
	ref, ok, _ := c.GetReferencePrice(ctx, "iron_ore", 24*time.Hour)
	if !ok || ref != 42 {
		t.Errorf("reference = %v (ok=%v), want 42 — the sentinel must be filtered", ref, ok)
	}
}
