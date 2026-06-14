package main

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestBuildDemandReportClassifiesAndFulfills(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-1 * time.Hour)

	deep := []knowledge.MarketBuyOrderRow{
		// Station order at 10, null-source (player) order above it at 12 -> PLR>SM.
		{StationID: "stnA", SystemID: "sysA", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: fresh},
		{StationID: "stnA", SystemID: "sysA", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 12, Quantity: 20, Source: "", CapturedAt: fresh},
		// Pure station demand -> STN.
		{StationID: "stnB", SystemID: "sysB", ItemID: "copper", ItemName: "Copper", PriceEach: 8, Quantity: 100, Source: "station", CapturedAt: fresh},
		// Lone null-source order, no station competitor -> PLR.
		{StationID: "stnC", SystemID: "sysC", ItemID: "titanium", ItemName: "Titanium", PriceEach: 30, Quantity: 40, Source: "", CapturedAt: fresh},
	}
	onHand := map[string]float64{"iron_ore": 30}
	canCraft := map[string]int{"titanium": 5}

	rep := buildDemandReport(deep, onHand, canCraft, now, demandOptions{sort: sortByPrice, showNoneOnHand: true})

	byItem := map[string]demandReportRow{}
	for _, r := range rep.Rows {
		byItem[r.ItemID] = r
	}
	if byItem["iron_ore"].Class != classAboveSM {
		t.Errorf("iron class: want %s got %s", classAboveSM, byItem["iron_ore"].Class)
	}
	if byItem["iron_ore"].Price != 12 || byItem["iron_ore"].Quantity != 70 {
		t.Errorf("iron price/qty: want 12/70 got %v/%v", byItem["iron_ore"].Price, byItem["iron_ore"].Quantity)
	}
	// FulfillValue walks the order book, not top_price * qty: selling 30 units
	// fills the player order (20 @ 12 = 240) then the station order (10 @ 10 =
	// 100) = 340, avg 11.333. The headline Price column stays the top price (12).
	if byItem["iron_ore"].FulfillQty != 30 || byItem["iron_ore"].FulfillValue != 340 {
		t.Errorf("iron fulfill: want 30/340 got %v/%v", byItem["iron_ore"].FulfillQty, byItem["iron_ore"].FulfillValue)
	}
	if got := byItem["iron_ore"].FulfillAvg; abs(got-340.0/30.0) > 1e-6 {
		t.Errorf("iron fulfill avg: want %v got %v", 340.0/30.0, got)
	}
	if byItem["copper"].Class != classStation {
		t.Errorf("copper class: want %s got %s", classStation, byItem["copper"].Class)
	}
	if byItem["titanium"].Class != classPlayer {
		t.Errorf("titanium class: want %s got %s", classPlayer, byItem["titanium"].Class)
	}
	if byItem["titanium"].CanCraft != 5 {
		t.Errorf("titanium craft: want 5 got %d", byItem["titanium"].CanCraft)
	}
}

func TestBuildDemandReportWalksOrderBook(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	// The real Grand Exchange Pulse Laser III book: one tiny top order over a
	// deep cheap rung. The old code reported top_price * fulfill_qty
	// (9978 * 33 = 329,274); the fix walks the ladder for 70,418.
	deep := []knowledge.MarketBuyOrderRow{
		{StationID: "grand_exchange", ItemID: "pulse_laser_iii", ItemName: "Pulse Laser III", PriceEach: 9978, Quantity: 1, Source: "station", CapturedAt: now},
		{StationID: "grand_exchange", ItemID: "pulse_laser_iii", ItemName: "Pulse Laser III", PriceEach: 9854, Quantity: 2, Source: "station", CapturedAt: now},
		{StationID: "grand_exchange", ItemID: "pulse_laser_iii", ItemName: "Pulse Laser III", PriceEach: 9158, Quantity: 1, Source: "station", CapturedAt: now},
		{StationID: "grand_exchange", ItemID: "pulse_laser_iii", ItemName: "Pulse Laser III", PriceEach: 5373, Quantity: 1, Source: "station", CapturedAt: now},
		{StationID: "grand_exchange", ItemID: "pulse_laser_iii", ItemName: "Pulse Laser III", PriceEach: 5306, Quantity: 4, Source: "station", CapturedAt: now},
		{StationID: "grand_exchange", ItemID: "pulse_laser_iii", ItemName: "Pulse Laser III", PriceEach: 4931, Quantity: 1, Source: "station", CapturedAt: now},
		{StationID: "grand_exchange", ItemID: "pulse_laser_iii", ItemName: "Pulse Laser III", PriceEach: 2, Quantity: 169, Source: "station", CapturedAt: now},
	}
	onHand := map[string]float64{"pulse_laser_iii": 33}

	rep := buildDemandReport(deep, onHand, nil, now, demandOptions{})
	if len(rep.Rows) != 1 {
		t.Fatalf("want 1 row, got %+v", rep.Rows)
	}
	r := rep.Rows[0]
	// Headline Price stays the best buy price; Quantity is total demand.
	if r.Price != 9978 || r.Quantity != 179 {
		t.Errorf("price/qty: want 9978/179 got %v/%v", r.Price, r.Quantity)
	}
	if r.FulfillQty != 33 || r.FulfillValue != 70418 {
		t.Errorf("fulfill: want 33/70418 got %v/%v", r.FulfillQty, r.FulfillValue)
	}
	if abs(r.FulfillAvg-70418.0/33.0) > 1e-6 {
		t.Errorf("fulfill avg: want %v got %v", 70418.0/33.0, r.FulfillAvg)
	}
	if rep.TotalFulfill != 70418 {
		t.Errorf("total: want 70418 got %v", rep.TotalFulfill)
	}
}

func TestBuildDemandReportFillExcludesOwnOrders(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	// A genuine station order at 50 (qty 5) and a basement player order at 2
	// (qty 100, all the player's own). Selling 30 units should fill the 5 real
	// units @ 50 = 250 and stop — the player's own buy orders are not proceeds.
	deep := []knowledge.MarketBuyOrderRow{
		{StationID: "s", ItemID: "widget", ItemName: "Widget", PriceEach: 50, Quantity: 5, Source: "station", CapturedAt: now},
		{StationID: "s", ItemID: "widget", ItemName: "Widget", PriceEach: 2, Quantity: 100, MyQuantity: 100, Source: "", CapturedAt: now},
	}
	onHand := map[string]float64{"widget": 30}

	rep := buildDemandReport(deep, onHand, nil, now, demandOptions{})
	if len(rep.Rows) != 1 {
		t.Fatalf("want 1 row, got %+v", rep.Rows)
	}
	r := rep.Rows[0]
	if r.FulfillQty != 5 || r.FulfillValue != 250 {
		t.Errorf("fill should exclude own orders: want 5/250 got %v/%v", r.FulfillQty, r.FulfillValue)
	}
}

func TestBuildDemandReportHidePlayerOnly(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	deep := []knowledge.MarketBuyOrderRow{
		// STN
		{StationID: "s1", ItemID: "copper", PriceEach: 8, Quantity: 100, Source: "station", CapturedAt: now},
		// PLR>SM (player order above a station order)
		{StationID: "s2", ItemID: "iron", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: now},
		{StationID: "s2", ItemID: "iron", PriceEach: 12, Quantity: 20, Source: "", CapturedAt: now},
		// PLR (lone player order)
		{StationID: "s3", ItemID: "titanium", PriceEach: 30, Quantity: 40, Source: "", CapturedAt: now},
	}

	rep := buildDemandReport(deep, nil, nil, now, demandOptions{hidePlayerOnly: true, showNoneOnHand: true})
	got := map[string]demandClass{}
	for _, r := range rep.Rows {
		got[r.ItemID] = r.Class
	}
	if _, ok := got["titanium"]; ok {
		t.Errorf("hide-player-only should drop the lone-player (PLR) row, got %+v", rep.Rows)
	}
	if got["copper"] != classStation {
		t.Errorf("hide-player-only must keep STN, got %+v", rep.Rows)
	}
	if got["iron"] != classAboveSM {
		t.Errorf("hide-player-only must keep PLR>SM, got %+v", rep.Rows)
	}
}

func TestBuildDemandReportSkipsWhollyMineByDefault(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	deep := []knowledge.MarketBuyOrderRow{
		// Entirely the player's own order — hidden by default.
		{StationID: "s1", ItemID: "mine_all", PriceEach: 9, Quantity: 30, MyQuantity: 30, Source: "", CapturedAt: now},
		// Partly mine (10 of 25) — still shown, with full quantity.
		{StationID: "s2", ItemID: "mixed", PriceEach: 15, Quantity: 25, MyQuantity: 10, Source: "", CapturedAt: now},
	}

	rep := buildDemandReport(deep, nil, nil, now, demandOptions{showNoneOnHand: true})
	got := map[string]demandReportRow{}
	for _, r := range rep.Rows {
		got[r.ItemID] = r
	}
	if _, ok := got["mine_all"]; ok {
		t.Errorf("wholly-mine row should be hidden by default, got %+v", rep.Rows)
	}
	if r, ok := got["mixed"]; !ok {
		t.Errorf("partly-mine row should still show, got %+v", rep.Rows)
	} else if r.Quantity != 25 || r.MyQuantity != 10 {
		t.Errorf("partly-mine row qty/mine: want 25/10, got %v/%v", r.Quantity, r.MyQuantity)
	}

	// --include-mine brings the wholly-mine row back.
	rep2 := buildDemandReport(deep, nil, nil, now, demandOptions{includeMine: true, showNoneOnHand: true})
	var sawMineAll bool
	for _, r := range rep2.Rows {
		if r.ItemID == "mine_all" {
			sawMineAll = true
		}
	}
	if !sawMineAll {
		t.Errorf("--include-mine should show the wholly-mine row, got %+v", rep2.Rows)
	}
}

func TestBuildDemandReportSkipsNoneOnHandByDefault(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	deep := []knowledge.MarketBuyOrderRow{
		{StationID: "s1", ItemID: "have_it", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: now},
		{StationID: "s2", ItemID: "can_craft", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: now},
		{StationID: "s3", ItemID: "neither", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: now},
	}
	onHand := map[string]float64{"have_it": 5}
	canCraft := map[string]int{"can_craft": 2}

	// Default: the row we can neither fulfill nor craft is hidden.
	rep := buildDemandReport(deep, onHand, canCraft, now, demandOptions{})
	got := map[string]bool{}
	for _, r := range rep.Rows {
		got[r.ItemID] = true
	}
	if got["neither"] {
		t.Errorf("none-onhand/none-craftable row should be hidden by default, got %+v", rep.Rows)
	}
	if !got["have_it"] || !got["can_craft"] {
		t.Errorf("rows with on-hand or craftable must remain, got %+v", rep.Rows)
	}

	// --show-none-onhand brings the hidden row back.
	rep2 := buildDemandReport(deep, onHand, canCraft, now, demandOptions{showNoneOnHand: true})
	var sawNeither bool
	for _, r := range rep2.Rows {
		if r.ItemID == "neither" {
			sawNeither = true
		}
	}
	if !sawNeither {
		t.Errorf("--show-none-onhand should include the row, got %+v", rep2.Rows)
	}
}

func TestBuildDemandReportTieStationWins(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	deep := []knowledge.MarketBuyOrderRow{
		{StationID: "s", ItemID: "x", PriceEach: 10, Quantity: 5, Source: "station", CapturedAt: now},
		{StationID: "s", ItemID: "x", PriceEach: 10, Quantity: 5, Source: "", CapturedAt: now},
	}
	rep := buildDemandReport(deep, nil, nil, now, demandOptions{showNoneOnHand: true})
	if len(rep.Rows) != 1 || rep.Rows[0].Class != classStation {
		t.Fatalf("tie should classify STN (station wins), got %+v", rep.Rows)
	}
}

func TestBuildDemandReportFiltersStalenessAndLimit(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)

	deep := []knowledge.MarketBuyOrderRow{
		{StationID: "s1", ItemID: "a", ItemName: "A", PriceEach: 5, Quantity: 10, Source: "station", CapturedAt: stale},
		{StationID: "s1", ItemID: "b", ItemName: "B", PriceEach: 50, Quantity: 10, Source: "station", CapturedAt: fresh},
	}

	// minPrice filters out item a (price 5).
	rep := buildDemandReport(deep, nil, nil, now, demandOptions{minPrice: 10, showNoneOnHand: true})
	if len(rep.Rows) != 1 || rep.Rows[0].ItemID != "b" {
		t.Fatalf("minPrice filter: want only b, got %+v", rep.Rows)
	}
	// Staleness flag set for the >24h-old row when not filtered out.
	rep2 := buildDemandReport(deep, nil, nil, now, demandOptions{showNoneOnHand: true})
	for _, r := range rep2.Rows {
		if want := r.ItemID == "a"; r.AgeStale != want {
			t.Errorf("item %s stale: want %v got %v", r.ItemID, want, r.AgeStale)
		}
	}
	// limit truncates and TotalFulfill reflects only returned rows.
	onHand := map[string]float64{"a": 10, "b": 10}
	rep3 := buildDemandReport(deep, onHand, nil, now, demandOptions{limit: 1, sort: sortByPrice})
	if len(rep3.Rows) != 1 || rep3.Rows[0].ItemID != "b" {
		t.Fatalf("limit: want only top row b, got %+v", rep3.Rows)
	}
	if rep3.TotalFulfill != 500 { // b: 10 * 50, a excluded by limit
		t.Errorf("total-after-limit: want 500, got %v", rep3.TotalFulfill)
	}
}
