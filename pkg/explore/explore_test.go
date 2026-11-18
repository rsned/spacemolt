package explore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// fakeClient is the slice of game.GameClient the explore touches. It records
// every command in calls, in order.
type fakeClient struct {
	game.GameClient

	state *game.State
	raw   map[string][]byte
	calls []string

	surveyReplies []string         // one per survey_system call; the last repeats
	revealOnSurv  []game.POI       // appended to the system by the first survey
	travelErr     map[string]error // by POI id
	onTravel      func(poiID string)
	poiReplies    map[string]string // get_poi reply by POI id
	rawCommand    func(command string) (reply string, err error)
	repairErr     error
	onRepair      func()
	dockErr       error
}

func newFakeClient(pois ...game.POI) *fakeClient {
	st := &game.State{}
	st.System.ID = "far_wick"
	st.System.Name = "Far Wick"
	st.System.POIs = pois
	if len(pois) > 0 {
		st.CurrentPOI = pois[0].ID
	}
	st.Ship.Speed = 2
	st.Ship.Hull, st.Ship.MaxHull = 480, 480
	return &fakeClient{state: st, raw: map[string][]byte{}}
}

func (c *fakeClient) GetState() *game.State        { return c.state }
func (c *fakeClient) GetRawJSON(key string) []byte { return c.raw[key] }
func (c *fakeClient) GetShip(context.Context) error {
	c.calls = append(c.calls, "get_ship")
	return nil
}
func (c *fakeClient) GetStatus(context.Context) error {
	c.calls = append(c.calls, "get_status")
	return nil
}
func (c *fakeClient) GetNearby(context.Context) error {
	c.calls = append(c.calls, "get_nearby")
	return nil
}

func (c *fakeClient) GetSystem(context.Context) error {
	c.calls = append(c.calls, "get_system")
	return nil
}

func (c *fakeClient) SurveySystem(context.Context) error {
	n := 0
	for _, call := range c.calls {
		if call == "survey_system" {
			n++
		}
	}
	c.calls = append(c.calls, "survey_system")
	if len(c.surveyReplies) == 0 {
		return errors.New("no scanner fitted")
	}
	if n == 0 {
		c.state.System.POIs = append(c.state.System.POIs, c.revealOnSurv...)
	}
	c.raw["_last"] = []byte(c.surveyReplies[min(n, len(c.surveyReplies)-1)])
	return nil
}

func (c *fakeClient) Travel(_ context.Context, poiID string) (*game.TravelResult, error) {
	c.calls = append(c.calls, "travel "+poiID)
	if err := c.travelErr[poiID]; err != nil {
		return nil, err
	}
	c.state.CurrentPOI = poiID
	c.state.Doc = false
	if c.onTravel != nil {
		c.onTravel(poiID)
	}
	return &game.TravelResult{POI: poiID}, nil
}

func (c *fakeClient) Dock(context.Context) error {
	c.calls = append(c.calls, "dock")
	if c.dockErr != nil {
		return c.dockErr
	}
	c.state.Doc = true
	return nil
}

func (c *fakeClient) Undock(context.Context) error {
	c.calls = append(c.calls, "undock")
	c.state.Doc = false
	return nil
}

func (c *fakeClient) Refuel(context.Context) error {
	c.calls = append(c.calls, "refuel")
	return nil
}

func (c *fakeClient) GetPOI(context.Context) error {
	c.calls = append(c.calls, "get_poi")
	c.raw["poi"] = []byte(c.poiReplies[c.state.CurrentPOI])
	return nil
}

func (c *fakeClient) RawCommand(_ context.Context, command string, _ map[string]any) error {
	c.calls = append(c.calls, command)
	if c.rawCommand == nil {
		return fmt.Errorf("unexpected %s", command)
	}
	reply, err := c.rawCommand(command)
	if err != nil {
		return err
	}
	c.raw["_last"] = []byte(reply)
	return nil
}

func (c *fakeClient) Repair(context.Context) error {
	c.calls = append(c.calls, "repair")
	if c.repairErr != nil {
		return c.repairErr
	}
	if c.onRepair != nil {
		c.onRepair()
	}
	return nil
}

func (c *fakeClient) travels() []string {
	var out []string
	for _, call := range c.calls {
		if id, ok := strings.CutPrefix(call, "travel "); ok {
			out = append(out, id)
		}
	}
	return out
}

func (c *fakeClient) count(call string) int {
	n := 0
	for _, got := range c.calls {
		if got == call {
			n++
		}
	}
	return n
}

// logReporter keeps the progress lines so tests can assert on what was logged.
type logReporter struct{ lines []string }

func (r *logReporter) Printf(format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}
func (*logReporter) Raw(string, []byte) {}

func (r *logReporter) has(sub string) bool {
	return slices.ContainsFunc(r.lines, func(l string) bool { return strings.Contains(l, sub) })
}

func testDeps(c *fakeClient) (Deps, *logReporter) {
	rep := &logReporter{}
	return Deps{
		Client:     c,
		KB:         knowledge.NewMemoryKB(),
		AgentID:    "explorer-1",
		Report:     rep,
		Settle:     func(context.Context, time.Duration) {},
		HasScanner: func(*game.State) bool { return true },
	}, rep
}

func poi(id, typ string, x float64) game.POI {
	return game.POI{ID: id, Name: id, Type: typ, Position: game.Position{X: x}}
}

// The survey runs first so the hidden POIs it reveals are part of the tour.
func TestSystemSurveysBeforeTouring(t *testing.T) {
	c := newFakeClient(poi("star", "star", 0), poi("belt", "asteroid_belt", 1))
	c.revealOnSurv = []game.POI{poi("hidden_vault", "relic", 2)}
	c.surveyReplies = []string{
		`{"result":{"system_id":"far_wick","survey_power":90,"newly_revealed":[{"id":"hidden_vault","name":"Hidden Vault","type":"relic"}]}}`,
		`{"result":{"system_id":"far_wick","survey_power":90}}`,
	}
	d, rep := testDeps(c)

	res, err := System(context.Background(), d, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// A pass that revealed something is repeated; the empty second pass ends it.
	if n := c.count("survey_system"); n != 2 {
		t.Errorf("survey_system sent %d times, want 2", n)
	}
	if !rep.has("Survey power: 90") {
		t.Errorf("survey power not reported; lines: %q", rep.lines)
	}

	first := func(call string) int { return slices.Index(c.calls, call) }
	if s, g, tr := first("survey_system"), first("get_system"), first("travel belt"); s < 0 || g < s || tr < g {
		t.Errorf("want survey_system, then get_system, then travel; calls: %v", c.calls)
	}
	if got, want := c.travels(), []string{"belt", "hidden_vault"}; !slices.Equal(got, want) {
		t.Errorf("travelled to %v, want %v", got, want)
	}
	if res.SurveyPower != 90 {
		t.Errorf("SurveyPower=%d, want the reply's 90", res.SurveyPower)
	}
	if res.Revealed != 1 {
		t.Errorf("Revealed=%d, want 1", res.Revealed)
	}
	if res.SystemID != "far_wick" || len(res.POIs) != 3 {
		t.Fatalf("result %+v, want far_wick with 3 POIs", res)
	}
	for _, p := range res.POIs {
		if !p.Visited || p.Skipped != "" {
			t.Errorf("POI %s: visited=%v skipped=%q, want visited", p.ID, p.Visited, p.Skipped)
		}
	}
	if res.Stopped != "" {
		t.Errorf("Stopped=%q, want none", res.Stopped)
	}
}

// A hop the ship cannot fuel ends the tour instead of failing every later hop.
func TestSystemStopsWhenOutOfFuel(t *testing.T) {
	c := newFakeClient(poi("star", "star", 0), poi("belt", "asteroid_belt", 1), poi("far", "planet", 9))
	c.travelErr = map[string]error{"belt": errors.New("Insufficient fuel for travel")}
	d, _ := testDeps(c)
	d.HasScanner = func(*game.State) bool { return false }

	res, err := System(context.Background(), d, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stopped != StoppedFuel {
		t.Errorf("Stopped=%q, want %q", res.Stopped, StoppedFuel)
	}
	if got := c.travels(); len(got) != 1 {
		t.Errorf("kept travelling after the fuel refusal: %v", got)
	}
}

// A station is docked at, captured and refuelled; other POIs get the POI update.
func TestSystemStationStop(t *testing.T) {
	c := newFakeClient(poi("star", "star", 0), poi("depot", "station", 1))
	d, _ := testDeps(c)
	d.HasScanner = func(*game.State) bool { return false }
	var captured, updated int
	d.StationCapture = func(context.Context) error { captured++; return nil }
	d.UpdatePOI = func(context.Context) error { updated++; return nil }

	if _, err := System(context.Background(), d, Options{RefuelAtStations: true}); err != nil {
		t.Fatal(err)
	}
	if captured != 1 || updated != 1 {
		t.Errorf("station captures=%d, POI updates=%d; want 1 and 1", captured, updated)
	}
	dock, refuel, undock := slices.Index(c.calls, "dock"), slices.Index(c.calls, "refuel"), slices.Index(c.calls, "undock")
	if dock < 0 || refuel < dock || undock < refuel {
		t.Errorf("want dock, refuel, undock in order; calls: %v", c.calls)
	}
}
