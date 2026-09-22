package market

import (
	"math"
	"testing"
)

// The volume-weighted median is the price at which half the UNITS sit, not the
// middle listing. Paired with vwap (also volume-based) the gap between them is
// a clean skew reading: vwap far below the median means the cheap tranche is
// thin, which is exactly the book shape that made best-bid arbitrage pricing
// overstate profit ~3x before OptimalArbitrage began walking the ladder.
func TestWeightedMedian_IsThePriceWhereHalfTheUnitsSit(t *testing.T) {
	// 10 units @1, 10 @2, 80 @3 -> the 50th unit lies in the 3 tranche.
	got, ok := weightedMedian([]pricedQty{{1, 10}, {2, 10}, {3, 80}})
	if !ok || got != 3 {
		t.Errorf("weightedMedian = %v (ok=%v), want 3", got, ok)
	}
}

// Weighting is the whole point: a single cheap listing must not drag the
// median the way it drags (high+low)/2.
func TestWeightedMedian_IgnoresATinyCheapListing(t *testing.T) {
	// 1 unit @1 against 999 @500: the midpoint says ~250, the truth is 500.
	got, ok := weightedMedian([]pricedQty{{1, 1}, {500, 999}})
	if !ok || got != 500 {
		t.Errorf("weightedMedian = %v (ok=%v), want 500", got, ok)
	}
	if mid := (1.0 + 500.0) / 2; math.Abs(got-mid) < 1 {
		t.Error("weighted median must not agree with the unweighted midpoint here")
	}
}

// Input arrives in capture order, not price order.
func TestWeightedMedian_SortsByPrice(t *testing.T) {
	got, ok := weightedMedian([]pricedQty{{3, 80}, {1, 10}, {2, 10}})
	if !ok || got != 3 {
		t.Errorf("weightedMedian = %v (ok=%v), want 3 regardless of input order", got, ok)
	}
}

// Exactly on the halfway boundary, take the lower price: the median must never
// read more expensive than the book justifies, the same conservative direction
// the anti-gouge reference uses.
func TestWeightedMedian_TiesTakeTheCheaperSide(t *testing.T) {
	got, ok := weightedMedian([]pricedQty{{10, 50}, {20, 50}})
	if !ok || got != 10 {
		t.Errorf("weightedMedian = %v (ok=%v), want 10 on an exact split", got, ok)
	}
}

// A single level has no distribution to speak of, but its price is still the
// honest answer.
func TestWeightedMedian_SingleLevel(t *testing.T) {
	got, ok := weightedMedian([]pricedQty{{42, 7}})
	if !ok || got != 42 {
		t.Errorf("weightedMedian = %v (ok=%v), want 42", got, ok)
	}
}

// No units means no median. Reporting 0 would look like a free item.
func TestWeightedMedian_EmptyAndZeroVolumeAreNotAMedian(t *testing.T) {
	if _, ok := weightedMedian(nil); ok {
		t.Error("empty input must not yield a median")
	}
	if _, ok := weightedMedian([]pricedQty{{10, 0}, {20, 0}}); ok {
		t.Error("zero total volume must not yield a median")
	}
}

// End to end: the aggregate row must carry it, so the skew is queryable
// alongside vwap without recomputing anything.
func TestComputeOHLCV_RecordsTheWeightedMedian(t *testing.T) {
	orders := []Order{
		{StationID: "s", ItemID: "iron_ore", Side: "sell", PriceEach: 1, Quantity: 10},
		{StationID: "s", ItemID: "iron_ore", Side: "sell", PriceEach: 2, Quantity: 10},
		{StationID: "s", ItemID: "iron_ore", Side: "sell", PriceEach: 3, Quantity: 80},
	}
	rows := computeOHLCV(orders, "2026-09-22T00:00:00Z")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].MedianPrice != 3 {
		t.Errorf("MedianPrice = %v, want 3", rows[0].MedianPrice)
	}
	// vwap = (1*10 + 2*10 + 3*80) / 100 = 2.7, below the median: the cheap end
	// is thin. That gap IS the signal.
	if math.Abs(rows[0].VWAP-2.7) > 1e-9 {
		t.Errorf("VWAP = %v, want 2.7", rows[0].VWAP)
	}
	if rows[0].MedianPrice <= rows[0].VWAP {
		t.Error("median above vwap is the expected skew here; the pair must be comparable")
	}
}

// The column has to survive the round trip, or the signal exists only in
// memory. Uses the collector's real write path rather than a hand-rolled
// INSERT, so the upsert and the schema are both exercised.
func TestUpsertOHLCV_PersistsTheMedian(t *testing.T) {
	c := newTestCollector(t)
	row := OHLCV{
		StationID: "s", ItemID: "iron_ore", Side: "sell", BucketUTC: "2026-09-22T00:00:00Z",
		OpenPrice: 1, HighPrice: 3, LowPrice: 1, ClosePrice: 3,
		Volume: 100, TradeCount: 3, VWAP: 2.7, MedianPrice: 3,
	}
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.upsertOHLCV(tx, row); err != nil {
		t.Fatalf("upsertOHLCV: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var median, vwap float64
	if err := c.db.QueryRow(`SELECT median_price, vwap FROM market_ohlcv
		WHERE station_id='s' AND item_id='iron_ore' AND side='sell'`).Scan(&median, &vwap); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if median != 3 || vwap != 2.7 {
		t.Errorf("median=%v vwap=%v, want 3 and 2.7", median, vwap)
	}
}
