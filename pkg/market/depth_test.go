package market

import (
	"math"
	"testing"
)

func TestCostToAcquire(t *testing.T) {
	tests := []struct {
		name                       string
		asks                       []AskLevel
		qty                        float64
		wantCost, wantFill, wantAvg float64
		wantEnough                 bool
	}{
		{"empty book", nil, 10, 0, 0, 0, false},
		{"single level exact", []AskLevel{{10, 5}}, 5, 50, 5, 10, true},
		{"single level partial", []AskLevel{{10, 5}}, 3, 30, 3, 10, true},
		{"thin book underfills", []AskLevel{{10, 2}}, 5, 20, 2, 10, false},
		{"walks up the ladder", []AskLevel{{10, 2}, {20, 2}, {2000, 100}}, 5, 10*2 + 20*2 + 2000*1, 5, (20 + 40 + 2000) / 5, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cost, fill, avg, enough := CostToAcquire(tc.asks, tc.qty)
			if math.Abs(cost-tc.wantCost) > 1e-6 || math.Abs(fill-tc.wantFill) > 1e-6 ||
				math.Abs(avg-tc.wantAvg) > 1e-6 || enough != tc.wantEnough {
				t.Fatalf("CostToAcquire(%v,%v) = (%v,%v,%v,%v), want (%v,%v,%v,%v)",
					tc.asks, tc.qty, cost, fill, avg, enough, tc.wantCost, tc.wantFill, tc.wantAvg, tc.wantEnough)
			}
		})
	}
}

func TestConsumeAsks(t *testing.T) {
	tests := []struct {
		name string
		asks []AskLevel
		qty  float64
		want []AskLevel
	}{
		{"empty ladder", nil, 10, nil},
		{"qty zero unchanged", []AskLevel{{10, 5}, {20, 3}}, 0, []AskLevel{{10, 5}, {20, 3}}},
		{"qty negative unchanged", []AskLevel{{10, 5}}, -1, []AskLevel{{10, 5}}},
		{"partial consume of first level", []AskLevel{{10, 5}, {20, 3}}, 2, []AskLevel{{10, 3}, {20, 3}}},
		{"exact level boundary drops it", []AskLevel{{10, 5}, {20, 3}}, 5, []AskLevel{{20, 3}}},
		{"multi-level consume", []AskLevel{{10, 5}, {20, 3}}, 6, []AskLevel{{20, 2}}},
		{"qty exceeds total depth", []AskLevel{{10, 5}, {20, 3}}, 100, nil},
		{"qty exactly total depth", []AskLevel{{10, 5}, {20, 3}}, 8, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Guard against input mutation: snapshot the backing array.
			orig := append([]AskLevel(nil), tc.asks...)
			got := ConsumeAsks(tc.asks, tc.qty)
			if len(got) != len(tc.want) {
				t.Fatalf("ConsumeAsks(%v,%v) len = %d (%v), want %d (%v)",
					tc.asks, tc.qty, len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if math.Abs(got[i].PriceEach-tc.want[i].PriceEach) > 1e-9 ||
					math.Abs(got[i].Quantity-tc.want[i].Quantity) > 1e-9 {
					t.Fatalf("ConsumeAsks(%v,%v)[%d] = %+v, want %+v", tc.asks, tc.qty, i, got[i], tc.want[i])
				}
			}
			for i := range orig {
				if tc.asks[i] != orig[i] {
					t.Fatalf("ConsumeAsks mutated input at %d: %+v, was %+v", i, tc.asks[i], orig[i])
				}
			}
		})
	}
}

// The scanner used to value a route as (bestBid - bestAsk) * qty, i.e. it
// assumed every unit clears at the TOP of the book. Live on 2026-09-20,
// opportunity #1224069 advertised platinum_ore Nova Terra -> Ironlight at
// 140 -> 350 for 2,461 units and 516,810 gross. The destination's real bid
// ladder was 252x1366, 175x3000, 105x2951: the 350 did not exist at all, and
// hauler-0's 1,900-unit load realised 171,682 -- 43% of the advertised
// pro-rata. Past ~4,366 units the bids fall to 105, BELOW the 140 ask, so
// further volume loses money while still counting as "profit" to the scanner.
func TestProceedsFromSale(t *testing.T) {
	bids := []BidLevel{{252, 1366}, {175, 3000}, {105, 2951}}

	t.Run("walks the ladder highest-first", func(t *testing.T) {
		rev, filled, avg, enough := ProceedsFromSale(bids, 1900)
		if want := 1366*252.0 + 534*175.0; rev != want {
			t.Errorf("revenue = %v, want %v", rev, want)
		}
		if filled != 1900 {
			t.Errorf("filled = %v, want 1900", filled)
		}
		if !enough {
			t.Error("enoughDepth = false, want true")
		}
		if avg <= 175 || avg >= 252 {
			t.Errorf("avg %v should sit between the two levels consumed", avg)
		}
	})

	t.Run("reports shallow depth", func(t *testing.T) {
		_, filled, _, enough := ProceedsFromSale(bids, 99999)
		if enough {
			t.Error("enoughDepth = true on a ladder that cannot fill")
		}
		if want := 1366 + 3000 + 2951.0; filled != want {
			t.Errorf("filled = %v, want the whole ladder %v", filled, want)
		}
	})

	t.Run("empty ladder yields nothing", func(t *testing.T) {
		rev, filled, avg, enough := ProceedsFromSale(nil, 100)
		if rev != 0 || filled != 0 || avg != 0 || enough {
			t.Errorf("got (%v,%v,%v,%v), want all zero/false", rev, filled, avg, enough)
		}
	})
}

// OptimalArbitrage is the fix: walk both ladders together and stop at the unit
// where the bid no longer beats the ask. It must never report a quantity whose
// marginal unit loses money, and its profit must be the REALISABLE profit.
func TestOptimalArbitrage(t *testing.T) {
	t.Run("stops where the spread closes", func(t *testing.T) {
		// asks 140 deep; bids fall through the ask price at the 105 level.
		asks := []AskLevel{{140, 111778}}
		bids := []BidLevel{{252, 1366}, {175, 3000}, {105, 2951}}

		qty, cost, rev, profit := OptimalArbitrage(asks, bids)

		if want := 1366 + 3000.0; qty != want {
			t.Errorf("qty = %v, want %v (the 105 bid is below the 140 ask)", qty, want)
		}
		if wantCost := 4366 * 140.0; cost != wantCost {
			t.Errorf("cost = %v, want %v", cost, wantCost)
		}
		if wantRev := 1366*252.0 + 3000*175.0; rev != wantRev {
			t.Errorf("revenue = %v, want %v", rev, wantRev)
		}
		if profit != rev-cost {
			t.Errorf("profit %v != revenue-cost %v", profit, rev-cost)
		}
		// The old top-of-book maths would have claimed far more than this.
		if naive := (252 - 140) * qty; profit >= naive {
			t.Errorf("depth-aware profit %v should be below the naive %v", profit, naive)
		}
	})

	t.Run("rising asks also close the spread", func(t *testing.T) {
		asks := []AskLevel{{100, 50}, {300, 500}}
		bids := []BidLevel{{200, 1000}}
		qty, _, _, profit := OptimalArbitrage(asks, bids)
		if qty != 50 {
			t.Errorf("qty = %v, want 50 (the 300 ask exceeds the 200 bid)", qty)
		}
		if want := 50 * (200 - 100.0); profit != want {
			t.Errorf("profit = %v, want %v", profit, want)
		}
	})

	t.Run("no profitable overlap", func(t *testing.T) {
		qty, cost, rev, profit := OptimalArbitrage(
			[]AskLevel{{500, 100}}, []BidLevel{{400, 100}})
		if qty != 0 || cost != 0 || rev != 0 || profit != 0 {
			t.Errorf("got (%v,%v,%v,%v), want all zero when the bid is under the ask", qty, cost, rev, profit)
		}
	})

	t.Run("empty ladders are safe", func(t *testing.T) {
		if q, _, _, _ := OptimalArbitrage(nil, []BidLevel{{10, 5}}); q != 0 {
			t.Errorf("qty = %v with no asks, want 0", q)
		}
		if q, _, _, _ := OptimalArbitrage([]AskLevel{{10, 5}}, nil); q != 0 {
			t.Errorf("qty = %v with no bids, want 0", q)
		}
	})
}
