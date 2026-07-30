package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
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
		t.Errorf("age = %v, want 10m (RequestedAt is the fallback for old records)", got)
	}
	if got := assistPendingAge(rescue.Record{RequestedAt: "garbage"}, now); got < 100*24*time.Hour {
		t.Errorf("unparsable timestamp must count as very old, got %v", got)
	}
}

// The ladder widens on how long a rescue has gone UNCLAIMED. Measuring from
// RequestedAt meant a days-old record cleared every rank's gate at once, which
// disabled nearest-home ranking outright: a Nexus Prime rescue went to the
// Krynn assister 20 jumps away while the Nexus Prime one idled in the strand
// system, and a Sol rescue went to Starfall's while Sol's sat docked at the
// strandee's own POI.
func TestAssistPendingAgePrefersPendingSinceOverRequestedAt(t *testing.T) {
	now := time.Date(2026, 7, 29, 21, 0, 0, 0, time.UTC)
	rec := rescue.Record{
		RequestedAt:  "2026-07-27T14:49:26Z", // filed two days ago
		PendingSince: "2026-07-29T20:58:00Z", // re-queued two minutes ago
	}
	if got := assistPendingAge(rec, now); got != 2*time.Minute {
		t.Errorf("age = %v, want 2m — the takeover clock runs from PendingSince", got)
	}
}

// End-to-end regression for the live misroute: the two functions composed.
// With the strandee at m2, assist-b (h2, 1 hop) is rank 0 and assist-a (h1,
// 3 hops) is rank 1. A record filed days ago but re-queued moments ago must be
// claimable ONLY by assist-b; reading the filing time instead let assist-a
// claim it too, and whoever polled first won.
func TestStaleRequestedAtNoLongerWidensTheElection(t *testing.T) {
	graph := assistTestGraph()
	homes := map[string]string{"assist-a": "h1", "assist-b": "h2"}
	now := time.Date(2026, 7, 29, 21, 0, 0, 0, time.UTC)
	rec := rescue.Record{
		RequestedAt:  "2026-07-27T14:49:26Z",
		PendingSince: "2026-07-29T20:58:00Z",
	}

	age := assistPendingAge(rec, now)
	if !assistElect("assist-b", homes, "m2", graph, age) {
		t.Error("the nearest assister must still claim immediately")
	}
	if assistElect("assist-a", homes, "m2", graph, age) {
		t.Error("the far assister must wait its rank out on a freshly re-queued record")
	}

	// Guard the premise: with the old (filing-time) age the far agent WOULD
	// have claimed, so this test fails if the fix is reverted.
	filed, err := time.Parse(time.RFC3339, rec.RequestedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !assistElect("assist-a", homes, "m2", graph, now.Sub(filed)) {
		t.Error("premise broken: a days-old age should clear every rank's gate")
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
		POI: "strand_star", RescueFuel: 15, Fuel: 0, MaxFuel: 200, Status: rescue.StatusPending,
	}}}
	client := &fakeClient{route: []game.RouteStep{{SystemID: "altais"}}, state: &game.State{Fuel: 120, MaxFuel: 120}}
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

// TestAssistEnsureHomeAtMobileCapitalDocksInPlace: when the rescuer is already
// sitting at its mobile home POI but undocked, ensure-home must dock in place
// rather than re-travel. Autopilot's travel auto-undocks, so re-navigating to a
// POI we already occupy thrashes undock<->dock forever and the ship is never
// aboard (docked) for the capital's daily jump. Regression for the 2026-07-07
// assist-frontier stuck-at-mobile_capital, fuel-pinned incident.
func TestAssistEnsureHomeAtMobileCapitalDocksInPlace(t *testing.T) {
	client := &fakeClient{
		route: []game.RouteStep{{SystemID: "altais"}},
		state: &game.State{CurrentSystem: "altais", CurrentPOI: "mobile_capital"},
	}
	client.state.System.ID = "altais"
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
	if len(visited) != 0 {
		t.Fatalf("already at mobile home POI must not re-travel (auto-undocks), visited %v", visited)
	}
	if !slices.Contains(client.calls, "dock") {
		t.Fatalf("must dock in place, calls = %v", client.calls)
	}
	if slices.Contains(client.calls, "undock") {
		t.Fatalf("must not undock when already at home POI, calls = %v", client.calls)
	}
}

// TestAssistDynamicFuelSizing: with live rescuer fuel and a KB graph, the
// transfer is sized by rescue.TransferQuantity, not the record's flat estimate.
// strand-altais is 1 jump; rescuer has 120 fuel; strandee tank 200 empty.
// spare = 120 - (5*1 + 5) = 110; need = 200 -> transfer 110.
func TestAssistDynamicFuelSizing(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:          "altais",
		Connections: []knowledge.SystemConnection{{SystemID: "strand"}},
	}); err != nil {
		t.Fatal(err)
	}
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 10, Fuel: 0, MaxFuel: 200,
		Status: rescue.StatusClaimed, ClaimedBy: "assist-frontier",
	}}}
	client := &fakeClient{route: []game.RouteStep{{SystemID: "altais"}}, state: &game.State{Fuel: 120, MaxFuel: 120}}
	deps := AssistDeps{
		Client: client, KB: kb, Queue: q, Out: io.Discard,
		AgentID: "assist-frontier", HomeStation: "mobile_capital",
		Navigate: func(ctx context.Context, system, poi string) error { return nil },
	}
	if err := Assist(ctx, deps); err != nil {
		t.Fatal(err)
	}
	if got := client.refuelShipCalls; len(got) != 1 || got[0].quantity != 110 {
		t.Fatalf("RefuelShip calls = %+v, want one call of quantity 110", got)
	}
	if q.recs[0].Status != rescue.StatusDone || q.recs[0].RescueFuel != 110 {
		t.Fatalf("record = %+v, want done with RescueFuel recorded as 110", q.recs[0])
	}
}

// TestAssistReleasesWhenCannotSpare: a low-fuel rescuer that cannot give any
// fuel without eating its trip home refuses the transfer and returns the claim
// to pending (ClaimedBy cleared) instead of stranding itself.
func TestAssistReleasesWhenCannotSpare(t *testing.T) {
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:          "altais",
		Connections: []knowledge.SystemConnection{{SystemID: "strand"}},
	}); err != nil {
		t.Fatal(err)
	}
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 10, Fuel: 0, MaxFuel: 200,
		Status: rescue.StatusClaimed, ClaimedBy: "assist-frontier",
	}}}
	// spare = 8 - (5*1 + 5) -> clamps to 0, so no transfer.
	client := &fakeClient{route: []game.RouteStep{{SystemID: "altais"}}, state: &game.State{Fuel: 8, MaxFuel: 120}}
	deps := AssistDeps{
		Client: client, KB: kb, Queue: q, Out: io.Discard,
		AgentID: "assist-frontier", HomeStation: "mobile_capital",
		Navigate: func(ctx context.Context, system, poi string) error { return nil },
	}
	if err := Assist(ctx, deps); err != nil {
		t.Fatal(err)
	}
	if len(client.refuelShipCalls) != 0 {
		t.Fatalf("rescuer that cannot spare must not refuel, got %+v", client.refuelShipCalls)
	}
	if q.recs[0].Status != rescue.StatusPending || q.recs[0].ClaimedBy != "" {
		t.Fatalf("record = %+v, want pending with ClaimedBy cleared", q.recs[0])
	}
}

// A failed rescue must RE-QUEUE, not terminate. A terminal record is a
// deadlock: quarantine is released only on `done`, and a quarantined worker has
// no process to rescue itself, so `failed` on the first try means stranded
// forever (live 2026-07-29: two workers sat quarantined for one and two days
// each on records that failed once).
func TestAssistFailureRequeuesForAnotherAssister(t *testing.T) {
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
	got := q.recs[0]
	if got.Status != rescue.StatusPending {
		t.Errorf("status = %s, want pending so another assister retries", got.Status)
	}
	if got.Error == "" {
		t.Error("the failure reason must be recorded even though we retry")
	}
	if got.ClaimedBy != "" {
		t.Errorf("ClaimedBy = %q, want cleared so the record is claimable", got.ClaimedBy)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", got.Attempts)
	}
	if !got.HasFailed("assist-a") {
		t.Errorf("FailedBy = %v, want it to record assist-a", got.FailedBy)
	}
}

// The agent that just failed must not immediately re-win the election it is
// usually rank 0 for — otherwise the re-queue is an infinite retry loop rather
// than a handover.
func TestAssistDoesNotReclaimARecordItAlreadyFailed(t *testing.T) {
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 15, Status: rescue.StatusPending,
		FailedBy: []string{"assist-a"}, Attempts: 1,
		// Freshly re-queued, so assist-a's bar is still in force.
		PendingSince: time.Now().UTC().Format(time.RFC3339),
	}}}
	client := &fakeClient{state: &game.State{}}
	deps := AssistDeps{
		Client: client, Queue: q, Out: io.Discard,
		AgentID: "assist-a", HomeStation: "h1_station",
		Navigate: func(ctx context.Context, system, poi string) error { return nil },
	}
	if err := Assist(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if q.recs[0].Status != rescue.StatusPending || q.recs[0].ClaimedBy != "" {
		t.Fatalf("record must stay pending and unclaimed, got %+v", q.recs[0])
	}
	if len(client.refuelShipCalls) != 0 {
		t.Fatalf("must not attempt a rescue it already failed, got %+v", client.refuelShipCalls)
	}
}

// The bar on a previous failer must expire. With fewer live assisters than
// RescueMaxAttempts, a permanent bar means every live agent lands in FailedBy,
// nobody can claim, and the record sits pending forever — never rescued and
// never escalated to the operator either.
func TestAssistRetriesItsOwnFailureAfterTheBarExpires(t *testing.T) {
	ctx := context.Background()
	// Only nexus_prime reaches the strandee, so assist-nexus is the sole
	// eligible claimant — the "everyone live has already failed it" case.
	kb := knowledge.NewMemoryKB()
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:          "nexus_prime",
		Connections: []knowledge.SystemConnection{{SystemID: "strand"}},
	}); err != nil {
		t.Fatal(err)
	}
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 15, Fuel: 0, MaxFuel: 200,
		Status: rescue.StatusPending, FailedBy: []string{"assist-nexus"}, Attempts: 1,
		PendingSince: time.Now().Add(-2 * rescueRetryAfter).UTC().Format(time.RFC3339),
	}}}
	client := &fakeClient{state: &game.State{Fuel: 120, MaxFuel: 120}}
	deps := AssistDeps{
		Client: client, KB: kb, Queue: q, Out: io.Discard,
		AgentID: "assist-nexus", HomeStation: "the_core",
		Navigate: func(ctx context.Context, system, poi string) error { return nil },
	}
	if err := Assist(ctx, deps); err != nil {
		t.Fatal(err)
	}
	if len(client.refuelShipCalls) != 1 {
		t.Fatalf("after the bar expires the only eligible assister must retry, got %+v", client.refuelShipCalls)
	}
	if q.recs[0].Status != rescue.StatusDone {
		t.Errorf("status = %s, want done", q.recs[0].Status)
	}
}

// Retrying forever is its own failure mode, so once every assister has had a
// turn the record goes terminal and waits for an operator.
func TestAssistGivesUpAfterMaxAttempts(t *testing.T) {
	prior := make([]string, 0, RescueMaxAttempts-1)
	for i := range RescueMaxAttempts - 1 {
		prior = append(prior, fmt.Sprintf("assist-prior-%d", i))
	}
	q := &fakeRescueQueue{recs: []rescue.Record{{
		AgentID: "trader-8", TargetUsername: "Big Jim", SystemID: "strand",
		POI: "strand_star", RescueFuel: 15,
		Status: rescue.StatusClaimed, ClaimedBy: "assist-a",
		Attempts: RescueMaxAttempts - 1, FailedBy: prior,
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
	if q.recs[0].Status != rescue.StatusFailed {
		t.Fatalf("status = %s, want failed after %d attempts", q.recs[0].Status, RescueMaxAttempts)
	}
	if q.recs[0].Attempts != RescueMaxAttempts {
		t.Errorf("Attempts = %d, want %d", q.recs[0].Attempts, RescueMaxAttempts)
	}
}
