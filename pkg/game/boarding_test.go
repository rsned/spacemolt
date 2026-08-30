package game

import (
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// v0.572.0: ships carry crew and marines. The live get_ship shape (captured
// from the operator's Survey Vessel on 2026-08-30) nests them under
// ship.personnel with the capacities beside it; both must reach State.Ship.
func TestParseShipData_DecodesPersonnel(t *testing.T) {
	c := newHandleResponseTestClient("sol")
	raw := `{"cargo_max":305,"cargo_used":0,"ship":{"id":"74ae","class_id":"survey_vessel",
		"hull":340,"max_hull":340,"fuel":495,"max_fuel":1020,"cargo_capacity":305,
		"crew_capacity":60,"marine_capacity":6,
		"personnel":{"fit_crew":58,"fit_marines":6,"injured_crew":2,"injured_marines":0,"version":1},
		"modules":[],"cargo":[]}}`
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: payload})

	ship := c.GetState().Ship
	if ship.CrewCapacity != 60 || ship.MarineCapacity != 6 {
		t.Errorf("capacities = crew %d marines %d, want 60/6", ship.CrewCapacity, ship.MarineCapacity)
	}
	if ship.Personnel.FitCrew != 58 || ship.Personnel.InjuredCrew != 2 || ship.Personnel.FitMarines != 6 {
		t.Errorf("personnel = %+v", ship.Personnel)
	}
}

// personnel_update is the post-commit push after an ally treats or transfers
// crew to us; it carries our ship's complete personnel block, which replaces
// whatever we had — but only for the ship we are flying.
func TestHandleResponse_PersonnelUpdateAppliesToCurrentShip(t *testing.T) {
	c := newHandleResponseTestClient("sol")
	c.state.Ship.ID = "ship-a"
	c.state.Ship.Personnel = Personnel{FitCrew: 50, InjuredCrew: 10}

	c.handleResponse(protocol.Response{Type: protocol.TypePersonnelUpdate, Payload: payloadMarshal(t, map[string]any{
		"action": "treat_personnel", "ship_id": "ship-b", "crew_treated": 10,
		"personnel": map[string]any{"fit_crew": 60, "injured_crew": 0},
	})})
	if got := c.GetState().Ship.Personnel; got.FitCrew != 50 {
		t.Errorf("update for another ship applied: %+v", got)
	}

	c.handleResponse(protocol.Response{Type: protocol.TypePersonnelUpdate, Payload: payloadMarshal(t, map[string]any{
		"action": "treat_personnel", "ship_id": "ship-a", "crew_treated": 10,
		"personnel": map[string]any{"fit_crew": 60, "injured_crew": 0},
	})})
	if got := c.GetState().Ship.Personnel; got.FitCrew != 60 || got.InjuredCrew != 0 {
		t.Errorf("personnel after update = %+v, want 60 fit / 0 injured", got)
	}
}

// ship_captured is the terminal boarding event. It lands in State for the
// pilot and goes out to the capture observer stamped with tick and observer,
// so the KB can record the new loss mode.
func TestHandleResponse_ShipCapturedRecordsAndNotifies(t *testing.T) {
	c := newHandleResponseTestClient("zaniah")
	c.state.Player.ID = "hauler-7"
	var got []ObservedCapture
	c.SetCaptureObserver(func(obs ObservedCapture) { got = append(got, obs) })

	c.handleResponse(protocol.Response{Type: protocol.TypeShipCaptured, Payload: payloadMarshal(t, map[string]any{
		"battle_id": "b1", "tick": 1800000, "boarding_operation_id": "op1",
		"captor_id": "molten", "captor_username": "MoltenOne",
		"former_owner_id": "hauler-7", "former_owner_username": "hauler-7",
		"ship_id": "ship7", "ship_class": "congregation",
	})})

	st := c.GetState()
	if st.LastCapture.BoardingOperationID != "op1" || st.LastCapture.CaptorID != "molten" || st.LastCapture.ShipClass != "congregation" {
		t.Errorf("LastCapture = %+v", st.LastCapture)
	}
	if len(got) != 1 {
		t.Fatalf("capture observer got %d, want 1", len(got))
	}
	if got[0].ObserverID != "hauler-7" || got[0].SystemID != "zaniah" || got[0].Capture.Tick != 1800000 {
		t.Errorf("observed capture = %+v", got[0])
	}
}

func TestHandleResponse_PrizeUpdateRecorded(t *testing.T) {
	c := newHandleResponseTestClient("sol")
	c.handleResponse(protocol.Response{Type: protocol.TypePrizeUpdate, Payload: payloadMarshal(t, map[string]any{
		"prize_id": "pz1", "ship_id": "ship7", "ship_class": "congregation", "status": "delivered",
		"destination_base_id": "haven_station", "message": "Prize delivered.",
	})})
	if got := c.GetState().LastPrizeUpdate; got.PrizeID != "pz1" || got.Status != "delivered" || got.DestinationBaseID != "haven_station" {
		t.Errorf("LastPrizeUpdate = %+v", got)
	}
}

// get_nearby now lists intact prizes beside players. They go to their own
// observer with the same place/tick/observer stamp the player sightings get.
func TestNotifyPrizes_FromGetNearby(t *testing.T) {
	c := newHandleResponseTestClient("zaniah")
	c.state.CurrentTick = 1800010
	c.state.Player.ID = "mb_zaniah"
	var got []ObservedPrize
	c.SetPrizeObserver(func(obs []ObservedPrize) { got = append(got, obs...) })

	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: payloadMarshal(t, map[string]any{
		"nearby": []serverapi.NearbyPlayer{}, "count": 0, "poi_id": "zaniah_gate",
		"prize_count": 1,
		"prizes": []map[string]any{{
			"prize_id": "pz1", "ship_id": "ship7", "ship_class": "congregation", "ship_name": "Old Faithful",
			"actor_id": "molten", "status": "in_transit", "hull": 100, "max_hull": 400, "in_combat": false,
		}},
	})})

	if len(got) != 1 {
		t.Fatalf("prize observer got %d, want 1", len(got))
	}
	p := got[0]
	if p.PrizeID != "pz1" || p.ActorID != "molten" || p.Status != "in_transit" || p.Hull != 100 ||
		p.SystemID != "zaniah" || p.POIID != "zaniah_gate" || p.Tick != 1800010 || p.ObserverID != "mb_zaniah" ||
		p.Source != "get_nearby" {
		t.Errorf("observed prize = %+v", p)
	}
}

// battle_update participants now say what kind of combatant they are; a
// moving intact prize is kind=prize, is_npc=true. boarding[] rides beside.
func TestBattleUpdate_DecodesKindAndBoarding(t *testing.T) {
	raw := `{"battle_id":"b1","tick":5,"participants":[
		{"player_id":"pz1","username":"","side_id":2,"zone":"close","kind":"prize","is_npc":true,"hull_pct":25},
		{"player_id":"molten","username":"MoltenOne","side_id":2,"kind":"player","stance":"board"}],
		"boarding":[{"operation_id":"op1","attacker_id":"molten","target_id":"ship7","phase":"assault","progress":"contested","self_destruct_countdown":3}]}`
	var ev serverapi.BattleUpdate
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Participants[0].Kind != "prize" || !ev.Participants[0].IsNPC || ev.Participants[1].Kind != "player" {
		t.Errorf("participants = %+v", ev.Participants)
	}
	if len(ev.Boarding) != 1 || ev.Boarding[0].Phase != "assault" || ev.Boarding[0].SelfDestructCountdown != 3 {
		t.Errorf("boarding = %+v", ev.Boarding)
	}
}

// Kill log entries now carry a cause, and battle summaries list captures.
func TestBattleLog_DecodesCauseAndCaptures(t *testing.T) {
	var kill serverapi.KillLogEntry
	if err := json.Unmarshal([]byte(`{"killer_id":"a","victim_id":"b","cause":"self_destruct"}`), &kill); err != nil {
		t.Fatal(err)
	}
	if kill.Cause != "self_destruct" {
		t.Errorf("Cause = %q", kill.Cause)
	}
	var sum serverapi.BattleSummaryResponse
	if err := json.Unmarshal([]byte(`{"battle_id":"b1","ships_captured":1,"captures":[{"boarding_operation_id":"op1","captor_id":"molten","ship_class":"congregation"}]}`), &sum); err != nil {
		t.Fatal(err)
	}
	if sum.ShipsCaptured != 1 || len(sum.Captures) != 1 || sum.Captures[0].CaptorID != "molten" {
		t.Errorf("summary = %+v", sum)
	}
}
