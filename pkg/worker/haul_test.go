package worker

import (
	"context"
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

func TestSizeBuyQuantityLimited(t *testing.T) {
	o := market.ArbitrageOpportunity{Quantity: 10}
	// Plenty of cargo and credits -> capped by opp quantity.
	if got := sizeBuy(o, 100, 100000, 5); got != 10 {
		t.Fatalf("want 10, got %v", got)
	}
}

func TestSizeBuyCargoLimited(t *testing.T) {
	o := market.ArbitrageOpportunity{Quantity: 10}
	if got := sizeBuy(o, 4, 100000, 5); got != 4 {
		t.Fatalf("want 4 (cargo), got %v", got)
	}
}

func TestSizeBuyCreditLimited(t *testing.T) {
	o := market.ArbitrageOpportunity{Quantity: 10}
	// 23 credits / 5 each = floor 4.
	if got := sizeBuy(o, 100, 23, 5); got != 4 {
		t.Fatalf("want 4 (credits), got %v", got)
	}
}

func TestSizeBuyZeroAsk(t *testing.T) {
	o := market.ArbitrageOpportunity{Quantity: 10}
	if got := sizeBuy(o, 100, 100, 0); got != 0 {
		t.Fatalf("want 0 on non-positive ask, got %v", got)
	}
}

type fakeStore struct {
	available    []market.ArbitrageOpportunity
	scanPopulate []market.ArbitrageOpportunity
	scanned      int
	claims       map[int]bool // id -> claim succeeds
	completed    []int
}

func (f *fakeStore) GetOpportunities(_ context.Context, status string, _ int) ([]market.ArbitrageOpportunity, error) {
	if status != "available" {
		return nil, nil
	}
	return f.available, nil
}
func (f *fakeStore) ClaimOpportunity(_ context.Context, id int, _ string) (bool, error) {
	return f.claims[id], nil
}
func (f *fakeStore) CompleteOpportunity(_ context.Context, id int, _ string) (bool, error) {
	f.completed = append(f.completed, id)
	return true, nil
}
func (f *fakeStore) ScanArbitrage(_ context.Context, _ market.ScanOptions) (market.ScanResult, error) {
	f.scanned++
	f.available = f.scanPopulate
	return market.ScanResult{}, nil
}

func TestLoadAvailableNonEmptyNoScan(t *testing.T) {
	f := &fakeStore{available: []market.ArbitrageOpportunity{opp(1, "b", "c", 100)}}
	got, err := loadAvailable(context.Background(), f, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || f.scanned != 0 {
		t.Fatalf("want 1 opp and no scan, got %d opps scanned=%d", len(got), f.scanned)
	}
}

func TestLoadAvailableEmptyTriggersScan(t *testing.T) {
	f := &fakeStore{
		available:    nil,
		scanPopulate: []market.ArbitrageOpportunity{opp(7, "b", "c", 100)},
	}
	got, err := loadAvailable(context.Background(), f, 50)
	if err != nil {
		t.Fatal(err)
	}
	if f.scanned != 1 || len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("want 1 scan + opp 7, got scanned=%d opps=%v", f.scanned, ids(got))
	}
}

func TestClaimBestFirstWins(t *testing.T) {
	f := &fakeStore{claims: map[int]bool{1: true, 2: true}}
	ranked := []market.ArbitrageOpportunity{opp(1, "b", "c", 100), opp(2, "b", "c", 90)}
	got, ok, err := claimBest(context.Background(), f, ranked, "hauler-1")
	if err != nil || !ok || got.ID != 1 {
		t.Fatalf("want claim id 1, got id=%d ok=%v err=%v", got.ID, ok, err)
	}
}

func TestClaimBestRaceFallthrough(t *testing.T) {
	// id 1 already taken (false), id 2 succeeds.
	f := &fakeStore{claims: map[int]bool{1: false, 2: true}}
	ranked := []market.ArbitrageOpportunity{opp(1, "b", "c", 100), opp(2, "b", "c", 90)}
	got, ok, err := claimBest(context.Background(), f, ranked, "hauler-1")
	if err != nil || !ok || got.ID != 2 {
		t.Fatalf("want fallthrough to id 2, got id=%d ok=%v err=%v", got.ID, ok, err)
	}
}

func TestClaimBestAllTaken(t *testing.T) {
	f := &fakeStore{claims: map[int]bool{1: false, 2: false}}
	ranked := []market.ArbitrageOpportunity{opp(1, "b", "c", 100), opp(2, "b", "c", 90)}
	_, ok, err := claimBest(context.Background(), f, ranked, "hauler-1")
	if err != nil || ok {
		t.Fatalf("want ok=false when all taken, got ok=%v err=%v", ok, err)
	}
}
