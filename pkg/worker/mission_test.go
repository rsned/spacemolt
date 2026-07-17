package worker

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

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
	fc := &fakeClient{
		state:          missionState(true, 5000, 0),
		completeReward: 3000,
		raw: map[string][]byte{
			"missions":        boardJSON(t, entry),
			"active_missions": activeJSON(t),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	for _, want := range []string{"get_active_missions", "get_missions", "accept:m1", "buy:steel", "complete:m1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("calls missing %q: %v", want, fc.calls)
		}
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 result row, got %d", len(store.results))
	}
	r := store.results[0]
	if r.Outcome != "completed" || r.MissionID != "m1" || r.CreditsEarned != 3000 || r.ItemCost != 400 {
		t.Fatalf("result mismatch: %+v", r)
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
	fc := &fakeClient{
		state:  missionState(true, 5000, 0),
		buyErr: context.DeadlineExceeded, // any error: the station ran dry
		raw: map[string][]byte{
			"missions":        boardJSON(t, entry),
			"active_missions": activeJSON(t),
		},
	}
	store := &fakeMissionStore{asks: map[string]float64{"steel": 20}}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	joined := strings.Join(fc.calls, " ")
	if !strings.Contains(joined, "abandon:m1") {
		t.Fatalf("buy failure must abandon the accepted mission: %v", fc.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "abandoned" {
		t.Fatalf("want 1 abandoned row, got %+v", store.results)
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
	fc := &fakeClient{
		state:  missionState(true, 5000, 0),
		buyErr: context.DeadlineExceeded,
		raw: map[string][]byte{
			"missions":        boardJSON(t, entry),
			"active_missions": activeJSON(t),
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
		Requirements: &serverapi.MissionRequirements{
			DeliverItemID: "steel", DeliverQuantity: 10, DeliverToBaseID: "sol_station",
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
	if !strings.Contains(strings.Join(fc.calls, " "), "complete:held") {
		t.Fatalf("goods-aboard active mission must be completed: %v", fc.calls)
	}
}

func TestMissionsResumeCargoShortAbandons(t *testing.T) {
	// Held mission needs 10 steel aboard to deliver; the ship only has 4 (lost
	// to a wreck, sold off, whatever) -> v1 abandons rather than trying to
	// complete an undeliverable mission.
	active := serverapi.ActiveMission{
		MissionID: "held", Type: "delivery", Title: "Held delivery",
		Requirements: &serverapi.MissionRequirements{
			DeliverItemID: "steel", DeliverQuantity: 10, DeliverToBaseID: "sol_station",
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
