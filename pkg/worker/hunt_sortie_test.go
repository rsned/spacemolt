package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/huntdecide"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Ground ids in sortieKB, nearest the board station first.
const (
	sortieGround1 = "pollux_outer_belt"
	sortieGround2 = "unclaimed_facets_azmidi"
	sortieGround3 = "castor_far_belt"
	sortieMission = "first_hunt_belt_grazers"
)

// sortieFake is the provisioning fake with three more behaviours a sortie
// needs: get_nearby answers per POI (grounds), each fight empties drain rounds
// from every magazine, and a dock only lands at the station.
type sortieFake struct {
	*huntProvisionFake
	grounds  map[string][]huntCreature
	drain    int
	onTravel func(poi string)
}

func (f *sortieFake) GetRawJSON(key string) []byte {
	if key == "nearby" && f.grounds != nil {
		poi := f.state.CurrentPOI
		var cs []serverapi.NearbyCreature
		for _, c := range f.grounds[poi] {
			cs = append(cs, serverapi.NearbyCreature{
				CreatureID: c.id, Species: "belt_grazer", Role: "grazer", Hull: 45, MaxHull: 45,
			})
		}
		b, _ := json.Marshal(serverapi.GetNearbyResponse{Creatures: cs, CreatureCount: len(cs), POIID: poi})
		return b
	}
	return f.huntProvisionFake.GetRawJSON(key)
}

func (f *sortieFake) Hunt(ctx context.Context, creatureID string) error {
	for i := range f.modules {
		f.modules[i].CurrentAmmo = max(0, f.modules[i].CurrentAmmo-f.drain)
	}
	return f.huntFake.Hunt(ctx, creatureID)
}

func (f *sortieFake) Travel(ctx context.Context, poi string) (*game.TravelResult, error) {
	res, err := f.huntFake.Travel(ctx, poi)
	if f.onTravel != nil {
		f.onTravel(poi)
	}
	return res, err
}

func (f *sortieFake) Dock(ctx context.Context) error {
	if f.state.CurrentPOI != "haven_station" {
		f.huntFake.calls = append(f.huntFake.calls, "dock_refused")
		return errNotAStation
	}
	return f.huntFake.Dock(ctx)
}

// sortieKB is huntKB plus a repair service at the station, the autocannon
// ammo catalog and two more belts, each one jump further out.
func sortieKB(t *testing.T) knowledge.Base {
	t.Helper()
	ctx := context.Background()
	kb := huntKB()
	_ = kb.RememberBase(ctx, knowledge.SpaceBase{
		ID: "haven_station", POIID: "haven_station", PublicAccess: true, Services: map[string]bool{"repair": true},
	})
	_ = kb.RememberSystem(ctx, knowledge.System{
		ID: "pollux", Name: "Pollux", PoliceLevel: 30, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "test_system"}, {SystemID: "azmidi"}},
	})
	_ = kb.RememberSystem(ctx, knowledge.System{
		ID: "azmidi", Name: "Azmidi", PoliceLevel: 30, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "pollux"}, {SystemID: "castor"}},
	})
	_ = kb.RememberPOI(ctx, knowledge.POI{
		ID: sortieGround2, SystemID: "azmidi", Type: "asteroid_belt",
		Resources: []game.POIResource{{ResourceID: huntBeltResource, Richness: 34, Remaining: 100000}},
	})
	_ = kb.RememberSystem(ctx, knowledge.System{
		ID: "castor", Name: "Castor", PoliceLevel: 30, LastVisitedTick: 1,
		Connections: []knowledge.SystemConnection{{SystemID: "azmidi"}},
	})
	_ = kb.RememberPOI(ctx, knowledge.POI{
		ID: sortieGround3, SystemID: "castor", Type: "asteroid_belt",
		Resources: []game.POIResource{{ResourceID: huntBeltResource, Richness: 30, Remaining: 100000}},
	})
	if err := kb.StoreItems(ctx, []knowledge.CatalogItem{
		{ID: "standard_rounds_box", Name: "standard_rounds_box", Ammo: &knowledge.ItemAmmo{AmmoType: "autocannon"}},
	}); err != nil {
		t.Fatal(err)
	}
	return kb
}

type sortieEnv struct {
	fake     *sortieFake
	deps     HuntDeps
	out      *strings.Builder
	dir      string // the agent directory
	reports  []string
	activity []string
}

// newSortieEnv is a hunter docked at haven_station with modules fitted and a
// recording decider, record log and ReportSortie.
func newSortieEnv(t *testing.T, opts huntFakeOpts, modules []serverapi.ShipModule) *sortieEnv {
	t.Helper()
	hf := newHuntFake(t, opts)
	hf.state.Credits = 100000
	f := &sortieFake{huntProvisionFake: &huntProvisionFake{
		huntFake: hf, modules: modules, cargo: map[string]int{}, storage: map[string]int{},
	}}
	e := &sortieEnv{fake: f, out: &strings.Builder{}}
	deps := huntDeps(hf, e.out)
	deps.Client = f
	deps.KB = sortieKB(t)
	e.dir = filepath.Join(hf.agentsDir, deps.AgentID)
	log := huntdecide.NewLog(e.dir)
	deps.Decisions = log
	deps.Decider = huntdecide.Recorder{Inner: huntRules{}, Log: log}
	deps.HomeStation = "haven_station"
	deps.ReportSortie = func(r string) { e.reports = append(e.reports, r) }
	deps.SetActivity = func(a string) { e.activity = append(e.activity, a) }
	e.deps = deps
	return e
}

// sortieMissionEnv: a kill mission for `quantity` belt grazers on the board.
func sortieMissionEnv(t *testing.T, quantity int, modules []serverapi.ShipModule) *sortieEnv {
	t.Helper()
	return newSortieEnv(t, huntFakeOpts{
		board: []boardMission{{id: sortieMission, difficulty: 1, quantity: quantity}},
	}, modules)
}

func (e *sortieEnv) pass(t *testing.T) {
	t.Helper()
	if err := Hunt(context.Background(), e.deps); err != nil {
		t.Fatalf("Hunt returned %v; it must always return nil", err)
	}
}

func (e *sortieEnv) records(t *testing.T) []map[string]any {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(e.dir, huntdecide.DecisionsDir, "*.jsonl"))
	var recs []map[string]any
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			var m map[string]any
			if err = json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Fatal(err)
			}
			recs = append(recs, m)
		}
		_ = f.Close()
	}
	return recs
}

func (e *sortieEnv) ofType(t *testing.T, typ string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, r := range e.records(t) {
		if r["type"] == typ {
			out = append(out, r)
		}
	}
	return out
}

func (e *sortieEnv) returnDecisions(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, r := range e.ofType(t, "decision") {
		if r["kind"] == string(huntdecide.KindReturn) {
			out = append(out, r)
		}
	}
	return out
}

func (e *sortieEnv) fileExists() bool {
	_, err := os.Stat(filepath.Join(e.dir, huntdecide.SortieFile))
	return err == nil
}

func (e *sortieEnv) loadFile(t *testing.T) *huntdecide.Sortie {
	t.Helper()
	s, err := huntdecide.LoadSortie(e.dir)
	if err != nil || s == nil {
		t.Fatalf("sortie file: %v, %v", s, err)
	}
	return s
}

func (e *sortieEnv) seed(t *testing.T, s *huntdecide.Sortie) {
	t.Helper()
	if err := s.Save(e.dir); err != nil {
		t.Fatal(err)
	}
}

func (e *sortieEnv) calls() []string { return e.fake.huntFake.calls }

func (e *sortieEnv) countCall(want string) int {
	n := 0
	for _, c := range e.calls() {
		if c == want {
			n++
		}
	}
	return n
}

func (e *sortieEnv) indexOf(want string) int { return slices.Index(e.calls(), want) }

func sortieIDs(recs []map[string]any) []string {
	var ids []string
	for _, r := range recs {
		if id, _ := r["sortie_id"].(string); !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func sortieState(t *testing.T, rec map[string]any) map[string]any {
	t.Helper()
	st, _ := rec["state"].(map[string]any)
	so, _ := st["sortie"].(map[string]any)
	if so == nil {
		t.Fatalf("record has no state.sortie: %v", rec)
	}
	return so
}

func cannon(rounds int) []serverapi.ShipModule {
	ms := provCannons(1)
	ms[0].CurrentAmmo = rounds
	ms[0].LoadedAmmoID = "standard_rounds_box"
	return ms
}

func optionIDsOf(rec map[string]any) []string {
	opts, _ := rec["options"].([]any)
	var ids []string
	for _, o := range opts {
		m, _ := o.(map[string]any)
		id, _ := m["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func TestHuntOpensSortieWhenLeavingWithAJob(t *testing.T) {
	e := sortieMissionEnv(t, 3, provLasers(1))
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
	openWhileOut := false
	e.fake.onTravel = func(poi string) {
		if poi == sortieGround1 {
			openWhileOut = e.fileExists()
		}
	}
	e.pass(t)

	starts := e.ofType(t, "sortie_start")
	if len(starts) != 1 {
		t.Fatalf("sortie_start records = %d, want 1\n%s", len(starts), e.out)
	}
	st, _ := starts[0]["state"].(map[string]any)
	job, _ := st["job"].(map[string]any)
	if job["kind"] != "mission" || job["id"] != "hex-"+sortieMission {
		t.Errorf("sortie_start job = %v", job)
	}
	if !openWhileOut {
		t.Errorf("hunt-sortie.json must exist while the ship is out\n%s", e.out)
	}
	if s := e.loadFile(t); s.ID != starts[0]["sortie_id"] || s.JobKind != "mission" {
		t.Errorf("sortie file = %+v, want id %v", s, starts[0]["sortie_id"])
	}
}

func TestHuntClosesCompletedOnMissionComplete(t *testing.T) {
	e := sortieMissionEnv(t, 1, provLasers(1))
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
	e.pass(t)

	if !slices.Contains(e.calls(), "complete:hex-"+sortieMission) {
		t.Fatalf("mission not completed, calls %v", e.calls())
	}
	ends := e.ofType(t, "sortie_end")
	if len(ends) != 1 || ends[0]["result"] != huntdecide.ResultCompleted {
		t.Fatalf("sortie_end = %v, want one completed", ends)
	}
	if ids := sortieIDs(append(e.ofType(t, "sortie_start"), ends...)); len(ids) != 1 {
		t.Errorf("start and end must share one sortie id, got %v", ids)
	}
	if !slices.Equal(e.reports, []string{huntdecide.ResultCompleted}) {
		t.Errorf("ReportSortie calls = %v, want [completed]", e.reports)
	}
	if e.fileExists() {
		t.Error("the sortie file must be cleared once the sortie closes")
	}
}

func TestHuntStaysOutForNextGroundWhenPoolRunsDry(t *testing.T) {
	e := sortieMissionEnv(t, 2, provLasers(1))
	e.fake.grounds = map[string][]huntCreature{
		sortieGround1: huntGrazers("g1"),
		sortieGround2: huntGrazers("g2", "g3"),
	}
	e.pass(t)

	first, second := e.indexOf("travel:"+sortieGround1), e.indexOf("travel:"+sortieGround2)
	if first < 0 || second < first {
		t.Fatalf("want ground 1 then ground 2, calls %v", e.calls())
	}
	for _, c := range e.calls()[first:second] {
		if c == "dock" || c == "dock_refused" {
			t.Errorf("docked between grounds: %v", e.calls())
		}
	}
	if e.fake.huntCalls != 2 {
		t.Errorf("hunt calls = %d, want 2", e.fake.huntCalls)
	}
	recs := e.records(t)
	if ids := sortieIDs(recs); len(ids) != 1 {
		t.Errorf("one sortie expected, got ids %v", ids)
	}
	ends := e.ofType(t, "sortie_end")
	if len(ends) != 1 || ends[0]["result"] != huntdecide.ResultCompleted {
		t.Fatalf("sortie_end = %v", ends)
	}
	so := sortieState(t, ends[0])
	if so["kills"] != float64(2) {
		t.Errorf("sortie kills = %v, want 2", so["kills"])
	}
	if g, _ := so["grounds"].([]any); len(g) != 2 {
		t.Errorf("sortie grounds = %v, want 2", so["grounds"])
	}
}

func TestHuntResumesOpenSortieUndocked(t *testing.T) {
	e := newSortieEnv(t, huntFakeOpts{
		board:          []boardMission{{id: sortieMission, difficulty: 1, quantity: 2}},
		activeMissions: []serverapi.ActiveMission{heldMission(sortieMission, 0, 2, 1)},
		heldAtStart:    true,
		startPOI:       sortieGround1,
		unsetDocked:    true,
	}, provLasers(1))
	e.fake.grounds = map[string][]huntCreature{
		sortieGround1: huntGrazers("g1"),
		sortieGround2: huntGrazers("g2", "g3"),
	}
	seeded := &huntdecide.Sortie{
		ID: "pirate-6-20261121T100000Z", Seq: 1, JobKind: "mission", JobID: "hex-" + sortieMission,
		Grounds: []huntdecide.GroundVisit{{POI: sortieGround1, Quarry: true}},
	}
	e.seed(t, seeded)
	e.pass(t)

	if slices.Contains(e.calls(), "travel:haven_station") || e.countCall("dock") > 0 || e.countCall("dock_refused") > 0 {
		t.Errorf("an open sortie out at a ground must not relocate to a station, calls %v", e.calls())
	}
	if slices.Contains(e.calls(), "get_missions") {
		t.Errorf("a resumed sortie must not read the board, calls %v", e.calls())
	}
	if slices.Contains(e.calls(), "travel:"+sortieGround1) || slices.Contains(e.calls(), "hunt:g1") {
		t.Errorf("visited ground revisited, calls %v", e.calls())
	}
	if !slices.Contains(e.calls(), "hunt:g2") {
		t.Fatalf("resumed sortie must hunt at the next ground, calls %v\n%s", e.calls(), e.out)
	}
	if ids := sortieIDs(e.records(t)); !slices.Equal(ids, []string{seeded.ID}) {
		t.Errorf("records carry sortie ids %v, want only %s", ids, seeded.ID)
	}
	if len(e.ofType(t, "sortie_start")) != 0 {
		t.Error("a resumed sortie writes no second sortie_start")
	}
}

func TestHuntClosesInterruptedWhenFoundDocked(t *testing.T) {
	e := sortieMissionEnv(t, 1, provLasers(1))
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
	e.seed(t, &huntdecide.Sortie{ID: "pirate-6-old", JobKind: "mission", JobID: "hex-old"})
	e.pass(t)

	ends := e.ofType(t, "sortie_end")
	if len(ends) == 0 || ends[0]["sortie_id"] != "pirate-6-old" || ends[0]["result"] != huntdecide.ResultInterrupted {
		t.Fatalf("first sortie_end = %v, want pirate-6-old interrupted", ends)
	}
	if len(e.reports) == 0 || e.reports[0] != huntdecide.ResultInterrupted {
		t.Errorf("ReportSortie = %v, want interrupted first", e.reports)
	}
	// The pass then runs as today: the mission is hunted and completed.
	if !slices.Contains(e.calls(), "complete:hex-"+sortieMission) {
		t.Errorf("the pass must go on after closing, calls %v", e.calls())
	}
}

func TestHuntInterruptedWhenJobGoneWhileOut(t *testing.T) {
	e := newSortieEnv(t, huntFakeOpts{
		board:       []boardMission{{id: sortieMission, difficulty: 1, quantity: 1}},
		startPOI:    sortieGround1,
		unsetDocked: true,
	}, provLasers(1))
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
	e.seed(t, &huntdecide.Sortie{ID: "pirate-6-gone", JobKind: "mission", JobID: "hex-expired"})

	e.pass(t)
	if !slices.Contains(e.calls(), "travel:haven_station") || e.countCall("dock") != 1 {
		t.Fatalf("must recover to a station, calls %v\n%s", e.calls(), e.out)
	}
	if s := e.loadFile(t); s.PendingResult != huntdecide.ResultInterrupted {
		t.Errorf("pending result = %q, want interrupted", s.PendingResult)
	}
	if len(e.reports) != 0 {
		t.Errorf("nothing closes before the docked pass, got %v", e.reports)
	}

	e.pass(t)
	if len(e.reports) == 0 || e.reports[0] != huntdecide.ResultInterrupted {
		t.Errorf("ReportSortie = %v, want interrupted first", e.reports)
	}
	ends := e.ofType(t, "sortie_end")
	if len(ends) == 0 || ends[0]["sortie_id"] != "pirate-6-gone" {
		t.Errorf("sortie_end = %v", ends)
	}
}

func TestHuntReturnsToRearmWhenReserveGone(t *testing.T) {
	e := sortieMissionEnv(t, 3, cannon(5))
	e.fake.cargo["standard_rounds_box"] = 1
	e.fake.drain = 12
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1", "g2", "g3")}
	e.pass(t)

	decs := e.returnDecisions(t)
	if len(decs) != 1 || decs[0]["option_id"] != huntOptRearm || !slices.Contains(optionIDsOf(decs[0]), huntOptRearm) {
		t.Fatalf("return decisions = %v, want one choosing rearm\n%s", decs, e.out)
	}
	if e.fake.huntCalls != 1 {
		t.Errorf("hunt calls = %d, want 1", e.fake.huntCalls)
	}
	hunt := e.indexOf("hunt:g1")
	home := e.indexOf("travel:haven_station")
	if home < hunt || e.countCall("dock") != 1 {
		t.Errorf("must fly to the home station and dock after the fight, calls %v", e.calls())
	}
	if s := e.loadFile(t); s.PendingResult != huntdecide.ResultReturnedDry {
		t.Errorf("pending = %q, want returned_dry", s.PendingResult)
	}

	e.pass(t)
	ends := e.ofType(t, "sortie_end")
	if len(ends) == 0 || ends[0]["result"] != huntdecide.ResultReturnedDry {
		t.Errorf("sortie_end = %v, want returned_dry first", ends)
	}
	if len(e.reports) == 0 || e.reports[0] != huntdecide.ResultReturnedDry {
		t.Errorf("ReportSortie = %v", e.reports)
	}
}

func TestHuntContinuesWithReserve(t *testing.T) {
	e := sortieMissionEnv(t, 3, cannon(5))
	e.fake.cargo["standard_rounds_box"] = 5
	e.fake.drain = 12
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1", "g2", "g3")}
	e.pass(t)

	decs := e.returnDecisions(t)
	if len(decs) == 0 || decs[0]["option_id"] != huntOptContinue {
		t.Fatalf("return decisions = %v, want continue\n%s", decs, e.out)
	}
	if e.fake.huntCalls < 2 {
		t.Errorf("hunt calls = %d, want a second engagement", e.fake.huntCalls)
	}
	ends := e.ofType(t, "sortie_end")
	if len(ends) != 1 || ends[0]["result"] != huntdecide.ResultCompleted {
		t.Fatalf("sortie_end = %v", ends)
	}
	// Behaviour 10: one box reloaded before each of fights 2 and 3.
	if so := sortieState(t, ends[0]); so["reloads_spent"] != float64(2) || so["fights"] != float64(3) {
		t.Errorf("sortie so far = %v, want reloads_spent 2, fights 3", so)
	}
}

func TestHuntReturnsToRepairBelowHalfHull(t *testing.T) {
	e := sortieMissionEnv(t, 3, provLasers(1))
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1", "g2", "g3")}
	wounded := false
	e.fake.onGetStatus = func() {
		if e.fake.huntCalls >= 1 && !wounded {
			wounded = true
			e.fake.state.Ship.Hull = 0.45 * e.fake.state.Ship.MaxHull
		}
	}
	e.pass(t)

	decs := e.returnDecisions(t)
	if len(decs) != 1 || decs[0]["option_id"] != huntOptRepair {
		t.Fatalf("return decisions = %v, want repair\n%s", decs, e.out)
	}
	if e.indexOf("travel:haven_station") < e.indexOf("hunt:g1") || e.countCall("dock") != 1 {
		t.Errorf("must fly to the repair station and dock, calls %v", e.calls())
	}
	if s := e.loadFile(t); s.PendingResult != huntdecide.ResultReturnedDamaged {
		t.Errorf("pending = %q", s.PendingResult)
	}
	e.pass(t)
	if len(e.reports) == 0 || e.reports[0] != huntdecide.ResultReturnedDamaged {
		t.Errorf("ReportSortie = %v, want returned_damaged first", e.reports)
	}
}

func TestHuntForcedDryReturnMakesNoDecision(t *testing.T) {
	t.Run("at the reload gate", func(t *testing.T) {
		e := sortieMissionEnv(t, 3, cannon(20))
		e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1", "g2")}
		e.fake.onTravel = func(poi string) {
			if poi == sortieGround1 {
				e.fake.modules[0].CurrentAmmo = 0 // spent on the way out
			}
		}
		e.pass(t)
		assertForcedDry(t, e, 0)
	})
	t.Run("after a fight", func(t *testing.T) {
		e := sortieMissionEnv(t, 3, cannon(12))
		e.fake.drain = 12
		e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1", "g2")}
		e.pass(t)
		assertForcedDry(t, e, 1)
	})
}

func assertForcedDry(t *testing.T, e *sortieEnv, hunts int) {
	t.Helper()
	if decs := e.returnDecisions(t); len(decs) != 0 {
		t.Errorf("a forced return makes no decision, got %v", decs)
	}
	if e.fake.huntCalls != hunts {
		t.Errorf("hunt calls = %d, want %d", e.fake.huntCalls, hunts)
	}
	if !slices.Contains(e.calls(), "travel:haven_station") {
		t.Errorf("must head home to rearm, calls %v\n%s", e.calls(), e.out)
	}
	if s := e.loadFile(t); s.PendingResult != huntdecide.ResultReturnedDry {
		t.Errorf("pending = %q, want returned_dry", s.PendingResult)
	}
	e.pass(t)
	if len(e.reports) == 0 || e.reports[0] != huntdecide.ResultReturnedDry {
		t.Errorf("ReportSortie = %v, want returned_dry first", e.reports)
	}
}

func TestHuntBlockedUnarmedNeverLeavesDock(t *testing.T) {
	e := sortieMissionEnv(t, 1, cannon(0))
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
	e.pass(t)

	if slices.Contains(e.calls(), "undock") {
		t.Errorf("an unarmed hunter must not undock, calls %v", e.calls())
	}
	if !slices.Equal(e.reports, []string{huntdecide.ResultBlockedUnarmed}) {
		t.Errorf("ReportSortie = %v, want [blocked_unarmed]", e.reports)
	}
	if e.fileExists() {
		t.Error("a blocked sortie must leave no sortie file")
	}
	if len(e.ofType(t, "sortie_start")) != 0 {
		t.Error("a blocked sortie never started")
	}
	ends := e.ofType(t, "sortie_end")
	if len(ends) != 1 || ends[0]["result"] != huntdecide.ResultBlockedUnarmed {
		t.Errorf("sortie_end = %v", ends)
	}
	if n := len(e.activity); n == 0 || e.activity[n-1] != "Hunt · unarmed" {
		t.Errorf("activity = %v, want last Hunt · unarmed", e.activity)
	}
}

func TestHuntSortieGroundCap(t *testing.T) {
	e := newSortieEnv(t, huntFakeOpts{
		board:          []boardMission{{id: sortieMission, difficulty: 1, quantity: 5}},
		activeMissions: []serverapi.ActiveMission{heldMission(sortieMission, 0, 5, 1)},
		heldAtStart:    true,
		startPOI:       "old_ground_6",
		unsetDocked:    true,
	}, provLasers(1))
	e.fake.grounds = map[string][]huntCreature{
		sortieGround1: huntGrazers("g1"),
		sortieGround2: huntGrazers("g2"),
		sortieGround3: huntGrazers("g3"),
	}
	s := &huntdecide.Sortie{ID: "pirate-6-capped", JobKind: "mission", JobID: "hex-" + sortieMission}
	for i := range 6 {
		s.Grounds = append(s.Grounds, huntdecide.GroundVisit{POI: fmt.Sprintf("old_ground_%d", i+1)})
	}
	e.seed(t, s)

	e.pass(t) // grounds 7 and 8, then the cap
	if !slices.Contains(e.calls(), "hunt:g1") || !slices.Contains(e.calls(), "hunt:g2") {
		t.Fatalf("want grounds 7 and 8 hunted, calls %v\n%s", e.calls(), e.out)
	}
	if slices.Contains(e.calls(), "travel:"+sortieGround3) {
		t.Errorf("a ninth ground was visited, calls %v", e.calls())
	}
	if got := e.loadFile(t); got.PendingResult != huntdecide.ResultNoQuarry || len(got.Grounds) != huntMaxSortieGrounds {
		t.Errorf("sortie = %+v, want no_quarry after %d grounds", got, huntMaxSortieGrounds)
	}
	e.pass(t) // recovers to a station
	e.pass(t) // docked: closes
	if len(e.reports) == 0 || e.reports[0] != huntdecide.ResultNoQuarry {
		t.Errorf("ReportSortie = %v, want no_quarry first\n%s", e.reports, e.out)
	}
}

func TestHuntCorruptSortieFileIsIgnored(t *testing.T) {
	for _, body := range []string{`{"id":`, `null`} {
		t.Run(body, func(t *testing.T) {
			e := sortieMissionEnv(t, 1, provLasers(1))
			e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
			if err := os.MkdirAll(e.dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(e.dir, huntdecide.SortieFile), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			e.pass(t)
			if n := strings.Count(e.out.String(), "sortie file unreadable"); n != 1 {
				t.Errorf("logged %d times, want once\n%s", n, e.out)
			}
			if !slices.Equal(e.reports, []string{huntdecide.ResultCompleted}) {
				t.Errorf("ReportSortie = %v, want only the fresh sortie's completed", e.reports)
			}
		})
	}
}

func TestHuntRunsWhenRecordDirUnwritable(t *testing.T) {
	e := sortieMissionEnv(t, 1, provLasers(1))
	e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, huntdecide.DecisionsDir), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.pass(t)
	if !slices.Contains(e.calls(), "complete:hex-"+sortieMission) {
		t.Errorf("the mission must still complete, calls %v\n%s", e.calls(), e.out)
	}
	if !slices.Equal(e.reports, []string{huntdecide.ResultCompleted}) {
		t.Errorf("ReportSortie = %v", e.reports)
	}
}

func TestHuntReturnOptionsExploreFlags(t *testing.T) {
	armed := huntdecide.State{
		Hull: 90, MaxHull: 100,
		Weapons: []huntdecide.Weapon{{ModuleID: "ac0", AmmoType: "autocannon", MagazineSize: 20, Loaded: 8}},
		Ammo:    []huntdecide.Ammo{{AmmoType: "autocannon", ItemID: "standard_rounds_box", Aboard: 2}},
	}
	byID := func(opts []huntdecide.Option) map[string]huntdecide.Option {
		m := map[string]huntdecide.Option{}
		for _, o := range opts {
			m[o.ID] = o
		}
		return m
	}

	before := byID(huntReturnOptions(armed, &huntdecide.Sortie{Fights: 0}))
	if !before[huntOptRearm].NoExplore || !before[huntOptRepair].NoExplore {
		t.Errorf("before any fight: rearm and repair must be NoExplore, got %+v", before)
	}
	if _, ok := before[huntOptContinue]; !ok {
		t.Errorf("continue must be offered when the ship can fire, got %+v", before)
	}

	after := byID(huntReturnOptions(armed, &huntdecide.Sortie{Fights: 1}))
	if after[huntOptRearm].NoExplore {
		t.Errorf("after a fight rearm may be explored, got %+v", after)
	}
	if !after[huntOptRepair].NoExplore {
		t.Errorf("repair is always NoExplore, got %+v", after)
	}

	dry := armed
	dry.Weapons = []huntdecide.Weapon{{ModuleID: "ac0", AmmoType: "autocannon", MagazineSize: 20}}
	dry.Ammo = nil
	got := byID(huntReturnOptions(dry, &huntdecide.Sortie{Fights: 1}))
	if _, ok := got[huntOptContinue]; ok {
		t.Errorf("continue must be absent when nothing can fire, got %+v", got)
	}

	lasers := huntdecide.State{Weapons: []huntdecide.Weapon{{ModuleID: "l0"}}}
	if _, ok := byID(huntReturnOptions(lasers, &huntdecide.Sortie{Fights: 1}))[huntOptRearm]; ok {
		t.Error("rearm is offered only with an ammo-fed weapon")
	}
}

// TestHuntAlwaysReturnsNil drives every sortie path above, plus a few
// failures, and requires a nil error from every pass.
func TestHuntAlwaysReturnsNil(t *testing.T) {
	paths := map[string]func(t *testing.T) (*sortieEnv, int){
		"completed": func(t *testing.T) (*sortieEnv, int) {
			e := sortieMissionEnv(t, 1, provLasers(1))
			e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
			return e, 1
		},
		"rearm": func(t *testing.T) (*sortieEnv, int) {
			e := sortieMissionEnv(t, 3, cannon(5))
			e.fake.cargo["standard_rounds_box"] = 1
			e.fake.drain = 12
			e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1", "g2", "g3")}
			return e, 2
		},
		"blocked unarmed": func(t *testing.T) (*sortieEnv, int) {
			return sortieMissionEnv(t, 1, cannon(0)), 1
		},
		"no quarry": func(t *testing.T) (*sortieEnv, int) {
			return sortieMissionEnv(t, 1, provLasers(1)), 3
		},
		"job gone": func(t *testing.T) (*sortieEnv, int) {
			e := newSortieEnv(t, huntFakeOpts{startPOI: sortieGround1, unsetDocked: true}, provLasers(1))
			e.seed(t, &huntdecide.Sortie{ID: "x", JobKind: "need", JobID: "gone"})
			return e, 2
		},
		"ship unreadable": func(t *testing.T) (*sortieEnv, int) {
			e := sortieMissionEnv(t, 2, provLasers(1))
			e.fake.shipErr = errors.New("get_ship: boom")
			e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
			return e, 2
		},
		"sortie dir unwritable": func(t *testing.T) (*sortieEnv, int) {
			e := sortieMissionEnv(t, 2, provLasers(1))
			e.fake.grounds = map[string][]huntCreature{sortieGround1: huntGrazers("g1")}
			e.deps.AgentsDir = filepath.Join(e.dir, "file")
			if err := os.MkdirAll(e.dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(e.deps.AgentsDir, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			return e, 2
		},
	}
	for name, build := range paths {
		t.Run(name, func(t *testing.T) {
			e, passes := build(t)
			for range passes {
				e.pass(t)
			}
		})
	}
}
