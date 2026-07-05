package worker

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
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
		age    time.Duration
		want   bool
	}{
		{"equidistant tie goes to lexicographic smaller", "assist-a", "strand", 0, true},
		{"equidistant tie loser", "assist-b", "strand", 0, false},
		{"strictly closer wins", "assist-b", "m2", 0, true},
		{"strictly farther loses", "assist-a", "m2", 0, false},
		{"unknown agent never claims", "assist-x", "strand", 0, false},
		{"unreachable system never claims", "assist-a", "nowhere", 0, false},
		// Takeover: rank×interval of pending age unlocks the next rank.
		{"tie loser takes over after one interval", "assist-b", "strand", assistTakeoverInterval, true},
		{"farther agent takes over after one interval", "assist-a", "m2", assistTakeoverInterval, true},
		{"farther agent still waits just under the interval", "assist-a", "m2", assistTakeoverInterval - time.Second, false},
		{"unreachable never claims regardless of age", "assist-a", "nowhere", 10 * assistTakeoverInterval, false},
	}
	for _, tc := range cases {
		if got := assistElect(tc.agent, homes, tc.strand, graph, tc.age); got != tc.want {
			t.Errorf("%s: assistElect = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestAssistElectGhostHomeTakeover reproduces the 2026-07-04 incident: the
// nearest home belongs to a benched agent that never claims. The live agents
// must take the rescue over after their rank's interval instead of deferring
// forever.
func TestAssistElectGhostHomeTakeover(t *testing.T) {
	graph := assistTestGraph()
	// ghost's home h1 is 2 jumps from m1; live agent's home h2 is 3 jumps.
	homes := map[string]string{"assist-ghost": "h1", "assist-live": "h2"}

	if assistElect("assist-live", homes, "m1", graph, 0) {
		t.Error("fresh record: live agent must defer to the nearer (ghost) home")
	}
	if !assistElect("assist-live", homes, "m1", graph, assistTakeoverInterval) {
		t.Error("aged record: live agent (rank 1) must take over after one interval")
	}
}

// TestAssistRetriesWhenInTransit: a worker restarted mid-hyperspace-jump gets
// "You are not in a system" from find_route on its first resumed pass. The
// record must stay claimed (retry next pass), not flip to failed.
func TestAssistRetriesWhenInTransit(t *testing.T) {
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "salvager-10", TargetUsername: "Junk Jackson", SystemID: "strand",
		POI: "strand_star", RescueFuel: 10,
		Status: rescue.StatusClaimed, ClaimedBy: "assist-a",
	}}}
	deps := AssistDeps{
		Client: &fakeClient{state: &game.State{}}, Queue: q, Out: io.Discard,
		AgentID: "assist-a", HomeStation: "h1_station",
		Navigate: func(ctx context.Context, system, poi string) error {
			return errors.New("find_route failed: You are not in a system")
		},
	}
	if err := Assist(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if q.recs[0].Status != rescue.StatusClaimed {
		t.Fatalf("in-transit navigate error must keep the claim, got status %s", q.recs[0].Status)
	}
}

func TestAssistPendingAge(t *testing.T) {
	now := time.Date(2026, 7, 4, 20, 0, 0, 0, time.UTC)
	rec := rescue.Record{RequestedAt: "2026-07-04T19:50:00Z"}
	if got := assistPendingAge(rec, now); got != 10*time.Minute {
		t.Errorf("age = %v, want 10m", got)
	}
	if got := assistPendingAge(rescue.Record{RequestedAt: "garbage"}, now); got < 100*24*time.Hour {
		t.Errorf("unparsable timestamp must count as very old, got %v", got)
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

func TestResolveAssistHomesMobile(t *testing.T) {
	ctx := context.Background()

	// The mobile capital's current system is the last route step; static
	// entries pass through untouched.
	client := &fakeClient{route: []game.RouteStep{{SystemID: "m2"}, {SystemID: "altais"}}}
	homes := resolveAssistHomes(ctx, AssistDeps{Client: client, Out: io.Discard})
	if homes["assist-frontier"] != "altais" {
		t.Errorf("assist-frontier home = %q, want altais", homes["assist-frontier"])
	}
	if homes["assist-sol"] != "sol" {
		t.Errorf("static home assist-sol = %q, want sol", homes["assist-sol"])
	}

	// Empty route means we are already in the capital's system.
	client = &fakeClient{state: &game.State{}}
	client.state.System.ID = "altais"
	homes = resolveAssistHomes(ctx, AssistDeps{Client: client, Out: io.Discard})
	if homes["assist-frontier"] != "altais" {
		t.Errorf("empty-route home = %q, want current system altais", homes["assist-frontier"])
	}

	// find_route failure drops the mobile home for this pass; the four
	// static capitals still elect.
	client = &fakeClient{routeErr: errors.New("route service down")}
	homes = resolveAssistHomes(ctx, AssistDeps{Client: client, Out: io.Discard})
	if _, ok := homes["assist-frontier"]; ok {
		t.Error("unresolvable mobile home must be dropped for the pass")
	}
	if len(homes) != len(assistHomes) {
		t.Errorf("static homes = %d entries, want %d", len(homes), len(assistHomes))
	}
}

// TestAssistClaimsPendingNearMobileCapital drives the full claim path for
// assist-frontier: its home comes from find_route (altais), which is 1 jump
// from the strandee while every static capital is unreachable in the graph,
// so it must win the election, rescue, and head home to the resolved system.
func TestAssistClaimsPendingNearMobileCapital(t *testing.T) {
	ctx := context.Background()
	// MemoryKB's GetConnections reads systems[].Connections, so seed via
	// RememberSystem rather than RememberConnection.
	kb := knowledge.NewMemoryKB()
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:          "altais",
		Connections: []knowledge.SystemConnection{{SystemID: "strand"}},
	}); err != nil {
		t.Fatal(err)
	}
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 15, Status: rescue.StatusPending,
	}}}
	client := &fakeClient{route: []game.RouteStep{{SystemID: "altais"}}, state: &game.State{}}
	var visited []string
	deps := AssistDeps{
		Client: client, KB: kb, Queue: q, Out: io.Discard,
		AgentID: "assist-frontier", HomeStation: "mobile_capital",
		Navigate: func(ctx context.Context, system, poi string) error {
			visited = append(visited, system+"/"+poi)
			return nil
		},
	}
	if err := Assist(ctx, deps); err != nil {
		t.Fatal(err)
	}
	if q.recs[0].Status != rescue.StatusDone || q.recs[0].ClaimedBy != "assist-frontier" {
		t.Fatalf("record = %+v, want done claimed by assist-frontier", q.recs[0])
	}
	if len(visited) != 2 || visited[0] != "strand/strand_star" || visited[1] != "altais/mobile_capital" {
		t.Fatalf("visited = %v, want strandee then resolved mobile home", visited)
	}
}

// TestAssistEnsureHomeMobileRetarget: the capital jumped while the rescuer was
// out — ensure-home must navigate to the freshly resolved system, not the one
// the rescuer last saw.
func TestAssistEnsureHomeMobileRetarget(t *testing.T) {
	client := &fakeClient{route: []game.RouteStep{{SystemID: "vega"}}, state: &game.State{}}
	client.state.System.ID = "altais" // where the capital used to be
	var visited []string
	deps := AssistDeps{
		Client: client, Out: io.Discard,
		AgentID: "assist-frontier", HomeStation: "mobile_capital",
		Navigate: func(ctx context.Context, system, poi string) error {
			visited = append(visited, system+"/"+poi)
			return nil
		},
	}
	if err := assistEnsureHome(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if len(visited) != 1 || visited[0] != "vega/mobile_capital" {
		t.Fatalf("visited = %v, want vega/mobile_capital", visited)
	}

	// Resolution failure: log, stay put, retry next pass.
	client = &fakeClient{routeErr: errors.New("route service down"), state: &game.State{}}
	visited = nil
	deps.Client = client
	if err := assistEnsureHome(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if len(visited) != 0 {
		t.Fatalf("unresolved home must not navigate, visited %v", visited)
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
