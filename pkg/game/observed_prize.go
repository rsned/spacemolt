package game

import (
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ObservedPrize is one sighting of an intact captured ship (server v0.572.0),
// stamped with where, when (game tick), and which of our agents saw it. It is
// the input to a PrizeObserver callback and the seen_prize_events timeline.
type ObservedPrize struct {
	PrizeID    string
	ShipID     string
	ShipClass  string
	ShipName   string
	ActorID    string
	Status     string
	WaitReason string
	Hull       int
	MaxHull    int
	Shield     int
	MaxShield  int
	InCombat   bool

	SystemID   string
	POIID      string
	Source     string
	SeenAt     time.Time
	Tick       int64
	ObserverID string
}

// ObservedCapture is a ship_captured push plus the observer stamp: the system
// we were in and our own player id when it arrived. The push carries its own
// tick.
type ObservedCapture struct {
	Capture    serverapi.ShipCaptured
	SystemID   string
	ObserverID string
	SeenAt     time.Time
}

// PrizeObserver receives batches of prize sightings as the client parses
// incoming server messages. Must not block — invoked on the response goroutine.
type PrizeObserver func(obs []ObservedPrize)

// CaptureObserver receives each ship_captured push. Must not block.
type CaptureObserver func(obs ObservedCapture)

// PrizeObserver returns the registered prize observer (nil if none).
func (c *Client) PrizeObserver() PrizeObserver {
	c.boardingObserverMu.RLock()
	defer c.boardingObserverMu.RUnlock()
	return c.prizeObserver
}

// CaptureObserver returns the registered capture observer (nil if none).
func (c *Client) CaptureObserver() CaptureObserver {
	c.boardingObserverMu.RLock()
	defer c.boardingObserverMu.RUnlock()
	return c.captureObserver
}

// notifyPrizes stamps a get_nearby prize list with place/tick/observer and
// hands it to the prize observer. Silent no-op without an observer.
func (c *Client) notifyPrizes(source string, prizes []serverapi.NearbyPrize, poiID string) {
	if len(prizes) == 0 {
		return
	}
	cb := c.PrizeObserver()
	if cb == nil {
		return
	}
	systemID, tick, observer := c.observerStamp()
	now := time.Now().UTC()
	out := make([]ObservedPrize, 0, len(prizes))
	for _, p := range prizes {
		out = append(out, ObservedPrize{
			PrizeID: p.PrizeID, ShipID: p.ShipID, ShipClass: p.ShipClass, ShipName: p.ShipName,
			ActorID: p.ActorID, Status: p.Status, WaitReason: p.WaitReason,
			Hull: p.Hull, MaxHull: p.MaxHull, Shield: p.Shield, MaxShield: p.MaxShield,
			InCombat: p.InCombat,
			SystemID: systemID, POIID: poiID, Source: source, SeenAt: now,
			Tick: tick, ObserverID: observer,
		})
	}
	cb(out)
}

// notifyCapture hands a ship_captured push to the capture observer with the
// observer stamp. Silent no-op without an observer.
func (c *Client) notifyCapture(ev serverapi.ShipCaptured) {
	cb := c.CaptureObserver()
	if cb == nil {
		return
	}
	systemID, _, observer := c.observerStamp()
	cb(ObservedCapture{Capture: ev, SystemID: systemID, ObserverID: observer, SeenAt: time.Now().UTC()})
}
