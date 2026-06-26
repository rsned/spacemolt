package worker

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// TestHaulGate covers the pre-buy profitability gate: it re-prices both legs from
// fresh station prices, sizes on the live ask, and requires the live spread to clear
// BOTH the margin and the net-profit floor.
func TestHaulGate(t *testing.T) {
	opp := func(qty float64) market.ArbitrageOpportunity {
		return market.ArbitrageOpportunity{FromStationID: "buyst", ToStationID: "sellst", ItemID: "x", Quantity: qty}
	}
	prices := func(ask, bid float64, hasAsk, hasBid bool) []market.ItemStationPrice {
		return []market.ItemStationPrice{
			{StationID: "buyst", BestAsk: ask, HasSell: hasAsk},
			{StationID: "sellst", BestBid: bid, HasBuy: hasBid},
		}
	}
	const bigCredits = 1e9
	tests := []struct {
		name              string
		opp               market.ArbitrageOpportunity
		prices            []market.ItemStationPrice
		cargoFree, credit float64
		wantOK            bool
		wantReason        string
	}{
		{"fat spread passes", opp(100), prices(100, 111, true, true), 100, bigCredits, true, ""},
		{"inverted spread (trader-3) fails", opp(21), prices(2654, 2631, true, true), 200, bigCredits, false, "spread too thin"},
		{"margin ok but net below floor fails", opp(5), prices(100, 104, true, true), 5, bigCredits, false, "spread too thin"},
		{"no live ask at buy station fails", opp(100), prices(0, 111, false, true), 100, bigCredits, false, "no live ask"},
		{"no live bid at sell station fails", opp(100), prices(100, 0, true, false), 100, bigCredits, false, "no live bid"},
		{"unaffordable fails before margin", opp(100), prices(100, 200, true, true), 100, 50, false, "unaffordable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			qty, _, _, ok, reason := haulGate(tc.opp, tc.prices, tc.cargoFree, tc.credit)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v (reason %q), want %v", ok, reason, tc.wantOK)
			}
			if tc.wantOK && qty < 1 {
				t.Errorf("passing gate returned qty=%.0f, want >=1", qty)
			}
			if !tc.wantOK && !strings.Contains(reason, tc.wantReason) {
				t.Errorf("reason=%q, want contains %q", reason, tc.wantReason)
			}
		})
	}
}

// TestBuildNameToIDResolvesNameAndID covers the live-validated case: market.db
// stores some system_name values as the display name and others as the id form,
// so both must resolve to the system id.
func TestBuildNameToIDResolvesNameAndID(t *testing.T) {
	m := buildNameToID([]knowledge.System{
		{ID: "alpha_centauri", Name: "Alpha Centauri"},
		{ID: "sol", Name: "Sol"},
	})
	if got := m["Alpha Centauri"]; got != "alpha_centauri" {
		t.Errorf("display name: got %q, want alpha_centauri", got)
	}
	if got := m["alpha_centauri"]; got != "alpha_centauri" {
		t.Errorf("id form: got %q, want alpha_centauri", got)
	}
	if got := m["Sol"]; got != "sol" {
		t.Errorf("display name Sol: got %q, want sol", got)
	}
	if got := m["sol"]; got != "sol" {
		t.Errorf("id form sol: got %q, want sol", got)
	}
}

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
	}, "a", n2id, g, 0)
	if len(got) != 2 || got[0].ID != 2 || got[1].ID != 1 {
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
	}, "a", n2id, g, 0)
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
	}, "a", n2id, g, 0)
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
	}, "a", n2id, g, 0)
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
	}, "a", n2id, g, 0)
	if got[0].ID != 2 {
		t.Fatalf("want lower id 2 first, got %v", ids(got))
	}
}

func TestRankDistanceCapDropsFarOpps(t *testing.T) {
	// Line a-b-c-d-e. current=a. opp 1 buys at b (1 jump, low gross), opp 2 buys at e
	// (4 jumps, huge gross). With maxJumps=2 the distant high-gross opp is dropped
	// despite its profit; only the nearby one survives. With no cap (0) the far opp
	// is kept (and, being far higher gross, sorts first).
	g, n2id := graphFor([]string{"a", "b", "c", "d", "e"},
		[2]string{"a", "b"}, [2]string{"b", "c"}, [2]string{"c", "d"}, [2]string{"d", "e"})
	opps := []market.ArbitrageOpportunity{
		opp(1, "b", "c", 100),
		opp(2, "e", "d", 999999),
	}
	capped := RankHaulOpportunities(opps, "a", n2id, g, 2)
	if len(capped) != 1 || capped[0].ID != 1 {
		t.Fatalf("maxJumps=2 should keep only the nearby opp 1, got %v", ids(capped))
	}
	uncapped := RankHaulOpportunities(opps, "a", n2id, g, 0)
	if len(uncapped) != 2 || uncapped[0].ID != 2 {
		t.Fatalf("no cap should keep both with high-gross id 2 first, got %v", ids(uncapped))
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
	claims       map[int]bool                  // id -> claim succeeds
	claimedByAgent []market.ArbitrageOpportunity // returned by GetClaimedByAgent (resume)
	completed    []int
	released     []int
	prices       []market.ItemStationPrice
}

func (f *fakeStore) GetOpportunities(_ context.Context, status string, _ int) ([]market.ArbitrageOpportunity, error) {
	if status != "available" {
		return nil, nil
	}
	return f.available, nil
}
func (f *fakeStore) GetClaimedByAgent(_ context.Context, _ string) ([]market.ArbitrageOpportunity, error) {
	return f.claimedByAgent, nil
}
func (f *fakeStore) ClaimOpportunity(_ context.Context, id int, _ string) (bool, error) {
	return f.claims[id], nil
}
func (f *fakeStore) ReleaseOpportunity(_ context.Context, id int, _ string) (bool, error) {
	f.released = append(f.released, id)
	return true, nil
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

func (f *fakeStore) GetItemStationPrices(_ context.Context, _ string) ([]market.ItemStationPrice, error) {
	return f.prices, nil
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

// TestHaulResumesHeldClaimBeforeClaiming covers the resume path: when the agent already
// holds a claim, Haul finishes that one and never reaches loadAvailable/claim. The held
// opp here has an unresolvable buy system, so it is released (proving resume ran it) —
// the distance cap and the available pool are not consulted.
func TestHaulResumesHeldClaimBeforeClaiming(t *testing.T) {
	f := &fakeStore{
		claimedByAgent: []market.ArbitrageOpportunity{opp(99, "ghost", "x", 500)},
		available:      []market.ArbitrageOpportunity{opp(1, "b", "c", 100)},
		claims:         map[int]bool{1: true},
	}
	fc := &fakeClient{state: &game.State{System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100}}
	kb := &fakeKB{
		systems: []knowledge.System{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		conns:   undirected([2]string{"a", "b"}, [2]string{"b", "c"}),
	}
	if err := Haul(context.Background(), HaulDeps{Client: fc, KB: kb, Market: f, AgentID: "trader-x", Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if len(f.released) != 1 || f.released[0] != 99 {
		t.Fatalf("want released [99] (resumed held claim), got %v", f.released)
	}
	if f.scanned != 0 || len(f.completed) != 0 {
		t.Fatalf("resume must short-circuit before loadAvailable/claim: scanned=%d completed=%v", f.scanned, f.completed)
	}
}

// TestRunClaimedHaulResumesWithGoodsAboard covers a haul resumed after the buy: goods are
// already in the hold, so the buy leg is skipped and it goes straight to sell + complete.
func TestRunClaimedHaulResumesWithGoodsAboard(t *testing.T) {
	o := opp(7, "b", "a", 100) // ItemID iron_ore, sell at "a" (== current system)
	fc := &fakeClient{
		state: &game.State{
			System:  game.SystemData{ID: "a", Name: "A"},
			Fuel:    100,
			MaxFuel: 100,
			Ship:    game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
		},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}}, // current only -> no jumps
	}
	f := &fakeStore{}
	_, n2id := graphFor([]string{"a", "b"}, [2]string{"a", "b"})
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, n2id); err != nil {
		t.Fatal(err)
	}
	if len(f.completed) != 1 || f.completed[0] != 7 {
		t.Fatalf("want completed [7] after resume->sell, got %v", f.completed)
	}
	if !slices.Contains(fc.calls, "sell:iron_ore") {
		t.Fatalf("want sell:iron_ore, got %v", fc.calls)
	}
	for _, c := range fc.calls {
		if strings.HasPrefix(c, "buy:") {
			t.Fatalf("resume must NOT buy a second load, got %v", fc.calls)
		}
	}
}
