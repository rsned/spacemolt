package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// graphFor builds a jump graph + name->id map from undirected system-id pairs,
// treating each id as also its display name capitalized-irrelevant (name==id here).
func graphFor(systems []string, pairs ...[2]string) (navigation.JumpGraph, map[string]string) {
	conns := undirected(pairs...) // from explore_test.go
	g := navigation.JumpGraphFromConnections(conns)
	n2id := map[string]string{}
	for _, s := range systems {
		n2id[s] = s // name == id in tests
	}
	return g, n2id
}

func opp(id int, fromSys, toSys string, gross float64) market.ArbitrageOpportunity {
	return market.ArbitrageOpportunity{
		ID: id, FromSystemName: fromSys, ToSystemName: toSys,
		FromStationID: fromSys + "-stn", ToStationID: toSys + "-stn",
		ItemID: "iron_ore", GrossProfit: gross, Quantity: 10, BuyPrice: 5,
	}
}

func ids(opps []market.ArbitrageOpportunity) []int {
	out := make([]int, len(opps))
	for i, o := range opps {
		out[i] = o.ID
	}
	return out
}

func TestRankProfitDominant(t *testing.T) {
	// a-b-c chain. Two opps both bought at b (1 jump from current a). 200 > 100.
	g, n2id := graphFor([]string{"a", "b", "c"}, [2]string{"a", "b"}, [2]string{"b", "c"})
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{
		opp(1, "b", "c", 100),
		opp(2, "b", "c", 200),
	}, "a", n2id, g)
	if len(got) != 2 || got[0].ID != 2 {
		t.Fatalf("want [2 1] by gross, got %v", ids(got))
	}
}

func TestRankNearTieProximityTiebreak(t *testing.T) {
	// Within 10%: 200 vs 195. opp 1 buys at b (1 jump), opp 2 buys at c (2 jumps).
	// Closer buy wins despite slightly lower gross.
	g, n2id := graphFor([]string{"a", "b", "c", "d"},
		[2]string{"a", "b"}, [2]string{"b", "c"}, [2]string{"c", "d"})
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{
		opp(2, "c", "d", 200),
		opp(1, "b", "d", 195),
	}, "a", n2id, g)
	if got[0].ID != 1 {
		t.Fatalf("want closer buy (id 1) first, got %v", ids(got))
	}
}

func TestRankChainingTiebreak(t *testing.T) {
	// Within 10%, equal jumps to buy (both at b). opp 1 sells at c; opp 3 buys at c,
	// so opp 1's drop-off chains into another opp. opp 2 sells at z (no chain).
	g, n2id := graphFor([]string{"a", "b", "c", "z"},
		[2]string{"a", "b"}, [2]string{"b", "c"}, [2]string{"b", "z"})
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{
		opp(2, "b", "z", 200),
		opp(1, "b", "c", 198),
		opp(3, "c", "z", 50), // the chain target (buys at c)
	}, "a", n2id, g)
	// opp 1 and 2 are in the band (>=180); opp 3 (50) is not. opp 1 chains -> first.
	if got[0].ID != 1 {
		t.Fatalf("want chaining opp (id 1) first, got %v", ids(got))
	}
}

func TestRankSkipsUnresolvedAndUnreachable(t *testing.T) {
	// opp 1 buys at "ghost" (not in name map) -> skipped.
	// opp 2 buys at "island" (no graph edge from a) -> unreachable -> skipped.
	// opp 3 buys at b (reachable) -> kept.
	g, n2id := graphFor([]string{"a", "b", "island"}, [2]string{"a", "b"})
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{
		opp(1, "ghost", "b", 999),
		opp(2, "island", "b", 999),
		opp(3, "b", "a", 100),
	}, "a", n2id, g)
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("want only reachable+resolved id 3, got %v", ids(got))
	}
}

func TestRankDeterministicByID(t *testing.T) {
	// Identical gross+jumps+chain -> lower id first.
	g, n2id := graphFor([]string{"a", "b", "z"}, [2]string{"a", "b"}, [2]string{"b", "z"})
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{
		opp(5, "b", "z", 100),
		opp(2, "b", "z", 100),
	}, "a", n2id, g)
	if got[0].ID != 2 {
		t.Fatalf("want lower id 2 first, got %v", ids(got))
	}
}
