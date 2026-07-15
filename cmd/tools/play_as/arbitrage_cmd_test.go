package main

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// graph: cur - s1 - s2 - dest ; s2 - s9 - s10 (off-path spur) ; iso isolated.
// baseline cur->dest = 3.
func testArbGraph() navigation.JumpGraph {
	return navigation.JumpGraph{
		"cur":  {"s1"},
		"s1":   {"cur", "s2"},
		"s2":   {"s1", "dest", "s9"},
		"dest": {"s2"},
		"s9":   {"s2", "s10"},
		"s10":  {"s9"},
		"iso":  {},
	}
}

func testArbNameToID() map[string]string {
	return map[string]string{
		"cur system": "cur", "sys1": "s1", "sys2": "s2", "dest system": "dest",
		"sys9": "s9", "sys10": "s10", "isolated": "iso",
	}
}

func opp(id int, from, to string, gross float64) market.ArbitrageOpportunity {
	return market.ArbitrageOpportunity{
		ID: id, FromSystemName: from, ToSystemName: to, FromStationID: "buyst",
		GrossProfit: gross, ItemID: "x", Quantity: 10,
	}
}

func TestRankDetourFiltersByBudget(t *testing.T) {
	g := testArbGraph()
	n2i := testArbNameToID()
	opps := []market.ArbitrageOpportunity{
		opp(1, "sys1", "sys2", 1000), // legs 1+1+1=3, detour 0
		opp(2, "sys1", "sys9", 1500), // legs 1+2+2=5, detour 2
		opp(3, "sys9", "sys10", 900), // legs 3+1+3=7, detour 4 -> dropped at budget 3
	}
	rows, skipped := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, -1, 0, nil, 0)
	if skipped != 0 {
		t.Fatalf("skipped=%d, want 0", skipped)
	}
	if len(rows) != 2 {
		t.Fatalf("kept %d rows, want 2 (opp3 exceeds budget)", len(rows))
	}
	for _, r := range rows {
		if r.Opp.ID == 3 {
			t.Fatal("opp3 (detour 4) should be dropped at budget 3")
		}
	}
	// Detours: opp1=0, opp2=2.
	got := map[int]int{}
	for _, r := range rows {
		got[r.Opp.ID] = r.Detour
	}
	if got[1] != 0 || got[2] != 2 {
		t.Fatalf("detours = %v, want opp1=0 opp2=2", got)
	}
}

func TestRankDetourNetOfFuelSortAndDegradation(t *testing.T) {
	g := testArbGraph()
	n2i := testArbNameToID()
	opps := []market.ArbitrageOpportunity{
		opp(1, "sys1", "sys2", 1000), // detour 0 -> net 1000
		opp(2, "sys1", "sys9", 1500), // detour 2 -> fuel 2*2*100=400 -> net 1100
	}
	price := func(string) float64 { return 100 }
	rows, _ := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, -1, 2, price, 0)
	if len(rows) != 2 {
		t.Fatalf("kept %d, want 2", len(rows))
	}
	if rows[0].Opp.ID != 2 || rows[1].Opp.ID != 1 {
		t.Fatalf("order = [%d,%d], want [2,1] (net 1100 > 1000)", rows[0].Opp.ID, rows[1].Opp.ID)
	}
	if rows[0].Net != 1100 || rows[1].Net != 1000 {
		t.Fatalf("nets = [%.0f,%.0f], want [1100,1000]", rows[0].Net, rows[1].Net)
	}
	// Degradation: fuelPerJump=0 -> net == gross -> opp2 (1500) leads by gross.
	rows0, _ := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, -1, 0, price, 0)
	if rows0[0].Opp.ID != 2 || rows0[0].Net != 1500 {
		t.Fatalf("degraded: leader id=%d net=%.0f, want id=2 net=1500", rows0[0].Opp.ID, rows0[0].Net)
	}
}

func TestRankDetourSkipsUnresolvedAndUnreachable(t *testing.T) {
	g := testArbGraph()
	n2i := testArbNameToID()
	opps := []market.ArbitrageOpportunity{
		opp(1, "sys1", "sys2", 1000),     // ok
		opp(2, "sys1", "nowhere", 5000),  // unresolved sell name -> skipped
		opp(3, "sys1", "isolated", 5000), // unreachable leg (iso) -> skipped
	}
	rows, skipped := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, -1, 0, nil, 0)
	if len(rows) != 1 || rows[0].Opp.ID != 1 {
		t.Fatalf("kept %d rows, want only opp1", len(rows))
	}
	if skipped != 2 {
		t.Fatalf("skipped=%d, want 2", skipped)
	}
}

func TestRankDetourLimit(t *testing.T) {
	g := testArbGraph()
	n2i := testArbNameToID()
	opps := []market.ArbitrageOpportunity{
		opp(1, "sys1", "sys2", 1000),
		opp(2, "sys1", "sys9", 1500),
	}
	rows, _ := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, -1, 0, nil, 1)
	if len(rows) != 1 || rows[0].Opp.ID != 2 {
		t.Fatalf("limit 1 returned %d rows (leader id %d), want 1 (id 2)", len(rows), func() int {
			if len(rows) > 0 {
				return rows[0].Opp.ID
			}
			return -1
		}())
	}
}

func TestRankDetourUnreachableDest(t *testing.T) {
	g := testArbGraph()
	n2i := testArbNameToID()
	opps := []market.ArbitrageOpportunity{opp(1, "sys1", "sys2", 1000)}
	// "iso" is an isolated node: baseline (cur->iso) is unreachable -> nil, 0.
	rows, skipped := rankDetourArbitrage(opps, "cur", "iso", g, n2i, 3, -1, 0, nil, 0)
	if rows != nil || skipped != 0 {
		t.Fatalf("unreachable dest: got rows=%v skipped=%d, want nil,0", rows, skipped)
	}
}

// nearLineGraph is a straight line cur-m1-m2-m3-m4-dest (baseline 5) with a
// dead-end pocket `pk` hanging off the midpoint m2. It isolates the
// near-endpoint gate from the detour gate: a haul buried at m2/pocket has an
// acceptable detour yet sits far from both endpoints.
func nearLineGraph() navigation.JumpGraph {
	return navigation.JumpGraph{
		"cur":  {"m1"},
		"m1":   {"cur", "m2"},
		"m2":   {"m1", "m3", "pk"},
		"m3":   {"m2", "m4"},
		"m4":   {"m3", "dest"},
		"dest": {"m4"},
		"pk":   {"m2"},
	}
}

func nearLineNameToID() map[string]string {
	return map[string]string{
		"cur system": "cur", "mid1": "m1", "mid2": "m2", "mid3": "m3",
		"mid4": "m4", "dest system": "dest", "pocket": "pk",
	}
}

func TestRankDetourNearEndpointGate(t *testing.T) {
	g := nearLineGraph()
	n2i := nearLineNameToID()
	// baseline cur->dest = 5.
	opps := []market.ArbitrageOpportunity{
		// A: buy m1 (1 out), sell m4 (1 to dest) — near both, detour 0. keep.
		opp(1, "mid1", "mid4", 1000),
		// B: buy m2 (2 out), sell pocket pk (pk->dest = pk,m2,m3,m4,dest = 4).
		//    detour = 2 + (m2->pk=1) + 4 - 5 = 2 <= budget, but 2>near and 4>near
		//    -> dropped by the near gate, NOT the detour gate.
		opp(2, "mid2", "pocket", 9000),
		// C: buy m1 (1 out), sell m3 (m3->dest = 2). near via buy-side OR. keep.
		opp(3, "mid1", "mid3", 500),
	}
	rows, skipped := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 1, 0, nil, 0)
	if skipped != 0 {
		t.Fatalf("skipped=%d, want 0", skipped)
	}
	ids := map[int]arbRow{}
	for _, r := range rows {
		ids[r.Opp.ID] = r
	}
	if _, ok := ids[2]; ok {
		t.Fatalf("opp2 (buy 2 out, sell 4 to dest) should be dropped by near gate; rows=%+v", rows)
	}
	if len(rows) != 2 || ids[1].Opp.ID == 0 || ids[3].Opp.ID == 0 {
		t.Fatalf("kept %d rows, want opp1 and opp3", len(rows))
	}
	// Distance columns are populated for display.
	if ids[1].BuyOut != 1 || ids[1].SellToDest != 1 {
		t.Fatalf("opp1 dist = buy %d / sell %d, want 1/1", ids[1].BuyOut, ids[1].SellToDest)
	}
	if ids[3].BuyOut != 1 || ids[3].SellToDest != 2 {
		t.Fatalf("opp3 dist = buy %d / sell %d, want 1/2", ids[3].BuyOut, ids[3].SellToDest)
	}
	// near = -1 disables the gate: opp2 comes back (detour 2 <= budget 3).
	all, _ := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, -1, 0, nil, 0)
	if len(all) != 3 {
		t.Fatalf("near disabled: kept %d rows, want 3 (gate off)", len(all))
	}
}

// fakeArbFuel is a stub arbFuelPriceSource for buildArbPriceOf tests.
type fakeArbFuel struct {
	prices   map[string]int
	median   int
	medianOK bool
}

func (f fakeArbFuel) GetStationFuelPrice(_ context.Context, stationID string) (int, time.Time, bool, error) {
	if p, ok := f.prices[stationID]; ok {
		return p, time.Time{}, true, nil
	}
	return 0, time.Time{}, false, nil
}

func (f fakeArbFuel) MedianStationFuelAllIn(_ context.Context) (int, bool, error) {
	return f.median, f.medianOK, nil
}

func TestBuildArbPriceOf(t *testing.T) {
	ctx := context.Background()

	// nil source -> constant 0.
	if got := buildArbPriceOf(ctx, nil)("anything"); got != 0 {
		t.Fatalf("nil src: got %v, want 0", got)
	}

	src := fakeArbFuel{prices: map[string]int{"stationA": 150}, median: 90, medianOK: true}
	p := buildArbPriceOf(ctx, src)
	if got := p("grand_exchange_station"); got != 0 {
		t.Fatalf("free pump: got %v, want 0", got)
	}
	if got := p("stationA"); got != 150 {
		t.Fatalf("captured all-in: got %v, want 150", got)
	}
	if got := p("uncaptured"); got != 90 {
		t.Fatalf("median fallback: got %v, want 90", got)
	}

	// No median available -> 0 for uncaptured stations.
	p2 := buildArbPriceOf(ctx, fakeArbFuel{prices: map[string]int{}, medianOK: false})
	if got := p2("uncaptured"); got != 0 {
		t.Fatalf("no median: got %v, want 0", got)
	}
}
