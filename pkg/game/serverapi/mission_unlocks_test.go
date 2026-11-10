package serverapi

import (
	"encoding/json"
	"testing"
)

// Shapes from openapi v0.685.0: MissionUnlock / MissionPrerequisite /
// MissionPointer / LockedMissionHint.
func TestCompleteMission_DecodesUnlocks(t *testing.T) {
	raw := `{"title":"Processing the Data","unlocks":[{"available":false,
"mission":{"mission_id":"joining_the_investigators","title":"Joining the Investigators","base_id":"first_step_memorial_station","base_name":"First Step Memorial Station","system_id":"first_step","system_name":"First Step"},
"still_requires":[{"mission":{"mission_id":"the_listening_foundry","title":"The Listening Foundry"},"next_step":{"mission_id":"signal_triage","title":"Signal Triage","base_id":"b1","base_name":"B One","system_id":"s1","system_name":"S One"}}]},
{"available":true,"mission":{"mission_id":"m2","title":"Two"}}]}`
	var r CompleteMissionResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Unlocks) != 2 || r.Unlocks[0].Mission.MissionID != "joining_the_investigators" || r.Unlocks[0].Available ||
		r.Unlocks[0].Mission.BaseName != "First Step Memorial Station" || !r.Unlocks[1].Available {
		t.Fatalf("unlocks decoded wrong: %+v", r.Unlocks)
	}
	req := r.Unlocks[0].StillRequires
	if len(req) != 1 || req[0].Mission.MissionID != "the_listening_foundry" || req[0].NextStep.MissionID != "signal_triage" || req[0].NextStep.SystemName != "S One" {
		t.Fatalf("still_requires decoded wrong: %+v", req)
	}
}

func TestGetMissions_DecodesLockedMissions(t *testing.T) {
	raw := `{"missions":[],"locked_missions":[{"still_requires":[{"mission":{"mission_id":"a","title":"A"},"next_step":{"mission_id":"a0","title":"A0"}}]}]}`
	var r GetMissionsResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.LockedMissions) != 1 || r.LockedMissions[0].StillRequires[0].NextStep.MissionID != "a0" {
		t.Fatalf("locked_missions decoded wrong: %+v", r.LockedMissions)
	}
}

func TestViewCompletedMission_DecodesUnlocks(t *testing.T) {
	var r ViewCompletedMissionResponse
	if err := json.Unmarshal([]byte(`{"template_id":"x","unlocks":[{"available":true,"mission":{"mission_id":"y","title":"Y"}}]}`), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Unlocks) != 1 || r.Unlocks[0].Mission.MissionID != "y" {
		t.Fatalf("got %+v", r.Unlocks)
	}
}

func TestStationConfig_DecodesPlayerUsernames(t *testing.T) {
	var r StationConfigResponse
	if err := json.Unmarshal([]byte(`{"allowed_players":["p1"],"player_usernames":{"p1":"Alice"}}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.PlayerUsernames["p1"] != "Alice" {
		t.Fatalf("got %+v", r.PlayerUsernames)
	}
}
