package main

import (
	"testing"
	"time"
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
