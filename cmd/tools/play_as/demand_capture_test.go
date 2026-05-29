package main

import (
	"testing"
	"time"
)

func TestParseDemandRowsFromCompact(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	raw := []byte(`{"items":[
		{"item_id":"iron_ore","item_name":"Iron Ore","best_buy":10.5,"buy_quantity":100},
		{"item_id":"copper","item_name":"Copper","buy_price":8,"buy_quantity":40},
		{"item_id":"junk","item_name":"Junk","best_buy":0,"buy_quantity":0}
	]}`)

	rows := parseDemandRows(raw, "stn1", "sys1", now)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (junk skipped), got %d: %+v", len(rows), rows)
	}
	byItem := map[string]float64{}
	for _, r := range rows {
		byItem[r.ItemID] = r.BestBuyPrice
		if r.StationID != "stn1" || r.SystemID != "sys1" || !r.CapturedAt.Equal(now) {
			t.Errorf("row metadata wrong: %+v", r)
		}
	}
	if byItem["iron_ore"] != 10.5 {
		t.Errorf("iron price: want 10.5 got %v", byItem["iron_ore"])
	}
	if byItem["copper"] != 8 { // falls back to buy_price when best_buy is 0
		t.Errorf("copper price: want 8 got %v", byItem["copper"])
	}
}

func TestParseDeepOrders(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	// Single-item view_market response: items[0].buy_orders carries source.
	raw := []byte(`{"items":[{"item_id":"iron_ore","item_name":"Iron Ore","buy_orders":[
		{"price_each":10,"quantity":50,"source":"station"},
		{"price_each":12,"quantity":20,"source":"player"},
		{"price_each":0,"quantity":5,"source":"player"}
	]}]}`)

	rows := parseDeepOrders(raw, "stn1", "sys1", "iron_ore", now)
	if len(rows) != 2 { // zero-price order skipped
		t.Fatalf("want 2 deep orders, got %d: %+v", len(rows), rows)
	}
	if rows[0].Source != "station" || rows[0].PriceEach != 10 || rows[0].Quantity != 50 {
		t.Errorf("row0 wrong: %+v", rows[0])
	}
	if rows[1].ItemName != "Iron Ore" || rows[1].StationID != "stn1" {
		t.Errorf("row1 metadata wrong: %+v", rows[1])
	}
}
