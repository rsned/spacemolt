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
