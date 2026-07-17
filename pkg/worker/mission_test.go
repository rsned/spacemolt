package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
)

// Mission-command fakes for the shared fakeClient (fields it needs are added
// to the struct in dispatch_test.go by this task — see Step 3 note).
//
// Deviation from the brief: fakeClient.GetMissions already exists in
// dispatch_test.go (identical body), so it is not redefined here.
func (f *fakeClient) GetActiveMissions(ctx context.Context) error {
	f.calls = append(f.calls, "get_active_missions")
	return nil
}
func (f *fakeClient) AcceptMission(ctx context.Context, id string) error {
	f.calls = append(f.calls, "accept:"+id)
	return f.acceptErr
}
func (f *fakeClient) CompleteMission(ctx context.Context, id string) error {
	f.calls = append(f.calls, "complete:"+id)
	f.state.Credits += f.completeReward // GetCredits() reads State.Credits
	return nil
}
func (f *fakeClient) AbandonMission(ctx context.Context, id string) error {
	f.calls = append(f.calls, "abandon:"+id)
	return nil
}

// fakeMissionStore records results and serves reference asks.
type fakeMissionStore struct {
	asks    map[string]float64
	results []market.MissionResult
}

func (s *fakeMissionStore) RecordMissionResult(ctx context.Context, r market.MissionResult) error {
	s.results = append(s.results, r)
	return nil
}
func (s *fakeMissionStore) GetReferenceAsk(ctx context.Context, itemID string) (market.ReferenceAsk, bool, error) {
	ask, ok := s.asks[itemID]
	return market.ReferenceAsk{ItemID: itemID, BestAsk: ask}, ok, nil
}

// missionKB returns a two-system KB (haven <-> sol) with the worker at haven.
func missionKB() *fakeKB {
	return &fakeKB{
		systems: []knowledge.System{{ID: "haven", Name: "Haven"}, {ID: "sol", Name: "Sol"}},
		conns:   []knowledge.Connection{{FromSystem: "haven", ToSystem: "sol"}},
	}
}

func boardJSON(t *testing.T, entries ...serverapi.MissionBoardEntry) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.GetMissionsResponse{Missions: entries, BaseID: "haven_station"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func activeJSON(t *testing.T, missions ...serverapi.ActiveMission) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.GetActiveMissionsResponse{Missions: missions})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// storageJSON builds a view_storage payload from an item-id -> quantity map.
func storageJSON(t *testing.T, items map[string]float64) []byte {
	t.Helper()
	entries := make([]serverapi.CargoItem, 0, len(items))
	for id, qty := range items {
		entries = append(entries, serverapi.CargoItem{ItemID: id, Quantity: qty})
	}
	b, err := json.Marshal(serverapi.ViewStorageResponse{Items: entries})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// completeMissionResultJSON builds the action_result payload complete_mission
// lands under the "complete_mission" raw-JSON key (wire-verified shape:
// {"command":"complete_mission","result":{"credits_earned":N,"mission_id":"..."},"tick":N}).
func completeMissionResultJSON(t *testing.T, missionID string, creditsEarned int64) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"command": "complete_mission",
		"tick":    1000,
		"result": serverapi.CompleteMissionResponse{
			MissionID:     missionID,
			CreditsEarned: creditsEarned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// marketJSON builds a full view_market payload from item-id -> total sell
// quantity (one synthetic sell order each, price 10).
func marketJSON(t *testing.T, sellQty map[string]float64) []byte {
	t.Helper()
	items := make([]serverapi.ViewMarketItem, 0, len(sellQty))
	for id, qty := range sellQty {
		items = append(items, serverapi.ViewMarketItem{
			ItemID:     id,
			SellOrders: []serverapi.MarketOrder{{PriceEach: 10, Quantity: qty}},
		})
	}
	b, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func missionState(docked bool, credits, cargoUsed float64) *game.State {
	return &game.State{
		System:     game.SystemData{ID: "haven", Name: "Haven"},
		CurrentPOI: "haven_station",
		Doc:        docked,
		Credits:    credits, // State.Credits is what GetCredits() returns (types.go:581)
		Ship:       game.Ship{CargoCapacity: 100, CargoUsed: cargoUsed},
	}
}

func missionDeps(fc *fakeClient, store *fakeMissionStore, kb *fakeKB) MissionDeps {
	return MissionDeps{
		Client: fc, KB: kb, Market: store, Out: io.Discard, AgentID: "engineer-1",
		nav:   func(ctx context.Context, system, poi string) error { return nil },
		sleep: func(ctx context.Context, d time.Duration) error { return nil },
	}
}

func TestMissionsHappyPath(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	// The board id ("m1") is template-ish; the server creates a fresh hex
	// active-mission instance on accept. activeMissionsSeq simulates that:
	// missionResume's pre-accept read sees nothing held, and the post-accept
	// id-resolution read sees the new instance (TemplateID == board id).
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		completeReward:    3000,
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions": boardJSON(t, entry),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	for _, want := range []string{"get_active_missions", "get_missions", "accept:m1", "buy:steel", "complete:hex-m1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("calls missing %q: %v", want, fc.calls)
		}
	}
	if strings.Contains(joined, "complete:m1 ") || strings.HasSuffix(joined, "complete:m1") {
		t.Fatalf("must complete using the resolved active id, not the board id: %v", fc.calls)
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 result row, got %d", len(store.results))
	}
	r := store.results[0]
	if r.Outcome != "completed" || r.MissionID != "m1" || r.CreditsEarned != 3000 || r.ItemCost != 400 {
		t.Fatalf("result mismatch: %+v", r)
	}
}

// TestMissionsRejectsStrongholdDestination covers the Dross Citadel incident: a
// live board entry targeted a pirate stronghold and a no-standing worker flying
// there was destroyed. The KB's is_stronghold flag must reject the candidate
// before accept — no accept call, no recorded row.
func TestMissionsRejectsStrongholdDestination(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	fc := &fakeClient{
		state: missionState(true, 5000, 0),
		raw: map[string][]byte{
			"missions":        boardJSON(t, entry),
			"active_missions": activeJSON(t),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "haven", Name: "Haven"},
			{ID: "sol", Name: "Sol", IsStronghold: true},
		},
		conns: []knowledge.Connection{{FromSystem: "haven", ToSystem: "sol"}},
	}
	if err := Missions(context.Background(), missionDeps(fc, store, kb)); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	for _, c := range fc.calls {
		if strings.HasPrefix(c, "accept:") {
			t.Fatalf("must not accept a mission whose destination is a pirate stronghold: %v", fc.calls)
		}
	}
	if len(store.results) != 0 {
		t.Fatalf("stronghold-rejected candidate must not be recorded: %+v", store.results)
	}
}

// TestMissionRouteClear covers missionRouteClear's guard cases: a clean path is
// clear, a stronghold waypoint blocks even a safe endpoint, and both a pathOf
// lookup failure and a degenerate from==to are treated as clear (never block on
// a graph gap).
func TestMissionRouteClear(t *testing.T) {
	// den is the stronghold waypoint.
	paths := map[[2]string][]string{
		{"haven", "sol"}:   {"haven", "sol"},
		{"haven", "krynn"}: {"haven", "den", "krynn"},
	}
	pathOf := func(from, to string, _ bool) (galaxy.Route, error) {
		if p, ok := paths[[2]string{from, to}]; ok {
			return galaxy.Route{Path: p, Hops: len(p) - 1}, nil
		}
		return galaxy.Route{}, errors.New("no path")
	}
	strongholds := map[string]bool{"den": true}

	tests := []struct {
		name      string
		from, to  string
		wantClear bool
	}{
		{"clear path", "haven", "sol", true},
		{"stronghold waypoint on path", "haven", "krynn", false},
		{"lookup error treated as clear", "haven", "unknown", true},
		{"same from/to treated as clear", "haven", "haven", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := missionRouteClear(pathOf, strongholds, tt.from, tt.to); got != tt.wantClear {
				t.Errorf("missionRouteClear(%s->%s) = %v, want %v", tt.from, tt.to, got, tt.wantClear)
			}
		})
	}
}

func TestMissionsNotDockedSkips(t *testing.T) {
	fc := &fakeClient{state: missionState(false, 5000, 0), raw: map[string][]byte{}}
	fc.state.CurrentPOI = "" // adrift in space, not at a station POI
	store := &fakeMissionStore{}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	for _, c := range fc.calls {
		if strings.HasPrefix(c, "accept:") {
			t.Fatalf("must not accept while not docked: %v", fc.calls)
		}
	}
}

func TestMissionsBuyFailureAbandons(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		buyErr:            context.DeadlineExceeded, // any error: the station ran dry
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions": boardJSON(t, entry),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	if !strings.Contains(joined, "abandon:hex-m1") {
		t.Fatalf("buy failure must abandon the resolved active mission id, not the board id: %v", fc.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "abandoned" {
		t.Fatalf("want 1 abandoned row, got %+v", store.results)
	}
	if store.results[0].MissionID != "m1" {
		t.Fatalf("recorded row must keep the board id for telemetry, got %+v", store.results[0])
	}
	if store.results[0].ItemCost != 0 {
		t.Fatalf("failed buy spent nothing; row must record ItemCost 0, got %+v", store.results[0])
	}
}

func TestMissionsDoesNotReacceptAttemptedMission(t *testing.T) {
	// A mission whose buy fails is abandoned+recorded; it must never be
	// re-selected on a later pass in the same session, and an all-abandoned
	// pass must still count as dry so reposition eventually fires.
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:  missionState(true, 5000, 0),
		buyErr: context.DeadlineExceeded,
		// pass 1: resume sees nothing held, accept resolves m1 -> hex-m1.
		// pass 2: m1 is already attempted, so it's filtered before any accept
		// call — only the resume read fires, and it sees nothing held (fake
		// doesn't simulate the abandon actually clearing the mission, but
		// nothing reads past index 1 again since the sequence is exhausted
		// onto its last entry).
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active), activeJSON(t)},
		raw: map[string][]byte{
			"missions": boardJSON(t, entry),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	deps := missionDeps(fc, store, missionKB())
	deps.State = &missionRunState{}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions (pass 1): %v", err)
	}
	if deps.State.dry != 1 {
		t.Fatalf("all-abandoned pass must count as dry: got dry=%d", deps.State.dry)
	}
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions (pass 2): %v", err)
	}
	if deps.State.dry != 2 {
		t.Fatalf("second dry pass must advance the streak: got dry=%d", deps.State.dry)
	}
	acceptCount := 0
	for _, c := range fc.calls {
		if c == "accept:m1" {
			acceptCount++
		}
	}
	if acceptCount != 1 {
		t.Fatalf("m1 must be accepted exactly once across sessions, got %d: %v", acceptCount, fc.calls)
	}
	if len(store.results) != 1 {
		t.Fatalf("want exactly 1 recorded row (no re-accept, no re-record), got %d", len(store.results))
	}
}

func TestMissionsDryPassesReposition(t *testing.T) {
	// Empty board every pass: the third consecutive dry pass must hop the
	// worker to the next nearby station instead of camping forever.
	fc := &fakeClient{state: missionState(true, 5000, 0), raw: map[string][]byte{
		"missions":        boardJSON(t),
		"active_missions": activeJSON(t),
	}}
	store := &fakeMissionStore{}
	deps := missionDeps(fc, store, missionKB())
	deps.State = &missionRunState{}
	deps.nearbyStations = func(ctx context.Context, limit int) ([]stationHop, error) {
		return []stationHop{{SystemID: "sol", POIID: "sol_station"}}, nil
	}
	var navTo []string
	deps.nav = func(ctx context.Context, system, poi string) error {
		navTo = append(navTo, system+"/"+poi)
		return nil
	}
	for range 3 {
		if err := Missions(context.Background(), deps); err != nil {
			t.Fatalf("Missions: %v", err)
		}
	}
	if len(navTo) != 1 || navTo[0] != "sol/sol_station" {
		t.Fatalf("3rd dry pass must reposition exactly once: %v", navTo)
	}
}

func TestMissionsResumesActiveDeliverable(t *testing.T) {
	active := serverapi.ActiveMission{
		MissionID: "held", Type: "delivery", Title: "Held delivery",
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "steel", Required: 10, Current: 0, SystemID: "sol", TargetBase: "sol_station"},
		},
	}
	fc := &fakeClient{
		state:          missionState(true, 5000, 10),
		completeReward: 2000,
		raw: map[string][]byte{
			"missions":        boardJSON(t),
			"active_missions": activeJSON(t, active),
		},
	}
	fc.state.Ship.Cargo = []game.CargoItem{{ItemID: "steel", Quantity: 10}}
	store := &fakeMissionStore{}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	if !strings.Contains(joined, "complete:held") {
		t.Fatalf("goods-aboard active mission must be completed: %v", fc.calls)
	}
	// SystemID is populated on the wire objective now; resume must nav
	// straight there and never fall back to FindRoute.
	if strings.Contains(joined, "find_route") {
		t.Fatalf("resume must not use the FindRoute fallback when SystemID is populated: %v", fc.calls)
	}
}

// TestMissionsAvailabilityGateSkipsUnacquirable covers the pre-accept gate:
// a mission whose cargo is neither in station storage nor on the local
// market must be skipped BEFORE accept_mission (accept-then-abandon churn
// pollutes telemetry and pays any abandon penalty), while an acquirable
// candidate on the same board proceeds normally.
func TestMissionsAvailabilityGateSkipsUnacquirable(t *testing.T) {
	rare := boardEntry("m_rare", "phase_matrix", 8, "haven_station2", "haven", 5500, 0)
	common := boardEntry("m_common", "steel", 20, "haven_station2", "haven", 3000, 0)
	active := serverapi.ActiveMission{
		MissionID: "hex_common", TemplateID: "m_common", Type: "delivery", Title: common.Title,
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "steel", Required: 20, SystemID: "haven", TargetBase: "haven_station2"},
		},
	}
	fc := &fakeClient{
		state: missionState(true, 5000, 0),
		raw: map[string][]byte{
			"missions": boardJSON(t, rare, common),
			"storage":  storageJSON(t, map[string]float64{}),
			"market":   marketJSON(t, map[string]float64{"steel": 100}), // no phase_matrix sold here
		},
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
	}
	store := &fakeMissionStore{asks: map[string]float64{"phase_matrix": 10, "steel": 10}}
	deps := missionDeps(fc, store, missionKB())
	deps.State = &missionRunState{}
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	if strings.Contains(joined, "accept:m_rare") {
		t.Fatalf("unacquirable mission must be skipped before accept: %v", fc.calls)
	}
	if !strings.Contains(joined, "accept:m_common") {
		t.Fatalf("acquirable mission must still be accepted: %v", fc.calls)
	}
	if !deps.State.wasAttempted("m_rare") {
		t.Fatal("unacquirable mission must be marked attempted for the session")
	}
	for _, r := range store.results {
		if r.MissionID == "m_rare" {
			t.Fatalf("gate-skipped mission must not produce a telemetry row: %+v", r)
		}
	}
}

// TestMissionsResumeClaimsCompletedObjective covers the storage-satisfied
// case found live 2026-07-16: the server counts storage AT the target base
// toward a deliver objective ("in_storage" on the actives wire), so an
// objective can be completed=true with zero cargo aboard. Resume must claim
// the reward (complete_mission at the target base), never skip it as
// "nothing to do" and never abandon it.
func TestMissionsResumeClaimsCompletedObjective(t *testing.T) {
	active := serverapi.ActiveMission{
		MissionID: "held", Type: "delivery", Title: "Supply Run: Iron Ore",
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "iron_ore", Required: 50, Current: 50, Completed: true, SystemID: "sol", TargetBase: "sol_station"},
		},
	}
	fc := &fakeClient{
		state:          missionState(true, 5000, 0), // hold empty: goods live in base storage
		completeReward: 2000,
		raw: map[string][]byte{
			"missions":        boardJSON(t),
			"active_missions": activeJSON(t, active),
		},
	}
	store := &fakeMissionStore{}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	if !strings.Contains(joined, "complete:held") {
		t.Fatalf("completed-objective active mission must be claimed: %v", fc.calls)
	}
	if strings.Contains(joined, "abandon:held") {
		t.Fatalf("completed-objective active mission must not be abandoned: %v", fc.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "completed" || store.results[0].CreditsEarned != 2000 {
		t.Fatalf("want 1 completed row with 2000 cr, got %+v", store.results)
	}
}

// TestMissionsRecoversWhenNotAtStation: a worker undocked in open space (empty
// CurrentPOI, e.g. after an interrupted jump) must fly back to a known public
// station in its system rather than idling forever (live-observed strand
// 2026-07-16 — the missionrunner role has no ensure_home behavior).
func TestMissionsRecoversWhenNotAtStation(t *testing.T) {
	fc := &fakeClient{
		state: missionState(false, 5000, 0),
		raw: map[string][]byte{
			"missions":        boardJSON(t),
			"active_missions": activeJSON(t),
		},
	}
	fc.state.CurrentPOI = "" // adrift in haven
	kb := missionKB()
	kb.pois = map[string][]knowledge.POI{
		"haven": {{ID: "haven_station", Type: "station", SystemID: "haven"}},
	}
	kb.bases = map[string]*knowledge.SpaceBase{
		"haven_station": {ID: "haven_base", POIID: "haven_station", PublicAccess: true},
	}
	var navTo string
	deps := missionDeps(fc, &fakeMissionStore{}, kb)
	deps.nav = func(ctx context.Context, system, poi string) error {
		navTo = system + "/" + poi
		fc.state.CurrentPOI = poi // autopilot arrival updates client state
		fc.state.Doc = true
		return nil
	}
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if navTo != "haven/haven_station" {
		t.Fatalf("adrift worker must recover to haven/haven_station, got %q", navTo)
	}
}

// TestMissionsRecoversFromNonStationPOI: a worker parked at a non-dockable
// POI (live-observed at an ice field after an interrupted transit) must fly
// to a known public station when dock is refused, not idle forever.
func TestMissionsRecoversFromNonStationPOI(t *testing.T) {
	fc := &fakeClient{
		state:   missionState(false, 5000, 0),
		dockErr: errors.New("No station at this location"),
		raw: map[string][]byte{
			"missions":        boardJSON(t),
			"active_missions": activeJSON(t),
		},
	}
	fc.state.CurrentPOI = "frostmarket_flats" // ice field in haven
	kb := missionKB()
	kb.pois = map[string][]knowledge.POI{
		"haven": {{ID: "haven_station", Type: "station", SystemID: "haven"}},
	}
	kb.bases = map[string]*knowledge.SpaceBase{
		"haven_station": {ID: "haven_base", POIID: "haven_station", PublicAccess: true},
	}
	var navTo string
	deps := missionDeps(fc, &fakeMissionStore{}, kb)
	deps.nav = func(ctx context.Context, system, poi string) error {
		navTo = system + "/" + poi
		fc.state.CurrentPOI = poi
		fc.state.Doc = true // autopilot arrival docks
		return nil
	}
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if navTo != "haven/haven_station" {
		t.Fatalf("dock-refused worker must recover to haven/haven_station, got %q", navTo)
	}
}

func TestMissionsResumeCargoShortAbandons(t *testing.T) {
	// Held mission needs 10 steel aboard to deliver; the ship only has 4 (lost
	// to a wreck, sold off, whatever) -> v1 abandons rather than trying to
	// complete an undeliverable mission.
	active := serverapi.ActiveMission{
		MissionID: "held", Type: "delivery", Title: "Held delivery",
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "steel", Required: 10, Current: 0, SystemID: "sol", TargetBase: "sol_station"},
		},
	}
	fc := &fakeClient{
		state: missionState(true, 5000, 4),
		raw: map[string][]byte{
			"missions":        boardJSON(t),
			"active_missions": activeJSON(t, active),
		},
	}
	fc.state.Ship.Cargo = []game.CargoItem{{ItemID: "steel", Quantity: 4}}
	store := &fakeMissionStore{}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if !strings.Contains(strings.Join(fc.calls, " "), "abandon:held") {
		t.Fatalf("cargo-short resume must abandon the held mission: %v", fc.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "abandoned" || store.results[0].MissionID != "held" {
		t.Fatalf("want 1 abandoned row for held, got %+v", store.results)
	}
}

// TestMissionsUnresolvedActiveIDDropsWithoutAbandon covers the case where
// AcceptMission succeeds but get_active_missions never shows a matching
// instance (server hiccup / naming drift): the candidate must be dropped
// silently — no buy, no abandon (there's no valid id to abandon with), no
// recorded row — and the pass still counts as dry.
func TestMissionsUnresolvedActiveIDDropsWithoutAbandon(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t)}, // never resolves
		raw: map[string][]byte{
			"missions": boardJSON(t, entry),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	deps := missionDeps(fc, store, missionKB())
	deps.State = &missionRunState{}
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	if strings.Contains(joined, "abandon:") {
		t.Fatalf("unresolved active id must not trigger an abandon call: %v", fc.calls)
	}
	if strings.Contains(joined, "buy:") {
		t.Fatalf("unresolved active id must not proceed to buy: %v", fc.calls)
	}
	if len(store.results) != 0 {
		t.Fatalf("unresolved candidate must not be recorded: %+v", store.results)
	}
	if deps.State.dry != 1 {
		t.Fatalf("an all-unresolved pass must count as dry, got dry=%d", deps.State.dry)
	}
}

// TestMissionsStorageFirstPartialStock covers Bug 2: a station pre-stocked
// with part of the required quantity (the engineer-1 smoke: 50 iron_ore
// sitting in storage while acquisition bought blind and failed
// item_not_available) must be withdrawn before buying the shortfall, and the
// recorded ItemCost must reflect only the bought units.
func TestMissionsStorageFirstPartialStock(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		completeReward:    3000,
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions": boardJSON(t, entry),
			"storage":  storageJSON(t, map[string]float64{"steel": 12}),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	if !strings.Contains(joined, "withdraw:steel:12") {
		t.Fatalf("must withdraw the available stock first: %v", fc.calls)
	}
	if !strings.Contains(joined, "buy:steel") {
		t.Fatalf("must buy the shortfall after withdrawing: %v", fc.calls)
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 result row, got %d", len(store.results))
	}
	if got := store.results[0].ItemCost; got != 160 {
		t.Fatalf("ItemCost must be prorated to the 8 bought units (8x20=160), got %v", got)
	}
}

// TestMissionsStorageFirstFullStockSkipsBuy covers the fully-covered case:
// no Buy call at all, ItemCost 0.
func TestMissionsStorageFirstFullStockSkipsBuy(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		completeReward:    3000,
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions": boardJSON(t, entry),
			"storage":  storageJSON(t, map[string]float64{"steel": 25}),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	if !strings.Contains(joined, "withdraw:steel:20") {
		t.Fatalf("must withdraw exactly BuyQty when storage fully covers it: %v", fc.calls)
	}
	if strings.Contains(joined, "buy:steel") {
		t.Fatalf("must not buy when storage fully covers the requirement: %v", fc.calls)
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 result row, got %d", len(store.results))
	}
	if got := store.results[0].ItemCost; got != 0 {
		t.Fatalf("ItemCost must be 0 when fully covered by storage, got %v", got)
	}
}

// TestMissionsCreditsFromActionResultOverridesDelta covers Bug 3: the
// complete_mission action_result's credits_earned must win over the wallet
// delta. completeReward (3000) simulates what the (broken) old delta
// measurement would have seen; the action_result reports a different amount
// (5000) so the two sources are unambiguous.
func TestMissionsCreditsFromActionResultOverridesDelta(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		completeReward:    3000,
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions":         boardJSON(t, entry),
			"complete_mission": completeMissionResultJSON(t, "hex-m1", 5000),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 result row, got %d", len(store.results))
	}
	if got := store.results[0].CreditsEarned; got != 5000 {
		t.Fatalf("CreditsEarned must come from the action_result (5000), not the wallet delta (3000); got %v", got)
	}
}

// TestMissionsCreditsFallsBackToDeltaWhenActionResultAbsent pins the
// fallback path explicitly (TestMissionsHappyPath also exercises it
// incidentally, since it sets no "complete_mission" raw key).
func TestMissionsCreditsFallsBackToDeltaWhenActionResultAbsent(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		completeReward:    3000,
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions": boardJSON(t, entry),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 result row, got %d", len(store.results))
	}
	if got := store.results[0].CreditsEarned; got != 3000 {
		t.Fatalf("must fall back to wallet delta when no action_result payload is present, got %v", got)
	}
}

// TestMissionsCreditsFallsBackOnStaleActionResult covers the staleness
// guard: a "complete_mission" raw entry left over from a different mission
// id must not be mistaken for this completion's reward.
func TestMissionsCreditsFallsBackOnStaleActionResult(t *testing.T) {
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	active := serverapi.ActiveMission{MissionID: "hex-m1", TemplateID: "m1", Type: "delivery", Title: "Deliver steel"}
	fc := &fakeClient{
		state:             missionState(true, 5000, 0),
		completeReward:    3000,
		activeMissionsSeq: [][]byte{activeJSON(t), activeJSON(t, active)},
		raw: map[string][]byte{
			"missions":         boardJSON(t, entry),
			"complete_mission": completeMissionResultJSON(t, "hex-other", 9999),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 result row, got %d", len(store.results))
	}
	if got := store.results[0].CreditsEarned; got != 3000 {
		t.Fatalf("a stale action_result (different mission id) must not be used; want delta 3000, got %v", got)
	}
}

// TestMissionStationPOI covers Bug 4's root cause directly:
// galaxy.NearestResult.POIs is only populated for resource lookups, never for
// POI-type ("station") lookups, so the reposition closure must resolve the
// station POI id itself from the KB instead of trusting that field.
func TestMissionStationPOI(t *testing.T) {
	kb := &fakeKB{
		pois: map[string][]knowledge.POI{
			"market_prime": {
				{ID: "mp_asteroid", Type: "asteroid", SystemID: "market_prime"},
				{ID: "mp_station", Type: "station", SystemID: "market_prime"},
			},
			"traders_rest": {
				{ID: "tr_station_private", Type: "station", SystemID: "traders_rest"},
			},
			"empty_system": {},
		},
		bases: map[string]*knowledge.SpaceBase{
			"mp_station":         {ID: "mp_base", POIID: "mp_station", PublicAccess: true},
			"tr_station_private": {ID: "tr_base", POIID: "tr_station_private", PublicAccess: false},
		},
	}

	t.Run("finds the public station POI", func(t *testing.T) {
		id, err := missionStationPOI(context.Background(), kb, "market_prime")
		if err != nil || id != "mp_station" {
			t.Fatalf("want mp_station, got %q err=%v", id, err)
		}
	})

	t.Run("skips a non-public station", func(t *testing.T) {
		id, err := missionStationPOI(context.Background(), kb, "traders_rest")
		if err != nil || id != "" {
			t.Fatalf("want empty (no public station), got %q err=%v", id, err)
		}
	})

	t.Run("no POIs at all", func(t *testing.T) {
		id, err := missionStationPOI(context.Background(), kb, "empty_system")
		if err != nil || id != "" {
			t.Fatalf("want empty, got %q err=%v", id, err)
		}
	})
}

// TestMissionsDefaultRepositionFindsRealStationPOI is the end-to-end
// regression for Bug 4 ("reposition lookup failed (<nil>, 0 targets); idling"
// on every dry pass): it exercises the PRODUCTION nearbyStations closure
// (deps.nearbyStations left nil) against a KB with real station POIs/bases,
// proving the fix actually wires the closure to a working hop, not just that
// missionStationPOI works in isolation.
func TestMissionsDefaultRepositionFindsRealStationPOI(t *testing.T) {
	kb := &fakeKB{
		systems: []knowledge.System{{ID: "haven", Name: "Haven"}, {ID: "sol", Name: "Sol"}},
		conns: []knowledge.Connection{
			{FromSystem: "haven", ToSystem: "sol"},
			{FromSystem: "sol", ToSystem: "haven"},
		},
		pois: map[string][]knowledge.POI{
			"haven": {{ID: "haven_station", Type: "station", SystemID: "haven"}},
			"sol":   {{ID: "sol_station", Type: "station", SystemID: "sol"}},
		},
		bases: map[string]*knowledge.SpaceBase{
			"haven_station": {ID: "haven_base", POIID: "haven_station", PublicAccess: true},
			"sol_station":   {ID: "sol_base", POIID: "sol_station", PublicAccess: true},
		},
	}
	fc := &fakeClient{
		state: missionState(true, 5000, 0),
		raw: map[string][]byte{
			"missions":        boardJSON(t),
			"active_missions": activeJSON(t),
		},
	}
	store := &fakeMissionStore{}
	deps := missionDeps(fc, store, kb)
	deps.State = &missionRunState{}
	var navTo []string
	deps.nav = func(ctx context.Context, system, poi string) error {
		navTo = append(navTo, system+"/"+poi)
		return nil
	}
	// deps.nearbyStations is intentionally left nil to exercise the real closure.
	for range 3 {
		if err := Missions(context.Background(), deps); err != nil {
			t.Fatalf("Missions: %v", err)
		}
	}
	if len(navTo) != 1 || navTo[0] != "sol/sol_station" {
		t.Fatalf("3rd dry pass must reposition to the real station POI, got %v", navTo)
	}
}
