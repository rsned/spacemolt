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

// insertRawOpp inserts a minimal opportunity row directly (white-box) for
// lifecycle tests. status may be any allowed value.
func insertRawOpp(t *testing.T, c *Collector, status string) {
	t.Helper()
	_, err := c.db.Exec(`INSERT INTO arbitrage_opportunities
		(from_station_id, to_station_id, item_id, action_type, buy_price, sell_price,
		 quantity, gross_profit, fuel_cost, travel_ticks, cargo_required, status,
		 expires_at, discovered_at, discovered_by)
		VALUES ('stnA','stnB','iron_ore','buy_then_sell',1,2,1,1,0,0,1,?,
		 '2030-01-01T00:00:00Z','2026-06-24T00:00:00Z','test')`, status)
	if err != nil {
		t.Fatalf("insertRawOpp: %v", err)
	}
}

func countStatus(t *testing.T, c *Collector, status string) int {
	t.Helper()
	var n int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM arbitrage_opportunities WHERE status = ?`, status).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", status, err)
	}
	return n
}

func TestScanArbitrageHappyPath(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{{StationID: "stnA", ItemID: "iron_ore", ItemName: "Iron Ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnA: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
		Orders: []Order{{StationID: "stnB", ItemID: "iron_ore", ItemName: "Iron Ore", Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnB: %v", err)
	}

	// buy at stnA ask 5, sell at stnB bid 8, qty min(10,5)=5, gross (8-5)*5=15.
	res, err := c.ScanArbitrage(ctx, ScanOptions{MinProfit: 1, MinPrice: 1, MinQuantity: 1, ExpiresIn: time.Hour})
	if err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if res.Inserted != 1 {
		t.Fatalf("inserted = %d, want 1", res.Inserted)
	}
	var buyPrice, sellPrice, qty, gross float64
	var actionType, notes, status string
	if err := c.db.QueryRow(`SELECT buy_price, sell_price, quantity, gross_profit, action_type, notes, status
		FROM arbitrage_opportunities WHERE item_id = ?`, "iron_ore").
		Scan(&buyPrice, &sellPrice, &qty, &gross, &actionType, &notes, &status); err != nil {
		t.Fatalf("query opp: %v", err)
	}
	if buyPrice != 5 || sellPrice != 8 || qty != 5 || gross != 15 {
		t.Errorf("row = buy %v sell %v qty %v gross %v, want 5/8/5/15", buyPrice, sellPrice, qty, gross)
	}
	if actionType != "buy_then_sell" || notes != "logistics:deferred" || status != "available" {
		t.Errorf("meta = action %q notes %q status %q", actionType, notes, status)
	}
}

func TestScanArbitrageSameStationExcluded(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	// One station with both a cheap ask and a high bid — no cross-station pair possible.
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{
			{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now},
			{StationID: "stnA", ItemID: "iron_ore", Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now},
		},
	}); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	res, err := c.ScanArbitrage(ctx, ScanOptions{MinProfit: 1, MinPrice: 1})
	if err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if res.Inserted != 0 {
		t.Errorf("same-station pairs must be excluded; inserted %d", res.Inserted)
	}
}

func TestScanArbitrageFiltersBasementByMinPrice(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 1, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnA: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
		Orders: []Order{{StationID: "stnB", ItemID: "iron_ore", Side: "buy", PriceEach: 200, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnB: %v", err)
	}
	// Default MinPrice 10 filters the 1cr ask (gross (200-1)*10=1990 would otherwise clear).
	def, err := c.ScanArbitrage(ctx, ScanOptions{})
	if err != nil {
		t.Fatalf("ScanArbitrage default: %v", err)
	}
	if def.Inserted != 0 {
		t.Errorf("default MinPrice=10 should filter 1cr ask; inserted %d", def.Inserted)
	}
	// Lowering the floor to 1 lets it through.
	loose, err := c.ScanArbitrage(ctx, ScanOptions{MinPrice: 1})
	if err != nil {
		t.Fatalf("ScanArbitrage loose: %v", err)
	}
	if loose.Inserted != 1 {
		t.Errorf("MinPrice=1 should let the spread through; inserted %d", loose.Inserted)
	}
}

func TestScanArbitrageExpiresAvailablePreservesClaimed(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertRawOpp(t, c, "available") // prior available → should be expired by the scan
	insertRawOpp(t, c, "claimed")   // claimed → preserved

	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnA: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
		Orders: []Order{{StationID: "stnB", ItemID: "iron_ore", Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnB: %v", err)
	}

	res, err := c.ScanArbitrage(ctx, ScanOptions{MinProfit: 1, MinPrice: 1})
	if err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if res.Expired != 1 {
		t.Errorf("expired = %d, want 1 (prior available)", res.Expired)
	}
	if got := countStatus(t, c, "available"); got != 1 {
		t.Errorf("available = %d, want 1 (the fresh insert)", got)
	}
	if got := countStatus(t, c, "claimed"); got != 1 {
		t.Errorf("claimed = %d, want 1 (preserved)", got)
	}
	if got := countStatus(t, c, "expired"); got != 1 {
		t.Errorf("expired rows = %d, want 1", got)
	}
}

func TestScanArbitrageLimitCap(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	// Two items each yielding a spread across stnA/stnB → 2 candidates; cap at 1.
	write := func(item string) {
		_ = c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
			Orders: []Order{{StationID: "stnA", ItemID: item, Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
		})
		_ = c.WriteSnapshot(ctx, MarketSnapshot{
			StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
			Orders: []Order{{StationID: "stnB", ItemID: item, Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now}},
		})
	}
	write("iron_ore")
	write("copper_ore")
	res, err := c.ScanArbitrage(ctx, ScanOptions{MinProfit: 1, MinPrice: 1, Limit: 1})
	if err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if res.Inserted != 1 {
		t.Errorf("inserted = %d, want 1 (Limit cap)", res.Inserted)
	}
}

func TestClaimOpportunity(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	insertRawOpp(t, c, "available") // id=1

	ok, err := c.ClaimOpportunity(ctx, 1, "agent1")
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	// Double-claim by another agent fails.
	ok2, _ := c.ClaimOpportunity(ctx, 1, "agent2")
	if ok2 {
		t.Errorf("double-claim should return false")
	}
	// Claiming an already-expired row fails.
	insertRawOpp(t, c, "expired") // id=2
	ok3, _ := c.ClaimOpportunity(ctx, 2, "agent1")
	if ok3 {
		t.Errorf("claim on expired should return false")
	}
}

func TestCompleteOpportunity(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	insertRawOpp(t, c, "available") // id=1
	if ok, _ := c.ClaimOpportunity(ctx, 1, "agent1"); !ok {
		t.Fatalf("setup claim failed")
	}

	ok, err := c.CompleteOpportunity(ctx, 1, "agent1")
	if err != nil || !ok {
		t.Errorf("complete by owner: ok=%v err=%v", ok, err)
	}

	// Wrong agent cannot complete.
	insertRawOpp(t, c, "available") // id=2
	if ok, _ := c.ClaimOpportunity(ctx, 2, "agent1"); !ok {
		t.Fatalf("setup claim failed")
	}
	ok2, _ := c.CompleteOpportunity(ctx, 2, "agent2")
	if ok2 {
		t.Errorf("complete by non-owner should return false")
	}
}
