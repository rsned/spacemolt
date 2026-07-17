package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// GetBase serves explorePOIFor's base->POI resolution. The fake's bases map is
// keyed by POI id, so scan values for a base-id match; a miss returns nil (the
// caller falls back to using the base id as the POI id).
func (f *fakeKB) GetBase(_ context.Context, baseID string) (*knowledge.SpaceBase, error) {
	for _, b := range f.bases {
		if b != nil && b.ID == baseID {
			return b, nil
		}
	}
	return nil, nil
}

func visitObj(system string) serverapi.MissionObjective {
	return serverapi.MissionObjective{Type: "visit_system", SystemID: system}
}

func dockObj(system, base string) serverapi.MissionObjective {
	return serverapi.MissionObjective{Type: "dock_at_base", SystemID: system, TargetBaseID: base}
}

func exploreEntry(id string, reward int, objs ...serverapi.MissionObjective) serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{
		MissionID: id, TemplateID: id, Type: "exploration", Title: "Tour " + id,
		Rewards:    &serverapi.MissionRewards{Credits: reward},
		Objectives: objs,
	}
}

func TestExploreShape(t *testing.T) {
	legs, ok := exploreShape(exploreEntry("m", 5000, visitObj("sol"), dockObj("haven", "haven_station")))
	if !ok || len(legs) != 2 || legs[0] != (missionLeg{SystemID: "sol"}) || legs[1] != (missionLeg{SystemID: "haven", BaseID: "haven_station"}) {
		t.Fatalf("pure-nav mission must shape in wire order, got %v ok=%v", legs, ok)
	}

	// Exploration-typed missions can smuggle in non-nav objectives (live
	// board: overdue_accounts carries deliver_item) — type alone can't be trusted.
	mixed := exploreEntry("m2", 5000, visitObj("sol"))
	mixed.Objectives = append(mixed.Objectives, serverapi.MissionObjective{Type: "deliver_item", ItemID: "ore", Quantity: 5, TargetBaseID: "b", SystemID: "sol"})
	if _, ok := exploreShape(mixed); ok {
		t.Fatal("mixed-objective mission must be rejected")
	}

	del := boardEntry("d", "steel", 5, "sol_station", "sol", 1000, 0)
	if _, ok := exploreShape(del); ok {
		t.Fatal("delivery-typed mission must be rejected")
	}

	bad := exploreEntry("m3", 5000, serverapi.MissionObjective{Type: "dock_at_base", SystemID: "sol"}) // no target base
	if _, ok := exploreShape(bad); ok {
		t.Fatal("dock objective without target_base_id must be rejected")
	}
}

func TestExploreTourPinsReturnAndOptimizes(t *testing.T) {
	// Open chain (no return): a is 1 jump out, b is 1 past a but 5 direct —
	// visiting b first costs 6, a-then-b costs 2. Wire order is the bad one.
	d := map[string]map[string]int{
		"haven": {"a": 1, "b": 5},
		"a":     {"haven": 1, "b": 1},
		"b":     {"haven": 5, "a": 1},
	}
	dist := func(x, y string) int {
		if x == y {
			return 0
		}
		return d[x][y]
	}
	ordered, jumps := exploreTour("haven", []missionLeg{{SystemID: "b"}, {SystemID: "a"}}, dist)
	if jumps != 2 || ordered[0].SystemID != "a" || ordered[1].SystemID != "b" {
		t.Fatalf("open tour must reorder to a-then-b (2 jumps), got %v in %d", ordered, jumps)
	}

	// Pinned return: the trailing dock leg stays last even though its system
	// (haven, distance 0 from the start) would be cheapest to "visit" first.
	legs := []missionLeg{{SystemID: "b"}, {SystemID: "a"}, {SystemID: "haven", BaseID: "haven_station"}}
	ordered, jumps = exploreTour("haven", legs, dist)
	if jumps != 7 { // a(1)+b(1)+return(5) and b(5)+a(1)+return(1) tie at 7
		t.Fatalf("closed tour = 7 jumps, got %d (%v)", jumps, ordered)
	}
	if ordered[len(ordered)-1].BaseID != "haven_station" {
		t.Fatalf("return dock must stay pinned last, got %v", ordered)
	}
}

func TestBuildExploreCandidateNetPerJumpGate(t *testing.T) {
	dist := func(a, b string) int {
		if a == b {
			return 0
		}
		return 4
	}
	noFuel := func(int) float64 { return 0 }

	// 900 cr over 4 jumps = 225/jump: clears missionMinNet (500) but not the
	// per-jump floor — the "Local Sector Survey" trap shape.
	if _, reason := buildExploreCandidate(exploreEntry("trap", 900, visitObj("far")), "haven", dist, noFuel); reason == "" {
		t.Fatal("sub-floor per-jump mission must be rejected")
	}
	// 4000 over 4 jumps = 1000/jump: accepted.
	c, reason := buildExploreCandidate(exploreEntry("good", 4000, visitObj("far")), "haven", dist, noFuel)
	if reason != "" {
		t.Fatalf("good candidate rejected: %s", reason)
	}
	if c.Jumps != 4 || c.Net != 4000 || len(c.Legs) != 1 {
		t.Fatalf("candidate = %+v, want 4 jumps net 4000", c)
	}
}

// exploreKB seeds haven<->sol plus station POIs/bases for both, so recovery,
// reposition, and explorePOIFor lookups all resolve.
func exploreKB() *fakeKB {
	return &fakeKB{
		systems: []knowledge.System{{ID: "haven", Name: "Haven"}, {ID: "sol", Name: "Sol"}},
		conns:   undirected([2]string{"haven", "sol"}),
		pois: map[string][]knowledge.POI{
			"haven": {{ID: "haven_station", SystemID: "haven", Type: "station"}},
			"sol":   {{ID: "sol_station", SystemID: "sol", Type: "station"}},
		},
		bases: map[string]*knowledge.SpaceBase{
			"haven_station": {ID: "haven_station", POIID: "haven_station", PublicAccess: true},
			"sol_station":   {ID: "sol_station", POIID: "sol_station", PublicAccess: true},
		},
	}
}

func exploreActive(id string, completed bool, objs ...serverapi.ActiveMissionObjective) serverapi.ActiveMission {
	for i := range objs {
		objs[i].Completed = completed
	}
	return serverapi.ActiveMission{
		MissionID: id, TemplateID: "tour1", Type: "exploration", Title: "Tour tour1", Objectives: objs,
	}
}

func TestMissionsExploreEndToEnd(t *testing.T) {
	// Board: one exploration mission — visit sol, return-dock at haven_station.
	// Tour = haven->sol->haven = 2 jumps; 3000 cr = 1500/jump.
	entry := exploreEntry("tour1", 3000, visitObj("sol"), dockObj("haven", "haven_station"))
	objs := []serverapi.ActiveMissionObjective{
		{Type: "visit_system", SystemID: "sol"},
		{Type: "dock_at_base", SystemID: "haven", TargetBase: "haven_station"},
	}
	fc := &fakeClient{
		state:          missionState(true, 5000, 0),
		completeReward: 3000,
		activeMissionsSeq: [][]byte{
			activeJSON(t), // resume read: nothing held
			activeJSON(t, exploreActive("hex-tour1", false, objs...)), // id resolution after accept
			activeJSON(t, exploreActive("hex-tour1", true, objs...)),  // post-tour re-read: all done
		},
		raw: map[string][]byte{"missions": boardJSON(t, entry)},
	}
	store := &fakeMissionStore{}
	deps := missionDeps(fc, store, exploreKB())
	deps.State = &missionRunState{}
	deps.Categories = []string{"delivery", "exploration"}
	var navTo []string
	deps.nav = func(ctx context.Context, system, poi string) error {
		navTo = append(navTo, system+"/"+poi)
		return nil
	}
	captures := 0
	deps.capture = func(ctx context.Context) error { captures++; return nil }

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if len(navTo) != 2 || navTo[0] != "sol/" || navTo[1] != "haven/haven_station" {
		t.Fatalf("tour must fly visit-then-return, got %v", navTo)
	}
	completed := false
	for _, c := range fc.calls {
		if c == "complete:hex-tour1" {
			completed = true
		}
	}
	if !completed {
		t.Fatalf("must complete via the resolved active id, calls=%v", fc.calls)
	}
	if captures != 1 {
		t.Fatalf("dock leg must fire exactly one market capture, got %d", captures)
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 telemetry row, got %+v", store.results)
	}
	r := store.results[0]
	if r.Outcome != "completed" || r.MissionType != "exploration" || r.CreditsEarned != 3000 || r.Jumps != 2 || r.Reason != "" {
		t.Fatalf("row = %+v, want completed exploration +3000 over 2 jumps", r)
	}
}

func TestMissionsExploreStagedNonNavAbandons(t *testing.T) {
	// The post-tour re-read reveals a freshly appended deliver_item objective:
	// the mission left the pure-navigation lane and must be abandoned with the
	// staged_non_nav slug (operator-observed staged story missions).
	entry := exploreEntry("tour1", 3000, visitObj("sol"), dockObj("haven", "haven_station"))
	staged := exploreActive("hex-tour1", true,
		serverapi.ActiveMissionObjective{Type: "visit_system", SystemID: "sol"},
		serverapi.ActiveMissionObjective{Type: "dock_at_base", SystemID: "haven", TargetBase: "haven_station"})
	staged.Objectives = append(staged.Objectives, serverapi.ActiveMissionObjective{
		Type: "deliver_item", ItemID: "ore", Required: 5, SystemID: "sol", TargetBase: "sol_station",
	})
	fc := &fakeClient{
		state: missionState(true, 5000, 0),
		activeMissionsSeq: [][]byte{
			activeJSON(t),
			activeJSON(t, exploreActive("hex-tour1", false,
				serverapi.ActiveMissionObjective{Type: "visit_system", SystemID: "sol"},
				serverapi.ActiveMissionObjective{Type: "dock_at_base", SystemID: "haven", TargetBase: "haven_station"})),
			activeJSON(t, staged),
		},
		raw: map[string][]byte{"missions": boardJSON(t, entry)},
	}
	store := &fakeMissionStore{}
	deps := missionDeps(fc, store, exploreKB())
	deps.State = &missionRunState{}
	deps.Categories = []string{"exploration"}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	abandoned := false
	for _, c := range fc.calls {
		if c == "abandon:hex-tour1" {
			abandoned = true
		}
	}
	if !abandoned {
		t.Fatalf("staged non-nav mission must be abandoned, calls=%v", fc.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "abandoned" || store.results[0].Reason != "staged_non_nav" {
		t.Fatalf("row must record staged_non_nav, got %+v", store.results)
	}
}

func TestMissionsExploreDisabledByDefault(t *testing.T) {
	// Default category allowlist is delivery-only: a board holding only an
	// exploration mission is a dry pass, with no accept.
	entry := exploreEntry("tour1", 3000, visitObj("sol"), dockObj("haven", "haven_station"))
	fc := &fakeClient{
		state: missionState(true, 5000, 0),
		raw: map[string][]byte{
			"missions":        boardJSON(t, entry),
			"active_missions": activeJSON(t),
		},
	}
	store := &fakeMissionStore{}
	deps := missionDeps(fc, store, exploreKB())
	deps.State = &missionRunState{}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	for _, c := range fc.calls {
		if c == "accept:tour1" {
			t.Fatalf("exploration must not be accepted without the category allowlisted, calls=%v", fc.calls)
		}
	}
	if deps.State.dry != 1 {
		t.Fatalf("pass must count as dry, got %d", deps.State.dry)
	}
}

func TestMissionsResumeExploreFliesRemainingLegs(t *testing.T) {
	// Held exploration mission with the visit leg already complete: resume
	// must fly only the remaining return dock, then complete in place.
	held := serverapi.ActiveMission{
		MissionID: "hex-held", TemplateID: "tour1", Type: "exploration", Title: "Tour tour1",
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "visit_system", SystemID: "sol", Completed: true},
			{Type: "dock_at_base", SystemID: "haven", TargetBase: "haven_station"},
		},
	}
	done := held
	done.Objectives = []serverapi.ActiveMissionObjective{
		{Type: "visit_system", SystemID: "sol", Completed: true},
		{Type: "dock_at_base", SystemID: "haven", TargetBase: "haven_station", Completed: true},
	}
	fc := &fakeClient{
		state:          missionState(true, 5000, 0),
		completeReward: 3000,
		activeMissionsSeq: [][]byte{
			activeJSON(t, held), // resume read
			activeJSON(t, done), // post-fly re-read
		},
		raw: map[string][]byte{"missions": boardJSON(t)},
	}
	store := &fakeMissionStore{}
	deps := missionDeps(fc, store, exploreKB())
	deps.State = &missionRunState{}
	deps.Categories = []string{"delivery", "exploration"}
	var navTo []string
	deps.nav = func(ctx context.Context, system, poi string) error {
		navTo = append(navTo, system+"/"+poi)
		return nil
	}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if len(navTo) != 1 || navTo[0] != "haven/haven_station" {
		t.Fatalf("resume must fly only the incomplete leg, got %v", navTo)
	}
	completed := false
	for _, c := range fc.calls {
		if c == "complete:hex-held" {
			completed = true
		}
	}
	if !completed {
		t.Fatalf("resumed tour must complete, calls=%v", fc.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "completed" || store.results[0].CreditsEarned != 3000 {
		t.Fatalf("row = %+v, want completed +3000", store.results)
	}
}

// TestExploreTourUnreachableLeg guards the RouteInf propagation: any
// unreachable leg poisons the whole tour cost.
func TestExploreTourUnreachableLeg(t *testing.T) {
	dist := func(a, b string) int {
		if a == b {
			return 0
		}
		if b == "island" || a == "island" {
			return navigation.RouteInf
		}
		return 1
	}
	_, jumps := exploreTour("haven", []missionLeg{{SystemID: "a"}, {SystemID: "island"}}, dist)
	if jumps < navigation.RouteInf {
		t.Fatalf("unreachable leg must poison the tour, got %d", jumps)
	}
}
