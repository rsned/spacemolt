package game

import "time"

// ObservedPassenger is a single passenger (citizen) record extracted from a
// passenger-bearing server response. It is the input to a PassengerObserver
// callback and the galaxy-wide passenger catalog.
//
// Citizenship and Bio may be empty when the source doesn't carry them (e.g. an
// aboard manifest lacks citizenship); the recorder COALESCE-merges so a sparse
// source never wipes data already captured from a richer one.
type ObservedPassenger struct {
	CitizenID   string
	Name        string
	Citizenship string
	Bio         string
	Class       string

	Source string
	SeenAt time.Time
}

// PassengerObserver receives batches of passenger observations as the game
// client parses incoming server messages. Implementations must not block — the
// callback is invoked from the response-handling goroutine.
type PassengerObserver func(obs []ObservedPassenger)

// PassengerObserver returns the currently registered observer (nil if none).
// Exposed for tests and runtime introspection to verify wiring without
// reaching into unexported fields.
func (c *Client) PassengerObserver() PassengerObserver {
	c.passengerObserverMu.RLock()
	defer c.passengerObserverMu.RUnlock()
	return c.passengerObserver
}
