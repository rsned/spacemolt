package main

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestParseStationBuyOrders(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	// Compact response: complete buy_orders per item, source "station" or null.
	raw := []byte(`{"items":[
		{"item_id":"iron_ore","item_name":"Iron Ore","buy_orders":[
			{"price_each":10,"quantity":50,"source":"station"},
			{"price_each":12,"quantity":20,"source":null}
		]},
		{"item_id":"copper","item_name":"Copper","buy_orders":[
			{"price_each":8,"quantity":100,"source":"station"},
			{"price_each":0,"quantity":5,"source":"station"}
		]},
		{"item_id":"junk","item_name":"Junk","buy_orders":[]}
	]}`)

	rows := parseStationBuyOrders(raw, "stn1", "sys1", now)
	if len(rows) != 3 { // 2 iron + 1 copper (zero-price copper + empty junk skipped)
		t.Fatalf("want 3 orders, got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.StationID != "stn1" || r.SystemID != "sys1" || !r.CapturedAt.Equal(now) {
			t.Errorf("row metadata wrong: %+v", r)
		}
	}
	// Null source becomes "".
	var nullSrc bool
	for _, r := range rows {
		if r.ItemID == "iron_ore" && r.PriceEach == 12 {
			if r.Source != "" {
				t.Errorf("null source: want empty string, got %q", r.Source)
			}
			nullSrc = true
		}
	}
	if !nullSrc {
		t.Error("expected the price-12 iron order to be present with empty source")
	}
}

func TestParseStationBuyOrdersEmpty(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	if got := parseStationBuyOrders(nil, "stn1", "sys1", now); got != nil {
		t.Errorf("empty raw: want nil, got %+v", got)
	}
	if got := parseStationBuyOrders([]byte(`{"items":[]}`), "", "sys1", now); got != nil {
		t.Errorf("empty station: want nil, got %+v", got)
	}
}

func TestAggregateDemandHistory(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 34, 56, 0, time.UTC)
	orders := []knowledge.MarketBuyOrderRow{
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 10, Quantity: 50, Source: "station"},
		{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore", PriceEach: 12, Quantity: 20, Source: ""},
		{StationID: "stn1", SystemID: "sys1", ItemID: "copper", ItemName: "Copper", PriceEach: 8, Quantity: 100, Source: "station"},
		{StationID: "stn1", SystemID: "sys1", ItemID: "copper", ItemName: "Copper", PriceEach: 0, Quantity: 5, Source: "station"}, // skipped
	}
	got := aggregateDemandHistory(orders, now, time.Hour)
	if len(got) != 2 {
		t.Fatalf("want 2 samples (iron, copper), got %d", len(got))
	}
	iron := got[0]
	if iron.ItemID != "iron_ore" {
		t.Fatalf("expected iron first (insertion order), got %s", iron.ItemID)
	}
	if iron.BestPrice != 12 || iron.TotalQty != 70 {
		t.Errorf("iron aggregate wrong: best=%v total=%v", iron.BestPrice, iron.TotalQty)
	}
	if iron.SMBestPrice != 10 || iron.SMQty != 50 {
		t.Errorf("iron SM split wrong: smBest=%v smQty=%v", iron.SMBestPrice, iron.SMQty)
	}
	if iron.OrderCount != 2 {
		t.Errorf("iron order count: want 2, got %d", iron.OrderCount)
	}
	want := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if !iron.BucketAt.Equal(want) {
		t.Errorf("bucket not hour-truncated: want %v got %v", want, iron.BucketAt)
	}
	if !iron.CapturedAt.Equal(now) {
		t.Errorf("captured should be now: %v", iron.CapturedAt)
	}
	copper := got[1]
	if copper.OrderCount != 1 || copper.TotalQty != 100 {
		t.Errorf("copper aggregate wrong (zero-price order must be skipped): %+v", copper)
	}
}

func TestIsFresh(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if !isFresh(now.Add(-2*time.Minute), now, 5*time.Minute) {
		t.Error("2 min ago should be fresh within a 5 min window")
	}
	if isFresh(now.Add(-10*time.Minute), now, 5*time.Minute) {
		t.Error("10 min ago should be stale within a 5 min window")
	}
	if isFresh(now.Add(-5*time.Minute), now, 5*time.Minute) {
		t.Error("exactly 5 min should be stale (strictly-less window)")
	}
}
