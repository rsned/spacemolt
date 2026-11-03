package agent

import (
	"context"
	"log"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// WireShipyardObserver registers a shipyard observer on the game client that
// persists commission quotes, commissions and license purchases into the
// knowledge base, stamped with agentID. A KB that cannot record shipyard data
// leaves the client unwired. Write errors are logged and swallowed so a
// failed write never breaks the response path.
func WireShipyardObserver(c *game.Client, kb knowledge.Base, agentID string) {
	if c == nil || kb == nil {
		return
	}
	rec, ok := kb.(knowledge.ShipyardRecorder)
	if !ok {
		return
	}
	c.SetShipyardObserver(func(o game.ObservedShipyard) {
		ctx := context.Background()
		switch o.Kind {
		case "quote", "commission":
			err := rec.RecordShipyardQuote(ctx, knowledge.ShipyardQuote{
				ShipClass: o.ShipClass, StationID: o.BaseID, FactionID: o.FactionID, AgentID: agentID,
				Mode: o.Kind, Labor: o.Labor, MaterialCost: o.MaterialCost,
				ProvideMaterialsTotal: o.ProvideMaterialsTotal, YardMargin: o.YardMargin,
				ShipyardTierHere: o.ShipyardTierHere, Royalty: o.Royalty,
				Tick: o.Tick, SeenAt: o.SeenAt, Raw: o.Raw,
			})
			if err != nil {
				log.Printf("[shipyard] RecordShipyardQuote: %v", err)
			}
		case "license":
			err := rec.RecordShipLicense(ctx, knowledge.ShipLicense{
				FactionID: o.FactionID, ShipClass: o.ShipClass, Empire: o.Empire, AgentID: agentID,
				CostPaid: o.CostPaid, Tick: o.Tick, SeenAt: o.SeenAt,
			})
			if err != nil {
				log.Printf("[shipyard] RecordShipLicense: %v", err)
			}
		}
	})
}
