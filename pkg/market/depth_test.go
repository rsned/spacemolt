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
