package worker

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/navigation"
	"github.com/rsned/spacemolt/pkg/rescue"
)

// line graph: h1 - m1 - strand - m2 - h2
func assistTestGraph() navigation.JumpGraph {
	return navigation.JumpGraph{
		"h1":     {"m1"},
		"m1":     {"h1", "strand"},
		"strand": {"m1", "m2"},
		"m2":     {"strand", "h2"},
		"h2":     {"m2"},
	}
}

func TestAssistElect(t *testing.T) {
	graph := assistTestGraph()
	homes := map[string]string{"assist-a": "h1", "assist-b": "h2"}
	cases := []struct {
		name   string
		agent  string
		strand string
		want   bool
	}{
		{"equidistant tie goes to lexicographic smaller", "assist-a", "strand", true},
		{"equidistant tie loser", "assist-b", "strand", false},
		{"strictly closer wins", "assist-b", "m2", true},
		{"strictly farther loses", "assist-a", "m2", false},
		{"unknown agent never claims", "assist-x", "strand", false},
		{"unreachable system never claims", "assist-a", "nowhere", false},
	}
	for _, tc := range cases {
		if got := assistElect(tc.agent, homes, tc.strand, graph); got != tc.want {
			t.Errorf("%s: assistElect = %v, want %v", tc.name, got, tc.want)
		}
	}
}

type fakeRescueQueue struct {
	recs []rescue.Record
}

func (f *fakeRescueQueue) List() ([]rescue.Record, error) { return f.recs, nil }
func (f *fakeRescueQueue) Transition(agentID string, from, to rescue.Status, mutate func(*rescue.Record)) (bool, error) {
	for i := range f.recs {
		if f.recs[i].AgentID == agentID && f.recs[i].Status == from {
			if mutate != nil {
				mutate(&f.recs[i])
			}
			f.recs[i].Status = to
			return true, nil
		}
	}
	return false, nil
}

// TestAssistRunsClaimedRescue drives Assist over a resumed claimed record using
// the package's fakeClient (dispatch_test.go) as the game.GameClient — its
// GetState() returns the state field verbatim (nil-safe: assistEnsureHome
// handles a nil state by just navigating home), and RefuelShip is recorded via
// refuelShipCalls (added alongside its other recorded methods).
func TestAssistRunsClaimedRescue(t *testing.T) {
	// Worker already owns a claimed record (e.g. resumed after restart):
	// travel -> RefuelShip(username, fuel) -> done -> home.
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 15,
		Status: rescue.StatusClaimed, ClaimedBy: "assist-a",
	}}}
	var visited []string
	client := &fakeClient{state: &game.State{}}
	deps := AssistDeps{
		Client: client, Queue: q, Out: io.Discard,
		AgentID: "assist-a", HomeStation: "h1_station",
		Navigate: func(ctx context.Context, system, poi string) error {
			visited = append(visited, system+"/"+poi)
			return nil
		},
	}
	if err := Assist(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if q.recs[0].Status != rescue.StatusDone {
		t.Fatalf("record status = %s, want done", q.recs[0].Status)
	}
	if got := client.refuelShipCalls; len(got) != 1 || got[0].target != "Big Jim" || got[0].quantity != 15 {
		t.Fatalf("RefuelShip calls = %+v", got)
	}
	if len(visited) == 0 || visited[0] != "strand/strand_star" {
		t.Fatalf("first hop must be the strandee, got %v", visited)
	}
}

func TestAssistFailureMarksFailed(t *testing.T) {
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 15,
		Status: rescue.StatusClaimed, ClaimedBy: "assist-a",
	}}}
	client := &fakeClient{state: &game.State{}}
	client.refuelShipErr = errors.New("target not found")
	deps := AssistDeps{
		Client: client, Queue: q, Out: io.Discard,
		AgentID: "assist-a", HomeStation: "h1_station",
		Navigate: func(ctx context.Context, system, poi string) error { return nil },
	}
	if err := Assist(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if q.recs[0].Status != rescue.StatusFailed || q.recs[0].Error == "" {
		t.Fatalf("failed rescue must mark record failed with error, got %+v", q.recs[0])
	}
}
