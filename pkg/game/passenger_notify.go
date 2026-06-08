package game

import (
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// notifyPassengersFromPayload scans a response payload for any of the
// passenger-bearing shapes and dispatches the identity records they carry to
// the registered observer. Additive and source-tagged:
//
//	list_station_passengers → "waiting"      (richest: empire + bio)
//	list_passengers         → "passengers"   (aboard manifest)
//	load_passenger          → "loaded"
//	dock                    → "passenger_arrivals".delivered
//
// It cheaply short-circuits when no observer is registered.
func (c *Client) notifyPassengersFromPayload(payload map[string]any) {
	c.passengerObserverMu.RLock()
	cb := c.passengerObserver
	c.passengerObserverMu.RUnlock()
	if cb == nil {
		return
	}

	var pax []serverapi.PassengerRecord
	if unmarshalPayloadKey(payload, "waiting", &pax) {
		c.notifyPassengers("list_station_passengers", pax)
	}
	if unmarshalPayloadKey(payload, "passengers", &pax) {
		c.notifyPassengers("list_passengers", pax)
	}
	if unmarshalPayloadKey(payload, "loaded", &pax) {
		c.notifyPassengers("load_passenger", pax)
	}
	var arrivals struct {
		Delivered []serverapi.PassengerRecord `json:"delivered"`
	}
	if unmarshalPayloadKey(payload, "passenger_arrivals", &arrivals) {
		c.notifyPassengers("dock", arrivals.Delivered)
	}
}

// notifyPassengers builds ObservedPassenger records from a PassengerRecord
// slice, stamps source/time, and dispatches to the registered observer. Silent
// no-op when no observer is registered or the slice is empty.
func (c *Client) notifyPassengers(source string, pax []serverapi.PassengerRecord) {
	if len(pax) == 0 {
		return
	}
	c.passengerObserverMu.RLock()
	cb := c.passengerObserver
	c.passengerObserverMu.RUnlock()
	if cb == nil {
		return
	}

	now := time.Now().UTC()
	out := make([]ObservedPassenger, 0, len(pax))
	for _, p := range pax {
		if p.CitizenID == "" {
			continue
		}
		out = append(out, ObservedPassenger{
			CitizenID:   p.CitizenID,
			Name:        p.Name,
			Citizenship: p.Citizenship,
			Bio:         p.Bio,
			Class:       p.Class,
			Source:      source,
			SeenAt:      now,
		})
	}
	if len(out) == 0 {
		return
	}
	cb(out)
}
