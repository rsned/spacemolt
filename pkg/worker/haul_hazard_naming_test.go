package worker

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/market"
)

// The idle line said "route through strongholds/danger zones" whatever the
// cause, and that cost hours on 2026-09-21. The fleet was idling on ONE stale
// danger zone (goldcrest, a degree-6 hub flagged a month earlier), but the
// wording implied strongholds -- which were categorically impossible: all 16
// haulers held the pirate unlock, so their stronghold set was empty, and all 9
// strongholds are degree-1 dead ends no route can transit. Naming the blocking
// system turns this into a one-line diagnosis.
func TestFilterStrongholdRoutes_NamesTheBlockingSystem(t *testing.T) {
	// sol -> goldcrest -> gemma is the only path to the buy station.
	pathOf := func(from, to string, _ bool) (galaxy.Route, error) {
		return galaxy.Route{Path: []string{from, "goldcrest", to}}, nil
	}
	nameToID := map[string]string{"Gemma": "gemma", "Bluerift": "bluerift"}
	ranked := []market.ArbitrageOpportunity{
		{ID: 1237649, ItemID: "liquid_hydrogen", FromSystemName: "Gemma", ToSystemName: "Bluerift"},
	}
	hazards := map[string]bool{"goldcrest": true, "Goldcrest": true}

	safe, dropped, blockers := filterStrongholdRoutes(ranked, "sol", nameToID, pathOf, hazards)
	if len(safe) != 0 || len(dropped) != 1 {
		t.Fatalf("safe=%v dropped=%v, want the opportunity dropped", safe, dropped)
	}
	if len(blockers) != 1 || blockers[0] != "goldcrest" {
		t.Errorf("blockers = %v, want [goldcrest] so the log can name it", blockers)
	}
}

// Several hazards on one pass must all be named, de-duplicated and ordered, so
// the line is stable enough to grep and compare between passes.
func TestFilterStrongholdRoutes_BlockersAreUniqueAndSorted(t *testing.T) {
	pathOf := func(from, to string, _ bool) (galaxy.Route, error) {
		switch to {
		case "a":
			return galaxy.Route{Path: []string{from, "zaniah", to}}, nil
		default:
			return galaxy.Route{Path: []string{from, "goldcrest", to}}, nil
		}
	}
	nameToID := map[string]string{"A": "a", "B": "b", "C": "c"}
	ranked := []market.ArbitrageOpportunity{
		{ID: 1, FromSystemName: "A", ToSystemName: "B"},
		{ID: 2, FromSystemName: "B", ToSystemName: "C"},
		{ID: 3, FromSystemName: "C", ToSystemName: "B"},
	}
	hazards := map[string]bool{"goldcrest": true, "zaniah": true}

	_, _, blockers := filterStrongholdRoutes(ranked, "sol", nameToID, pathOf, hazards)
	if strings.Join(blockers, ",") != "goldcrest,zaniah" {
		t.Errorf("blockers = %v, want [goldcrest zaniah] unique and sorted", blockers)
	}
}

// Nothing blocked means nothing to name -- the caller must not print an empty
// hazard list on a healthy pass.
func TestFilterStrongholdRoutes_NoBlockersWhenRoutesAreClear(t *testing.T) {
	pathOf := func(from, to string, _ bool) (galaxy.Route, error) {
		return galaxy.Route{Path: []string{from, to}}, nil
	}
	nameToID := map[string]string{"A": "a", "B": "b"}
	ranked := []market.ArbitrageOpportunity{{ID: 1, FromSystemName: "A", ToSystemName: "B"}}

	safe, dropped, blockers := filterStrongholdRoutes(ranked, "sol", nameToID, pathOf, map[string]bool{"goldcrest": true})
	if len(safe) != 1 || len(dropped) != 0 || len(blockers) != 0 {
		t.Errorf("safe=%d dropped=%v blockers=%v, want 1/none/none", len(safe), dropped, blockers)
	}
}
