package worker

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// huntRoamGraph builds an undirected jump graph from edge pairs.
func huntRoamGraph(edges ...[2]string) navigation.JumpGraph {
	g := navigation.JumpGraph{}
	for _, e := range edges {
		g[e[0]] = append(g[e[0]], e[1])
		g[e[1]] = append(g[e[1]], e[0])
	}
	return g
}

// huntRoamChain returns the edges of a chain from -> n0 -> ... -> to that is
// exactly hops jumps long, with intermediate systems named prefix0..prefixN.
func huntRoamChain(from, to, prefix string, hops int) [][2]string {
	edges := make([][2]string, 0, hops)
	prev := from
	for i := range hops - 1 {
		next := fmt.Sprintf("%s%d", prefix, i)
		edges = append(edges, [2]string{prev, next})
		prev = next
	}
	return append(edges, [2]string{prev, to})
}

func TestHuntNextMoveTriesNearbyStationFirst(t *testing.T) {
	t.Parallel()
	edges := slices.Concat(
		huntRoamChain("here", "near_st", "a", 2),
		huntRoamChain("here", "mid_st", "b", 4),
		huntRoamChain("here", "far_st", "c", 7),
		huntRoamChain("here", huntHomeSystem, "h", 3),
	)
	g := huntRoamGraph(edges...)
	stations := []string{"far_st", "mid_st", "near_st"}

	kind, target := huntNextMove(g, "here", stations, huntRoam{}, nil)
	if kind != huntMoveStation || target != "near_st" {
		t.Fatalf("first move = %s %s, want station near_st (stations come before home)", kind, target)
	}
	kind, target = huntNextMove(g, "here", stations, huntRoam{Visited: []string{"here", "near_st"}}, nil)
	if kind != huntMoveStation || target != "mid_st" {
		t.Fatalf("with near_st visited = %s %s, want station mid_st", kind, target)
	}
	// far_st is 7 jumps out, beyond huntNearbyJumps: once the nearby ones are
	// tried, home (3 jumps) wins.
	kind, target = huntNextMove(g, "here", stations, huntRoam{Visited: []string{"near_st", "mid_st"}}, nil)
	if kind != huntMoveHome || target != huntHomeSystem {
		t.Fatalf("nearby stations all visited = %s %s, want home", kind, target)
	}
}

func TestHuntNextMoveGoesHomeWithin12(t *testing.T) {
	t.Parallel()
	systems := []knowledge.System{
		{ID: "capital_x", PoliceLevel: HaulCapitalPoliceLevel},
		{ID: huntHomeSystem, PoliceLevel: 0},
	}
	for _, tc := range []struct {
		hops       int
		wantKind   string
		wantTarget string
	}{
		{11, huntMoveHome, huntHomeSystem},
		{12, huntMoveHome, huntHomeSystem},
		{13, huntMoveCapital, "capital_x"},
	} {
		edges := append(huntRoamChain("here", huntHomeSystem, "h", tc.hops), [2]string{"here", "capital_x"})
		kind, target := huntNextMove(huntRoamGraph(edges...), "here", nil, huntRoam{}, systems)
		if kind != tc.wantKind || target != tc.wantTarget {
			t.Errorf("home %d jumps away: move = %s %s, want %s %s", tc.hops, kind, target, tc.wantKind, tc.wantTarget)
		}
	}
	// Standing at home counts as within reach.
	g := huntRoamGraph([2]string{huntHomeSystem, "capital_x"})
	if kind, _ := huntNextMove(g, huntHomeSystem, nil, huntRoam{}, systems); kind != huntMoveHome {
		t.Errorf("at home: move = %s, want home", kind)
	}
}

func TestHuntNextMoveSkipsVisitedCapitals(t *testing.T) {
	t.Parallel()
	systems := []knowledge.System{
		{ID: "cap_a", PoliceLevel: HaulCapitalPoliceLevel},
		{ID: "cap_b", PoliceLevel: HaulCapitalPoliceLevel},
		{ID: "town", PoliceLevel: 55},
	}
	g := huntRoamGraph(slices.Concat(
		huntRoamChain("here", "cap_a", "a", 2),
		huntRoamChain("here", "cap_b", "b", 4),
		[][2]string{{"here", "town"}},
	)...)

	if kind, target := huntNextMove(g, "here", nil, huntRoam{}, systems); kind != huntMoveCapital || target != "cap_a" {
		t.Fatalf("move = %s %s, want capital cap_a", kind, target)
	}
	if kind, target := huntNextMove(g, "here", nil, huntRoam{Visited: []string{"cap_a"}}, systems); kind != huntMoveCapital || target != "cap_b" {
		t.Fatalf("cap_a visited: move = %s %s, want capital cap_b", kind, target)
	}
	if kind, target := huntNextMove(g, "here", nil, huntRoam{Visited: []string{"cap_a", "cap_b"}}, systems); kind != "" {
		t.Fatalf("every capital visited: move = %s %s, want none", kind, target)
	}
}

// Cloverfield is Lawless, and home hunting is the one place that may hunt
// there; every other Lawless system stays refused, near home or not.
func TestHuntHomeGroundsWaiveLawlessOnlyForHome(t *testing.T) {
	t.Parallel()
	systems := []knowledge.System{
		{ID: huntHomeSystem, PoliceLevel: 0},
		{ID: "lawless_near", PoliceLevel: 0},
		{ID: "policed_near", PoliceLevel: 55},
		{ID: "policed_far", PoliceLevel: 80},
	}
	g := huntRoamGraph(slices.Concat(
		[][2]string{{huntHomeSystem, "lawless_near"}},
		huntRoamChain(huntHomeSystem, "policed_near", "p", 2),
		huntRoamChain(huntHomeSystem, "policed_far", "q", 4),
	)...)

	got := huntHomeGrounds(g, systems)
	if want := []string{huntHomeSystem, "policed_near"}; !slices.Equal(got, want) {
		t.Fatalf("home grounds = %v, want %v", got, want)
	}
	if huntHomeLawlessWaiver("lawless_near") || !huntHomeLawlessWaiver(huntHomeSystem) {
		t.Fatal("the Lawless waiver must cover cloverfield and nothing else")
	}

	// Outside home mode the ordinary gate still refuses cloverfield.
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, knowledge.System{ID: huntHomeSystem, PoliceLevel: 0, LastVisitedTick: 1}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "clover_cloud", SystemID: huntHomeSystem, Type: "gas_cloud"}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := huntGroundIn(ctx, kb, huntHomeSystem, "gas_cloud", true, nil); ok {
		t.Fatal("the policed hunting-ground gate must still refuse Lawless cloverfield")
	}
}

// huntRoamKB is a small galaxy for the pass-level roaming tests:
//
//	test_system (55, empire station haven_station)
//	  - east (55, empire station east_station), 2 jumps via e0
//	  - cloverfield (0, Conjunction Junction + clover_cloud), 1 jump
//	  - lawless_rim (0, rim_cloud), 1 jump from cloverfield
//
// It is wrapped as a huntNeedKB with an empty field guide, so a gauntlet with
// an empty board reaches the dry Station Need search.
func huntRoamKB(t *testing.T, withEast bool) *huntNeedFakeKB {
	t.Helper()
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	testConns := []knowledge.SystemConnection{{SystemID: huntHomeSystem}}
	if withEast {
		testConns = append(testConns, knowledge.SystemConnection{SystemID: "e0"})
		must(kb.RememberSystem(ctx, knowledge.System{ID: "e0", PoliceLevel: 55, LastVisitedTick: 1,
			Connections: []knowledge.SystemConnection{{SystemID: "test_system"}, {SystemID: "east"}}}))
		must(kb.RememberSystem(ctx, knowledge.System{ID: "east", PoliceLevel: 55, LastVisitedTick: 1,
			Connections: []knowledge.SystemConnection{{SystemID: "e0"}}}))
		must(kb.RememberPOI(ctx, knowledge.POI{ID: "east_station", SystemID: "east", Type: "station"}))
		must(kb.RememberBase(ctx, knowledge.SpaceBase{ID: "east_station", POIID: "east_station", PublicAccess: true, Empire: "solarian"}))
	}
	must(kb.RememberSystem(ctx, knowledge.System{ID: "test_system", PoliceLevel: 55, LastVisitedTick: 1, Connections: testConns}))
	must(kb.RememberPOI(ctx, knowledge.POI{ID: "haven_station", SystemID: "test_system", Type: "station"}))
	must(kb.RememberBase(ctx, knowledge.SpaceBase{ID: "haven_station", POIID: "haven_station", PublicAccess: true, Empire: "solarian"}))
	must(kb.RememberSystem(ctx, knowledge.System{ID: huntHomeSystem, PoliceLevel: 0, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "test_system"}, {SystemID: "lawless_rim"}}}))
	must(kb.RememberPOI(ctx, knowledge.POI{ID: huntHomePOI, SystemID: huntHomeSystem, Type: "station"}))
	must(kb.RememberBase(ctx, knowledge.SpaceBase{ID: huntHomePOI, POIID: huntHomePOI, PublicAccess: true}))
	must(kb.RememberPOI(ctx, knowledge.POI{ID: "clover_cloud", SystemID: huntHomeSystem, Type: "gas_cloud"}))
	must(kb.RememberSystem(ctx, knowledge.System{ID: "lawless_rim", PoliceLevel: 0, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: huntHomeSystem}}}))
	// Ranked above clover_cloud were it eligible: an asteroid belt.
	must(kb.RememberPOI(ctx, knowledge.POI{ID: "rim_belt", SystemID: "lawless_rim", Type: "asteroid_belt"}))
	must(kb.StoreShipClasses(ctx, []knowledge.ShipClassDef{{ID: "gauntlet", Category: "Combat", Tier: 2}}))
	return &huntNeedFakeKB{Base: kb, byName: map[string]string{}}
}

func huntRoamFake(t *testing.T, opts huntFakeOpts) *huntFake {
	t.Helper()
	f := newHuntFake(t, opts)
	f.state.Ship.ClassID = "gauntlet"
	return f
}

func TestHuntDryPassRoamsToNearbyStation(t *testing.T) {
	t.Parallel()
	f := huntRoamFake(t, huntFakeOpts{creatures: huntGrazers("c1")})
	var log strings.Builder
	deps := huntDeps(f, &log)
	deps.KB = huntRoamKB(t, true)

	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"undock", "find_route:east", "travel:east_station", "dock"} {
		if !called(f, want) {
			t.Errorf("dry pass did not %s; calls %v\nlog:\n%s", want, f.calls, log.String())
		}
	}
	if f.huntCalls != 0 {
		t.Errorf("a roaming pass hunted %d time(s); it should only move", f.huntCalls)
	}
	r, err := loadHuntRoam(huntRoamPath(deps.AgentsDir, deps.AgentID))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"test_system", "east"}; !slices.Equal(r.Visited, want) {
		t.Errorf("visited = %v, want %v", r.Visited, want)
	}
}

func TestHuntDryPassGoesHomeWhenStationsAreTried(t *testing.T) {
	t.Parallel()
	f := huntRoamFake(t, huntFakeOpts{creatures: huntGrazers("c1")})
	var log strings.Builder
	deps := huntDeps(f, &log)
	deps.KB = huntRoamKB(t, false)

	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"find_route:" + huntHomeSystem, "travel:" + huntHomePOI} {
		if !called(f, want) {
			t.Errorf("dry pass did not %s; calls %v\nlog:\n%s", want, f.calls, log.String())
		}
	}
	if f.huntCalls != 0 {
		t.Errorf("the pass that travels home hunted %d time(s); hunting waits for the next pass", f.huntCalls)
	}
}

func TestHuntDryPassAtHomeHuntsHomeGrounds(t *testing.T) {
	t.Parallel()
	f := huntRoamFake(t, huntFakeOpts{
		creatures: []huntCreature{{id: "c1"}, {id: "c2", species: "patina_grazer"}},
		startPOI:  huntHomePOI,
	})
	f.state.System.ID = huntHomeSystem
	var log strings.Builder
	deps := huntDeps(f, &log)
	deps.KB = huntRoamKB(t, false)
	// The one nearby empire station has already been tried since the last
	// job, so the next move is home, where the hunter already stands.
	if err := saveHuntRoam(huntRoamPath(deps.AgentsDir, deps.AgentID), huntRoam{Visited: []string{"test_system"}}); err != nil {
		t.Fatal(err)
	}

	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if !called(f, "travel:clover_cloud") {
		t.Fatalf("home hunt did not go to the cloverfield ground; calls %v\nlog:\n%s", f.calls, log.String())
	}
	if called(f, "rim_belt") {
		t.Errorf("home hunt went to a Lawless system other than cloverfield; calls %v", f.calls)
	}
	if f.huntCalls != 2 || slices.Contains(f.calls, "hunt:eel") {
		t.Errorf("hunt calls = %d (%v), want the two grazers\nlog:\n%s", f.huntCalls, f.calls, log.String())
	}
	if called(f, "accept_mission") || called(f, "complete_mission") {
		t.Errorf("home hunting takes no mission; calls %v", f.calls)
	}
}

func TestHuntFoundJobClearsVisited(t *testing.T) {
	t.Parallel()
	f := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	var log strings.Builder
	deps := huntDeps(f, &log)
	path := huntRoamPath(deps.AgentsDir, deps.AgentID)
	if err := saveHuntRoam(path, huntRoam{DryPasses: 3, Visited: []string{"east", "cap_a"}}); err != nil {
		t.Fatal(err)
	}
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	r, err := loadHuntRoam(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.DryPasses != 0 || len(r.Visited) != 0 {
		t.Errorf("after a found job: %+v, want dry passes 0 and nothing visited", r)
	}
}

// A dock repair can fail and is swallowed, so a dry pass on a damaged hull
// must neither roam nor start home hunting; the hunter stays docked.
func TestHuntDryPassHeldForRepair(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		atHome bool
	}{
		{"roam move", false},
		{"home hunt", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := huntFakeOpts{creatures: huntGrazers("c1"), shipHullFrac: 0.5}
			if tc.atHome {
				opts.startPOI = huntHomePOI
			}
			f := huntRoamFake(t, opts)
			var log strings.Builder
			deps := huntDeps(f, &log)
			deps.KB = huntRoamKB(t, !tc.atHome)
			if tc.atHome {
				f.state.System.ID = huntHomeSystem
				if err := saveHuntRoam(huntRoamPath(deps.AgentsDir, deps.AgentID), huntRoam{Visited: []string{"test_system"}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := Hunt(context.Background(), deps); err != nil {
				t.Fatal(err)
			}
			if called(f, "undock") || called(f, "find_route") || called(f, "travel:") || f.huntCalls != 0 {
				t.Errorf("damaged hull moved or hunted; calls %v\nlog:\n%s", f.calls, log.String())
			}
			if !strings.Contains(log.String(), "held for repair") {
				t.Errorf("no held-for-repair line; log:\n%s", log.String())
			}
		})
	}
}
