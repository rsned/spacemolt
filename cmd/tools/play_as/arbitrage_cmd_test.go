package main

import (
	"testing"

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
	rows, skipped := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 0, nil, 0)
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
	rows, _ := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 2, price, 0)
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
	rows0, _ := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 0, price, 0)
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
	rows, skipped := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 0, nil, 0)
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
	rows, _ := rankDetourArbitrage(opps, "cur", "dest", g, n2i, 3, 0, nil, 1)
	if len(rows) != 1 || rows[0].Opp.ID != 2 {
		t.Fatalf("limit 1 returned %d rows (leader id %d), want 1 (id 2)", len(rows), func() int {
			if len(rows) > 0 {
				return rows[0].Opp.ID
			}
			return -1
		}())
	}
}
