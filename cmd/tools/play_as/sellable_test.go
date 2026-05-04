package main

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestFillItem(t *testing.T) {
	cases := []struct {
		name         string
		cargo        float64
		orders       []serverapi.MarketOrder
		wantQty      float64
		wantProceeds float64
		wantAvg      float64
		wantFills    []sellableFill
	}{
		{
			name:  "zero cargo returns empty",
			cargo: 0,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 0, wantProceeds: 0, wantAvg: 0, wantFills: nil,
		},
		{
			name:         "no buy orders returns empty",
			cargo:        50,
			orders:       nil,
			wantQty:      0,
			wantProceeds: 0,
			wantAvg:      0,
			wantFills:    nil,
		},
		{
			name:  "single buyer exact match",
			cargo: 100,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 100, wantProceeds: 1000, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 100, Proceeds: 1000}},
		},
		{
			name:  "single buyer cargo less than order",
			cargo: 30,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 30, wantProceeds: 300, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 30, Proceeds: 300}},
		},
		{
			name:  "single buyer cargo greater than order — leftover unsold",
			cargo: 200,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 100, wantProceeds: 1000, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 100, Proceeds: 1000}},
		},
		{
			name:  "multi-buyer ladder — cargo exceeds total demand, all consumed, sorted desc",
			cargo: 5000,
			orders: []serverapi.MarketOrder{
				// Intentionally out of order so the test exercises the sort.
				{PriceEach: 14, Quantity: 2246},
				{PriceEach: 26, Quantity: 676},
				{PriceEach: 20, Quantity: 1570},
			},
			// 676*26 + 1570*20 + 2246*14 = 17576 + 31400 + 31444 = 80420; qty = 4492; avg = 17.9029...
			wantQty: 4492, wantProceeds: 80420, wantAvg: 80420.0 / 4492.0,
			wantFills: []sellableFill{
				{Price: 26, Qty: 676, Proceeds: 17576},
				{Price: 20, Qty: 1570, Proceeds: 31400},
				{Price: 14, Qty: 2246, Proceeds: 31444},
			},
		},
		{
			name:  "multi-buyer ladder — cargo exhausts mid-ladder",
			cargo: 1000,
			orders: []serverapi.MarketOrder{
				{PriceEach: 26, Quantity: 676},
				{PriceEach: 20, Quantity: 1570},
				{PriceEach: 14, Quantity: 2246},
			},
			// 676 @ 26 = 17576, then 324 @ 20 = 6480; total 24056; qty 1000; avg 24.056
			wantQty: 1000, wantProceeds: 24056, wantAvg: 24.056,
			wantFills: []sellableFill{
				{Price: 26, Qty: 676, Proceeds: 17576},
				{Price: 20, Qty: 324, Proceeds: 6480},
			},
		},
		{
			name:  "ties at same price keep server order",
			cargo: 200,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 50, Source: "station"},
				{PriceEach: 10, Quantity: 50, Source: "player"},
				{PriceEach: 10, Quantity: 50, Source: "station"},
			},
			wantQty: 150, wantProceeds: 1500, wantAvg: 10,
			wantFills: []sellableFill{
				{Price: 10, Qty: 50, Proceeds: 500},
				{Price: 10, Qty: 50, Proceeds: 500},
				{Price: 10, Qty: 50, Proceeds: 500},
			},
		},
		{
			name:  "skips zero-price and zero-quantity orders defensively",
			cargo: 100,
			orders: []serverapi.MarketOrder{
				{PriceEach: 0, Quantity: 50},
				{PriceEach: 5, Quantity: 0},
				{PriceEach: 10, Quantity: 30},
			},
			wantQty: 30, wantProceeds: 300, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 30, Proceeds: 300}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQty, gotProceeds, gotAvg, gotFills := fillItem(tc.cargo, tc.orders)
			if gotQty != tc.wantQty {
				t.Errorf("sellable_qty = %v, want %v", gotQty, tc.wantQty)
			}
			if gotProceeds != tc.wantProceeds {
				t.Errorf("total_proceeds = %v, want %v", gotProceeds, tc.wantProceeds)
			}
			// Use a small epsilon for the weighted average.
			if abs(gotAvg-tc.wantAvg) > 1e-6 {
				t.Errorf("avg_price = %v, want %v", gotAvg, tc.wantAvg)
			}
			if len(gotFills) != len(tc.wantFills) {
				t.Fatalf("fills len = %d, want %d (got %+v)", len(gotFills), len(tc.wantFills), gotFills)
			}
			for i, f := range gotFills {
				if f != tc.wantFills[i] {
					t.Errorf("fill[%d] = %+v, want %+v", i, f, tc.wantFills[i])
				}
			}
		})
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestBuildSellablePlan(t *testing.T) {
	mkOrder := func(price, qty float64) serverapi.MarketOrder {
		return serverapi.MarketOrder{PriceEach: price, Quantity: qty}
	}

	t.Run("inventory union: cargo only, storage only, both", func(t *testing.T) {
		market := []serverapi.ViewMarketItem{
			{ItemID: "iron_ore", ItemName: "Iron Ore",
				BuyOrders: []serverapi.MarketOrder{mkOrder(10, 1000)}},
		}
		cargo := []storageItem{{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 50}}
		storage := []storageItem{
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 200},
			{ItemID: "carbon_ore", Name: "Carbon Ore", Quantity: 75},
		}
		plan := buildSellablePlan("nova_terra_central", market, cargo, storage)

		if plan.StationID != "nova_terra_central" {
			t.Errorf("station_id = %q, want nova_terra_central", plan.StationID)
		}
		if got, want := len(plan.Items), 2; got != want {
			t.Fatalf("len(plan.Items) = %d, want %d", got, want)
		}
		if plan.Items[0].ItemID != "carbon_ore" {
			t.Errorf("Items[0].ItemID = %q, want carbon_ore", plan.Items[0].ItemID)
		}
		iron := plan.Items[1]
		if iron.Cargo != 50 || iron.Storage != 200 {
			t.Errorf("iron cargo/storage = %v/%v, want 50/200", iron.Cargo, iron.Storage)
		}
		if iron.SellableQty != 50 || iron.TotalProceeds != 500 {
			t.Errorf("iron sellable/proceeds = %v/%v, want 50/500", iron.SellableQty, iron.TotalProceeds)
		}
		carbon := plan.Items[0]
		if carbon.SellableQty != 0 {
			t.Errorf("carbon sellable = %v, want 0", carbon.SellableQty)
		}
	})

	t.Run("name fallback: cargo > storage > market.item_name > item_id", func(t *testing.T) {
		market := []serverapi.ViewMarketItem{
			{ItemID: "x_a", ItemName: "X-A from market"},
			{ItemID: "x_b", ItemName: "X-B from market"},
			{ItemID: "x_c"},
		}
		cargo := []storageItem{
			{ItemID: "x_a", Name: "Cargo Name", Quantity: 1},
			{ItemID: "x_b", Name: "", Quantity: 1},
			{ItemID: "x_c", Name: "", Quantity: 1},
		}
		storage := []storageItem{
			{ItemID: "x_b", Name: "Storage Name", Quantity: 1},
		}
		plan := buildSellablePlan("s", market, cargo, storage)
		want := map[string]string{"x_a": "Cargo Name", "x_b": "Storage Name", "x_c": "x_c"}
		for _, row := range plan.Items {
			if got := row.Name; got != want[row.ItemID] {
				t.Errorf("name for %s = %q, want %q", row.ItemID, got, want[row.ItemID])
			}
		}
	})

	t.Run("duplicate cargo / storage entries are summed", func(t *testing.T) {
		cargo := []storageItem{
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 30},
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 20},
		}
		storage := []storageItem{
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 5},
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 7},
		}
		plan := buildSellablePlan("s", nil, cargo, storage)
		if got, want := len(plan.Items), 1; got != want {
			t.Fatalf("len = %d, want %d", got, want)
		}
		row := plan.Items[0]
		if row.Cargo != 50 || row.Storage != 12 {
			t.Errorf("cargo/storage = %v/%v, want 50/12", row.Cargo, row.Storage)
		}
	})

	t.Run("plan totals roll up", func(t *testing.T) {
		market := []serverapi.ViewMarketItem{
			{ItemID: "a", BuyOrders: []serverapi.MarketOrder{mkOrder(10, 100)}},
			{ItemID: "b", BuyOrders: []serverapi.MarketOrder{mkOrder(5, 50)}},
		}
		cargo := []storageItem{
			{ItemID: "a", Quantity: 10},
			{ItemID: "b", Quantity: 50},
		}
		plan := buildSellablePlan("s", market, cargo, nil)
		if plan.TotalProceeds != 350 {
			t.Errorf("plan.TotalProceeds = %v, want 350", plan.TotalProceeds)
		}
		if plan.ItemCount != 2 {
			t.Errorf("plan.ItemCount = %v, want 2", plan.ItemCount)
		}
	})

	t.Run("no inventory yields empty items slice", func(t *testing.T) {
		plan := buildSellablePlan("s", nil, nil, nil)
		if len(plan.Items) != 0 {
			t.Errorf("Items len = %d, want 0", len(plan.Items))
		}
		if plan.TotalProceeds != 0 || plan.ItemCount != 0 {
			t.Errorf("totals = %v/%v, want 0/0", plan.TotalProceeds, plan.ItemCount)
		}
	})
}
