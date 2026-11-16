package worker

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// burnFake is fakeClient with a ship that burns perJump fuel on every jump,
// docks only after travelling to a station POI, and refuels to full there.
type burnFake struct {
	*fakeClient
	perJump  float64
	at       string // current POI ("" = arrival point)
	stations map[string]bool
	// pendingTravel is how many Travel calls fail with the server's
	// action_pending error before one goes through.
	pendingTravel int
}

func (b *burnFake) Jump(ctx context.Context, sys string) (*game.JumpResult, error) {
	b.calls = append(b.calls, "jump:"+sys)
	if b.state.Fuel < b.perJump {
		return nil, errors.New("Insufficient fuel for jump")
	}
	b.state.Fuel -= b.perJump
	b.state.Ship.Fuel = b.state.Fuel
	b.state.Doc = false
	b.at = ""
	return &game.JumpResult{}, nil
}

func (b *burnFake) Travel(ctx context.Context, poi string) (*game.TravelResult, error) {
	b.calls = append(b.calls, "travel:"+poi)
	if b.pendingTravel > 0 {
		b.pendingTravel--
		return nil, errors.New("action pending: another action () is in progress")
	}
	if b.state.Fuel < 1 {
		return nil, errors.New("Insufficient fuel for travel")
	}
	b.state.Fuel--
	b.state.Ship.Fuel = b.state.Fuel
	b.at = poi
	return &game.TravelResult{}, nil
}

func (b *burnFake) Dock(ctx context.Context) error {
	b.calls = append(b.calls, "dock")
	if !b.stations[b.at] {
		return errors.New("No station at this location")
	}
	b.state.Doc = true
	return nil
}

func (b *burnFake) Refuel(ctx context.Context) error {
	b.calls = append(b.calls, "refuel")
	if !b.state.Doc {
		return errors.New("not docked")
	}
	b.state.Fuel = b.state.MaxFuel
	b.state.Ship.Fuel = b.state.Fuel
	return nil
}

// routeFake is a five-jump route at 3 fuel a jump (15 total) from a
// station-less origin point, with a public station in the third system.
func routeFake(t *testing.T, fuel float64) (*burnFake, knowledge.Base) {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	for _, id := range []string{"s0", "s1", "s2", "s3", "s4", "s5"} {
		if err := kb.RememberSystem(ctx, knowledge.System{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "s3_stn", SystemID: "s3", Type: "station"}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: "s3_base", POIID: "s3_stn", PublicAccess: true}); err != nil {
		t.Fatal(err)
	}
	st := &game.State{Fuel: fuel, MaxFuel: 110}
	st.Ship.Fuel, st.Ship.MaxFuel = fuel, 110
	st.Player.Standings = map[string]game.EmpireStanding{"pirate_x": {Baseline: 10}}
	f := &fakeClient{
		state: st,
		route: []game.RouteStep{{SystemID: "s0"}, {SystemID: "s1"}, {SystemID: "s2"}, {SystemID: "s3"}, {SystemID: "s4"}, {SystemID: "s5"}},
		raw:   map[string][]byte{"_last": []byte(`{"fuel_per_jump":3,"estimated_fuel":15,"fuel_available":15}`)},
	}
	return &burnFake{fakeClient: f, perJump: 3, stations: map[string]bool{"s3_stn": true}}, kb
}

// trader-4 on 2026-11-13 re-planned a 16-jump route from a station-less POI
// with exactly the 48 fuel it needed, arrived in The Anvil with 0 and could
// not make the in-system hop to the Arsenal. A ship short of the reserve must
// stop at a station on the way.
func TestAutopilotStopsToRefuelWhenBelowReserve(t *testing.T) {
	b, kb := routeFake(t, 15)
	if err := Autopilot(context.Background(), AutopilotDeps{Client: b, Out: io.Discard, KB: kb, AvoidSystemsPath: t.TempDir() + "/none.json"}, "s5", "s5_market"); err != nil {
		t.Fatalf("Autopilot: %v (calls %v)", err, b.calls)
	}
	i := slices.Index(b.calls, "travel:s3_stn")
	if i < 0 || !slices.Contains(b.calls[i:], "refuel") {
		t.Fatalf("calls %v, want a station stop and refuel at s3", b.calls)
	}
	if !slices.Contains(b.calls, "travel:s5_market") {
		t.Errorf("calls %v, want the final hop to s5_market", b.calls)
	}
	if b.state.Fuel < 3 {
		t.Errorf("arrived with %.0f fuel, want a reserve left", b.state.Fuel)
	}
}

// trucker-09 on 2026-11-14: the refuel stop's travel went out while the jump
// into the system was still pending server-side, failed with action_pending,
// and the ship flew on unrefuelled. The stop must wait for the pending action
// to clear and try again.
func TestRefuelStopWaitsOutPendingAction(t *testing.T) {
	b, kb := routeFake(t, 15)
	b.pendingTravel = 2
	if err := Autopilot(context.Background(), AutopilotDeps{Client: b, Out: io.Discard, KB: kb, AvoidSystemsPath: t.TempDir() + "/none.json"}, "s5", "s5_market"); err != nil {
		t.Fatalf("Autopilot: %v (calls %v)", err, b.calls)
	}
	i := slices.Index(b.calls, "travel:s3_stn")
	if i < 0 || !slices.Contains(b.calls[i:], "refuel") {
		t.Fatalf("calls %v, want the s3 stop to retry past action_pending and refuel", b.calls)
	}
}

func TestAutopilotNoStopWithReserve(t *testing.T) {
	b, kb := routeFake(t, 100)
	if err := Autopilot(context.Background(), AutopilotDeps{Client: b, Out: io.Discard, KB: kb, AvoidSystemsPath: t.TempDir() + "/none.json"}, "s5", "s5_market"); err != nil {
		t.Fatalf("Autopilot: %v", err)
	}
	for _, c := range b.calls {
		if strings.HasPrefix(c, "travel:s3_stn") || c == "refuel" {
			t.Fatalf("calls %v, want no refuel stop with fuel to spare", b.calls)
		}
	}
}

func TestRouteReserve(t *testing.T) {
	for _, tc := range []struct {
		perJump int
		poi     string
		want    int
	}{
		{3, "", 3},    // one spare jump
		{3, "stn", 6}, // plus the in-system hop
		{0, "stn", 0}, // unknown cost: no reserve
	} {
		if got := routeReserve(tc.perJump, tc.poi); got != tc.want {
			t.Errorf("routeReserve(%d,%q) = %d, want %d", tc.perJump, tc.poi, got, tc.want)
		}
	}
}

// longRouteFake: five jumps at 10 fuel (50 total) on a 40-fuel tank, with a
// station in s3 whose services are svc. Only a stop at s3 makes it flyable.
func longRouteFake(t *testing.T, svc map[string]bool) (*burnFake, knowledge.Base) {
	t.Helper()
	b, kb := routeFake(t, 40)
	if err := kb.RememberBase(context.Background(), knowledge.SpaceBase{ID: "s3_base", POIID: "s3_stn", PublicAccess: true, Services: svc}); err != nil {
		t.Fatal(err)
	}
	b.perJump = 10
	b.state.MaxFuel, b.state.Ship.MaxFuel = 40, 40
	b.raw["_last"] = []byte(`{"fuel_per_jump":10,"estimated_fuel":50,"fuel_available":40}`)
	return b, kb
}

// trader-8 on 2026-11-15 looped on a sale whose route needed ~111 fuel on a
// 110 tank: the departure gate compared the whole route to the tank and never
// counted the refuel stations on the way. A route longer than one tank flies
// when known refuel stations split it into legs the tank covers.
func TestAutopilotDepartsWhenRefuelStationsSplitALongRoute(t *testing.T) {
	b, kb := longRouteFake(t, map[string]bool{"refuel": true, "market": true})
	if err := Autopilot(context.Background(), AutopilotDeps{Client: b, Out: io.Discard, KB: kb, AvoidSystemsPath: t.TempDir() + "/none.json"}, "s5", ""); err != nil {
		t.Fatalf("Autopilot: %v (calls %v)", err, b.calls)
	}
	i := slices.Index(b.calls, "travel:s3_stn")
	if i < 0 || !slices.Contains(b.calls[i:], "refuel") || !slices.Contains(b.calls, "jump:s5") {
		t.Fatalf("calls %v, want a refuel stop at s3 and arrival in s5", b.calls)
	}
}

// A station without the refuel service does not count as a stop.
func TestAutopilotRefusesALongRouteWithoutRefuelService(t *testing.T) {
	b, kb := longRouteFake(t, map[string]bool{"market": true})
	err := Autopilot(context.Background(), AutopilotDeps{Client: b, Out: io.Discard, KB: kb, AvoidSystemsPath: t.TempDir() + "/none.json"}, "s5", "")
	if !errors.Is(err, ErrInsufficientRouteFuel) {
		t.Fatalf("want ErrInsufficientRouteFuel, got %v (calls %v)", err, b.calls)
	}
	if slices.ContainsFunc(b.calls, func(c string) bool { return strings.HasPrefix(c, "jump:") }) {
		t.Fatalf("must not jump, calls=%v", b.calls)
	}
}

func TestRouteFuelFeasible(t *testing.T) {
	cases := []struct {
		name                string
		fuel, tank, perJump int
		refuelAt            []bool
		want                bool
	}{
		{"fits without stops", 50, 50, 10, []bool{false, false, false, false, false}, true},
		{"no stations", 30, 30, 10, []bool{false, false, false, false, false}, false},
		{"stop splits it", 40, 40, 10, []bool{false, false, false, true, false}, true},
		{"arrives too dry to reach the station", 30, 30, 10, []bool{false, false, false, true, false}, false},
		{"stop too late", 30, 30, 10, []bool{false, false, false, false, true}, false},
		{"leg longer than the tank", 30, 30, 10, []bool{true, false, false, false, false}, false},
		{"origin top-up covers it", 10, 50, 10, []bool{true, false, false, false, false}, true},
	}
	for _, c := range cases {
		if got := routeFuelFeasible(c.fuel, c.tank, c.perJump, c.refuelAt); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// staleBurnFake is burnFake against the real client's cache: a jump burns fuel
// server-side but never touches the cached reading; only GetStatus (or a
// refuel reply) publishes it.
type staleBurnFake struct {
	*burnFake
	server float64
}

func (s *staleBurnFake) Jump(ctx context.Context, sys string) (*game.JumpResult, error) {
	s.calls = append(s.calls, "jump:"+sys)
	if s.server < s.perJump {
		return nil, errors.New("Insufficient fuel for jump")
	}
	s.server -= s.perJump
	s.state.Doc = false
	s.at = ""
	return &game.JumpResult{}, nil
}

func (s *staleBurnFake) Refuel(ctx context.Context) error {
	if err := s.burnFake.Refuel(ctx); err != nil {
		return err
	}
	s.server = s.state.Fuel
	return nil
}

func (s *staleBurnFake) Travel(ctx context.Context, poi string) (*game.TravelResult, error) {
	r, err := s.burnFake.Travel(ctx, poi)
	if err == nil {
		s.server--
	}
	return r, err
}

func (s *staleBurnFake) GetStatus(ctx context.Context) error {
	s.calls = append(s.calls, "get_status")
	s.state.Fuel, s.state.Ship.Fuel = s.server, s.server
	return nil
}

// trader-8, 2026-11-15 18:42: departed a 34-jump route on 89 fuel counting on
// refuel stations, but jump arrivals never refresh the cached fuel, so every
// stop check read the departure 89 and it flew past them all to 5/110.
func TestAutopilotRefuelStopReadsFreshFuel(t *testing.T) {
	b, kb := longRouteFake(t, map[string]bool{"refuel": true, "market": true})
	s := &staleBurnFake{burnFake: b, server: b.state.Fuel}
	if err := Autopilot(context.Background(), AutopilotDeps{Client: s, Out: io.Discard, KB: kb, AvoidSystemsPath: t.TempDir() + "/none.json"}, "s5", ""); err != nil {
		t.Fatalf("Autopilot: %v (calls %v)", err, s.calls)
	}
	if i := slices.Index(s.calls, "travel:s3_stn"); i < 0 || !slices.Contains(s.calls[i:], "refuel") {
		t.Fatalf("calls %v, want a refuel stop at s3", s.calls)
	}
}
