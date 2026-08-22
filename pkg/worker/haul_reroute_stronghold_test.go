package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/market"
)

// The claimed opportunity is filtered against pirate strongholds, but the
// MID-ROUTE re-route was not. When demand at the claimed destination thinned
// below break-even, haulSellLeg called haulFindReroute, which ranked
// replacement markets purely on price and reassigned the destination:
//
//	haul: opp 659254 demand thinned mid-route; re-routing to zaniah @mera_sanctum
//
// A pirate stronghold market prices well precisely because nobody safely trades
// there, so the re-route was actively drawn to the most dangerous destination on
// the board. An agent without the pirate unlock is attacked on sight there.
//
// Live cost (2026-08-19): salvager-4 and salvager-9 were re-routed to
// zaniah @mera_sanctum within TEN SECONDS of each other from different positions
// on different routes, and died 100 seconds apart. Six of the haul fleet's
// twelve recorded losses are at zaniah, including trader-2's floor_price — a
// 420-cargo freight hull, not a starter.
func TestDropStrongholdPricesRemovesForbiddenMarkets(t *testing.T) {
	prices := []market.BestPrice{
		{StationID: "mera_sanctum", SystemID: "zaniah", SystemName: "Zaniah", Price: 900},
		{StationID: "haven_market", SystemID: "haven", SystemName: "Haven", Price: 400},
		{StationID: "crix_post", SystemID: "crix", SystemName: "Crix", Price: 850},
	}
	// buildStrongholdRefs keys by BOTH id and name; mirror that here.
	strongholds := map[string]bool{"zaniah": true, "Zaniah": true, "crix": true, "Crix": true}

	kept, dropped := dropStrongholdPrices(prices, strongholds)
	if len(kept) != 1 || kept[0].StationID != "haven_market" {
		t.Fatalf("kept %+v, want only haven_market — the two stronghold markets must go", kept)
	}
	if len(dropped) != 2 {
		t.Errorf("dropped %v, want both stronghold systems reported for the log", dropped)
	}
}

// An agent holding the pirate unlock gets an empty stronghold set, and stronghold
// markets are the richest on the board — it must keep them.
func TestDropStrongholdPricesIsANoOpWhenUnlocked(t *testing.T) {
	prices := []market.BestPrice{
		{StationID: "mera_sanctum", SystemID: "zaniah", SystemName: "Zaniah", Price: 900},
		{StationID: "haven_market", SystemID: "haven", SystemName: "Haven", Price: 400},
	}
	kept, dropped := dropStrongholdPrices(prices, nil)
	if len(kept) != 2 {
		t.Errorf("kept %d, want all 2: an unlocked agent may trade at strongholds", len(kept))
	}
	if len(dropped) != 0 {
		t.Errorf("dropped %v, want none", dropped)
	}
}

// Matching on the system NAME alone must be enough: 7 strongholds are dual-named
// between base id and poi id, so an id that fails to match is expected.
func TestDropStrongholdPricesMatchesOnNameToo(t *testing.T) {
	prices := []market.BestPrice{
		{StationID: "s", SystemID: "some_other_id", SystemName: "Zaniah", Price: 900},
	}
	kept, _ := dropStrongholdPrices(prices, map[string]bool{"Zaniah": true})
	if len(kept) != 0 {
		t.Errorf("kept %+v; a name-only match must still drop the market", kept)
	}
}
