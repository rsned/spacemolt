package pricing

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// The market-wide MEAN hides the cheapest source: ghost_rounds' titanium_ore
// showed a 10000 mkt-avg driven by one outlier ask, which made a BoM look
// 96k/unit. A "best anywhere" figure answers the question the mean cannot —
// what does this actually cost if you go get it. Distinct from Nearby (which
// is capped at hops) and from Mkt (a mean).
func TestAskStatsBestIsCheapestAnywhereRegardlessOfDistance(t *testing.T) {
	rs := []finditem.Result{
		res(100, 0), // local
		res(80, 2),  // within hops
		res(10, 5),  // far — cheapest overall, outside hops
	}
	got := askStats(rs, 2)

	if !got.BestFound || !approx(got.Best, 10) {
		t.Fatalf("best should be the cheapest ask anywhere (10), got found=%v unit=%v", got.BestFound, got.Best)
	}
	// Best must not collapse into either existing basis.
	if approx(got.Best, got.Nearby) {
		t.Error("best must differ from nearby when the cheapest station is out of range")
	}
	if approx(got.Best, got.Mkt) {
		t.Error("best must differ from the market mean")
	}
}

// A station the router cannot reach still counts: "best anywhere" is a sourcing
// floor, not a reachability claim.
func TestAskStatsBestIncludesUnreachableStations(t *testing.T) {
	rs := []finditem.Result{res(500, 1), res(5, finditem.JumpsUnknown), res(400, navigation.RouteInf)}
	got := askStats(rs, 2)

	if !got.BestFound || !approx(got.Best, 5) {
		t.Fatalf("best must include unreachable stations, got found=%v unit=%v", got.BestFound, got.Best)
	}
}

func TestAskStatsBestEmpty(t *testing.T) {
	got := askStats(nil, 2)
	if got.BestFound || got.Best != 0 {
		t.Fatalf("no asks -> no best, got found=%v unit=%v", got.BestFound, got.Best)
	}
}

// The best-anywhere unit prices must roll up into their own basis so the
// report can show a build cost / per unit / margin / SUGGESTED column for it.
func TestRollUpProducesMktBestBasis(t *testing.T) {
	comps := []PricedComponent{
		{Component: Component{ItemID: "a", Qty: 2}, MktUnit: 100, MktFound: true, MktBestUnit: 10, MktBestFound: true},
		{Component: Component{ItemID: "b", Qty: 1}, MktUnit: 50, MktFound: true, MktBestUnit: 30, MktBestFound: true},
	}
	_, mkt, best := rollUp(comps, 2, 20)

	// hand-derived: (2*10 + 1*30) = 50 build, /2 units = 25, +20% = 5, = 30
	if !approx(best.BuildCost, 50) || !approx(best.PerUnit, 25) || !approx(best.Margin, 5) || !approx(best.Suggested, 30) {
		t.Fatalf("mkt-best basis wrong: %+v", best)
	}
	if best.Covered != 2 || best.Total != 2 {
		t.Fatalf("mkt-best coverage wrong: %d/%d", best.Covered, best.Total)
	}
	// The mean basis must be unaffected: (2*100 + 1*50)/2 = 125 per unit.
	if !approx(mkt.PerUnit, 125) {
		t.Fatalf("mkt mean basis changed: %+v", mkt)
	}
}
