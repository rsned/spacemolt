package worker

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

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
		name                string
		opp                 market.ArbitrageOpportunity
		prices              []market.ItemStationPrice
		cargoFree, cargoCap float64
		credit              float64
		wantOK              bool
		wantReason          string
	}{
		{"fat spread passes", opp(100), prices(100, 111, true, true), 100, 200, bigCredits, true, ""},
		{"inverted spread (trader-3) fails", opp(21), prices(2654, 2631, true, true), 200, 200, bigCredits, false, "spread too thin"},
		{"margin ok but net below floor fails", opp(5), prices(100, 104, true, true), 5, 200, bigCredits, false, "spread too thin"},
		{"no live ask at buy station fails", opp(100), prices(0, 111, false, true), 100, 200, bigCredits, false, "no live ask"},
		{"no live bid at sell station fails", opp(100), prices(100, 0, true, false), 100, 200, bigCredits, false, "no live bid"},
		{"unaffordable fails before margin", opp(100), prices(100, 200, true, true), 100, 200, 50, false, "unaffordable"},
		// Small holds (capacity < haulSmallHoldCap) clear a reduced net floor so they can
		// close fat-margin trades too small to make 1000cr (the interim pre-cargo-expander fix).
		{"small hold passes reduced net floor", opp(80), prices(100, 105, true, true), 80, 80, bigCredits, true, ""},
		{"small hold still fails below reduced floor", opp(40), prices(100, 105, true, true), 40, 40, bigCredits, false, "spread too thin"},
		{"large hold same trade fails full floor", opp(80), prices(100, 105, true, true), 80, 200, bigCredits, false, "spread too thin"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			qty, _, _, ok, reason := haulGate(tc.opp, tc.prices, tc.cargoFree, tc.cargoCap, tc.credit)
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

func TestRankStabilityBoostPrefersDurable(t *testing.T) {
	// a-b-c-d line. A fresh opp (1 cycle, gross 100) buys 1 jump away; a durable opp
	// (6 cycles → +50% boost, raw gross 96) buys 2 jumps away. Raw gross + proximity
	// would rank the fresh one first; the stability boost (effective 96*1.5=144 vs 100)
	// lifts the durable route above the near-tie band and ranks it first instead.
	g, n2id := graphFor([]string{"a", "b", "c", "d"},
		[2]string{"a", "b"}, [2]string{"b", "c"}, [2]string{"c", "d"})
	fresh := opp(1, "b", "c", 100)
	fresh.CyclesSeen = 1
	durable := opp(2, "c", "d", 96)
	durable.CyclesSeen = 6
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{fresh, durable}, "a", n2id, g, 0)
	if got[0].ID != 2 {
		t.Fatalf("want durable opp 2 first (stability boost), got %v", ids(got))
	}
}

func TestStabilityBoostBoundsAndShape(t *testing.T) {
	cases := []struct {
		cycles int
		want   float64
	}{
		{0, 1.0}, {1, 1.0}, {2, 1.1}, {3, 1.2}, {6, 1.5}, {20, 1.5},
	}
	for _, tc := range cases {
		if got := stabilityBoost(tc.cycles); got != tc.want {
			t.Errorf("stabilityBoost(%d) = %.2f, want %.2f", tc.cycles, got, tc.want)
		}
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
	available      []market.ArbitrageOpportunity
	scanPopulate   []market.ArbitrageOpportunity
	scanned        int
	claims         map[int]bool                  // id -> claim succeeds
	claimedByAgent []market.ArbitrageOpportunity // returned by GetClaimedByAgent (resume)
	completed      []int
	released       []int
	prices         []market.ItemStationPrice
	orders         []market.Order
	recorded       []market.HaulResult
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
func (f *fakeStore) GetStationOrders(_ context.Context, _, _ string) ([]market.Order, error) {
	return f.orders, nil
}
func (f *fakeStore) RecordHaulResult(_ context.Context, r market.HaulResult) error {
	f.recorded = append(f.recorded, r)
	return nil
}

func TestFakeStoreServesStationOrders(t *testing.T) {
	f := &fakeStore{orders: []market.Order{{Side: "buy", PriceEach: 50, Quantity: 4}}}
	got, err := f.GetStationOrders(context.Background(), "stn", "iron_ore")
	if err != nil || len(got) != 1 || got[0].PriceEach != 50 {
		t.Fatalf("want 1 order @50, got %v err=%v", got, err)
	}
}

func TestHaulSellLegPostsCostOrderOnThinDemand(t *testing.T) {
	o := opp(7, "b", "a", 100) // iron_ore, sell station a-stn in current system "a"
	fc := &fakeClient{state: &game.State{
		System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
	}, route: []game.RouteStep{{SystemID: "a", Name: "A"}}}
	// bought 10 @100 = 1000; dest demand only 2 units @50 -> proceeds 100 << floor 900.
	f := &fakeStore{orders: []market.Order{{Side: "buy", PriceEach: 50, Quantity: 2}}}
	m := &haulMetrics{buyPrice: 100, qty: 10}
	deps := HaulDeps{Client: fc, Market: f, AgentID: "t"}
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(fc.calls, func(c string) bool { return strings.HasPrefix(c, "sell:iron_ore") }) {
		t.Fatalf("thin demand must NOT market-sell, got %v", fc.calls)
	}
	if !slices.ContainsFunc(fc.calls, func(c string) bool { return strings.HasPrefix(c, "sell_order:iron_ore") }) {
		t.Fatalf("want a cost-price sell_order, got %v", fc.calls)
	}
	if len(f.completed) != 1 || f.completed[0] != 7 {
		t.Fatalf("cost-order path must complete the claim, got %v", f.completed)
	}
}

func TestHaulSellLegSellsOnHealthyDemand(t *testing.T) {
	o := opp(7, "b", "a", 100)
	fc := &fakeClient{state: &game.State{
		System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
	}, route: []game.RouteStep{{SystemID: "a", Name: "A"}}}
	f := &fakeStore{orders: []market.Order{{Side: "buy", PriceEach: 130, Quantity: 50}}}
	m := &haulMetrics{buyPrice: 100, qty: 10}
	deps := HaulDeps{Client: fc, Market: f, AgentID: "t"}
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(fc.calls, "sell:iron_ore") {
		t.Fatalf("healthy demand must market-sell, got %v", fc.calls)
	}
	if slices.ContainsFunc(fc.calls, func(c string) bool { return strings.HasPrefix(c, "sell_order:iron_ore") }) {
		t.Fatalf("healthy demand must NOT post a cost order, got %v", fc.calls)
	}
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

// TestLiquidateCargo covers clearing leftover cargo: sell into a local buy order when one
// exists, else post a sell order 10% under the best ask; fuel_cells are reserved.
func TestLiquidateCargo(t *testing.T) {
	station := "stn-x"
	mkClient := func(cargo ...game.CargoItem) *fakeClient {
		return &fakeClient{state: &game.State{
			CurrentPOI: station,
			Ship:       game.Ship{Cargo: cargo},
		}}
	}

	t.Run("sells into a local buy order", func(t *testing.T) {
		fc := mkClient(game.CargoItem{ItemID: "iron_ore", Quantity: 10})
		f := &fakeStore{prices: []market.ItemStationPrice{{StationID: station, HasBuy: true, BestBid: 42}}}
		if !liquidateCargo(context.Background(), HaulDeps{Client: fc, Market: f, Out: io.Discard}, io.Discard) {
			t.Fatal("expected liquidation to act")
		}
		if !slices.Contains(fc.calls, "dock") || !slices.Contains(fc.calls, "sell:iron_ore") {
			t.Fatalf("want dock + sell:iron_ore, got %v", fc.calls)
		}
	})

	t.Run("already docked: proceeds without re-docking", func(t *testing.T) {
		fc := mkClient(game.CargoItem{ItemID: "iron_ore", Quantity: 10})
		fc.state.Doc = true
		f := &fakeStore{prices: []market.ItemStationPrice{{StationID: station, HasBuy: true, BestBid: 42}}}
		if !liquidateCargo(context.Background(), HaulDeps{Client: fc, Market: f, Out: io.Discard}, io.Discard) {
			t.Fatal("expected liquidation to act when already docked")
		}
		if slices.Contains(fc.calls, "dock") {
			t.Fatalf("must not re-dock when already docked, got %v", fc.calls)
		}
		if !slices.Contains(fc.calls, "sell:iron_ore") {
			t.Fatalf("want sell:iron_ore, got %v", fc.calls)
		}
	})

	t.Run("posts a sell order 10% under best ask when no buyer", func(t *testing.T) {
		fc := mkClient(game.CargoItem{ItemID: "iron_ore", Quantity: 10})
		f := &fakeStore{prices: []market.ItemStationPrice{{StationID: station, HasSell: true, BestAsk: 100}}}
		if !liquidateCargo(context.Background(), HaulDeps{Client: fc, Market: f, Out: io.Discard}, io.Discard) {
			t.Fatal("expected liquidation to act")
		}
		// 100 * 0.9 = 90.
		if !slices.Contains(fc.calls, "sell_order:iron_ore@90") {
			t.Fatalf("want sell_order:iron_ore@90, got %v", fc.calls)
		}
	})

	t.Run("posts sell order off the best ask at another station", func(t *testing.T) {
		fc := mkClient(game.CargoItem{ItemID: "silver_ore", Quantity: 50})
		fc.state.Doc = true
		// No price entry for the current station; another station lists an ask.
		f := &fakeStore{prices: []market.ItemStationPrice{{StationID: "other-stn", HasSell: true, BestAsk: 60}}}
		if !liquidateCargo(context.Background(), HaulDeps{Client: fc, Market: f, Out: io.Discard}, io.Discard) {
			t.Fatal("expected liquidation via global best ask")
		}
		// 60 * 0.9 = 54.
		if !slices.Contains(fc.calls, "sell_order:silver_ore@54") {
			t.Fatalf("want sell_order:silver_ore@54 (10%% under global ask 60), got %v", fc.calls)
		}
	})

	t.Run("no market price anywhere leaves cargo in hold", func(t *testing.T) {
		fc := mkClient(game.CargoItem{ItemID: "mystery_widget", Quantity: 5})
		fc.state.Doc = true
		f := &fakeStore{prices: nil}
		if liquidateCargo(context.Background(), HaulDeps{Client: fc, Market: f, Out: io.Discard}, io.Discard) {
			t.Fatal("no price reference -> must not act")
		}
		if slices.Contains(fc.calls, "sell:mystery_widget") {
			t.Fatalf("must not sell without a price reference, got %v", fc.calls)
		}
	})

	t.Run("reserves fuel_cell and no-ops empty holds", func(t *testing.T) {
		fc := mkClient(game.CargoItem{ItemID: "fuel_cell", Quantity: 3})
		f := &fakeStore{prices: []market.ItemStationPrice{{StationID: station, HasBuy: true, BestBid: 5}}}
		if liquidateCargo(context.Background(), HaulDeps{Client: fc, Market: f, Out: io.Discard}, io.Discard) {
			t.Fatal("fuel_cell must not be liquidated")
		}
		if slices.Contains(fc.calls, "dock") {
			t.Fatalf("reserved-only hold should not even dock, got %v", fc.calls)
		}
	})

	t.Run("not at a dockable station leaves cargo", func(t *testing.T) {
		fc := mkClient(game.CargoItem{ItemID: "iron_ore", Quantity: 10})
		fc.dockErr = errors.New("No base at this location")
		f := &fakeStore{prices: []market.ItemStationPrice{{StationID: station, HasBuy: true, BestBid: 42}}}
		if liquidateCargo(context.Background(), HaulDeps{Client: fc, Market: f, Out: io.Discard}, io.Discard) {
			t.Fatal("should not act when undockable")
		}
		if slices.Contains(fc.calls, "sell:iron_ore") {
			t.Fatalf("must not sell when dock failed, got %v", fc.calls)
		}
	})
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
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, n2id, nil); err != nil {
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

// TestHaulSellLegRecordsResult covers the haul_results write on completion: with a
// populated *haulMetrics, a completed sell records one row carrying the real cargo-capped
// realized profit (sellPrice-buyPrice)*soldQty, the jump count, and the leg stamps.
func TestHaulSellLegRecordsResult(t *testing.T) {
	o := opp(7, "b", "a", 100) // ItemID iron_ore, sell station "a-stn" in system "a" (== current)
	fc := &fakeClient{
		state: &game.State{
			System:  game.SystemData{ID: "a", Name: "A"},
			Fuel:    100,
			MaxFuel: 100,
			Ship:    game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
		},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}}, // current only -> autopilot no-op
	}
	f := &fakeStore{}
	fixed := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	m := &haulMetrics{
		jumps: 3, buyPrice: 100, sellPrice: 130, qty: 10,
		claimedAt:    fixed.Add(-3 * time.Minute),
		arrivedSrcAt: fixed.Add(-2 * time.Minute),
		boughtAt:     fixed.Add(-90 * time.Second),
		claimedTick:  1000, arrivedSrcTick: 1006, boughtTick: 1007,
	}
	deps := HaulDeps{Client: fc, Market: f, AgentID: "trader-z", Now: func() time.Time { return fixed }}
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m); err != nil {
		t.Fatal(err)
	}
	if len(f.completed) != 1 || f.completed[0] != 7 {
		t.Fatalf("want completed [7], got %v", f.completed)
	}
	if len(f.recorded) != 1 {
		t.Fatalf("want 1 recorded haul result, got %d", len(f.recorded))
	}
	r := f.recorded[0]
	if r.OppID != 7 || r.AgentID != "trader-z" || r.ItemID != "iron_ore" {
		t.Fatalf("identity mismatch: %+v", r)
	}
	if r.RealizedProfit != (130-100)*10 || r.Qty != 10 || r.JumpsTraveled != 3 {
		t.Fatalf("want realized 300 qty 10 jumps 3, got %+v", r)
	}
	if r.ClaimedAt != "2026-06-26T11:57:00Z" || r.SoldAt != "2026-06-26T12:00:00Z" || r.SoldTick != 0 {
		t.Fatalf("leg-stamp mismatch: claimed=%s sold=%s soldTick=%d", r.ClaimedAt, r.SoldAt, r.SoldTick)
	}
}

// TestHaulSellLegRecordsActualSellFill covers the realized-profit accuracy fix: the recorder
// must use the ACTUAL sell fill (from the cached sell response) for SellPriceGot, not the
// pre-trade quote in haulMetrics. Here the gate quoted a sell of 7878 but the market filled
// at 6060 — below the 6330 buy — so the recorded haul is a real loss, which the old
// quote-based recorder could never show.
func TestHaulSellLegRecordsActualSellFill(t *testing.T) {
	o := opp(7, "b", "a", 100) // ItemID iron_ore, sell system "a" (== current)
	fc := &fakeClient{
		state: &game.State{
			System:  game.SystemData{ID: "a", Name: "A"},
			Fuel:    100,
			MaxFuel: 100,
			Ship:    game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 31}}},
		},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}},
		// Real fill: sold 31 @ 6060 = 187860, well below the quoted 7878.
		raw: map[string][]byte{"sell": []byte(`{"action":"sell","quantity_sold":31,"total_earned":187860}`)},
	}
	f := &fakeStore{}
	fixed := time.Date(2026, 6, 27, 14, 43, 0, 0, time.UTC)
	m := &haulMetrics{jumps: 1, buyPrice: 6330, sellPrice: 7878, qty: 31}
	deps := HaulDeps{Client: fc, Market: f, AgentID: "salvager-6", Now: func() time.Time { return fixed }}
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m); err != nil {
		t.Fatal(err)
	}
	if len(f.recorded) != 1 {
		t.Fatalf("want 1 recorded haul result, got %d", len(f.recorded))
	}
	r := f.recorded[0]
	if r.SellPriceGot != 6060 {
		t.Errorf("want actual sell fill 6060 (187860/31), got %v", r.SellPriceGot)
	}
	if r.RealizedProfit != (6060-6330)*31 {
		t.Errorf("want realized -8370 from the real fill (a loss), got %v", r.RealizedProfit)
	}
}

// TestHaulSellLegNilMetricsRecordsNothing covers the resume path: a nil *haulMetrics
// completes the opp but writes no haul_results row (the legs were not measured this run).
func TestHaulSellLegNilMetricsRecordsNothing(t *testing.T) {
	o := opp(7, "b", "a", 100)
	fc := &fakeClient{
		state: &game.State{
			System:  game.SystemData{ID: "a", Name: "A"},
			Fuel:    100,
			MaxFuel: 100,
			Ship:    game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
		},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}},
	}
	f := &fakeStore{}
	if err := haulSellLeg(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, "a", nil); err != nil {
		t.Fatal(err)
	}
	if len(f.completed) != 1 {
		t.Fatalf("want completed [7], got %v", f.completed)
	}
	if len(f.recorded) != 0 {
		t.Fatalf("nil metrics must record nothing, got %d", len(f.recorded))
	}
}
