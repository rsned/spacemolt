package game

import "time"

// ObservedPlayer is a single player record extracted from a server
// response or push event. It is the input to a PlayerObserver callback.
//
// Fields ShipClass, POIID may be empty when the source doesn't carry
// them (e.g. chat_message has no ship/POI). SystemID is empty when the
// observation has no spatial context (chat) — the recorder uses that to
// decide whether to write a sightings row.
type ObservedPlayer struct {
	PlayerID       string
	Username       string
	ShipClass      string
	FactionID      string
	FactionTag     string
	ClanTag        string
	PrimaryColor   string
	SecondaryColor string
	StatusMessage  string
	Anonymous      bool
	InCombat       bool

	SystemID string
	POIID    string
	Source   string
	SeenAt   time.Time
}

// PlayerObserver receives batches of player observations as the game
// client parses incoming server messages. Implementations must not
// block — the callback is invoked from the response-handling goroutine.
type PlayerObserver func(obs []ObservedPlayer)
