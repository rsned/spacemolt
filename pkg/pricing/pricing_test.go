package pricing

import (
	"math"
	"testing"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestRollUpBothBasesFound(t *testing.T) {
	comps := []PricedComponent{
		{Component: Component{ItemID: "iron_ore", Qty: 20}, NearbyUnit: 12, MktUnit: 14, NearbyFound: true, MktFound: true},
		{Component: Component{ItemID: "copper_ore", Qty: 8}, NearbyUnit: 30, MktUnit: 28, NearbyFound: true, MktFound: true},
	}
	// nearby build = 20*12 + 8*30 = 480; per unit (÷5) = 96; +20% = 19.2; suggested 115.2
	nearby, mkt := rollUp(comps, 5, 20)
	if !approx(nearby.BuildCost, 480) || !approx(nearby.PerUnit, 96) || !approx(nearby.Margin, 19.2) || !approx(nearby.Suggested, 115.2) {
		t.Fatalf("nearby wrong: %+v", nearby)
	}
	if nearby.Covered != 2 || nearby.Total != 2 || !nearby.Complete() {
		t.Fatalf("nearby coverage wrong: %+v", nearby)
	}
	// mkt build = 20*14 + 8*28 = 504; per unit = 100.8
	if !approx(mkt.BuildCost, 504) || !approx(mkt.PerUnit, 100.8) {
		t.Fatalf("mkt wrong: %+v", mkt)
	}
}

func TestRollUpMissingNearbyComponentContributesZeroAndMarksIncomplete(t *testing.T) {
	comps := []PricedComponent{
		{Component: Component{ItemID: "iron_ore", Qty: 10}, NearbyUnit: 5, MktUnit: 5, NearbyFound: true, MktFound: true},
		{Component: Component{ItemID: "rare_ore", Qty: 2}, MktUnit: 100, MktFound: true}, // no nearby price
	}
	nearby, mkt := rollUp(comps, 1, 0)
	if !approx(nearby.BuildCost, 50) { // only iron_ore counted
		t.Fatalf("nearby build should skip missing: %+v", nearby)
	}
	if nearby.Covered != 1 || nearby.Total != 2 || nearby.Complete() {
		t.Fatalf("nearby should be incomplete: %+v", nearby)
	}
	if !approx(mkt.BuildCost, 250) || !mkt.Complete() { // 50 + 200
		t.Fatalf("mkt should be complete: %+v", mkt)
	}
}

func TestRollUpOutputUnitsFloorsAtOne(t *testing.T) {
	comps := []PricedComponent{{Component: Component{ItemID: "x", Qty: 3}, NearbyUnit: 10, MktUnit: 10, NearbyFound: true, MktFound: true}}
	nearby, _ := rollUp(comps, 0, 0) // outputUnits 0 must be treated as 1
	if !approx(nearby.PerUnit, 30) {
		t.Fatalf("perUnit with outputUnits<=0 should divide by 1: %+v", nearby)
	}
}

func res(price float64, jumps int) finditem.Result {
	return finditem.Result{ItemSeller: market.ItemSeller{BestPrice: price}, Jumps: jumps}
}

func TestAskStatsNearbyMinWithinHopsAndMktMean(t *testing.T) {
	rs := []finditem.Result{
		res(100, 0), // local
		res(80, 2),  // within 2 hops, cheaper
		res(10, 5),  // far — cheapest overall but outside 2 hops
	}
	nu, nf, mu, mf := askStats(rs, 2)
	if !nf || !approx(nu, 80) { // cheapest within <=2 hops
		t.Fatalf("nearby wrong: found=%v unit=%v", nf, nu)
	}
	// mkt mean over all three asks = (100+80+10)/3
	if !mf || !approx(mu, (100+80+10)/3.0) {
		t.Fatalf("mkt wrong: found=%v unit=%v", mf, mu)
	}
}

func TestAskStatsNoNearbyWhenAllTooFarOrUnknown(t *testing.T) {
	rs := []finditem.Result{res(50, finditem.JumpsUnknown), res(60, navigation.RouteInf), res(70, 4)}
	nu, nf, _, mf := askStats(rs, 2)
	if nf || nu != 0 {
		t.Fatalf("expected no nearby, got found=%v unit=%v", nf, nu)
	}
	if !mf {
		t.Fatalf("mkt should still be found from all asks")
	}
}

func TestAskStatsEmpty(t *testing.T) {
	nu, nf, mu, mf := askStats(nil, 2)
	if nf || mf || nu != 0 || mu != 0 {
		t.Fatalf("empty should yield nothing: %v %v %v %v", nu, nf, mu, mf)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		ask, sug float64
		want     string
	}{
		{450, 130, ClassUnder}, // 3.46× -> underpriced
		{100, 130, ClassOver},  // 0.77× -> overpriced (market below cost+margin)
		{130, 130, ClassFair},  // 1.0×
		{0, 130, ""},           // no market ask
		{130, 0, ""},           // no suggestion
	}
	for _, c := range cases {
		if got := classify(c.ask, c.sug); got != c.want {
			t.Fatalf("classify(%v,%v)=%q want %q", c.ask, c.sug, got, c.want)
		}
	}
}
