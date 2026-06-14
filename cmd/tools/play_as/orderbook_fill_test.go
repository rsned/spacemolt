package main

import "testing"

func TestFillOrderBook(t *testing.T) {
	cases := []struct {
		name         string
		supply       float64
		rungs        []orderRung
		wantQty      float64
		wantProceeds float64
		wantAvg      float64
		wantFills    []sellableFill
	}{
		{
			name:    "zero supply returns empty",
			supply:  0,
			rungs:   []orderRung{{Price: 10, Qty: 100}},
			wantQty: 0, wantProceeds: 0, wantAvg: 0, wantFills: nil,
		},
		{
			name:    "no rungs returns empty",
			supply:  50,
			rungs:   nil,
			wantQty: 0, wantProceeds: 0, wantAvg: 0, wantFills: nil,
		},
		{
			name:   "walks highest-price-first, exhausts mid-ladder",
			supply: 1000,
			rungs: []orderRung{
				{Price: 14, Qty: 2246},
				{Price: 26, Qty: 676},
				{Price: 20, Qty: 1570},
			},
			// 676@26 = 17576, then 324@20 = 6480; total 24056; qty 1000.
			wantQty: 1000, wantProceeds: 24056, wantAvg: 24.056,
			wantFills: []sellableFill{
				{Price: 26, Qty: 676, Proceeds: 17576},
				{Price: 20, Qty: 324, Proceeds: 6480},
			},
		},
		{
			name:   "skips non-positive price/qty defensively",
			supply: 100,
			rungs: []orderRung{
				{Price: 0, Qty: 50},
				{Price: 5, Qty: 0},
				{Price: 10, Qty: 30},
			},
			wantQty: 30, wantProceeds: 300, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 30, Proceeds: 300}},
		},
		{
			// The bug scenario: a tiny top order followed by a deep cheap rung.
			// Naive top_price*qty would report 33*9978 = 329,274; the real
			// ladder walk yields 70,418.
			name:   "pulse_laser_iii skewed ladder",
			supply: 33,
			rungs: []orderRung{
				{Price: 9978, Qty: 1},
				{Price: 9854, Qty: 2},
				{Price: 9158, Qty: 1},
				{Price: 5373, Qty: 1},
				{Price: 5306, Qty: 4},
				{Price: 4931, Qty: 1},
				{Price: 2, Qty: 169},
			},
			// 9978 + 2*9854 + 9158 + 5373 + 4*5306 + 4931 + 23*2 = 70418; qty 33.
			wantQty: 33, wantProceeds: 70418, wantAvg: 70418.0 / 33.0,
			wantFills: []sellableFill{
				{Price: 9978, Qty: 1, Proceeds: 9978},
				{Price: 9854, Qty: 2, Proceeds: 19708},
				{Price: 9158, Qty: 1, Proceeds: 9158},
				{Price: 5373, Qty: 1, Proceeds: 5373},
				{Price: 5306, Qty: 4, Proceeds: 21224},
				{Price: 4931, Qty: 1, Proceeds: 4931},
				{Price: 2, Qty: 23, Proceeds: 46},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQty, gotProceeds, gotAvg, gotFills := fillOrderBook(tc.supply, tc.rungs)
			if gotQty != tc.wantQty {
				t.Errorf("qty = %v, want %v", gotQty, tc.wantQty)
			}
			if gotProceeds != tc.wantProceeds {
				t.Errorf("proceeds = %v, want %v", gotProceeds, tc.wantProceeds)
			}
			if abs(gotAvg-tc.wantAvg) > 1e-6 {
				t.Errorf("avg = %v, want %v", gotAvg, tc.wantAvg)
			}
			if len(gotFills) != len(tc.wantFills) {
				t.Fatalf("fills len = %d, want %d (%+v)", len(gotFills), len(tc.wantFills), gotFills)
			}
			for i, f := range gotFills {
				if f != tc.wantFills[i] {
					t.Errorf("fill[%d] = %+v, want %+v", i, f, tc.wantFills[i])
				}
			}
		})
	}
}
