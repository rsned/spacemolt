package agent

import (
	"log"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// WirePlayerObserver registers a PlayerObserver on the given game client
// that persists each batch of observations to the knowledge base. Errors
// from RecordSightings are logged and swallowed — a failed write must
// never break the game response path.
func WirePlayerObserver(c *game.Client, kb knowledge.Base) {
	if c == nil || kb == nil {
		return
	}
	c.SetPlayerObserver(func(obs []game.ObservedPlayer) {
		if len(obs) == 0 {
			return
		}
		seen := make([]knowledge.SeenPlayer, 0, len(obs))
		for _, o := range obs {
			seen = append(seen, knowledge.SeenPlayer{
				PlayerID:       o.PlayerID,
				Username:       o.Username,
				ShipClass:      o.ShipClass,
				FactionID:      o.FactionID,
				FactionTag:     o.FactionTag,
				ClanTag:        o.ClanTag,
				PrimaryColor:   o.PrimaryColor,
				SecondaryColor: o.SecondaryColor,
				StatusMessage:  o.StatusMessage,
				Anonymous:      o.Anonymous,
				InCombat:       o.InCombat,
				SystemID:       o.SystemID,
				POIID:          o.POIID,
				Source:         o.Source,
				SeenAt:         o.SeenAt,
			})
		}
		if err := kb.RecordSightings(seen); err != nil {
			log.Printf("[seen] RecordSightings: %v", err)
		}
	})
}
