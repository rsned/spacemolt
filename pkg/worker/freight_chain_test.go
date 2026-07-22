package worker

import "testing"

func TestChainOrderNearestFirstDeadlineTiebreak(t *testing.T) {
	got := chainOrder([]chainStop{
		{ContractID: "c", Hops: 5, DeadlineTick: 100},
		{ContractID: "a", Hops: 2, DeadlineTick: 900},
		{ContractID: "b", Hops: 5, DeadlineTick: 50},
	})
	want := []string{"a", "b", "c"} // 2 first; ties on 5 by tighter deadline
	for i, s := range got {
		if s.ContractID != want[i] {
			t.Fatalf("order[%d] = %s, want %s", i, s.ContractID, want[i])
		}
	}
}

func TestChainCumulativeRoundTripBound(t *testing.T) {
	cum := chainCumulative([]chainStop{{Hops: 2}, {Hops: 5}, {Hops: 6}})
	// cum_1 = 2; cum_2 = 2*2+5 = 9; cum_3 = 2*(2+5)+6 = 20
	want := []int{2, 9, 20}
	for i := range want {
		if cum[i] != want[i] {
			t.Fatalf("cum[%d] = %d, want %d", i, cum[i], want[i])
		}
	}
}

func TestChainFeasibleFailsOnLaterStop(t *testing.T) {
	// Stop b sits at cumulative 9 hops -> needs 9*19*1.5 = 256.5 ticks.
	stops := []chainStop{
		{ContractID: "a", Hops: 2, DeadlineTick: 1000},
		{ContractID: "b", Hops: 5, DeadlineTick: 250}, // 250 < 256.5 -> infeasible
	}
	if ok, _ := chainFeasible(stops, 0); ok {
		t.Fatal("chain with a blown later-stop deadline reported feasible")
	}
	stops[1].DeadlineTick = 300 // 300 >= 256.5 -> feasible
	if ok, reason := chainFeasible(stops, 0); !ok {
		t.Fatalf("healthy chain reported infeasible: %s", reason)
	}
}

func TestChainFeasibleSkipsZeroDeadline(t *testing.T) {
	// Pre-accept candidates carry DeadlineTick 0 (server sets it at accept);
	// their own check is deferred to freightAccept.
	stops := []chainStop{{ContractID: "cand", Hops: 50, DeadlineTick: 0}}
	if ok, _ := chainFeasible(stops, 0); !ok {
		t.Fatal("zero-deadline stop must not fail feasibility")
	}
}

func TestChainMarginalHops(t *testing.T) {
	held := []chainStop{{ContractID: "h", Hops: 5, DeadlineTick: 1000}}
	// with cand h=2: order [2,5], total = 2*2+5 = 9; without = 5; marginal 4.
	if got := chainMarginalHops(held, chainStop{ContractID: "c", Hops: 2}); got != 4 {
		t.Fatalf("marginal = %d, want 4", got)
	}
	// empty held degenerates to the candidate's own hops (v1 pricing).
	if got := chainMarginalHops(nil, chainStop{ContractID: "c", Hops: 7}); got != 7 {
		t.Fatalf("marginal on empty held = %d, want 7", got)
	}
}
