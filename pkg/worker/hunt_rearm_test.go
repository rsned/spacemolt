package worker

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/reload"
)

// huntRearmKB is a chain here -> a -> b -> homesys (the home depot, 3 jumps)
// with a market station in mkt, one jump from here. Every system is policed
// unless a test lowers mkt's level.
func huntRearmKB(t *testing.T, mktPolice int) knowledge.Base {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	sys := func(id string, police int, conns ...string) {
		s := knowledge.System{ID: id, Name: id, PoliceLevel: police, LastVisitedTick: 1}
		for _, c := range conns {
			s.Connections = append(s.Connections, knowledge.SystemConnection{SystemID: c})
		}
		if err := kb.RememberSystem(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	station := func(sysID, poi, base string) {
		if err := kb.RememberPOI(ctx, knowledge.POI{ID: poi, SystemID: sysID, Type: "station"}); err != nil {
			t.Fatal(err)
		}
		if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: base, POIID: poi, PublicAccess: true}); err != nil {
			t.Fatal(err)
		}
	}
	sys("here", 55, "mkt", "a")
	sys("mkt", mktPolice, "here")
	sys("a", 55, "here", "b")
	sys("b", 55, "a", "homesys")
	sys("homesys", 55, "b")
	station("mkt", "mkt_poi", "mkt_base")
	station("homesys", "home_poi", "home_base")
	return kb
}

func huntRearmDeps(t *testing.T, mktPolice int, sellers []market.ItemSeller) HuntDeps {
	t.Helper()
	return HuntDeps{
		KB: huntRearmKB(t, mktPolice), HomeStation: "home_base",
		Sellers: func(context.Context, string, float64) ([]market.ItemSeller, error) { return sellers, nil },
	}
}

func huntRearmSeller(system, station string) market.ItemSeller {
	return market.ItemSeller{ItemID: "std_rounds", StationID: station, SystemID: system, BestPrice: 10, TotalQty: 100}
}

func TestHuntRearmPrefersNearerMarketOverHome(t *testing.T) {
	deps := huntRearmDeps(t, 55, []market.ItemSeller{huntRearmSeller("mkt", "mkt_base")})
	sys, poi, why, ok := huntRearmDestination(context.Background(), deps, "here", []string{"std_rounds"})
	if !ok || sys != "mkt" || poi != "mkt_poi" {
		t.Fatalf("got %q/%q ok=%v (%s), want mkt/mkt_poi (base id mapped to its POI)", sys, poi, ok, why)
	}
	if why == "" {
		t.Error("why must say what was chosen")
	}
}

func TestHuntRearmPrefersHomeOnTie(t *testing.T) {
	// From a, home is 2 jumps (b, homesys) and mkt is 2 jumps (here, mkt).
	deps := huntRearmDeps(t, 55, []market.ItemSeller{huntRearmSeller("mkt", "mkt_base")})
	sys, poi, _, ok := huntRearmDestination(context.Background(), deps, "a", []string{"std_rounds"})
	if !ok || sys != "homesys" || poi != "home_poi" {
		t.Fatalf("tie: got %q/%q ok=%v, want home", sys, poi, ok)
	}
}

func TestHuntRearmSkipsUnpolicedSellers(t *testing.T) {
	deps := huntRearmDeps(t, huntPolicedMinLevel-1, []market.ItemSeller{huntRearmSeller("mkt", "mkt_base")})
	sys, poi, _, ok := huntRearmDestination(context.Background(), deps, "here", []string{"std_rounds"})
	if !ok || sys != "homesys" || poi != "home_poi" {
		t.Fatalf("got %q/%q ok=%v, want home: the nearer seller is unpoliced", sys, poi, ok)
	}
}

func TestHuntRearmFallsBackHomeWithNoSellers(t *testing.T) {
	deps := huntRearmDeps(t, 55, nil)
	deps.Sellers = nil
	sys, poi, _, ok := huntRearmDestination(context.Background(), deps, "here", []string{"std_rounds"})
	if !ok || sys != "homesys" || poi != "home_poi" {
		t.Fatalf("got %q/%q ok=%v, want home", sys, poi, ok)
	}
}

func TestHuntRearmNoHomeNoSellers(t *testing.T) {
	deps := huntRearmDeps(t, 55, nil)
	deps.Sellers = nil
	deps.HomeStation = ""
	if _, _, _, ok := huntRearmDestination(context.Background(), deps, "here", []string{"std_rounds"}); ok {
		t.Fatal("ok = true with no home and no sellers")
	}
}

// With the ammo item unknown (no LoadedAmmoID), the seller lookup is asked
// about every catalog ammo item of the weapon's ammo type, and the hunter ends
// docked at the chosen station.
func TestHuntGoRearmAsksForCatalogAmmoWhenNoneLoaded(t *testing.T) {
	f := newHuntFake(t, huntFakeOpts{cargoCapacity: 100})
	deps := huntDeps(f, io.Discard)
	deps.KB = huntRearmKB(t, 55)
	ctx := context.Background()
	if err := deps.KB.StoreItems(ctx, []knowledge.CatalogItem{
		{ID: "std_rounds", Name: "Std", Ammo: &knowledge.ItemAmmo{AmmoType: "autocannon"}},
		{ID: "ap_rounds", Name: "AP", Ammo: &knowledge.ItemAmmo{AmmoType: "autocannon"}},
		{ID: "missile_x", Name: "Missile", Ammo: &knowledge.ItemAmmo{AmmoType: "missile"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := deps.KB.RememberPOI(ctx, knowledge.POI{ID: "here_poi", SystemID: "here", Type: "station"}); err != nil {
		t.Fatal(err)
	}
	if err := deps.KB.RememberBase(ctx, knowledge.SpaceBase{ID: "here_base", POIID: "here_poi", PublicAccess: true}); err != nil {
		t.Fatal(err)
	}
	deps.HomeStation = "home_base"
	var asked []string
	deps.Sellers = func(_ context.Context, item string, _ float64) ([]market.ItemSeller, error) {
		asked = append(asked, item)
		if item == "ap_rounds" {
			return []market.ItemSeller{huntRearmSeller("here", "here_base")}, nil
		}
		return nil, nil
	}
	f.state.System.ID, f.state.CurrentSystem = "here", "here"
	f.state.CurrentPOI, f.state.Doc = "far_poi", true
	f.route = []game.RouteStep{{SystemID: "here"}}

	lo := reload.Loadout{Plan: []reload.Entry{{WeaponID: "w1", WeaponTypeID: "autocannon_ii", AmmoType: "autocannon"}}}
	if err := huntGoRearm(ctx, deps, io.Discard, "pirate-6", lo); err != nil {
		t.Fatalf("huntGoRearm: %v", err)
	}
	slices.Sort(asked)
	if !slices.Equal(asked, []string{"ap_rounds", "std_rounds"}) {
		t.Errorf("sellers asked about %v, want both autocannon ammo items and no missile", asked)
	}
	if f.state.CurrentPOI != "here_poi" || !f.state.Doc {
		t.Errorf("ended at %q docked=%v, want docked at here_poi", f.state.CurrentPOI, f.state.Doc)
	}
}

func TestHuntGoRearmNoDestinationErrors(t *testing.T) {
	f := newHuntFake(t, huntFakeOpts{cargoCapacity: 100})
	deps := huntDeps(f, io.Discard)
	deps.HomeStation = ""
	err := huntGoRearm(context.Background(), deps, io.Discard, "pirate-6", reload.Loadout{})
	if err == nil || !strings.Contains(err.Error(), "rearm") {
		t.Fatalf("err = %v, want a rearm destination error", err)
	}
}
