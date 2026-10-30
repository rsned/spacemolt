package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/knowledge/knowledgetest"
)

// surveyFake replays one survey_system reply per call, the way the real client
// leaves each reply under the "_last" raw-JSON key.
type surveyFake struct {
	game.GameClient
	replies []serverapi.SurveySystemResponse
	err     error
	calls   int
	last    []byte
}

func (f *surveyFake) SurveySystem(context.Context) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	r := serverapi.SurveySystemResponse{SystemID: "ashford"}
	if f.calls <= len(f.replies) {
		r = f.replies[f.calls-1]
	} else if len(f.replies) > 0 {
		r = f.replies[len(f.replies)-1]
	}
	// The terminal frame nests the payload under "result".
	f.last, _ = json.Marshal(map[string]any{"command": "survey_system", "result": r, "tick": 100})
	return nil
}

func (f *surveyFake) GetRawJSON(key string) []byte {
	if key == "_last" {
		return f.last
	}
	return nil
}

func (f *surveyFake) GetState() *game.State {
	return &game.State{CurrentTick: 100}
}

func revealed(id string) []serverapi.RevealedPOI {
	return []serverapi.RevealedPOI{{
		ID: id, Name: id, Type: "asteroid_belt",
		Resources: []serverapi.SurveyResource{{ResourceID: "iron_ore", Richness: 40, Remaining: 900, MaxRemaining: 1000}},
	}}
}

func surveyKB(t *testing.T) *knowledge.SQLiteKB {
	t.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: knowledgetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	return kb
}

// An explorer flew past hidden POIs without ever looking for them: the explore
// step never ran survey_system, so a Survey Vessel's high-tier scanners did
// nothing. A survey re-runs while it keeps revealing, and records what it finds.
func TestSurveyHereLoopsUntilNothingNewAndSavesPOIs(t *testing.T) {
	kb := surveyKB(t)
	c := &surveyFake{replies: []serverapi.SurveySystemResponse{
		{SystemID: "ashford", NewlyRevealed: revealed("deep_vein")},
		{SystemID: "ashford", AlreadyRevealed: revealed("deep_vein")},
	}}

	n, err := SurveyHere(context.Background(), c, kb, "explorer-8", nil)
	if err != nil {
		t.Fatalf("SurveyHere: %v", err)
	}
	if c.calls != 2 || n != 1 {
		t.Errorf("calls=%d newly=%d, want 2 calls and 1 newly revealed", c.calls, n)
	}
	pois, err := kb.GetPOIs(context.Background(), "ashford")
	if err != nil {
		t.Fatal(err)
	}
	if len(pois) != 1 || pois[0].ID != "deep_vein" || !pois[0].Hidden {
		t.Fatalf("KB POIs = %+v, want deep_vein marked hidden", pois)
	}
}

// A ship without a scanner gets one refusal and stops, with a sentinel the
// explore step can treat as "nothing to do here".
func TestSurveyHereNoScanner(t *testing.T) {
	c := &surveyFake{err: errors.New("survey_system: requires a survey scanner module")}

	_, err := SurveyHere(context.Background(), c, surveyKB(t), "explorer-1", nil)
	if !errors.Is(err, ErrNoSurveyScanner) {
		t.Fatalf("err = %v, want ErrNoSurveyScanner", err)
	}
	if c.calls != 1 {
		t.Errorf("calls = %d, want 1", c.calls)
	}
}

// A server that reveals something on every pass must not hold the explorer in
// one system forever.
func TestSurveyHereCapsPasses(t *testing.T) {
	c := &surveyFake{replies: []serverapi.SurveySystemResponse{{SystemID: "ashford", NewlyRevealed: revealed("x")}}}

	if _, err := SurveyHere(context.Background(), c, surveyKB(t), "explorer-8", nil); err != nil {
		t.Fatal(err)
	}
	if c.calls != maxSurveyPasses {
		t.Errorf("calls = %d, want the cap of %d", c.calls, maxSurveyPasses)
	}
}

// exploreFake is a surveyFake that also stands in a known system and answers
// find_route, so an explore step can run end to end.
type exploreFake struct {
	surveyFake
	route []game.RouteStep
}

func (f *exploreFake) GetState() *game.State {
	return &game.State{CurrentTick: 100, System: game.SystemData{ID: "a", Name: "A"}}
}

func (f *exploreFake) FindRoute(context.Context, string) ([]game.RouteStep, error) {
	return f.route, nil
}

// The explore step must survey once it arrives, or the scanners never run.
func TestExploreSurveysOnArrival(t *testing.T) {
	kb := &fakeKB{
		systems: []knowledge.System{{ID: "a", LastVisitedTick: 100, LastUpdatedTick: 100}},
		conns:   undirected([2]string{"a", "b"}),
	}
	c := &exploreFake{
		surveyFake: surveyFake{replies: []serverapi.SurveySystemResponse{{SystemID: "b"}}},
		route:      []game.RouteStep{{SystemID: "b"}}, // one entry: already there
	}

	if err := Explore(context.Background(), ExploreDeps{Client: c, KB: kb, AgentID: "explorer-8"}); err != nil {
		t.Fatalf("Explore: %v", err)
	}
	if c.calls != 1 {
		t.Errorf("survey_system calls = %d, want 1 after arriving", c.calls)
	}
}

// No scanner is not a failure of the explore step: it keeps exploring.
func TestExploreWithoutScannerStillSucceeds(t *testing.T) {
	kb := &fakeKB{
		systems: []knowledge.System{{ID: "a", LastVisitedTick: 100, LastUpdatedTick: 100}},
		conns:   undirected([2]string{"a", "b"}),
	}
	c := &exploreFake{
		surveyFake: surveyFake{err: errors.New("requires a survey scanner")},
		route:      []game.RouteStep{{SystemID: "b"}},
	}

	if err := Explore(context.Background(), ExploreDeps{Client: c, KB: kb, AgentID: "explorer-1"}); err != nil {
		t.Errorf("Explore returned %v; a missing scanner must not fail the step", err)
	}
}
