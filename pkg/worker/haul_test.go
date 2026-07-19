package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
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
			qty, _, _, ok, reason := haulGate(tc.opp, tc.prices, tc.cargoFree, tc.cargoCap, tc.credit, 0)
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

// TestHaulGateRejectsOnFuel covers the net-of-fuel gate term: a spread that clears
// the net-profit floor at fuelCost 0 is rejected once the haul-leg fuel cost eats
// into the margin.
func TestHaulGateRejectsOnFuel(t *testing.T) {
	// qty=100, ask=100, bid=110 -> spread net = (110-100)*100 = 1000 (== floor, passes at fuelCost 0).
	opp := market.ArbitrageOpportunity{FromStationID: "buyst", ToStationID: "sellst", ItemID: "x", Quantity: 100}
	prices := []market.ItemStationPrice{
		{StationID: "buyst", HasSell: true, BestAsk: 100},
		{StationID: "sellst", HasBuy: true, BestBid: 110},
	}
	// fuelCost 0 -> passes.
	if _, _, _, ok, _ := haulGate(opp, prices, 100, 100, 1_000_000, 0); !ok {
		t.Fatal("fuelCost=0: expected pass")
	}
	// fuelCost 200 -> net 800 < floor 1000 -> reject.
	if _, _, _, ok, reason := haulGate(opp, prices, 100, 100, 1_000_000, 200); ok {
		t.Fatalf("fuelCost=200: expected reject, got pass")
	} else if reason == "" {
		t.Fatal("expected a rejection reason")
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

// TestBuildStrongholdRefsDualKeyed verifies stronghold systems are indexed under
// both their id and display name (so an arbitrage row referencing either form
// matches), and non-strongholds are absent.
func TestBuildStrongholdRefsDualKeyed(t *testing.T) {
	refs := buildStrongholdRefs([]knowledge.System{
		{ID: "bellatrix", Name: "Bellatrix", IsStronghold: true},
		{ID: "sol", Name: "Sol", IsStronghold: false},
	})
	if !refs["bellatrix"] || !refs["Bellatrix"] {
		t.Errorf("stronghold not dual-keyed: %v", refs)
	}
	if refs["sol"] || refs["Sol"] {
		t.Errorf("non-stronghold Sol should be absent: %v", refs)
	}
}

// TestDropStrongholdOpps covers the Crix incident: an opportunity whose sell- or
// buy-system is a stronghold is dropped (by either name or id reference), safe
// routes survive, and an empty stronghold set is a no-op.
func TestDropStrongholdOpps(t *testing.T) {
	refs := buildStrongholdRefs([]knowledge.System{
		{ID: "bellatrix", Name: "Bellatrix", IsStronghold: true},
	})
	opps := []market.ArbitrageOpportunity{
		opp(1, "sol", "alpha_centauri", 100), // safe
		opp(2, "sol", "Bellatrix", 999),      // stronghold SELL by name (the trap)
		opp(3, "bellatrix", "sol", 500),      // stronghold BUY by id
		opp(4, "alpha_centauri", "sol", 80),  // safe
	}
	kept, dropped := dropStrongholdOpps(opps, refs)
	if got := ids(kept); len(got) != 2 || got[0] != 1 || got[1] != 4 {
		t.Errorf("kept = %v, want [1 4]", got)
	}
	if len(dropped) != 2 {
		t.Errorf("dropped = %v, want 2 stronghold refs", dropped)
	}

	// Empty stronghold set returns the input untouched.
	kept2, dropped2 := dropStrongholdOpps(opps, nil)
	if len(kept2) != len(opps) || dropped2 != nil {
		t.Errorf("empty set should be no-op: kept=%d dropped=%v", len(kept2), dropped2)
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
	}, "a", n2id, g, 0, 0, nil)
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
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{fresh, durable}, "a", n2id, g, 0, 0, nil)
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
	}, "a", n2id, g, 0, 0, nil)
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
	}, "a", n2id, g, 0, 0, nil)
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
	}, "a", n2id, g, 0, 0, nil)
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
	}, "a", n2id, g, 0, 0, nil)
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
	capped := RankHaulOpportunities(opps, "a", n2id, g, 2, 0, nil)
	if len(capped) != 1 || capped[0].ID != 1 {
		t.Fatalf("maxJumps=2 should keep only the nearby opp 1, got %v", ids(capped))
	}
	uncapped := RankHaulOpportunities(opps, "a", n2id, g, 0, 0, nil)
	if len(uncapped) != 2 || uncapped[0].ID != 2 {
		t.Fatalf("no cap should keep both with high-gross id 2 first, got %v", ids(uncapped))
	}
}

// mkRankOpp builds an opp whose buy/sell SYSTEM ids equal buySys/sellSys and whose
// FromStationID is derived from buySys (priceOf sees a concrete station).
func mkRankOpp(id int, buySys, sellSys string, gross float64) market.ArbitrageOpportunity {
	return market.ArbitrageOpportunity{
		ID: id, GrossProfit: gross, CyclesSeen: 1,
		FromSystemName: buySys, ToSystemName: sellSys,
		FromStationID: buySys + "_st", ToStationID: sellSys + "_st",
	}
}

func TestRankNetOfFuelFlipsOrder(t *testing.T) {
	// current=a. near: buy b (a->b=1), sell c (b->c=1), gross 4000.
	//            far:  buy d (a->d=1), sell g (d->e->f->g=3), gross 5200.
	g, n2id := graphFor(
		[]string{"a", "b", "c", "d", "e", "f", "g"},
		[2]string{"a", "b"}, [2]string{"b", "c"},
		[2]string{"a", "d"}, [2]string{"d", "e"}, [2]string{"e", "f"}, [2]string{"f", "g"},
	)
	near := mkRankOpp(1, "b", "c", 4000)
	far := mkRankOpp(2, "d", "g", 5200)
	opps := []market.ArbitrageOpportunity{near, far}

	// Gross-only (fuelPerJump=0): far (5200) leads, near (4000) is in the lower band.
	gross := RankHaulOpportunities(opps, "a", n2id, g, 0, 0, nil)
	if gross[0].ID != 2 {
		t.Fatalf("gross order: want far(2) first, got %d", gross[0].ID)
	}
	// Net-of-fuel (rate 10, price 100): net_near=2000 > net_far=1200 -> near leads.
	net := RankHaulOpportunities(opps, "a", n2id, g, 0, 10, func(string) float64 { return 100 })
	if net[0].ID != 1 {
		t.Fatalf("net order: want near(1) first after fuel, got %d", net[0].ID)
	}
}

func TestRankDropsOverlongHaulLeg(t *testing.T) {
	// Chain buy -> h0 -> h1 -> ... so the haul leg exceeds HaulMaxHaulJumps.
	systems := []string{"a", "buy"}
	pairs := [][2]string{{"a", "buy"}}
	prev := "buy"
	for i := 0; i <= HaulMaxHaulJumps; i++ { // HaulMaxHaulJumps+1 hops buy->sell
		n := fmt.Sprintf("h%d", i)
		systems = append(systems, n)
		pairs = append(pairs, [2]string{prev, n})
		prev = n
	}
	g, n2id := graphFor(systems, pairs...)
	overlong := mkRankOpp(1, "buy", prev, 9999)
	got := RankHaulOpportunities([]market.ArbitrageOpportunity{overlong}, "a", n2id, g, 0, 10, func(string) float64 { return 1 })
	if len(got) != 0 {
		t.Fatalf("want overlong haul dropped, got %d opp(s)", len(got))
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
	orders         []market.Order // item-specific buy/sell book (itemID != "")
	stationOrders  []market.Order // station-wide book (itemID == ""); presence = station captured
	bestPrices     []market.BestPrice
	recorded       []market.HaulResult
	reaped         int            // count of ReapExpiredBookClaims calls

	admitOK             bool               // AdmitBookClaim returns this ok
	admitResult         market.AdmitResult // returned when admitOK (ClaimID/OppID/ToStationID)
	settled             []float64          // bought_units passed to SettleBookClaim
	bookClaimsCompleted []int64            // claimIDs completed
	bookClaimsReleased  []int64            // claimIDs released
	invalidated         []string           // "item/from" strings passed to InvalidateBook

	// admitByBook scripts a per-book response for AdmitBookClaim: when non-nil, the
	// response for a given book is looked up here; a missing key falls back to the
	// admitOK/admitResult default behavior below. nil preserves old callers unchanged.
	admitByBook map[market.BookKey]market.AdmitResult
	admitCalls  []admitCall // every AdmitBookClaim invocation, in call order
}

// admitCall records one AdmitBookClaim invocation for assertions.
type admitCall struct {
	book  market.BookKey
	capN  int
	cands []market.BookCandidate
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
func (f *fakeStore) GetStationOrders(_ context.Context, _, itemID string) ([]market.Order, error) {
	if itemID == "" {
		return f.stationOrders, nil
	}
	return f.orders, nil
}
func (f *fakeStore) FindBestPrices(_ context.Context, _, _ string, _ int) ([]market.BestPrice, error) {
	return f.bestPrices, nil
}
func (f *fakeStore) RecordHaulResult(_ context.Context, r market.HaulResult) error {
	f.recorded = append(f.recorded, r)
	return nil
}
func (f *fakeStore) AdmitBookClaim(_ context.Context, book market.BookKey, cands []market.BookCandidate, _ string, capN int, _ time.Duration) (market.AdmitResult, error) {
	f.admitCalls = append(f.admitCalls, admitCall{book: book, capN: capN, cands: cands})

	if f.admitByBook != nil {
		if r, ok := f.admitByBook[book]; ok {
			return r, nil
		}
		return market.AdmitResult{}, nil
	}

	if !f.admitOK {
		return market.AdmitResult{}, nil
	}
	r := f.admitResult
	if r.OppID == 0 && len(cands) > 0 { // default: admit the first candidate
		r = market.AdmitResult{OK: true, ClaimID: 1, OppID: cands[0].OppID, ToStationID: cands[0].ToStationID}
	}
	r.OK = true
	return r, nil
}
func (f *fakeStore) SettleBookClaim(_ context.Context, _ int64, _ string, boughtUnits float64) error {
	f.settled = append(f.settled, boughtUnits)
	return nil
}
func (f *fakeStore) CompleteBookClaim(_ context.Context, claimID int64, _ string) error {
	f.bookClaimsCompleted = append(f.bookClaimsCompleted, claimID)
	return nil
}
func (f *fakeStore) ReleaseBookClaim(_ context.Context, claimID int64, _ string) error {
	f.bookClaimsReleased = append(f.bookClaimsReleased, claimID)
	return nil
}
func (f *fakeStore) InvalidateBook(_ context.Context, itemID, fromStation, _, _ string) error {
	f.invalidated = append(f.invalidated, itemID+"/"+fromStation)
	return nil
}
func (f *fakeStore) ReapExpiredBookClaims(_ context.Context) (int, error) {
	f.reaped++
	return 0, nil
}

func TestFakeStoreServesStationOrders(t *testing.T) {
	f := &fakeStore{orders: []market.Order{{Side: "buy", PriceEach: 50, Quantity: 4}}}
	got, err := f.GetStationOrders(context.Background(), "stn", "iron_ore")
	if err != nil || len(got) != 1 || got[0].PriceEach != 50 {
		t.Fatalf("want 1 order @50, got %v err=%v", got, err)
	}
}

func TestHaulFindRerouteChoosesBetterMarket(t *testing.T) {
	// Cargo: 10 iron_ore bought @100 (buyCost 1000) in current system "a". Original dest
	// "x-stn" is thin (1 unit @50 -> continueNet 50-1000 = -950). Candidate buyer g-stn in
	// system "g" (2 jumps): bid 130 x50 -> net 300 >> threshold (-950 + 150 margin = -800).
	fc := &fakeClient{state: &game.State{System: game.SystemData{ID: "a"},
		Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}}}}
	f := &fakeStore{
		orders:     []market.Order{{Side: "buy", PriceEach: 50, Quantity: 1}}, // orig dest demand (thin)
		bestPrices: []market.BestPrice{{ListingType: "buy", ItemID: "iron_ore", SystemID: "g", StationID: "g-stn", Price: 130, Quantity: 50}},
	}
	kb := &fakeKB{
		systems: []knowledge.System{{ID: "a"}, {ID: "b"}, {ID: "g"}},
		conns:   undirected([2]string{"a", "b"}, [2]string{"b", "g"}), // a -> g in 2 jumps
	}
	o := opp(7, "src", "x", 100) // ToStationID "x-stn"
	m := &haulMetrics{buyPrice: 100}
	deps := HaulDeps{Client: fc, Market: f, KB: kb, AgentID: "t"}
	sys, stn, ok := haulFindReroute(context.Background(), deps, o, m)
	if !ok || sys != "g" || stn != "g-stn" {
		t.Fatalf("want reroute to g/g-stn, got sys=%q stn=%q ok=%v", sys, stn, ok)
	}
}

func TestHaulFindRerouteNoneWhenNotBetter(t *testing.T) {
	// Original dest is healthy (130 x50 -> continueNet 300); the only candidate (120 x50 ->
	// net 200) does not beat continue + margin (300 + 150 = 450), so stay the course.
	fc := &fakeClient{state: &game.State{System: game.SystemData{ID: "a"},
		Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}}}}
	f := &fakeStore{
		orders:     []market.Order{{Side: "buy", PriceEach: 130, Quantity: 50}},
		bestPrices: []market.BestPrice{{ListingType: "buy", ItemID: "iron_ore", SystemID: "g", StationID: "g-stn", Price: 120, Quantity: 50}},
	}
	kb := &fakeKB{systems: []knowledge.System{{ID: "a"}, {ID: "g"}}, conns: undirected([2]string{"a", "g"})}
	o := opp(7, "src", "x", 100)
	if _, _, ok := haulFindReroute(context.Background(), deps2(fc, f, kb), o, &haulMetrics{buyPrice: 100}); ok {
		t.Fatalf("candidate does not beat continue+margin; want ok=false")
	}
}

func deps2(fc *fakeClient, f *fakeStore, kb *fakeKB) HaulDeps {
	return HaulDeps{Client: fc, Market: f, KB: kb, AgentID: "t"}
}

// TestHaulResolveSellSystemChasesMovingCapital covers the mobile_capital stall fix: a
// moving station's sell system is resolved live from the server's router (where the
// capital is now), not from the opportunity's discovery-time ToSystemName — so a hauler
// no longer strands at the system the capital jumped away from.
func TestHaulResolveSellSystemChasesMovingCapital(t *testing.T) {
	nameToID := map[string]string{"The Telescope": "the_telescope", "Frontier": "frontier"}
	// Opportunity discovered while the capital was in The Telescope; it has since jumped.
	moving := market.ArbitrageOpportunity{ToStationID: "mobile_capital", ToSystemName: "The Telescope"}

	// Router now routes to Frontier — the capital's current system — via one hop.
	fc := &fakeClient{route: []game.RouteStep{{SystemID: "hop"}, {SystemID: "frontier"}}}
	if got := haulResolveSellSystem(context.Background(), HaulDeps{Client: fc}, moving, nameToID); got != "frontier" {
		t.Fatalf("moving capital: want live system frontier, got %q", got)
	}

	// A fixed station never consults the router: static name->id lookup only.
	fixed := market.ArbitrageOpportunity{ToStationID: "grand_exchange", ToSystemName: "The Telescope"}
	fc2 := &fakeClient{route: []game.RouteStep{{SystemID: "frontier"}}}
	if got := haulResolveSellSystem(context.Background(), HaulDeps{Client: fc2}, fixed, nameToID); got != "the_telescope" {
		t.Fatalf("fixed station: want the_telescope, got %q", got)
	}
	if len(fc2.calls) != 0 {
		t.Fatalf("fixed station must not call FindRoute, got %v", fc2.calls)
	}

	// Router error on a moving station falls back to the static (stale) id, never empty.
	fc3 := &fakeClient{routeErr: errors.New("router down")}
	if got := haulResolveSellSystem(context.Background(), HaulDeps{Client: fc3}, moving, nameToID); got != "the_telescope" {
		t.Fatalf("router error: want static fallback the_telescope, got %q", got)
	}
}

// TestHaulSellLegReroutesOnThinDemandMidRoute covers the Step-2 reaction: while transiting
// to the claimed sell station, the per-jump watchdog sees the destination demand has gone
// thin, autopilot stops early, and the leg re-routes once to a better live market — so the
// final in-system hop targets the re-route station, not the abandoned original.
func TestHaulSellLegReroutesOnThinDemandMidRoute(t *testing.T) {
	fc := autopilotFake() // route: current -> sys_b -> sys_c (2 jumps); FindRoute ignores target
	fc.state.System = game.SystemData{ID: "a", Name: "A"}
	fc.state.Ship.Cargo = []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}
	f := &fakeStore{
		orders:     []market.Order{{Side: "buy", PriceEach: 50, Quantity: 1}}, // claimed dest thin -> stop fires
		bestPrices: []market.BestPrice{{ListingType: "buy", ItemID: "iron_ore", SystemID: "g", StationID: "g-stn", Price: 130, Quantity: 50}},
	}
	kb := &fakeKB{
		systems: []knowledge.System{{ID: "a"}, {ID: "b"}, {ID: "g"}},
		conns:   undirected([2]string{"a", "b"}, [2]string{"b", "g"}),
	}
	o := opp(7, "src", "x", 100) // original sell station x-stn in system x
	deps := HaulDeps{Client: fc, Market: f, KB: kb, AgentID: "t"}
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "x", &haulMetrics{buyPrice: 100, qty: 10}, 0); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(fc.calls, "travel:g-stn") {
		t.Fatalf("expected re-route travel to g-stn, got %v", fc.calls)
	}
	if slices.Contains(fc.calls, "travel:x-stn") {
		t.Fatalf("must not complete to the thinned original dest x-stn, got %v", fc.calls)
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
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m, 0); err != nil {
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

func TestHaulSellLegPostsCostOrderOnEmptyButCapturedBook(t *testing.T) {
	// The item has zero buyers, but the destination station IS in recent capture (it shows
	// other orders) -> genuine dead demand -> cost-order instead of looping a market sell.
	o := opp(7, "b", "a", 100)
	fc := &fakeClient{state: &game.State{
		System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
	}, route: []game.RouteStep{{SystemID: "a", Name: "A"}}}
	f := &fakeStore{
		orders:        nil,                                                         // no buy orders for iron_ore here
		stationOrders: []market.Order{{Side: "sell", PriceEach: 200, Quantity: 5}}, // station captured (has data)
	}
	m := &haulMetrics{buyPrice: 100, qty: 10}
	deps := HaulDeps{Client: fc, Market: f, AgentID: "t"}
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m, 0); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(fc.calls, func(c string) bool { return strings.HasPrefix(c, "sell:iron_ore") }) {
		t.Fatalf("dead market must NOT loop a market-sell, got %v", fc.calls)
	}
	if !slices.ContainsFunc(fc.calls, func(c string) bool { return strings.HasPrefix(c, "sell_order:iron_ore") }) {
		t.Fatalf("empty-but-captured book must post a cost order, got %v", fc.calls)
	}
}

func TestHaulSellLegSellsWhenNoCaptureData(t *testing.T) {
	// No item buyers AND no station data at all -> freshness rule -> sell normally; do not
	// cost-order merely because we lack a recent capture.
	o := opp(7, "b", "a", 100)
	fc := &fakeClient{state: &game.State{
		System: game.SystemData{ID: "a", Name: "A"}, Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "iron_ore", Quantity: 10}}},
	}, route: []game.RouteStep{{SystemID: "a", Name: "A"}}}
	f := &fakeStore{orders: nil, stationOrders: nil} // no data at all
	m := &haulMetrics{buyPrice: 100, qty: 10}
	deps := HaulDeps{Client: fc, Market: f, AgentID: "t"}
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m, 0); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(fc.calls, "sell:iron_ore") {
		t.Fatalf("no capture data -> must sell normally, got %v", fc.calls)
	}
	if slices.ContainsFunc(fc.calls, func(c string) bool { return strings.HasPrefix(c, "sell_order:iron_ore") }) {
		t.Fatalf("must not cost-order on mere absence of data, got %v", fc.calls)
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
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m, 0); err != nil {
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

func TestLoadAvailableReapsExpiredBookClaims(t *testing.T) {
	f := &fakeStore{available: []market.ArbitrageOpportunity{{ID: 1, ItemID: "x", FromStationID: "s", ToStationID: "d", SourceUnits: 10}}}
	if _, err := loadAvailable(context.Background(), f, 50); err != nil {
		t.Fatalf("loadAvailable: %v", err)
	}
	if f.reaped != 1 {
		t.Fatalf("loadAvailable must reap expired book claims once per pass, got %d", f.reaped)
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

func TestBookCap(t *testing.T) {
	cases := []struct {
		src, cargo float64
		want       int
	}{
		{42, 300, 1},
		{900, 300, 3},
		{4367, 480, 10},
		{0, 300, 1},
		{300, 0, 1},
	}
	for _, tc := range cases {
		if got := bookCap(tc.src, tc.cargo); got != tc.want {
			t.Errorf("bookCap(%v,%v)=%d want %d", tc.src, tc.cargo, got, tc.want)
		}
	}
}

// TestClaimBestBookCapComputation verifies bookCap(sourceUnits, cargoCap) is wired
// into the AdmitBookClaim call: SourceUnits=900 over a 300-cargo ship -> capN=3.
func TestClaimBestBookCapComputation(t *testing.T) {
	o := opp(1, "a", "x", 500)
	o.SourceUnits = 900
	ranked := []market.ArbitrageOpportunity{o}
	bk := market.BookKey{ItemID: "iron_ore", FromStationID: "a-stn"}
	f := &fakeStore{admitByBook: map[market.BookKey]market.AdmitResult{
		bk: {OK: true, ClaimID: 1, OppID: 1, ToStationID: "x-stn"},
	}}

	got, claimID, ok, err := claimBestBook(context.Background(), f, ranked, "hauler-1", 300)
	if err != nil || !ok || got.ID != 1 || claimID != 1 {
		t.Fatalf("want admitted id=1 claimID=1, got id=%d claimID=%d ok=%v err=%v", got.ID, claimID, ok, err)
	}
	if len(f.admitCalls) != 1 || f.admitCalls[0].capN != 3 {
		t.Fatalf("want one call with capN=3, got calls=%+v", f.admitCalls)
	}
}

// TestClaimBestBookAdvancesPastFullBook covers book grouping and the attempted-once
// guard: two dest rows share book A (which is full); claimBestBook must call
// AdmitBookClaim for book A exactly once (not once per dest row) before advancing to
// book B, which admits.
func TestClaimBestBookAdvancesPastFullBook(t *testing.T) {
	a1 := opp(1, "a", "x", 300)
	a1.SourceUnits = 900
	a2 := opp(2, "a", "y", 200)
	a2.SourceUnits = 900
	b1 := opp(3, "b", "z", 100)
	b1.SourceUnits = 900
	ranked := []market.ArbitrageOpportunity{a1, a2, b1}

	bookA := market.BookKey{ItemID: "iron_ore", FromStationID: "a-stn"}
	bookB := market.BookKey{ItemID: "iron_ore", FromStationID: "b-stn"}
	f := &fakeStore{admitByBook: map[market.BookKey]market.AdmitResult{
		bookA: {OK: false},
		bookB: {OK: true, ClaimID: 42, OppID: 3, ToStationID: "z-stn"},
	}}

	got, claimID, ok, err := claimBestBook(context.Background(), f, ranked, "hauler-1", 300)
	if err != nil || !ok || got.ID != 3 || claimID != 42 {
		t.Fatalf("want admitted id=3 claimID=42, got id=%d claimID=%d ok=%v err=%v", got.ID, claimID, ok, err)
	}
	if len(f.admitCalls) != 2 {
		t.Fatalf("want exactly 2 AdmitBookClaim calls (one per book), got %d: %+v", len(f.admitCalls), f.admitCalls)
	}
	if f.admitCalls[0].book != bookA || len(f.admitCalls[0].cands) != 2 {
		t.Fatalf("want first call on book A with 2 candidates, got %+v", f.admitCalls[0])
	}
	if f.admitCalls[1].book != bookB || len(f.admitCalls[1].cands) != 1 {
		t.Fatalf("want second call on book B with 1 candidate, got %+v", f.admitCalls[1])
	}
}

// TestClaimBestBookReturnsAssignedDestination confirms claimBestBook returns the
// ranked opp matching AdmitResult.OppID (the destination the admission assigned),
// even when that row is not the top-ranked one in the book.
func TestClaimBestBookReturnsAssignedDestination(t *testing.T) {
	top := opp(10, "src", "top", 500)
	second := opp(11, "src", "second", 400)
	ranked := []market.ArbitrageOpportunity{top, second}

	bk := market.BookKey{ItemID: "iron_ore", FromStationID: "src-stn"}
	f := &fakeStore{admitByBook: map[market.BookKey]market.AdmitResult{
		bk: {OK: true, ClaimID: 77, OppID: 11, ToStationID: "second-stn"},
	}}

	got, claimID, ok, err := claimBestBook(context.Background(), f, ranked, "hauler-1", 300)
	if err != nil || !ok || claimID != 77 {
		t.Fatalf("want ok claimID=77, got ok=%v claimID=%d err=%v", ok, claimID, err)
	}
	if got.ID != 11 {
		t.Fatalf("want assigned-destination opp id=11 (not top-ranked id=10), got id=%d", got.ID)
	}
}

// TestClaimBestBookAllFull covers the case where every reachable book is at capacity:
// claimBestBook must report ok=false without error.
func TestClaimBestBookAllFull(t *testing.T) {
	a := opp(1, "a", "x", 300)
	b := opp(2, "b", "y", 200)
	ranked := []market.ArbitrageOpportunity{a, b}

	f := &fakeStore{admitByBook: map[market.BookKey]market.AdmitResult{
		{ItemID: "iron_ore", FromStationID: "a-stn"}: {OK: false},
		{ItemID: "iron_ore", FromStationID: "b-stn"}: {OK: false},
	}}

	got, claimID, ok, err := claimBestBook(context.Background(), f, ranked, "hauler-1", 300)
	if err != nil || ok {
		t.Fatalf("want ok=false when all books full, got ok=%v err=%v", ok, err)
	}
	if got.ID != 0 || claimID != 0 {
		t.Fatalf("want zero-value opp/claimID on all-full, got id=%d claimID=%d", got.ID, claimID)
	}
	if len(f.admitCalls) != 2 {
		t.Fatalf("want one call per book (2), got %d", len(f.admitCalls))
	}
}

func TestHaulActivityLabelCapsToShip(t *testing.T) {
	opp := market.ArbitrageOpportunity{ID: 921069, ItemName: "Steel Plate", SourceUnits: 4367,
		FromStationName: "Ironhearth Station", ToStationName: "Ramen's Rest"}
	got := haulActivityLabel(opp, 480)
	if !strings.Contains(got, "up to 480 of 4367") {
		t.Fatalf("label must cap to ship slice; got %q", got)
	}
	if strings.Contains(got, "4847") {
		t.Fatalf("label must not show raw impossible quantity: %q", got)
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
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, n2id, nil, haulFuel{}, 0); err != nil {
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

func buyLegOpp() market.ArbitrageOpportunity {
	return market.ArbitrageOpportunity{
		ID: 7, FromSystemName: "a", ToSystemName: "a",
		FromStationID: "buy-stn", ToStationID: "sell-stn",
		ItemID: "iron_ore", GrossProfit: 9000, Quantity: 10, SourceUnits: 400, BuyPrice: 5,
	}
}

// TestRunClaimedHaulSettlesBoughtQuantity drives the full buy leg: with a live spread
// that clears the gate, the hauler buys and records the bought quantity via
// SettleBookClaim so other haulers see the reduced book remainder.
func TestRunClaimedHaulSettlesBoughtQuantity(t *testing.T) {
	o := buyLegOpp()
	fc := &fakeClient{
		state: &game.State{
			System:  game.SystemData{ID: "a", Name: "A"},
			Fuel:    100,
			MaxFuel: 100,
			Credits: 1_000_000,
			Ship:    game.Ship{CargoCapacity: 500}, // empty cargo, room to buy
		},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}},
	}
	f := &fakeStore{
		admitOK: true,
		prices: []market.ItemStationPrice{
			{StationID: "buy-stn", HasSell: true, BestAsk: 10, AskQty: 400},
			{StationID: "sell-stn", HasBuy: true, BestBid: 200, BidQty: 400},
		},
	}
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, n2id, nil, haulFuel{}, 7); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(f.settled) != 1 {
		t.Fatalf("expected one settle write after buy, got %d", len(f.settled))
	}
}

// TestRunClaimedHaulInvalidatesBookOnCollapse drives the full buy leg into a collapse:
// the source book has no live ask at arrival, so the hauler invalidates the book
// (drawing no further haulers in) and still releases its own cap slot.
func TestRunClaimedHaulInvalidatesBookOnCollapse(t *testing.T) {
	o := buyLegOpp()
	fc := &fakeClient{
		state: &game.State{
			System:  game.SystemData{ID: "a", Name: "A"},
			Fuel:    100,
			MaxFuel: 100,
			Credits: 1_000_000,
			Ship:    game.Ship{CargoCapacity: 500},
		},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}},
	}
	// No sell side at the buy station -> haulGate returns "no live ask at buy station"
	// (collapse-confirmed after the live recapture, which is nil here).
	f := &fakeStore{
		admitOK: true,
		prices:  []market.ItemStationPrice{{StationID: "sell-stn", HasBuy: true, BestBid: 100, BidQty: 400}},
	}
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, n2id, nil, haulFuel{}, 7); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(f.invalidated) != 1 {
		t.Fatalf("collapse must invalidate the book, got %d invalidations", len(f.invalidated))
	}
	if len(f.bookClaimsReleased) == 0 {
		t.Fatalf("collapse abandon must still release the book claim slot")
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
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m, 0); err != nil {
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
	if err := haulSellLeg(context.Background(), deps, io.Discard, o, "a", m, 0); err != nil {
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
	if err := haulSellLeg(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, "a", nil, 0); err != nil {
		t.Fatal(err)
	}
	if len(f.completed) != 1 {
		t.Fatalf("want completed [7], got %v", f.completed)
	}
	if len(f.recorded) != 0 {
		t.Fatalf("nil metrics must record nothing, got %d", len(f.recorded))
	}
}

// filterStrongholdRoutes must drop an opportunity when EITHER its reposition leg
// (current->buy) or its haul leg (buy->sell) routes through a stronghold waypoint,
// keep clean-route opportunities, and never drop on a path-lookup failure (the
// endpoint guard remains the backstop).
func TestFilterStrongholdRoutes(t *testing.T) {
	// den is the stronghold waypoint.
	paths := map[[2]string][]string{
		{"sol", "ac"}:      {"sol", "ac"},
		{"ac", "procyon"}:  {"ac", "procyon"},
		{"sol", "faraway"}: {"sol", "den", "faraway"}, // reposition leg crosses den
		{"ac", "beyond"}:   {"ac", "den", "beyond"},   // haul leg crosses den
		{"faraway", "ac"}:  {"faraway", "ac"},
	}
	pathOf := func(from, to string, _ bool) (galaxy.Route, error) {
		if p, ok := paths[[2]string{from, to}]; ok {
			return galaxy.Route{Path: p, Hops: len(p) - 1}, nil
		}
		return galaxy.Route{}, errors.New("no path")
	}
	nameToID := map[string]string{
		"Alpha Centauri": "ac", "Procyon": "procyon",
		"Faraway": "faraway", "Beyond": "beyond", "Unknown": "unknown",
	}
	strongholds := map[string]bool{"den": true}
	ranked := []market.ArbitrageOpportunity{
		{ID: 1, ItemID: "iron", FromSystemName: "Alpha Centauri", ToSystemName: "Procyon"}, // clean -> keep
		{ID: 2, ItemID: "gold", FromSystemName: "Faraway", ToSystemName: "Alpha Centauri"}, // reposition crosses den -> drop
		{ID: 3, ItemID: "copper", FromSystemName: "Unknown", ToSystemName: "Procyon"},      // lookup fails -> keep
		{ID: 4, ItemID: "plat", FromSystemName: "Alpha Centauri", ToSystemName: "Beyond"},  // haul leg crosses den -> drop
	}

	safe, dropped := filterStrongholdRoutes(ranked, "sol", nameToID, pathOf, strongholds)

	if got := ids(safe); !slices.Equal(got, []int{1, 3}) {
		t.Fatalf("safe opp ids = %v, want [1 3]", got)
	}
	if !slices.Equal(dropped, []string{"2:gold", "4:plat"}) {
		t.Fatalf("dropped = %v, want [2:gold 4:plat]", dropped)
	}
}
