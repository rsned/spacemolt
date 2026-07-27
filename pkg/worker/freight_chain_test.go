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

// A detour flown BEFORE any delivery pushes every stop back by the round trip
// through here: 2*detourHops on top of each cumulative bound. Charging it is
// what stops a fly-home-and-return from silently killing the packages still
// aboard.
func TestChainFeasibleAfterDetourChargesTheRoundTrip(t *testing.T) {
	// One stop, 1 hop out: needs 1*19*1.5 = 28.5 ticks with no detour.
	stops := []chainStop{{ContractID: "keep", Hops: 1, DeadlineTick: 100}}
	if ok, reason := chainFeasibleAfterDetour(stops, 0, 0); !ok {
		t.Fatalf("zero detour must match chainFeasible exactly, got %s", reason)
	}
	// A 2-hop detour makes it (2*2+1)*19*1.5 = 142.5 > 100.
	if ok, _ := chainFeasibleAfterDetour(stops, 0, 2); ok {
		t.Fatal("a 2-hop detour blows a 100-tick deadline; must report infeasible")
	}
	// A 1-hop detour is (2*1+1)*19*1.5 = 85.5 <= 100: still affordable.
	if ok, reason := chainFeasibleAfterDetour(stops, 0, 1); !ok {
		t.Fatalf("an affordable detour must not be refused: %s", reason)
	}
}

// --- v0.549.x late-delivery semantics ---------------------------------------
//
// Blowing DeadlineTick now only forfeits the speed bonus/reward; the package
// stays deliverable until RecoveryDeadlineTick for a capped fee. So the
// feasibility gate — whose only job is deciding whether to hand a package
// back — must measure against the RECOVERY deadline.

func TestChainFeasibleMeasuresRecoveryDeadlineNotRewardDeadline(t *testing.T) {
	// Reward deadline already blown, recovery deadline comfortably clear.
	// Nothing here should be returned: it is merely late, not undeliverable.
	stops := []chainStop{{ContractID: "late", Hops: 3, DeadlineTick: 1150, RecoveryDeadlineTick: 4000}}
	if ok, reason := chainFeasible(stops, 1200); !ok {
		t.Fatalf("a merely-late contract must stay feasible, got infeasible: %s", reason)
	}
}

func TestChainFeasibleFailsPastRecoveryDeadline(t *testing.T) {
	// Past the recovery deadline the package really is undeliverable.
	stops := []chainStop{{ContractID: "dead", Hops: 3, DeadlineTick: 1150, RecoveryDeadlineTick: 1210}}
	if ok, _ := chainFeasible(stops, 1200); ok {
		t.Fatal("a contract past its recovery deadline must report infeasible")
	}
}

func TestChainFeasibleFallsBackToDeadlineWithoutRecovery(t *testing.T) {
	// Pre-v0.549 contracts (and board candidates) carry no recovery deadline;
	// the reward deadline remains the only bound we have.
	stops := []chainStop{{ContractID: "old", Hops: 3, DeadlineTick: 1210}}
	if ok, _ := chainFeasible(stops, 1200); ok {
		t.Fatal("with no recovery deadline the reward deadline must still bind")
	}
}

func TestWorstReturnableStopIgnoresMerelyLateContract(t *testing.T) {
	// A contract past its reward deadline but inside recovery must never be
	// nominated for return — delivering late beats handing it back.
	stops := []chainStop{{ContractID: "late", Hops: 3, DeadlineTick: 1150, RecoveryDeadlineTick: 4000}}
	if _, found := freightWorstReturnableStop(stops, 1200, nil); found {
		t.Fatal("merely-late contract nominated for return")
	}
}

func TestWorstReturnableStopNominatesPastRecovery(t *testing.T) {
	stops := []chainStop{
		{ContractID: "healthy", Hops: 1, DeadlineTick: 9000, RecoveryDeadlineTick: 9000},
		{ContractID: "dead", Hops: 3, DeadlineTick: 1150, RecoveryDeadlineTick: 1205},
	}
	got, found := freightWorstReturnableStop(stops, 1200, nil)
	if !found {
		t.Fatal("contract past recovery deadline not nominated")
	}
	if got.ContractID != "dead" {
		t.Fatalf("nominated %s, want dead", got.ContractID)
	}
}
