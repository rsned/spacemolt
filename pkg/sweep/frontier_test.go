package sweep

import (
	"math"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

var frontierNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// frontierFixture writes a ledger of: a three-system chain a-b-c to the east,
// a singleton d to the east and farther out, a pair n1-n2 to the north.
func frontierFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "frontier-run.json")
	r := NewFrontierRun([]FrontierInput{
		{ID: "a", Name: "A", X: 100, Y: 0},
		{ID: "b", Name: "B", X: 110, Y: 5},
		{ID: "c", Name: "C", X: 120, Y: -5},
		{ID: "d", Name: "D", X: 300, Y: 10},
		{ID: "n1", Name: "N1", X: 0, Y: 200},
		{ID: "n2", Name: "N2", X: 10, Y: 210},
	}, [][2]string{{"a", "b"}, {"b", "c"}, {"n1", "n2"}, {"a", "sol"}}, frontierNow)
	if err := SaveFrontierRun(path, r); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadFrontier(t *testing.T, path string) *FrontierRun {
	t.Helper()
	r, err := LoadFrontierRun(path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWedge(t *testing.T) {
	at := func(deg float64) (float64, float64) {
		rad := deg * math.Pi / 180
		return 100 * math.Cos(rad), 100 * math.Sin(rad)
	}
	for _, tc := range []struct {
		deg  float64
		want string
	}{
		{0, "E"}, {22.4, "E"}, {22.6, "NE"}, {45, "NE"}, {90, "N"}, {135, "NW"},
		{180, "W"}, {225, "SW"}, {270, "S"}, {315, "SE"}, {337.4, "SE"}, {337.6, "E"}, {359, "E"},
	} {
		x, y := at(tc.deg)
		if got := Wedge(x, y); got != tc.want {
			t.Errorf("Wedge at %v° = %s, want %s", tc.deg, got, tc.want)
		}
	}
}

func TestNewFrontierRunClusters(t *testing.T) {
	r := loadFrontier(t, frontierFixture(t))
	if len(r.Systems) != 6 {
		t.Fatalf("%d systems, want 6 (sol is not a frontier system)", len(r.Systems))
	}
	if r.Systems["a"].Cluster != r.Systems["c"].Cluster || r.Systems["a"].Cluster != r.Systems["b"].Cluster {
		t.Errorf("a, b, c are lane-connected but in clusters %q %q %q",
			r.Systems["a"].Cluster, r.Systems["b"].Cluster, r.Systems["c"].Cluster)
	}
	if r.Systems["d"].Cluster == r.Systems["a"].Cluster || r.Systems["d"].Cluster == "" {
		t.Errorf("singleton d has cluster %q", r.Systems["d"].Cluster)
	}
	if r.Systems["a"].Wedge != "E" || r.Systems["n1"].Wedge != "N" {
		t.Errorf("wedges a=%s n1=%s, want E and N", r.Systems["a"].Wedge, r.Systems["n1"].Wedge)
	}
	for id, s := range r.Systems {
		if s.Status != FrontierTodo {
			t.Errorf("%s generated %q, want todo", id, s.Status)
		}
	}
	// The lane into charted space is not kept: it joins no frontier systems.
	if len(r.Lanes) != 3 {
		t.Errorf("%d lanes kept, want 3: %v", len(r.Lanes), r.Lanes)
	}
}

func TestLethalGeneratedIncomplete(t *testing.T) {
	r := NewFrontierRun([]FrontierInput{
		{ID: "a", X: 100}, {ID: "b", X: 110, Lethal: "Repeater Monolith at b_core"},
	}, [][2]string{{"a", "b"}}, frontierNow)
	b := r.Systems["b"]
	if b.Status != FrontierIncomplete || b.Reason != "hostile: Repeater Monolith at b_core" {
		t.Errorf("lethal system generated %q / %q", b.Status, b.Reason)
	}
}

func TestClaimClusterIsExclusive(t *testing.T) {
	path := frontierFixture(t)
	agents := []string{"explorer-1", "explorer-2"}
	oks := make([]bool, len(agents))
	ids := make([][]string, len(agents))
	var wg sync.WaitGroup
	for i, agent := range agents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			// Each ship arrives at a different system of the same cluster.
			ids[i], oks[i], err = ClaimCluster(path, agent, []string{"a", "c"}[i], frontierNow)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if oks[0] == oks[1] {
		t.Fatalf("claims ok=%v, want exactly one winner", oks)
	}
	winner := 0
	if oks[1] {
		winner = 1
	}
	got := slices.Clone(ids[winner])
	slices.Sort(got)
	if !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("winner claimed %v, want the whole cluster a b c", ids[winner])
	}
	if ids[winner][0] != []string{"a", "c"}[winner] {
		t.Errorf("claim order %v does not start at the arrival system", ids[winner])
	}
	r := loadFrontier(t, path)
	for _, id := range []string{"a", "b", "c"} {
		if s := r.Systems[id]; s.Status != FrontierClaimed || s.Claimant != agents[winner] {
			t.Errorf("%s is %q by %q, want claimed by %s", id, s.Status, s.Claimant, agents[winner])
		}
	}
	if r.Systems["d"].Status != FrontierTodo {
		t.Error("the claim spread beyond the cluster")
	}
}

// A restarted ship gets its own cluster back, less what it already finished.
func TestClaimClusterResumesOwnClaim(t *testing.T) {
	path := frontierFixture(t)
	if _, ok, err := ClaimCluster(path, "explorer-1", "a", frontierNow); err != nil || !ok {
		t.Fatalf("first claim ok=%v err=%v", ok, err)
	}
	if err := FinishSystem(path, "explorer-1", FrontierSystem{ID: "a", Status: FrontierDone, SurveyPower: 90}, frontierNow); err != nil {
		t.Fatal(err)
	}
	ids, ok, err := ClaimCluster(path, "explorer-1", "b", frontierNow)
	if err != nil || !ok {
		t.Fatalf("resume ok=%v err=%v", ok, err)
	}
	if !slices.Equal(ids, []string{"b", "c"}) {
		t.Errorf("resumed with %v, want the unfinished b c", ids)
	}
}

func TestAddSystemJoinsClaimedCluster(t *testing.T) {
	path := frontierFixture(t)
	if _, ok, err := ClaimCluster(path, "explorer-1", "a", frontierNow); err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if err := AddSystem(path, FrontierInput{ID: "e", Name: "E", X: 130, Y: 0}, "c", "explorer-1", frontierNow); err != nil {
		t.Fatal(err)
	}
	r := loadFrontier(t, path)
	e := r.Systems["e"]
	if e == nil || e.Cluster != r.Systems["c"].Cluster || e.Status != FrontierClaimed || e.Claimant != "explorer-1" || e.Wedge != "E" {
		t.Fatalf("added system %+v, want claimed by explorer-1 in c's cluster", e)
	}
	// A second ship arriving at the new system finds the cluster taken.
	if _, ok, _ := ClaimCluster(path, "explorer-2", "e", frontierNow); ok {
		t.Error("explorer-2 claimed a system joined to explorer-1's cluster")
	}
	// Adding it again changes nothing.
	if err := AddSystem(path, FrontierInput{ID: "e", X: 1, Y: 1}, "c", "explorer-2", frontierNow); err != nil {
		t.Fatal(err)
	}
	if again := loadFrontier(t, path).Systems["e"]; again.Claimant != "explorer-1" || again.X != 130 {
		t.Errorf("re-adding overwrote the system: %+v", again)
	}
}

func TestNextFrontierTargetPrefersOwnWedgeThenAny(t *testing.T) {
	path := frontierFixture(t)
	// From near n1, the nearest todo system is n1 — but an eastern ship takes
	// the nearest eastern system first.
	got, err := NextFrontierTarget(path, "E", 0, 190)
	if err != nil || got == nil || got.ID != "a" {
		t.Fatalf("NextFrontierTarget(E) = %+v, %v; want a", got, err)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if err = FinishSystem(path, "explorer-1", FrontierSystem{ID: id, Status: FrontierDone}, frontierNow); err != nil {
			t.Fatal(err)
		}
	}
	got, err = NextFrontierTarget(path, "E", 0, 190)
	if err != nil || got == nil || got.ID != "n1" {
		t.Fatalf("with the east done, NextFrontierTarget(E) = %+v, %v; want n1", got, err)
	}
	for _, id := range []string{"n1", "n2"} {
		if err = FinishSystem(path, "explorer-1", FrontierSystem{ID: id, Status: FrontierIncomplete, Reason: "unreachable"}, frontierNow); err != nil {
			t.Fatal(err)
		}
	}
	if got, err = NextFrontierTarget(path, "E", 0, 190); err != nil || got != nil {
		t.Errorf("with nothing todo, NextFrontierTarget = %+v, %v; want nil", got, err)
	}
}

func TestStartFrontierTargetIsNearestCentre(t *testing.T) {
	path := frontierFixture(t)
	got, err := StartFrontierTarget(path, "E")
	if err != nil || got == nil || got.ID != "a" {
		t.Fatalf("StartFrontierTarget(E) = %+v, %v; want a (100 from centre)", got, err)
	}
	if got, err = StartFrontierTarget(path, "SW"); err != nil || got != nil {
		t.Errorf("StartFrontierTarget in an empty wedge = %+v, %v; want nil", got, err)
	}
}

func TestFinishSystemRejectsOtherClaimant(t *testing.T) {
	path := frontierFixture(t)
	if _, ok, err := ClaimCluster(path, "explorer-1", "a", frontierNow); err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if err := FinishSystem(path, "explorer-2", FrontierSystem{ID: "a", Status: FrontierDone}, frontierNow); err == nil {
		t.Error("explorer-2 finished a system claimed by explorer-1")
	}
	done := FrontierSystem{ID: "a", Status: FrontierIncomplete, SurveyPower: 90, POIsSeen: 5, POIsVisited: 4,
		Relics: 1, Skipped: []string{"a_scar"}, Reason: "1 POI skipped"}
	if err := FinishSystem(path, "explorer-1", done, frontierNow); err != nil {
		t.Fatal(err)
	}
	a := loadFrontier(t, path).Systems["a"]
	if a.Status != FrontierIncomplete || a.SurveyPower != 90 || a.POIsSeen != 5 || a.POIsVisited != 4 ||
		a.Relics != 1 || !slices.Equal(a.Skipped, []string{"a_scar"}) || a.Reason != "1 POI skipped" || a.Claimant != "explorer-1" {
		t.Errorf("finished system saved as %+v", a)
	}
	if a.Name != "A" || a.X != 100 || a.Wedge != "E" {
		t.Errorf("finishing lost the system's identity: %+v", a)
	}
}

func TestReleaseAgentReturnsClaimsToTodo(t *testing.T) {
	path := frontierFixture(t)
	if _, ok, err := ClaimCluster(path, "explorer-1", "a", frontierNow); err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if err := FinishSystem(path, "explorer-1", FrontierSystem{ID: "a", Status: FrontierDone}, frontierNow); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseAgent(path, "explorer-1", frontierNow); err != nil {
		t.Fatal(err)
	}
	r := loadFrontier(t, path)
	if r.Systems["a"].Status != FrontierDone {
		t.Error("release undid a finished system")
	}
	for _, id := range []string{"b", "c"} {
		if s := r.Systems[id]; s.Status != FrontierTodo || s.Claimant != "" {
			t.Errorf("%s after release: %q by %q, want todo", id, s.Status, s.Claimant)
		}
	}
	// Another ship can now take what is left, reaching it through the done system.
	ids, ok, err := ClaimCluster(path, "explorer-2", "b", frontierNow)
	if err != nil || !ok || !slices.Equal(ids, []string{"b", "c"}) {
		t.Errorf("explorer-2 claim after release: %v ok=%v err=%v", ids, ok, err)
	}
}
