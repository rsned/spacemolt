package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openArbDB(t *testing.T) *Collector {
	t.Helper()
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestGetItemStationPrices(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	write := func(stn, sys string, orders []Order) {
		if err := c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: stn, StationName: stn, SystemID: sys, SystemName: sys,
			CapturedAt: now, Orders: orders,
		}); err != nil {
			t.Fatalf("WriteSnapshot %s: %v", stn, err)
		}
	}
	write("stnA", "sysA", []Order{
		{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now},
		{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 7, Quantity: 20, CapturedAt: now},
		{StationID: "stnA", ItemID: "iron_ore", Side: "buy", PriceEach: 3, Quantity: 5, CapturedAt: now},
	})
	write("stnB", "sysB", []Order{
		{StationID: "stnB", ItemID: "iron_ore", Side: "sell", PriceEach: 9, Quantity: 4, CapturedAt: now},
		{StationID: "stnB", ItemID: "iron_ore", Side: "buy", PriceEach: 8, Quantity: 1, CapturedAt: now},
		{StationID: "stnB", ItemID: "iron_ore", Side: "buy", PriceEach: 10, Quantity: 2, CapturedAt: now},
	})

	prices, err := c.GetItemStationPrices(ctx, "iron_ore")
	if err != nil {
		t.Fatalf("GetItemStationPrices: %v", err)
	}
	if len(prices) != 2 {
		t.Fatalf("prices = %d stations, want 2", len(prices))
	}
	by := map[string]ItemStationPrice{}
	for _, p := range prices {
		by[p.StationID] = p
	}
	a := by["stnA"]
	if !a.HasSell || a.BestAsk != 5 || a.AskQty != 10 {
		t.Errorf("stnA ask = %v qty %v (has %v), want 5/10", a.BestAsk, a.AskQty, a.HasSell)
	}
	if !a.HasBuy || a.BestBid != 3 || a.BidQty != 5 {
		t.Errorf("stnA bid = %v qty %v, want 3/5", a.BestBid, a.BidQty)
	}
	b := by["stnB"]
	if !b.HasSell || b.BestAsk != 9 || b.AskQty != 4 {
		t.Errorf("stnB ask = %v qty %v, want 9/4", b.BestAsk, b.AskQty)
	}
	if !b.HasBuy || b.BestBid != 10 || b.BidQty != 2 {
		t.Errorf("stnB bid = %v qty %v, want 10/2 (ties at 10 summed)", b.BestBid, b.BidQty)
	}
	none, err := c.GetItemStationPrices(ctx, "nope")
	if err != nil {
		t.Fatalf("absent: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("absent = %d, want 0", len(none))
	}
}

func TestGetItemStationPricesLatestCaptureWins(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	t1 := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 24, 11, 0, 0, 0, time.UTC)
	snap := func(at time.Time, price float64) {
		if err := c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: "stn1", StationName: "One", SystemID: "sys1", SystemName: "S1",
			CapturedAt: at,
			Orders:     []Order{{StationID: "stn1", ItemID: "iron_ore", Side: "sell", PriceEach: price, Quantity: 1, CapturedAt: at}},
		}); err != nil {
			t.Fatalf("WriteSnapshot %v: %v", at, err)
		}
	}
	snap(t1, 9)
	snap(t2, 4) // newer capture supersedes

	prices, err := c.GetItemStationPrices(ctx, "iron_ore")
	if err != nil {
		t.Fatalf("GetItemStationPrices: %v", err)
	}
	if len(prices) != 1 || prices[0].BestAsk != 4 {
		t.Errorf("expected latest-capture best ask 4, got %+v", prices)
	}
}
