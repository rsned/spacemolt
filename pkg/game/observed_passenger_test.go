package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func capturePassengerObserver(t *testing.T, c *Client) *[]ObservedPassenger {
	t.Helper()
	var captured []ObservedPassenger
	c.SetPassengerObserver(func(obs []ObservedPassenger) {
		captured = append(captured, obs...)
	})
	return &captured
}

func TestSetPassengerObserver_StoresCallback(t *testing.T) {
	c := &Client{}
	if c.PassengerObserver() != nil {
		t.Fatal("expected nil observer before registration")
	}
	c.SetPassengerObserver(func(_ []ObservedPassenger) {})
	if c.PassengerObserver() == nil {
		t.Fatal("passengerObserver not registered")
	}
}

func TestNotifyPassengers_StampsAndDropsEmptyID(t *testing.T) {
	c := &Client{}
	got := capturePassengerObserver(t, c)

	c.notifyPassengers("list_station_passengers", []serverapi.PassengerRecord{
		{CitizenID: "ziggy_stardrift", Name: "Ziggy Stardrift", Citizenship: "nebula", Bio: "Glam legend.", Class: "first"},
		{CitizenID: "", Name: "Anon"}, // dropped
	})

	if len(*got) != 1 {
		t.Fatalf("got %d observations, want 1 (empty-id dropped)", len(*got))
	}
	o := (*got)[0]
	if o.CitizenID != "ziggy_stardrift" || o.Citizenship != "nebula" || o.Class != "first" {
		t.Errorf("unexpected record: %+v", o)
	}
	if o.Source != "list_station_passengers" {
		t.Errorf("Source=%q, want list_station_passengers", o.Source)
	}
	if o.SeenAt.IsZero() {
		t.Error("SeenAt is zero")
	}
}

func TestNotifyPassengers_NoObserverIsNoOp(t *testing.T) {
	c := &Client{}
	// Should not panic.
	c.notifyPassengers("list_station_passengers", []serverapi.PassengerRecord{{CitizenID: "x"}})
}

func TestHandleResponse_FiresOnStationPassengers(t *testing.T) {
	c := newHandleResponseTestClient("sys-A")
	got := capturePassengerObserver(t, c)

	payload := payloadMarshal(t, map[string]any{
		"count":   2,
		"station": "Grand Exchange Station",
		"waiting": []serverapi.PassengerRecord{
			{CitizenID: "mabani_perranda", Name: "Mabani Perranda", Citizenship: "nebula", Bio: "Senior partner.", Class: "first"},
			{CitizenID: "ziggy_stardrift", Name: "Ziggy Stardrift", Citizenship: "nebula", Bio: "Glam legend.", Class: "first"},
		},
	})
	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: payload})

	if len(*got) != 2 {
		t.Fatalf("observer got %d, want 2", len(*got))
	}
	if (*got)[0].Source != "list_station_passengers" {
		t.Errorf("Source=%q, want list_station_passengers", (*got)[0].Source)
	}
	if (*got)[0].Citizenship != "nebula" {
		t.Errorf("Citizenship=%q, want nebula", (*got)[0].Citizenship)
	}
}

func TestHandleResponse_FiresOnDockArrivals(t *testing.T) {
	c := newHandleResponseTestClient("sys-A")
	got := capturePassengerObserver(t, c)

	// dock arrives as a type=ok frame; passenger_arrivals.delivered carries the
	// disembarking passengers (bio present, citizenship absent).
	payload := payloadMarshal(t, map[string]any{
		"action": "dock",
		"base":   "The Levy Customs Station",
		"passenger_arrivals": map[string]any{
			"delivered": []serverapi.PassengerRecord{
				{CitizenID: "lin_mantari", Name: "Lin Mantari", Bio: "A clock restorer.", Class: "first"},
			},
			"fare_collected": 6545,
		},
	})
	c.handleResponse(protocol.Response{Type: protocol.TypeOK, Payload: payload})

	if len(*got) != 1 {
		t.Fatalf("observer got %d, want 1", len(*got))
	}
	o := (*got)[0]
	if o.CitizenID != "lin_mantari" || o.Source != "dock" {
		t.Errorf("unexpected dock arrival: %+v", o)
	}
}
