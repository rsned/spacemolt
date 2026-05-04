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
			// 676*26 + 1570*20 + 2246*14 = 17576 + 31400 + 31444 = 80420; qty = 4492; avg = 17.9074...
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
