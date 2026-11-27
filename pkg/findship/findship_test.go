package findship

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// seedKB is a chain home - mid - far, plus an island system no lane reaches.
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
	for _, id := range []string{"far", "island"} {
		if err := kb.RememberSystem(ctx, knowledge.System{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	return kb
}

func listing(sys, stn, class string, price int) knowledge.StationShipListing {
	return knowledge.StationShipListing{
		SystemID: sys, SystemName: sys, StationID: stn, StationName: stn,
		ShipListing: knowledge.ShipListing{ClassID: class, ShipName: class, Price: price},
	}
}

func TestRankByJumpsThenPrice(t *testing.T) {
	in := []knowledge.StationShipListing{
		listing("far", "far_stn", "gauntlet", 100),
		listing("island", "island_stn", "gauntlet", 50),
		listing("mid", "mid_stn_b", "gauntlet", 300),
		listing("mid", "mid_stn_a", "gauntlet", 200),
	}
	got := Rank(context.Background(), seedKB(t), in, "home", 0)
	want := []string{"mid_stn_a", "mid_stn_b", "far_stn", "island_stn"}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].StationID != w {
			t.Fatalf("result %d = %s, want %s (all: %+v)", i, got[i].StationID, w, got)
		}
	}
	if got[0].Jumps != 1 || got[2].Jumps != 2 || got[3].Jumps < navigation.RouteInf {
		t.Fatalf("jumps = %d,%d,%d; want 1,2,unreachable", got[0].Jumps, got[2].Jumps, got[3].Jumps)
	}
}

func TestRankLimitAndNoKB(t *testing.T) {
	in := []knowledge.StationShipListing{
		listing("far", "b", "gauntlet", 300),
		listing("mid", "a", "gauntlet", 100),
	}
	got := Rank(context.Background(), nil, in, "home", 1)
	if len(got) != 1 || got[0].StationID != "a" || got[0].Jumps != JumpsUnknown {
		t.Fatalf("no KB: want only the cheapest with unknown jumps, got %+v", got)
	}
}

func TestSearchShipListingsMatchesClassAndName(t *testing.T) {
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: filepath.Join(t.TempDir(), "kb.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	ctx := context.Background()
	store := func(stn string, ls ...knowledge.ShipListing) {
		if err := kb.StoreShipListings(ctx, knowledge.ShipListings{
			SystemID: "krynn", SystemName: "Krynn", StationID: stn, StationName: stn,
			Listings: ls, CapturedAt: time.Now().UTC(),
		}, "test"); err != nil {
			t.Fatal(err)
		}
	}
	store("war_citadel",
		knowledge.ShipListing{ListingID: "1", ClassID: "gauntlet", ShipName: "Gauntlet", Price: 180808},
		knowledge.ShipListing{ListingID: "2", ClassID: "war_wagon", ShipName: "War Wagon", Price: 90000})
	store("other", knowledge.ShipListing{ListingID: "3", ClassID: "cobble", ShipName: "Old Gauntlet Hull", Price: 5})

	for _, tc := range []struct {
		q    string
		want int
	}{{"gauntlet", 2}, {"GAUNT", 2}, {"war wagon", 1}, {"war_wagon", 1}, {"harmonic", 0}} {
		got, err := kb.SearchShipListings(ctx, tc.q)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != tc.want {
			t.Errorf("SearchShipListings(%q) = %d listings, want %d", tc.q, len(got), tc.want)
		}
	}
	got, _ := kb.SearchShipListings(ctx, "war wagon")
	if len(got) == 1 && (got[0].StationName != "war_citadel" || got[0].SystemID != "krynn" || got[0].Price != 90000 || got[0].CapturedAt.IsZero()) {
		t.Errorf("listing fields not carried: %+v", got[0])
	}
}
