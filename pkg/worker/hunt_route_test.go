package worker

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Autopilot's per-route ExtraAvoid works like the operator's avoid list: a
// route through one is replaced by a KB detour, or refused.
func TestAutopilotExtraAvoid(t *testing.T) {
	f := izarRouteFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard, KB: avoidKB(false, true),
		AvoidSystemsPath: writeAvoidSystems(t, `{"systems":{}}`),
		ExtraAvoid:       map[string]string{"izar": "Lawless"},
	}, "rigel", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := jumps(f.calls); !slices.Equal(got, []string{"tidewater", "vega", "rigel"}) {
		t.Errorf("jumps = %v, want the detour tidewater, vega, rigel", got)
	}

	f = izarRouteFake()
	err = Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard, KB: avoidKB(false, false),
		AvoidSystemsPath: writeAvoidSystems(t, `{"systems":{}}`),
		ExtraAvoid:       map[string]string{"izar": "Lawless"},
	}, "rigel", "")
	if !errors.Is(err, ErrRouteThroughAvoided) || len(jumps(f.calls)) != 0 {
		t.Errorf("no detour: err %v, jumps %v; want refused and no jump", err, jumps(f.calls))
	}
}

// huntAddWild adds a Lawless system "wild" linking a and b.
func huntAddWild(t *testing.T, kb knowledge.Base, a, b string) {
	t.Helper()
	if err := kb.RememberSystem(context.Background(), knowledge.System{
		ID: "wild", Name: "Wild", PoliceLevel: 0, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: a}, {SystemID: b}},
	}); err != nil {
		t.Fatal(err)
	}
}

// find_route plans through a Lawless system to the need's ground; the hunter
// flies the policed detour instead (test_system -> pollux directly).
func TestHuntNeedGroundRouteStaysPoliced(t *testing.T) {
	kb := huntNeedKBFixture(t)
	huntAddWild(t, kb.Base, "test_system", "pollux")
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{creatures: huntGrazers("c1")})
	f.route = []game.RouteStep{{SystemID: "test_system"}, {SystemID: "wild", Name: "Wild"}, {SystemID: "pollux", Name: "Pollux"}}
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "jump:wild") {
		t.Fatalf("a hunter leg crossed a Lawless system; calls %v\n%s", f.calls, log.String())
	}
	if got := jumps(f.calls); len(got) == 0 || got[0] != "pollux" {
		t.Errorf("want the policed detour straight to pollux, jumps %v\n%s", got, log.String())
	}
}

// The same for a wildlife kill mission's ground.
func TestHuntKillMissionRouteStaysPoliced(t *testing.T) {
	kb := huntKB()
	huntAddWild(t, kb, "test_system", "pollux")
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 1}},
		creatures: huntGrazers("c1"),
	})
	c.route = []game.RouteStep{{SystemID: "test_system"}, {SystemID: "wild", Name: "Wild"}, {SystemID: "pollux", Name: "Pollux"}}
	var log strings.Builder
	deps := huntDeps(c, &log)
	deps.KB = kb
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(c, "jump:wild") {
		t.Fatalf("a wildlife kill leg crossed a Lawless system; calls %v\n%s", c.calls, log.String())
	}
}

// The delivery leg: when the only way to the target crosses Lawless space,
// the leg is refused and the parts wait aboard.
func TestHuntDeliveryRefusesALawlessRoute(t *testing.T) {
	kb := huntNeedKBFixture(t)
	ctx := context.Background()
	// levy is reachable only through wild.
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID: "pollux", Name: "Pollux", PoliceLevel: 30, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "test_system"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID: "levy", Name: "Levy", PoliceLevel: 80, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "wild"}},
	}); err != nil {
		t.Fatal(err)
	}
	huntAddWild(t, kb.Base, "test_system", "levy")
	f := newHuntNeedFake(t, "gauntlet", huntFakeOpts{
		heldAtStart:    true,
		activeMissions: []serverapi.ActiveMission{huntCausticActive(2)},
	})
	f.state.Ship.Cargo = []game.CargoItem{{ItemID: "caustic_sac", Quantity: 10}}
	f.route = []game.RouteStep{{SystemID: "test_system"}, {SystemID: "wild", Name: "Wild"}, {SystemID: "levy", Name: "Levy"}}
	var log strings.Builder
	if err := Hunt(context.Background(), huntNeedDeps(f, kb, &log)); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if called(f.huntFake, "jump:") || called(f.huntFake, "complete:") {
		t.Fatalf("delivered across Lawless space; calls %v\n%s", f.calls, log.String())
	}
	if !strings.Contains(log.String(), "NOT DEPARTING") {
		t.Errorf("the refusal must be logged, got:\n%s", log.String())
	}
}

// Going home: the final hop INTO cloverfield is the operator's waiver.
func TestHuntGoesHomeOnTheFinalHopIntoCloverfield(t *testing.T) {
	t.Parallel()
	f := huntRoamFake(t, huntFakeOpts{creatures: huntGrazers("c1")})
	f.route = []game.RouteStep{{SystemID: "test_system"}, {SystemID: huntHomeSystem, Name: "Cloverfield"}}
	var log strings.Builder
	deps := huntDeps(f, &log)
	deps.KB = huntRoamKB(t, false)
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if !called(f, "jump:"+huntHomeSystem) || !called(f, "travel:"+huntHomePOI) {
		t.Fatalf("the final hop into cloverfield is waived; calls %v\n%s", f.calls, log.String())
	}
}

// Roaming to a nearby station: find_route's plan through Lawless space is
// replaced by the policed detour (via e0), and a station reachable ONLY
// through Lawless space is never chosen at all.
func TestHuntRoamStaysPoliced(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		e0Police  int
		wantJumps []string
	}{
		{"detour", 55, []string{"e0", "east"}},
		{"only via Lawless", 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := huntRoamFake(t, huntFakeOpts{creatures: huntGrazers("c1")})
			f.route = []game.RouteStep{{SystemID: "test_system"}, {SystemID: "wild", Name: "Wild"}, {SystemID: "east", Name: "East"}}
			var log strings.Builder
			deps := huntDeps(f, &log)
			kb := huntRoamKB(t, true)
			huntAddWild(t, kb.Base, "test_system", "east")
			if err := kb.RememberSystem(context.Background(), knowledge.System{ID: "e0", PoliceLevel: tc.e0Police, LastVisitedTick: 1,
				Connections: []knowledge.SystemConnection{{SystemID: "test_system"}, {SystemID: "east"}}}); err != nil {
				t.Fatal(err)
			}
			deps.KB = kb
			if err := Hunt(context.Background(), deps); err != nil {
				t.Fatal(err)
			}
			if called(f, "jump:wild") {
				t.Fatalf("roamed across Lawless space; calls %v\n%s", f.calls, log.String())
			}
			if tc.wantJumps == nil {
				if called(f, "find_route:east") {
					t.Errorf("chose a station reachable only through Lawless space; calls %v\n%s", f.calls, log.String())
				}
				return
			}
			if got := jumps(f.calls); !slices.Equal(got, tc.wantJumps) {
				t.Errorf("jumps = %v, want the policed detour %v\n%s", got, tc.wantJumps, log.String())
			}
		})
	}
}

// Station Need choice: the delivery target must be within the job radius too.
func TestHuntPickNeedCapsTheDeliveryTarget(t *testing.T) {
	t.Parallel()
	jumpsTo := func(s string) int { return map[string]int{"near": 1, "far_target": 30, "t": 2}[s] }
	needs := []huntNeed{
		{MissionID: "far", Reward: 60000, GroundSystem: "near", SystemID: "far_target"},
		{MissionID: "close", Reward: 1000, GroundSystem: "near", SystemID: "t"},
	}
	got, ok := huntPickNeed(needs, jumpsTo, huntNearbyJumps)
	if !ok || got.MissionID != "close" {
		t.Fatalf("got %q ok=%v; a 30-jump delivery target is beyond radius %d", got.MissionID, ok, huntNearbyJumps)
	}
}

// Hop counts for need choice are policed hops: a Lawless shortcut does not
// count, and a target reachable only through Lawless space is unreachable.
func TestHuntJumpsFromCountsPolicedHops(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	sys := func(id string, police int, conns ...string) {
		t.Helper()
		var cs []knowledge.SystemConnection
		for _, c := range conns {
			cs = append(cs, knowledge.SystemConnection{SystemID: c})
		}
		if err := kb.RememberSystem(ctx, knowledge.System{ID: id, PoliceLevel: police, LastVisitedTick: 1, Connections: cs}); err != nil {
			t.Fatal(err)
		}
	}
	sys("a", 55, "lawless", "d")
	sys("lawless", 0, "a", "c", "island")
	sys("c", 55, "lawless", "e")
	sys("d", 55, "a", "e")
	sys("e", 55, "d", "c")
	sys("island", 80, "lawless")
	hops := huntJumpsFrom(ctx, kb, "a")
	if hops == nil {
		t.Fatal("no hop counter")
	}
	if got := hops("c"); got != 3 {
		t.Errorf("hops(c) = %d, want 3 (a-d-e-c, not through lawless)", got)
	}
	if got := hops("island"); got >= 0 {
		t.Errorf("hops(island) = %d, want unreachable (only via Lawless)", got)
	}
}
