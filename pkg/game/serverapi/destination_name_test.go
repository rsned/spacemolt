package serverapi

import (
	"encoding/json"
	"testing"
)

// v0.686.0 pending travel/jump replies name the destination.
func TestTravelAndJump_DecodeDestinationName(t *testing.T) {
	var tr TravelResponse
	if err := json.Unmarshal([]byte(`{"action":"travel","arrival_tick":2370410,"destination":"first_step_memorial_station","destination_name":"First Step Memorial Station"}`), &tr); err != nil {
		t.Fatal(err)
	}
	if tr.DestinationName != "First Step Memorial Station" {
		t.Fatalf("travel: %+v", tr)
	}
	var jr JumpResponse
	if err := json.Unmarshal([]byte(`{"action":"jump","destination":"sol","destination_name":"Sol"}`), &jr); err != nil {
		t.Fatal(err)
	}
	if jr.DestinationName != "Sol" {
		t.Fatalf("jump: %+v", jr)
	}
}
