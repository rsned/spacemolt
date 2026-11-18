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

// tourFake is a game client standing in one system: Travel moves the ship,
// Dock docks it, Refuel fills the tank. Calls are recorded in order.
type tourFake struct {
	game.GameClient
	st        *game.State
	route     []game.RouteStep
	travelErr map[string]error
	calls     []string
	surveyErr error
	// poiReplies are get_poi replies by POI id; last is the latest raw reply.
	poiReplies map[string]string
	last       []byte
}

func (f *tourFake) SurveySystem(context.Context) error {
	f.calls = append(f.calls, "survey")
	f.last = []byte(`{"system_id":"a","survey_power":40,"newly_revealed":[],"already_revealed":[]}`)
	return f.surveyErr
}
func (f *tourFake) GetShip(context.Context) error   { return nil }
func (f *tourFake) GetNearby(context.Context) error { f.last = []byte(`{}`); return nil }
func (f *tourFake) GetPOI(context.Context) error {
	f.last = []byte(`{}`)
	if r, ok := f.poiReplies[f.st.CurrentPOI]; ok {
		f.last = []byte(r)
	}
	return nil
}
func (f *tourFake) RawCommand(_ context.Context, command string, _ map[string]any) error {
	if command == "interact_poi" {
		f.calls = append(f.calls, "interact "+f.st.CurrentPOI)
		f.last = []byte(`{"message":"The archive opens.","data_copied":true}`)
	}
	return nil
}
func (f *tourFake) GetRawJSON(key string) []byte {
	if key == "_last" {
		return f.last
	}
	return nil
}
func (f *tourFake) Undock(context.Context) error {
	f.calls = append(f.calls, "undock "+f.st.CurrentPOI)
	f.st.Doc = false
	return nil
}

func (f *tourFake) GetState() *game.State           { return f.st }
func (f *tourFake) GetSystem(context.Context) error { return nil }
func (f *tourFake) GetStatus(context.Context) error { return nil }
func (f *tourFake) GetCargo(context.Context) error  { return nil }
func (f *tourFake) FactionWithdrawItems(context.Context, string, int) error {
	return errors.New("no_faction_storage")
}
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
			{ID: "veil", Type: "wormhole_entrance", Name: "The Veil", Hidden: true, Position: game.Position{X: 4}},
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
// docked at and refueled; hazardous and outpost POIs handled safely.
func TestSystemTourDocksAndRefuelsAtStations(t *testing.T) {
	f := &tourFake{st: tourSystem()}

	if err := systemTour(context.Background(), ExploreDeps{Client: f}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"survey", // first, so the POIs it reveals are in the route
		"travel belt",
		"travel outpost", // visited and captured, never docked: outposts refuse outsiders
		"travel veil",    // a charted hidden POI (wormhole mouth) is toured, never entered
		"travel far_station",
		"dock far_station",
		"refuel far_station",
		"undock far_station",
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

// A relic on the route is interacted with, once, and the reply recorded.
func TestSystemTourInteractsWithRelic(t *testing.T) {
	st := tourSystem()
	st.System.POIs = []game.POI{{ID: "star", Type: "sun"}, {ID: "belt", Type: "asteroid_belt", Position: game.Position{X: 2}},
		{ID: "vault", Type: "relic", Name: "Vault", Position: game.Position{X: 2.5}}}
	f := &tourFake{st: st, poiReplies: map[string]string{
		"vault": `{"poi":{"id":"vault","type":"relic"},"interaction":{"message":"A sealed archive hums.","command":"interact_poi"}}`,
	}}
	kb := knowledge.NewMemoryKB()

	if err := systemTour(context.Background(), ExploreDeps{Client: f, KB: kb, AgentID: "explorer-3"}, nil); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range f.calls {
		if c == "interact vault" {
			n++
		} else if strings.HasPrefix(c, "interact ") {
			t.Errorf("interacted with a POI that is no relic: %s", c)
		}
	}
	if n != 1 {
		t.Errorf("calls = %v, want exactly one interact at the relic", f.calls)
	}
	rows, err := kb.RelicInteractions(context.Background())
	if err != nil || len(rows) != 1 || rows[0].POIID != "vault" || rows[0].AgentID != "explorer-3" || rows[0].Outcome != knowledge.RelicDataCopied {
		t.Errorf("recorded %+v (err %v), want one data_copied row for vault", rows, err)
	}
}

// A ship with no survey gear still tours: the server's refusal is not fatal.
func TestSystemTourGoesOnWhenTheSurveyIsRefused(t *testing.T) {
	f := &tourFake{st: tourSystem(), surveyErr: errors.New("no survey scanner installed")}
	if err := systemTour(context.Background(), ExploreDeps{Client: f}, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.calls, "travel far_station") {
		t.Errorf("calls = %v, want the tour to run without a survey", f.calls)
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

// Every wormhole entrance in the system is reported, NEW when the KB lacks it.
func TestReportWormholesFlagsNewEntrances(t *testing.T) {
	st := tourSystem()
	st.System.POIs = append(st.System.POIs, game.POI{ID: "old_mouth", Type: "wormhole_entrance", Name: "Old Mouth",
		WormholeDestination: "praecipua", ExpiresAt: "2026-11-11T00:00:00Z"})
	kb := &fakeKB{pois: map[string][]knowledge.POI{"a": {{ID: "old_mouth"}}}}
	var b strings.Builder

	reportWormholes(context.Background(), ExploreDeps{KB: kb}, &b, st.System, st.System.POIs)

	out := b.String()
	for _, want := range []string{
		"WORMHOLE ENTRANCE (NEW): The Veil [veil] in A — destination uncharted",
		"WORMHOLE ENTRANCE (known): Old Mouth [old_mouth] in A — destination praecipua, expires 2026-11-11T00:00:00Z",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "WORMHOLE") != 2 {
		t.Errorf("want exactly the two entrances reported:\n%s", out)
	}
}

func TestLiveLaneList(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      bool
	}{
		{"own system, no lanes", `{"action":"get_system","system":{"id":"the_sanctum","name":"The Sanctum","connections":[]}}`, true},
		{"own system, lanes", `{"action":"get_system","system":{"id":"the_sanctum","connections":[{"system_id":"x"}]}}`, true},
		{"other system", `{"action":"get_system","system":{"id":"nexus_prime","connections":[]}}`, false},
		{"lanes omitted", `{"action":"get_system","system":{"id":"the_sanctum"}}`, false},
		{"transit variant", `{"action":"get_system","in_transit":true,"to_system":"the_sanctum"}`, false},
		{"wrapped", `{"command":"get_system","result":{"system":{"id":"the_sanctum","connections":[]}}}`, true},
		{"nothing", ``, false},
	} {
		if got := liveLaneList([]byte(tc.raw), "the_sanctum", "The Sanctum"); got != tc.want {
			t.Errorf("%s: liveLaneList = %v, want %v", tc.name, got, tc.want)
		}
	}
}
