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

	rep := buildDemandReport(deep, onHand, canCraft, now, demandOptions{sort: sortByPrice})

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
	if byItem["iron_ore"].FulfillQty != 30 || byItem["iron_ore"].FulfillValue != 360 {
		t.Errorf("iron fulfill: want 30/360 got %v/%v", byItem["iron_ore"].FulfillQty, byItem["iron_ore"].FulfillValue)
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

	rep := buildDemandReport(deep, nil, nil, now, demandOptions{hidePlayerOnly: true})
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

	rep := buildDemandReport(deep, nil, nil, now, demandOptions{})
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
	rep2 := buildDemandReport(deep, nil, nil, now, demandOptions{includeMine: true})
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

func TestBuildDemandReportTieStationWins(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	deep := []knowledge.MarketBuyOrderRow{
		{StationID: "s", ItemID: "x", PriceEach: 10, Quantity: 5, Source: "station", CapturedAt: now},
		{StationID: "s", ItemID: "x", PriceEach: 10, Quantity: 5, Source: "", CapturedAt: now},
	}
	rep := buildDemandReport(deep, nil, nil, now, demandOptions{})
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
	rep := buildDemandReport(deep, nil, nil, now, demandOptions{minPrice: 10})
	if len(rep.Rows) != 1 || rep.Rows[0].ItemID != "b" {
		t.Fatalf("minPrice filter: want only b, got %+v", rep.Rows)
	}
	// Staleness flag set for the >24h-old row when not filtered out.
	rep2 := buildDemandReport(deep, nil, nil, now, demandOptions{})
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
