package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/huntdecide"
	"github.com/rsned/spacemolt/pkg/outcomes"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// The dispatch tests below all run the pass with a NIL knowledge base. That is
// not a shortcut around the executor — it is the cheapest way to reach it: the
// pass gets as far as picking up its mission and publishing what it is hunting,
// then huntFindQuarry refuses to travel without a KB and the pass ends. Every
// dependency the dispatch site is responsible for wiring is exercised before
// that point, and none of the sleeps or the fight are, so these cost the suite
// nothing.

// huntHeldFake is a fake holding one already-accepted combat mission, which is
// what a worker looks like on the pass after it broke off. The resume path
// needs no accept and therefore no post-accept settle sleep.
func huntHeldFake(t *testing.T, id string, difficulty int) *huntFake {
	t.Helper()
	return newHuntFake(t, huntFakeOpts{
		activeMissions: []serverapi.ActiveMission{heldMission(id, 1, 3, difficulty)},
		heldAtStart:    true,
	})
}

// TestDispatchHuntReachesTheExecutor is the reachability proof: the `hunt`
// token must actually run pkg/worker.Hunt with the dispatch's own client,
// agent id, agents dir and activity sink. Asserting that "hunt" is in the
// supported map would prove none of that — the switch could be missing the
// case entirely and the map would still say yes (Run would error "unknown
// command", which is the second assertion here).
func TestDispatchHuntReachesTheExecutor(t *testing.T) {
	c := huntHeldFake(t, "first_hunt_belt_grazers", 1)

	// A hunt-chain file that cannot be decoded makes huntRecordedContinuations
	// log the path it read, which is the only observable that names the
	// AgentsDir the dispatch handed down. A well-formed file is silent, and a
	// silent success cannot tell a forwarded directory from a defaulted one.
	agentsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(agentsDir, "pirate-6"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	chainPath := filepath.Join(agentsDir, "pirate-6", huntChainFile)
	if err := os.WriteFile(chainPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var log strings.Builder
	d := NewWorkerDispatch(c, nil, nil, &log)
	d.AgentID = "pirate-6"
	d.AgentsDir = agentsDir
	var sink atomic.Pointer[string]
	d.SetActivitySink(&sink)

	if err := d.Run(context.Background(), []string{"hunt"}); err != nil {
		t.Fatalf("Run hunt: %v", err)
	}

	// "resuming <id>" is printed by huntResumeJob and by nothing else, so it
	// cannot appear unless Hunt ran with this client.
	if !strings.Contains(log.String(), "resuming first_hunt_belt_grazers") {
		t.Fatalf("dispatch must run the hunt executor against its own client, got:\n%s", log.String())
	}
	// AgentID reached the executor: the resume line is prefixed with it.
	if !strings.Contains(log.String(), "pirate-6") {
		t.Errorf("dispatch must pass its AgentID to Hunt, got:\n%s", log.String())
	}
	// AgentsDir reached the executor: the chain read names the temp path.
	if !strings.Contains(log.String(), chainPath) {
		t.Errorf("dispatch must pass its AgentsDir to Hunt (want %s in the log), got:\n%s", chainPath, log.String())
	}
	// SetActivity reached the executor: the status page names the quarry.
	got := sink.Load()
	if got == nil {
		t.Fatal("dispatch must wire SetActivity: nothing was published")
	}
	if want := "Hunt first_hunt_belt_grazers · belt_grazer 1/3"; *got != want {
		t.Errorf("activity = %q, want %q", *got, want)
	}
}

// TestDispatchHuntKeepsTheWildlifeInterlockOn is the safety proof. The dispatch
// site names neither gate — MaxDifficulty and WildlifeOnly are left at their
// zero values so hunt_gate.go stays the single source of the fleet's risk
// posture — so this pins that the zero values still REFUSE a combat mission
// that is not on the wildlife allowlist. pirate_bounty shoots back; a fleet of
// starter hulls that accepted one would be flying into a fight it cannot win.
func TestDispatchHuntKeepsTheWildlifeInterlockOn(t *testing.T) {
	c := huntHeldFake(t, "pirate_bounty", 1)

	var log strings.Builder
	d := NewWorkerDispatch(c, nil, nil, &log)
	d.AgentID = "pirate-6"
	d.AgentsDir = t.TempDir()
	var sink atomic.Pointer[string]
	d.SetActivitySink(&sink)

	if err := d.Run(context.Background(), []string{"hunt"}); err != nil {
		t.Fatalf("Run hunt: %v", err)
	}
	if !strings.Contains(log.String(), "not resuming pirate_bounty") {
		t.Fatalf("a non-wildlife mission must be refused when the dispatch leaves WildlifeOnly unset, got:\n%s", log.String())
	}
	if c.huntCalls != 0 {
		t.Errorf("a refused mission must not engage anything, hunt calls = %d", c.huntCalls)
	}
	// Nothing was taken up, so the status page must not claim otherwise.
	if got := sink.Load(); got == nil || *got != "" {
		t.Errorf("activity = %v, want the cleared empty string", got)
	}
}

// TestDispatchHuntUnknownWithoutTheCase guards the vocabulary map and the
// switch case against drifting apart: Supports says the token is dispatchable,
// and Run must agree. A token in `supported` with no case reaches the default
// branch and errors on every pass of a live fleet.
func TestDispatchHuntIsSupportedAndDispatchable(t *testing.T) {
	d := NewWorkerDispatch(nil, nil, nil, nil)
	if !d.Supports("hunt") {
		t.Fatal("hunt must be in the supported command set")
	}
	// Nil client -> Hunt logs and returns nil (the degraded-no-op contract the
	// haul and missions commands share). An "unknown command" error here would
	// mean the case is missing.
	if err := d.Run(context.Background(), []string{"hunt"}); err != nil {
		t.Fatalf("hunt with no client must no-op, got %v", err)
	}
}

// The two tests below pin the publish SITES rather than the label functions
// (activity_label_test.go covers those). A break-off is the outcome the status
// page exists to surface: it is the difference between a fleet that is hunting
// and a fleet that is quietly losing hull it cannot get back, and both look
// identical if the line still reads "Hunt <mission>".

// The between-engagement hull gate: a worker that starts the pass wounded
// breaks off before engaging, and must say so.
func TestHuntPublishesTheBreakOffBeforeTheFirstEngagement(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:     []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures: huntGrazers("c1", "c2"),
	})
	huntWoundAtGround(c, 0.1)
	var log strings.Builder
	deps := huntDeps(c, &log)
	deps.FleeAtHull = 0.3
	var activity string
	deps.SetActivity = func(s string) { activity = s }
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if want := "Hunt first_hunt_belt_grazers · broke off at 0/3"; activity != want {
		t.Errorf("activity = %q, want %q", activity, want)
	}
}

// The in-fight abort: hull crosses the threshold once the fight is joined.
func TestHuntPublishesTheBreakOffWhenItFlees(t *testing.T) {
	c := newHuntFake(t, huntFakeOpts{
		board:         []boardMission{{id: "first_hunt_belt_grazers", difficulty: 1, quantity: 3}},
		creatures:     huntGrazers("c1", "c2", "c3"),
		battleHullPct: 10,
	})
	var log strings.Builder
	deps := huntDeps(c, &log)
	deps.FleeAtHull = 0.3
	var activity string
	deps.SetActivity = func(s string) { activity = s }
	if err := Hunt(context.Background(), deps); err != nil {
		t.Fatalf("Hunt: %v", err)
	}
	if !c.fled {
		t.Fatal("fixture did not flee; the break-off publish is untested")
	}
	if want := "Hunt first_hunt_belt_grazers · broke off at 0/3"; activity != want {
		t.Errorf("activity = %q, want %q", activity, want)
	}
}

// TestHuntFleetYAMLParses reads the checked-in roster rather than a fixture:
// the failure this catches is a mistyped key in the file the operator will
// actually launch, which a temp-file test by construction cannot see. The role
// it names must also exist in roles.yaml, or the supervisor spawns workers with
// no standing behavior at all.
func TestHuntFleetYAMLParses(t *testing.T) {
	specs, err := supervisor.LoadFleet(filepath.Join("..", "..", "data", "overmind", "hunt-fleet.yaml"))
	if err != nil {
		t.Fatalf("LoadFleet: %v", err)
	}
	// Deliberately NOT an exact count. The pool started at two as a risk posture
	// for a behaviour that had never run unattended; all five were canaried by
	// hand on 2026-08-10 and it grew to five. Fleet size is an operational
	// choice the operator re-makes, so pinning it here only manufactures a red
	// suite every time the roster changes. What must hold is that the file the
	// operator actually launches parses, is not empty, and names a real role.
	if len(specs) == 0 {
		t.Fatal("hunt fleet is empty")
	}
	roles, err := LoadRoles(filepath.Join("..", "..", "data", "overmind", "roles.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	// Members are seconded to other roles for a while (idle for a hull buy,
	// unlock for the pirate chain), so only require that the fleet still
	// hunts at all and that every role it names exists.
	hunters := 0
	for _, s := range specs {
		if s.Role == "hunt" {
			hunters++
		}
		if _, ok := roles[s.Role]; !ok {
			t.Errorf("%s: role %q is not defined in roles.yaml", s.AgentID, s.Role)
		}
	}
	if hunters == 0 {
		t.Error("hunt fleet has no member with role hunt")
	}
	if roles["hunt"].Idle != "hunt" {
		t.Errorf("hunt role idle = %q, want hunt", roles["hunt"].Idle)
	}
}

func TestParseHuntExplore(t *testing.T) {
	got, err := ParseHuntExplore("")
	if err != nil || len(got) != 0 {
		t.Fatalf("empty spec = %v, %v; want empty map, nil", got, err)
	}
	got, err = ParseHuntExplore("provision=0.15,return=0.05")
	if err != nil {
		t.Fatalf("valid spec: %v", err)
	}
	if len(got) != 2 || got[huntdecide.KindProvision] != 0.15 || got[huntdecide.KindReturn] != 0.05 {
		t.Errorf("valid spec = %v", got)
	}
	for _, bad := range []string{"ground=0.1", "return", "return=2", "return=-0.1", "return=x", "return=NaN", "return=Inf", "provision=-Inf"} {
		if _, err := ParseHuntExplore(bad); err == nil {
			t.Errorf("ParseHuntExplore(%q) must fail", bad)
		}
	}
}

// huntDispatchFor builds a dispatch around a sortie environment's fake, KB and
// agent directory, so Run("hunt") drives the same pass the sortie tests do.
func huntDispatchFor(e *sortieEnv) *WorkerDispatch {
	d := NewWorkerDispatch(e.fake, e.deps.KB, nil, e.out)
	d.AgentID = e.deps.AgentID
	d.AgentsDir = e.deps.AgentsDir
	d.Station = "haven_station"
	d.Outcomes = outcomes.New(e.dir)
	return d
}

func TestHuntDispatchWiresRecorderAndDefaults(t *testing.T) {
	e := sortieMissionEnv(t, 1, cannon(0))
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
	d := huntDispatchFor(e)
	if err := d.Run(context.Background(), []string{"hunt"}); err != nil {
		t.Fatalf("Run hunt: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(e.dir, "hunt-decisions", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("decision files = %v, want one under %s/hunt-decisions\n%s", files, e.dir, e.out)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var sawDecision bool
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad record %q: %v", line, err)
		}
		if rec["type"] == "decision" {
			sawDecision = true
			if rec["source"] != "rule" {
				t.Errorf("decision source = %v, want rule", rec["source"])
			}
		}
	}
	if !sawDecision {
		t.Errorf("no decision record written:\n%s", raw)
	}
}

func TestHuntDispatchReportsSortieOutcome(t *testing.T) {
	const key = "behaviour:hunt_sortie"
	e := sortieMissionEnv(t, 1, cannon(0))
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
	d := huntDispatchFor(e)
	if err := d.Run(context.Background(), []string{"hunt"}); err != nil {
		t.Fatalf("Run hunt: %v", err)
	}
	got, err := outcomes.Load(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	o := got[key]
	if o.Status != outcomes.StatusFailed || o.Reason != huntdecide.ResultBlockedUnarmed || o.Command != "hunt" {
		t.Errorf("blocked outcome = %+v", o)
	}

	// A sortie that has already visited the ground cap ends no_quarry once the
	// hunter is docked again.
	e = newSortieEnv(t, huntFakeOpts{
		board:          []boardMission{{id: sortieMission, difficulty: 1, quantity: 5}},
		activeMissions: []serverapi.ActiveMission{heldMission(sortieMission, 0, 5, 1)},
		heldAtStart:    true,
		startPOI:       "old_ground_6",
		unsetDocked:    true,
	}, provLasers(1))
	// No creatures anywhere: the dispatch has no zero-delay tick seam, so the
	// pass must never reach a fight.
	e.fake.grounds = map[string][]huntCreature{}
	s := &huntdecide.Sortie{ID: "pirate-6-capped", JobKind: "mission", JobID: "hex-" + sortieMission}
	for i := range huntMaxSortieGrounds - 1 {
		s.Grounds = append(s.Grounds, huntdecide.GroundVisit{POI: fmt.Sprintf("old_ground_%d", i+1)})
	}
	e.seed(t, s)
	d = huntDispatchFor(e)
	for range 3 {
		if err = d.Run(context.Background(), []string{"hunt"}); err != nil {
			t.Fatalf("Run hunt: %v", err)
		}
	}
	got, err = outcomes.Load(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	o = got[key]
	if o.Status != outcomes.StatusOK || o.Reason != huntdecide.ResultNoQuarry {
		t.Errorf("no_quarry outcome = %+v\n%s", o, e.out)
	}
}
