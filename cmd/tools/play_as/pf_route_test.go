package main

import (
	"math"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// pfSys builds a KB system with lanes to the given ids.
func pfSys(id string, x, y float64, stronghold bool, lanes ...string) knowledge.System {
	s := knowledge.System{ID: id, Name: strings.ToUpper(id[:1]) + id[1:], Position: game.Position{X: x, Y: y}, IsStronghold: stronghold}
	for _, l := range lanes {
		s.Connections = append(s.Connections, knowledge.SystemConnection{SystemID: l})
	}
	return s
}

// pfFixture: an old-galaxy chain sol—a—b—c along +x, a stronghold off c,
// an unpositioned system off c, and a two-system frontier cluster f—t
// with no lane to the old galaxy.
func pfFixture() ([]knowledge.System, map[string]bool) {
	systems := []knowledge.System{
		pfSys("sol", 0, 0, false, "a"),
		pfSys("a", 100, 0, false, "sol", "b"),
		pfSys("b", 1000, 0, false, "a", "c"),
		pfSys("c", 1500, 0, false, "b", "s", "u"),
		pfSys("s", 1650, 300, true, "c"),
		pfSys("u", 0, 0, false, "c"),
		pfSys("f", 1700, 0, false, "t"),
		pfSys("t", 2100, 0, false, "f"),
	}
	return systems, map[string]bool{"sol": true, "b": true, "s": true}
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestPlanPathfinderRouteFrontierTarget(t *testing.T) {
	systems, stations := pfFixture()
	p, err := planPathfinderRoute(systems, stations, "sol", "t", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.LaneJumps != -1 {
		t.Errorf("LaneJumps = %d, want -1 (frontier)", p.LaneJumps)
	}
	if p.Station == nil || p.Station.ID != "b" || p.Station.Jumps != 2 || p.Station.Arrival != "f" ||
		!near(p.Station.Distance, 700) || !near(p.Station.Bearing, 0) || p.Station.Ticks != 70 || p.Station.ArrivalJumps != 1 {
		t.Errorf("Station = %+v, want b, 2 jumps, arrive f 700 LY bearing 0, 70 ticks, 1 lane jump on", p.Station)
	}
	if p.Nearest == nil || p.Nearest.ID != "c" || p.Nearest.Jumps != 3 || !near(p.Nearest.Distance, 200) {
		t.Errorf("Nearest = %+v, want c, 3 jumps, 200 LY", p.Nearest)
	}
	if p.Direct == nil || p.Direct.ID != "sol" || p.Direct.Jumps != 0 || p.Direct.Arrival != "f" || !near(p.Direct.Distance, 1700) {
		t.Errorf("Direct = %+v, want from sol to f, 1700 LY", p.Direct)
	}
}

func TestPlanPathfinderRouteSkipsStrongholdsUnlessUnlocked(t *testing.T) {
	systems, stations := pfFixture()
	p, err := planPathfinderRoute(systems, stations, "sol", "t", true)
	if err != nil {
		t.Fatal(err)
	}
	want := math.Hypot(50, 300)
	if p.Station == nil || p.Station.ID != "s" || !near(p.Station.Distance, want) {
		t.Errorf("unlocked Station = %+v, want stronghold s at %.1f LY", p.Station, want)
	}
}

func TestPlanPathfinderRouteFlagsSystemsOnTheLine(t *testing.T) {
	systems, stations := pfFixture()
	// A system 50 LY off the b→f line could capture the drift; one 150 LY
	// off it is clear.
	systems = append(systems, pfSys("x", 1300, 50, false), pfSys("y", 1200, 150, false))
	p, err := planPathfinderRoute(systems, stations, "sol", "t", false)
	if err != nil {
		t.Fatal(err)
	}
	// c sits on the line too; nearest along the line comes first.
	if w := p.Station.InTheWay; len(w) != 2 || w[0].ID != "x" || w[1].ID != "c" {
		t.Errorf("Station.InTheWay = %+v, want [x c]", w)
	}
}

func TestPlanPathfinderRouteLaneReachableTarget(t *testing.T) {
	systems, stations := pfFixture()
	p, err := planPathfinderRoute(systems, stations, "sol", "c", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.LaneJumps != 3 {
		t.Errorf("LaneJumps = %d, want 3", p.LaneJumps)
	}
}

func TestPlanPathfinderRouteUnknownPosition(t *testing.T) {
	systems, stations := pfFixture()
	if _, err := planPathfinderRoute(systems, stations, "sol", "u", false); err == nil ||
		!strings.Contains(err.Error(), "no known position") {
		t.Errorf("err = %v, want no known position", err)
	}
}

func TestResolvePFTargetByName(t *testing.T) {
	systems, _ := pfFixture()
	if s := resolvePFTarget("  T ", systems); s == nil || s.ID != "t" {
		t.Errorf("resolve by name = %+v, want t", s)
	}
	if s := resolvePFTarget("nope", systems); s != nil {
		t.Errorf("resolve nope = %+v, want nil", s)
	}
}

func TestRenderPathfinderRoute(t *testing.T) {
	systems, stations := pfFixture()
	p, err := planPathfinderRoute(systems, stations, "sol", "t", false)
	if err != nil {
		t.Fatal(err)
	}
	p.Source = "estimate"
	out := renderPathfinderRoute(p)
	for _, want := range []string{
		"No lane route", "estimate", "Nearest station", "B (b)", "jump 0", "then 1 lane jump",
		"Nearest system", "C (c)", "From here",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
}

func TestPathfinderDriveState(t *testing.T) {
	defs := map[string]game.ModuleDefinition{
		"m1": {ID: "m1", Name: "Pulse Laser", TypeID: "pulse_laser_i"},
		"m2": {ID: "m2", Name: "Pathfinder Drive", TypeID: "pathfinder_drive"},
	}
	cases := []struct {
		mods []string
		want string
	}{
		{[]string{"m1", "m2"}, "fitted"},
		{[]string{"pathfinder_drive"}, "fitted"},
		{[]string{"m1"}, "missing"},
		{nil, "missing"},
		{[]string{"uuid-without-def"}, ""},
	}
	for _, c := range cases {
		if got := pathfinderDriveState(game.Ship{Modules: c.mods}, defs); got != c.want {
			t.Errorf("modules %v: got %q, want %q", c.mods, got, c.want)
		}
	}
}

func TestRenderPathfinderRouteWarnsMissingDrive(t *testing.T) {
	systems, stations := pfFixture()
	p, err := planPathfinderRoute(systems, stations, "sol", "t", false)
	if err != nil {
		t.Fatal(err)
	}
	p.Drive = "missing"
	if out := renderPathfinderRoute(p); !strings.Contains(out, "active ship has no pathfinder_drive") {
		t.Errorf("render missing drive warning:\n%s", out)
	}
	p.Drive = "fitted"
	if out := renderPathfinderRoute(p); strings.Contains(out, "pathfinder_drive") {
		t.Errorf("render warns although fitted:\n%s", out)
	}
}
