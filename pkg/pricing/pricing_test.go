package pricing

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/knowledge"
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

// seedPriceMarket writes sell/buy orders for pricing tests. Graph: home-mid-far.
func seedPriceMarket(t *testing.T) *market.Collector {
	t.Helper()
	c, err := market.Open(market.Config{DBPath: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	now := time.Now().UTC()
	order := func(stn, item, side string, price, qty float64) market.Order {
		return market.Order{StationID: stn, ItemID: item, Side: side, PriceEach: price, Quantity: qty, CapturedAt: now}
	}
	snap := func(stn, sys string, orders ...market.Order) {
		if err := c.WriteSnapshot(context.Background(), market.MarketSnapshot{
			StationID: stn, StationName: stn, SystemID: sys, SystemName: sys, CapturedAt: now, Orders: orders,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// iron_ore: home@12, far@8 (far is 2 jumps). copper_ore: home@30 only.
	// widget (finished good): home sell(ask) 500, home buy(bid) 400.
	snap("home_stn", "home",
		order("home_stn", "iron_ore", "sell", 12, 100),
		order("home_stn", "copper_ore", "sell", 30, 100),
		order("home_stn", "widget", "sell", 500, 5),
		order("home_stn", "widget", "buy", 400, 5),
	)
	snap("far_stn", "far", order("far_stn", "iron_ore", "sell", 8, 100))
	return c
}

func seedPriceKB(t *testing.T) knowledge.Base {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	for _, s := range []struct{ id, to string }{{"home", "mid"}, {"mid", "far"}} {
		if err := kb.RememberSystem(ctx, knowledge.System{ID: s.id, Name: s.id, Connections: []knowledge.SystemConnection{{SystemID: s.to}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "far", Name: "far"}); err != nil {
		t.Fatal(err)
	}
	return kb
}

func TestReportEndToEnd(t *testing.T) {
	col := seedPriceMarket(t)
	kb := seedPriceKB(t)
	comps := []Component{{ItemID: "iron_ore", Qty: 10}, {ItemID: "copper_ore", Qty: 2}}
	// outputUnits 1, margin 20, from "home", hops 2.
	rep, err := Report(context.Background(), col, kb, "home", 2, "widget", "recipe_widget", 1, comps, 20)
	if err != nil {
		t.Fatal(err)
	}
	// Nearby iron_ore: cheapest within 2 hops = far@8. copper_ore only home@30.
	// nearby build = 10*8 + 2*30 = 140; +20% = 168.
	if !approx(rep.Nearby.BuildCost, 140) || !approx(rep.Nearby.Suggested, 168) {
		t.Fatalf("nearby: %+v", rep.Nearby)
	}
	if !rep.Nearby.Complete() {
		t.Fatalf("nearby should be complete: %+v", rep.Nearby)
	}
	// Finished good: nearby ask 500 (home is 0 hops), bid 400.
	if !rep.HasAskNearby || !approx(rep.CurAskNearby, 500) {
		t.Fatalf("ask nearby wrong: %+v", rep)
	}
	if !rep.HasBid || !approx(rep.CurBid, 400) {
		t.Fatalf("bid wrong: %+v", rep)
	}
	// 500 / 168 = 2.98× -> underpriced.
	if rep.Class != ClassUnder {
		t.Fatalf("class: %q", rep.Class)
	}
	if rep.ItemID != "widget" || rep.RecipeName != "recipe_widget" || rep.OutputUnits != 1 {
		t.Fatalf("header fields: %+v", rep)
	}
}
