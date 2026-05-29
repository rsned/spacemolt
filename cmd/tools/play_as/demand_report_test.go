package main

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestBuildDemandReportClassifiesAndFulfills(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-1 * time.Hour)

	summary := []knowledge.MarketDemandRow{
		// Compact-only item -> class "?".
		{StationID: "stnA", SystemID: "sysA", ItemID: "titanium", ItemName: "Titanium", BestBuyPrice: 30, BuyQuantity: 40, CapturedAt: fresh},
	}
	deep := []knowledge.MarketBuyOrderRow{
		// Station order at 10, player order above it at 12 -> top is PLR>SM.
		{StationID: "stnA", SystemID: "sysA", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: fresh},
		{StationID: "stnA", SystemID: "sysA", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 12, Quantity: 20, Source: "player", CapturedAt: fresh},
		// Pure station demand at another station -> STN.
		{StationID: "stnB", SystemID: "sysB", ItemID: "copper", ItemName: "Copper", PriceEach: 8, Quantity: 100, Source: "station", CapturedAt: fresh},
	}
	onHand := map[string]float64{
		"iron_ore": 30, // can fulfill 30 of the 70 total iron demand
		"copper":   0,
	}
	canCraft := map[string]int{
		"titanium": 5, // craftable to fulfill
	}

	rep := buildDemandReport(summary, deep, onHand, canCraft, now, demandOptions{sort: sortByPrice})

	byItem := map[string]demandReportRow{}
	for _, r := range rep.Rows {
		byItem[r.ItemID] = r
	}

	iron := byItem["iron_ore"]
	if iron.Class != classAboveSM {
		t.Errorf("iron class: want %s got %s", classAboveSM, iron.Class)
	}
	if iron.Price != 12 || iron.Quantity != 70 {
		t.Errorf("iron price/qty: want 12/70 got %v/%v", iron.Price, iron.Quantity)
	}
	if iron.FulfillQty != 30 || iron.FulfillValue != 360 {
		t.Errorf("iron fulfill: want 30/360 got %v/%v", iron.FulfillQty, iron.FulfillValue)
	}
	if byItem["copper"].Class != classStation {
		t.Errorf("copper class: want %s got %s", classStation, byItem["copper"].Class)
	}
	if byItem["titanium"].Class != classUnknown {
		t.Errorf("titanium class: want %s got %s", classUnknown, byItem["titanium"].Class)
	}
	if byItem["titanium"].CanCraft != 5 {
		t.Errorf("titanium craft: want 5 got %d", byItem["titanium"].CanCraft)
	}
}

func TestBuildDemandReportFiltersAndStaleness(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)

	summary := []knowledge.MarketDemandRow{
		{StationID: "s1", ItemID: "a", ItemName: "A", BestBuyPrice: 5, BuyQuantity: 10, CapturedAt: stale},
		{StationID: "s1", ItemID: "b", ItemName: "B", BestBuyPrice: 50, BuyQuantity: 10, CapturedAt: fresh},
	}

	// minPrice filters out item a (price 5).
	rep := buildDemandReport(summary, nil, nil, nil, now, demandOptions{minPrice: 10})
	if len(rep.Rows) != 1 || rep.Rows[0].ItemID != "b" {
		t.Fatalf("minPrice filter: want only b, got %+v", rep.Rows)
	}
	// Staleness flag set for the >24h-old row when not filtered out.
	rep2 := buildDemandReport(summary, nil, nil, nil, now, demandOptions{})
	for _, r := range rep2.Rows {
		wantStale := r.ItemID == "a"
		if r.AgeStale != wantStale {
			t.Errorf("item %s stale: want %v got %v", r.ItemID, wantStale, r.AgeStale)
		}
	}
}

func TestBuildDemandReportTieAndZeroPrice(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-1 * time.Hour)

	deep := []knowledge.MarketBuyOrderRow{
		// Player order tied at the same price as the best station order -> the
		// station wins the tie (top-source check is strict), so class STN.
		{StationID: "stnA", SystemID: "sysA", ItemID: "tie_item", ItemName: "Tie Item", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: fresh},
		{StationID: "stnA", SystemID: "sysA", ItemID: "tie_item", ItemName: "Tie Item", PriceEach: 10, Quantity: 20, Source: "player", CapturedAt: fresh},
		// Zero-price station order is skipped; the only positive order is a
		// player order, so class PLR (the zero-price row must not make it STN).
		{StationID: "stnB", SystemID: "sysB", ItemID: "zero_item", ItemName: "Zero Item", PriceEach: 0, Quantity: 99, Source: "station", CapturedAt: fresh},
		{StationID: "stnB", SystemID: "sysB", ItemID: "zero_item", ItemName: "Zero Item", PriceEach: 7, Quantity: 30, Source: "player", CapturedAt: fresh},
	}

	rep := buildDemandReport(nil, deep, nil, nil, now, demandOptions{})
	byItem := map[string]demandReportRow{}
	for _, r := range rep.Rows {
		byItem[r.ItemID] = r
	}

	tie := byItem["tie_item"]
	if tie.Class != classStation {
		t.Errorf("tie class: want %s got %s", classStation, tie.Class)
	}
	if tie.Quantity != 70 {
		t.Errorf("tie qty: want 70 got %v", tie.Quantity)
	}

	zero := byItem["zero_item"]
	if zero.Class != classPlayer {
		t.Errorf("zero-price class: want %s got %s", classPlayer, zero.Class)
	}
	// The zero-price station order is excluded from totals (qty/price), so only
	// the 30-unit player order remains.
	if zero.Quantity != 30 || zero.Price != 7 {
		t.Errorf("zero-price qty/price: want 30/7 got %v/%v", zero.Quantity, zero.Price)
	}
}

func TestBuildDemandReportTotalAfterLimit(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-1 * time.Hour)

	summary := []knowledge.MarketDemandRow{
		{StationID: "s1", ItemID: "hi", ItemName: "Hi", BestBuyPrice: 100, BuyQuantity: 10, CapturedAt: fresh},
		{StationID: "s1", ItemID: "lo", ItemName: "Lo", BestBuyPrice: 1, BuyQuantity: 10, CapturedAt: fresh},
	}
	onHand := map[string]float64{"hi": 10, "lo": 10}

	// Limit to 1 row (sorted by proceeds desc -> "hi" with value 1000 survives).
	rep := buildDemandReport(summary, nil, onHand, nil, now, demandOptions{limit: 1})
	if len(rep.Rows) != 1 || rep.Rows[0].ItemID != "hi" {
		t.Fatalf("limit: want only hi, got %+v", rep.Rows)
	}
	// TotalFulfill must reflect only the returned row, not the dropped "lo".
	if rep.TotalFulfill != 1000 {
		t.Errorf("total after limit: want 1000 got %v", rep.TotalFulfill)
	}
}
