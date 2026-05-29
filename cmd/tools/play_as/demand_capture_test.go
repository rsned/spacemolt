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
