package agent

import (
	"context"
	"log"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// WireBoardingObservers registers prize and capture observers on the game
// client that persist v0.572.0 boarding data — intact prizes seen in
// get_nearby and every ship_captured push — into the knowledge base. A KB
// that cannot record boarding (in-memory, mocks) leaves the client unwired.
// Write errors are logged and swallowed: a failed write must never break the
// response path.
func WireBoardingObservers(c *game.Client, kb knowledge.Base) {
	if c == nil || kb == nil {
		return
	}
	rec, ok := kb.(knowledge.BoardingRecorder)
	if !ok {
		return
	}
	c.SetCaptureObserver(func(o game.ObservedCapture) {
		row := knowledge.ShipCapture{
			BattleID:            o.Capture.BattleID,
			Tick:                o.Capture.Tick,
			BoardingOperationID: o.Capture.BoardingOperationID,
			CaptorID:            o.Capture.CaptorID,
			CaptorUsername:      o.Capture.CaptorUsername,
			FormerOwnerID:       o.Capture.FormerOwnerID,
			FormerOwnerUsername: o.Capture.FormerOwnerUsername,
			ShipID:              o.Capture.ShipID,
			ShipClass:           o.Capture.ShipClass,
			ObserverID:          o.ObserverID,
			SeenAt:              o.SeenAt,
		}
		if err := rec.RecordShipCaptures(context.Background(), []knowledge.ShipCapture{row}); err != nil {
			log.Printf("[boarding] RecordShipCaptures: %v", err)
		}
	})
	c.SetPrizeObserver(func(obs []game.ObservedPrize) {
		if len(obs) == 0 {
			return
		}
		rows := make([]knowledge.SeenPrize, 0, len(obs))
		for _, o := range obs {
			rows = append(rows, knowledge.SeenPrize{
				PrizeID: o.PrizeID, ShipID: o.ShipID, ShipClass: o.ShipClass, ShipName: o.ShipName,
				ActorID: o.ActorID, Status: o.Status, WaitReason: o.WaitReason,
				Hull: o.Hull, MaxHull: o.MaxHull, Shield: o.Shield, MaxShield: o.MaxShield,
				InCombat: o.InCombat,
				SystemID: o.SystemID, POIID: o.POIID, Source: o.Source,
				Tick: o.Tick, ObserverID: o.ObserverID, SeenAt: o.SeenAt,
			})
		}
		if err := rec.RecordPrizeSightings(context.Background(), rows); err != nil {
			log.Printf("[boarding] RecordPrizeSightings: %v", err)
		}
	})
}
