package worker

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// tourFake is a game client standing in one system: Travel moves the ship,
// Dock docks it, Refuel fills the tank. Calls are recorded in order.
type tourFake struct {
	game.GameClient
	st        *game.State
	route     []game.RouteStep
	travelErr map[string]error
	calls     []string
}

func (f *tourFake) GetState() *game.State             { return f.st }
func (f *tourFake) GetSystem(context.Context) error   { return nil }
func (f *tourFake) GetStatus(context.Context) error   { return nil }
func (f *tourFake) GetLocation(context.Context) error { return nil }
func (f *tourFake) FindRoute(context.Context, string) ([]game.RouteStep, error) {
	return f.route, nil
}

func (f *tourFake) Travel(_ context.Context, poi string) (*game.TravelResult, error) {
	f.calls = append(f.calls, "travel "+poi)
	if err := f.travelErr[poi]; err != nil {
		return nil, err
	}
	f.st.CurrentPOI, f.st.Doc = poi, false
	return &game.TravelResult{POI: poi}, nil
}

func (f *tourFake) Dock(context.Context) error {
	f.calls = append(f.calls, "dock "+f.st.CurrentPOI)
	if p := systemPOI(f.st, f.st.CurrentPOI); p == nil || !p.HasBase {
		return errors.New("No station at this location")
	}
	f.st.Doc = true
	return nil
}

func (f *tourFake) Refuel(context.Context) error {
	f.calls = append(f.calls, "refuel "+f.st.CurrentPOI)
	f.st.Fuel = f.st.MaxFuel
	return nil
}

func tourSystem() *game.State {
	return &game.State{
		CurrentTick: 100,
		CurrentPOI:  "star",
		Fuel:        500, MaxFuel: 1000,
		System: game.SystemData{ID: "a", Name: "A", POIs: []game.POI{
			{ID: "star", Type: "sun"},
			{ID: "far_station", Type: "station", HasBase: true, BaseType: game.BaseTypeStation, Position: game.Position{X: 9}},
			{ID: "maw", Type: "black_hole", Position: game.Position{X: 1},
				Hazard: &serverapi.POIHazard{Type: serverapi.HazardTidalShear, Severity: 3}},
			{ID: "belt", Type: "asteroid_belt", Position: game.Position{X: 2}},
			{ID: "outpost", Type: "station", HasBase: true, BaseType: game.BaseTypeOutpost, Position: game.Position{X: 3}},
			{ID: "veil", Type: "nebula", Hidden: true, Position: game.Position{X: 4}},
		}},
	}
}

func TestPlanTourRouteNearestNeighbourFromStart(t *testing.T) {
	st := tourSystem()
	var got []string
	for _, p := range planTourRoute(st.System.POIs, "star") {
		got = append(got, p.ID)
	}
	want := []string{"star", "maw", "belt", "outpost", "veil", "far_station"}
	if !slices.Equal(got, want) {
		t.Errorf("route = %v, want %v", got, want)
	}
}

// The tour is what keeps the explorer fueled: every POI visited, the station
// docked at and refueled; hazardous, hidden and outpost POIs handled safely.
func TestSystemTourDocksAndRefuelsAtStations(t *testing.T) {
	f := &tourFake{st: tourSystem()}

	if err := systemTour(context.Background(), ExploreDeps{Client: f}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"travel belt",
		"travel outpost", // visited and captured, never docked: outposts refuse outsiders
		"travel far_station",
		"dock far_station",
		"refuel far_station",
	}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls =\n  %v\nwant\n  %v", f.calls, want)
	}
}

// A station visit with a full tank buys nothing.
func TestSystemTourSkipsRefuelWhenFull(t *testing.T) {
	st := tourSystem()
	st.Fuel = st.MaxFuel
	f := &tourFake{st: st}

	if err := systemTour(context.Background(), ExploreDeps{Client: f}, nil); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(f.calls, "refuel far_station") {
		t.Errorf("refueled a full tank: %v", f.calls)
	}
}

// Out of fuel for in-system hops ends the tour instead of failing every hop.
func TestSystemTourStopsWhenOutOfFuel(t *testing.T) {
	f := &tourFake{st: tourSystem(), travelErr: map[string]error{"belt": errors.New("Insufficient fuel for travel")}}

	if err := systemTour(context.Background(), ExploreDeps{Client: f}, nil); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(f.calls, "travel outpost") || slices.Contains(f.calls, "travel far_station") {
		t.Errorf("calls = %v, want the tour to stop after the failed hop", f.calls)
	}
}

func TestExploreNeedsFuelStop(t *testing.T) {
	for _, tc := range []struct {
		fuel, max float64
		want      bool
	}{
		{1000, 1020, false},
		{306, 1020, false}, // exactly 30%
		{305, 1020, true},
		{12, 1020, true},
		{0, 0, false}, // capacity unknown
	} {
		if got := exploreNeedsFuelStop(tc.fuel, tc.max); got != tc.want {
			t.Errorf("exploreNeedsFuelStop(%v, %v) = %v, want %v", tc.fuel, tc.max, got, tc.want)
		}
	}
}

// A low explorer goes to the nearest station and refuels instead of exploring.
func TestExploreFuelStopRefuelsAtNearestStation(t *testing.T) {
	st := tourSystem()
	st.Fuel = 100
	f := &tourFake{st: st, route: []game.RouteStep{{SystemID: "a"}}}
	kb := &fakeKB{
		systems: []knowledge.System{{ID: "a"}},
		conns:   undirected([2]string{"a", "b"}),
		pois:    map[string][]knowledge.POI{"a": {{ID: "far_station", SystemID: "a", Type: "station"}}},
		bases:   map[string]*knowledge.SpaceBase{"far_station": {PublicAccess: true}},
	}

	handled, err := exploreFuelStop(context.Background(), ExploreDeps{Client: f, KB: kb}, io.Discard, "a")
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v, want handled", handled, err)
	}
	want := []string{"travel far_station", "dock far_station", "refuel far_station"}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
}

// Healthy fuel leaves the explore step alone.
func TestExploreFuelStopNoopWhenFuelHealthy(t *testing.T) {
	f := &tourFake{st: tourSystem()}
	handled, err := exploreFuelStop(context.Background(), ExploreDeps{Client: f, KB: &fakeKB{}}, io.Discard, "a")
	if handled || err != nil || len(f.calls) != 0 {
		t.Errorf("handled=%v err=%v calls=%v, want untouched", handled, err, f.calls)
	}
}
