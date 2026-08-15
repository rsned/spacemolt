package main

import (
	"context"
	"encoding/json"
	"strings"
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

// claimOpp builds a claim row as GetOpportunitiesByAgent returns it.
func claimOpp(id int, status, toSystem, expires string) market.ArbitrageOpportunity {
	return market.ArbitrageOpportunity{
		ID: id, Status: status, ToSystemName: toSystem, ExpiresAt: expires,
		ItemID: "plasma_gas", Quantity: 37, ClaimedAt: "2026-08-13T22:56:51Z",
	}
}

// TestPartitionClaimsSplitsAndFlags covers the three judgements my_claims makes:
// held vs finished, a hold that outlived its window, and a delivery leg that
// lands in a stronghold.
func TestPartitionClaimsSplitsAndFlags(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	strongholds := map[string]bool{"algol": true}
	opps := []market.ArbitrageOpportunity{
		claimOpp(1, "claimed", "Algol", "2026-08-14T04:23:00Z"),   // stale + stronghold
		claimOpp(2, "claimed", "haven", "2026-08-16T00:00:00Z"),   // live hold
		claimOpp(3, "completed", "haven", "2026-08-14T00:00:00Z"), // history
		claimOpp(4, "expired", "algol", "2026-08-14T00:00:00Z"),   // history, stronghold
	}

	held, history := partitionClaims(opps, strongholds, now)

	if len(held) != 2 || held[0].Opp.ID != 1 || held[1].Opp.ID != 2 {
		t.Fatalf("held = %v, want ids [1 2]", held)
	}
	if !held[0].Expired {
		t.Error("a claim held past expires_at must be marked expired")
	}
	if !held[0].DestStronghold {
		t.Error("Algol destination must be flagged as a stronghold")
	}
	if held[1].Expired || held[1].DestStronghold {
		t.Errorf("live non-stronghold hold must carry no marks: %+v", held[1])
	}
	if len(history) != 2 || history[0].Opp.ID != 3 || history[1].Opp.ID != 4 {
		t.Fatalf("history = %v, want ids [3 4]", history)
	}
	if !history[1].DestStronghold {
		t.Error("stronghold flag must apply to history rows too")
	}
}

// TestPartitionClaimsWithoutKnowledgeBase pins the graceful-degradation path: no
// stronghold map (knowledge base unavailable) must still classify claims, just
// without the destination flag.
func TestPartitionClaimsWithoutKnowledgeBase(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	held, history := partitionClaims([]market.ArbitrageOpportunity{
		claimOpp(1, "claimed", "Algol", "2026-08-14T04:23:00Z"),
	}, nil, now)
	if len(held) != 1 || len(history) != 0 {
		t.Fatalf("held=%d history=%d, want 1/0", len(held), len(history))
	}
	if !held[0].Expired {
		t.Error("expiry is computed from the row itself, not the knowledge base")
	}
	if held[0].DestStronghold {
		t.Error("no stronghold map means no flag, not a false positive")
	}
}

// TestPartitionClaimsUnparseableExpiry: a row whose expires_at cannot be parsed
// must not be reported as expired — an unreadable stamp is not evidence.
func TestPartitionClaimsUnparseableExpiry(t *testing.T) {
	held, _ := partitionClaims([]market.ArbitrageOpportunity{
		claimOpp(1, "claimed", "haven", "not-a-timestamp"),
	}, nil, time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC))
	if len(held) != 1 || held[0].Expired {
		t.Fatalf("unparseable expiry must not mark a claim expired: %+v", held)
	}
}

// TestRenderClaimsStyledMarksStaleStrongholdHold pins the output an operator
// reads when diagnosing a stalled hauler: the held claim carries both warnings
// and the release command, and finished work lands under Recent.
func TestRenderClaimsStyledMarksStaleStrongholdHold(t *testing.T) {
	held := []claimRow{{
		Opp: market.ArbitrageOpportunity{
			ID: 458031, Status: "claimed", ItemName: "Plasma Gas", ItemID: "plasma_gas",
			Quantity: 4923, GrossProfit: 2973492,
			FromStationName: "The Rampart Checkpoint", FromSystemName: "Rampart",
			ToStationName: "Dross Citadel", ToSystemName: "Algol",
			ClaimedAt: "2026-08-13T22:56:51Z", ExpiresAt: "2026-08-14T04:23:00Z",
		},
		Expired: true, DestStronghold: true,
	}}
	history := []claimRow{{Opp: market.ArbitrageOpportunity{
		ID: 464455, Status: "completed", ItemName: "Water Ice", Quantity: 119,
		GrossProfit: 6069, FromStationName: "Starfall", ToStationName: "Factory Belt",
	}}}

	out := captureStdout(t, func() { renderClaims("trader-10", held, history, formatStyled) })

	for _, want := range []string{
		"trader-10", "Holding (1)", "#458031", "Plasma Gas",
		"2,973,492", "EXPIRED HOLD", "delivers to a stronghold",
		"The Rampart Checkpoint (Rampart)", "Dross Citadel (Algol)",
		"release_arbitrage 458031", "Recent (1)", "#464455", "completed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("styled output missing %q:\n%s", want, out)
		}
	}
}

// TestRenderClaimsEmptyAndJSON covers the no-claims message and the machine
// format's shape.
func TestRenderClaimsEmptyAndJSON(t *testing.T) {
	empty := captureStdout(t, func() { renderClaims("assist-sol", nil, nil, formatStyled) })
	if !strings.Contains(empty, "No claims on record") {
		t.Errorf("empty listing should say so, got:\n%s", empty)
	}

	out := captureStdout(t, func() {
		renderClaims("trader-10", []claimRow{{
			Opp:     market.ArbitrageOpportunity{ID: 7, Status: "claimed", ItemID: "iron_ore", Quantity: 3},
			Expired: true,
		}}, nil, formatRaw)
	})
	var got struct {
		AgentID string `json:"agent_id"`
		Held    []struct {
			ID      int    `json:"id"`
			Item    string `json:"item"`
			Expired bool   `json:"expired_hold"`
		} `json:"held"`
		History []struct{} `json:"history"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON output did not parse: %v\n%s", err, out)
	}
	if got.AgentID != "trader-10" || len(got.Held) != 1 {
		t.Fatalf("unexpected JSON payload: %+v", got)
	}
	if got.Held[0].ID != 7 || got.Held[0].Item != "iron_ore" || !got.Held[0].Expired {
		t.Errorf("held row wrong (item must fall back to the id): %+v", got.Held[0])
	}
}
