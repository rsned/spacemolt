package finditem

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// seedMarket writes one sell order per station into a temp market DB.
// stations maps station id -> {system, price, qty}.
func seedMarket(t *testing.T) *market.Collector {
	t.Helper()
	c, err := market.Open(market.Config{DBPath: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	now := time.Now().UTC()
	write := func(stn, sys string, price, qty float64) {
		if err := c.WriteSnapshot(context.Background(), market.MarketSnapshot{
			StationID: stn, StationName: stn + " Station", SystemID: sys, SystemName: sys,
			CapturedAt: now,
			Orders: []market.Order{{
				StationID: stn, ItemID: "fusion_core", Side: "sell",
				PriceEach: price, Quantity: qty, CapturedAt: now,
			}},
		}); err != nil {
			t.Fatalf("seed %s: %v", stn, err)
		}
	}
	// Graph below: home - mid - far (chain). near_stn is 1 jump, far_stn is 2.
	write("near_stn", "mid", 100, 50)
	write("far_stn", "far", 10, 50) // cheapest but farthest
	write("home_stn", "home", 200, 3)
	return c
}

func seedKB(t *testing.T) knowledge.Base {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	for _, s := range []struct{ id, to string }{{"home", "mid"}, {"mid", "far"}} {
		if err := kb.RememberSystem(ctx, knowledge.System{
			ID: s.id, Name: s.id,
			Connections: []knowledge.SystemConnection{{SystemID: s.to}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "far", Name: "far"}); err != nil {
		t.Fatal(err)
	}
	return kb
}

func TestFindRanksByJumpsThenPrice(t *testing.T) {
	col := seedMarket(t)
	kb := seedKB(t)

	got, err := Find(context.Background(), col, kb, "fusion_core", 0, "home", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 results, got %d: %+v", len(got), got)
	}
	// home_stn (0 jumps) first despite worst price; far_stn (2 jumps) last
	// despite best price.
	want := []struct {
		station string
		jumps   int
	}{{"home_stn", 0}, {"near_stn", 1}, {"far_stn", 2}}
	for i, w := range want {
		if got[i].StationID != w.station || got[i].Jumps != w.jumps {
			t.Errorf("result[%d] = %s (%d jumps), want %s (%d jumps)",
				i, got[i].StationID, got[i].Jumps, w.station, w.jumps)
		}
	}
}

func TestFindQuantityFloorAndLimit(t *testing.T) {
	col := seedMarket(t)
	kb := seedKB(t)

	// home_stn has only 3 units; a floor of 10 drops it.
	got, err := Find(context.Background(), col, kb, "fusion_core", 10, "home", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].StationID != "near_stn" {
		t.Fatalf("want [near_stn], got %+v", got)
	}
}

func TestFindWithoutKBSortsByPrice(t *testing.T) {
	col := seedMarket(t)

	got, err := Find(context.Background(), col, nil, "fusion_core", 0, "home", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 results, got %d", len(got))
	}
	if got[0].StationID != "far_stn" || got[0].Jumps != JumpsUnknown {
		t.Errorf("without KB, cheapest first with unknown jumps; got %+v", got[0])
	}
}

func TestFindUnknownItemReturnsEmpty(t *testing.T) {
	col := seedMarket(t)
	got, err := Find(context.Background(), col, seedKB(t), "no_such_item", 0, "home", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want no results, got %+v", got)
	}
}

func TestFindResolvesDisplayNameSystemIDs(t *testing.T) {
	// Some market.db station rows store the system's display name instead of
	// its id; those must still rank by distance via the KB name→id mapping.
	col, err := market.Open(market.Config{DBPath: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = col.Close() })

	ctx := context.Background()
	now := time.Now().UTC()
	if err := col.WriteSnapshot(ctx, market.MarketSnapshot{
		StationID: "drift_stn", StationName: "Drift", SystemID: "Trader's Rest", SystemName: "Trader's Rest",
		CapturedAt: now,
		Orders: []market.Order{{
			StationID: "drift_stn", ItemID: "fusion_core", Side: "sell",
			PriceEach: 5, Quantity: 10, CapturedAt: now,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	kb := knowledge.NewMemoryKB()
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID: "home", Name: "Home",
		Connections: []knowledge.SystemConnection{{SystemID: "traders_rest"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "traders_rest", Name: "Trader's Rest"}); err != nil {
		t.Fatal(err)
	}

	got, err := Find(ctx, col, kb, "fusion_core", 0, "home", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 result, got %+v", got)
	}
	if got[0].Jumps != 1 {
		t.Errorf("display-name system must resolve to 1 jump, got %d", got[0].Jumps)
	}
}

func TestFindUnreachableSortsLast(t *testing.T) {
	col := seedMarket(t)
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	// Graph knows home-mid only; "far" exists in the market but not the graph.
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID: "home", Name: "home",
		Connections: []knowledge.SystemConnection{{SystemID: "mid"}},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := Find(ctx, col, kb, "fusion_core", 0, "home", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 results, got %d", len(got))
	}
	last := got[2]
	if last.StationID != "far_stn" || (last.Jumps >= 0 && last.Jumps < navigation.RouteInf) {
		t.Errorf("unreachable station must sort last with non-finite jumps, got %+v", last)
	}
}
