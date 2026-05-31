package agent

import (
	"log"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// FactionEnqueuer schedules background backfill of faction details for the
// given faction ids. Satisfied by *faction.FactionBackfiller. Kept as a local
// interface so this package needn't import pkg/faction.
type FactionEnqueuer interface {
	Enqueue(ids ...string)
}

// WirePlayerObserver registers a PlayerObserver on the given game client
// that persists each batch of observations to the knowledge base. Errors
// from RecordSightings are logged and swallowed — a failed write must
// never break the game response path. When enq is non-nil, the distinct
// non-empty faction ids in each batch are also enqueued for faction_info
// backfill (a non-blocking call — the fetch happens on enq's own goroutine).
func WirePlayerObserver(c *game.Client, kb knowledge.Base, enq FactionEnqueuer) {
	if c == nil || kb == nil {
		return
	}
	c.SetPlayerObserver(func(obs []game.ObservedPlayer) {
		if len(obs) == 0 {
			return
		}
		seen := make([]knowledge.SeenPlayer, 0, len(obs))
		factionIDs := make([]string, 0, len(obs))
		seenFaction := make(map[string]bool, len(obs))
		for _, o := range obs {
			if o.FactionID != "" && !seenFaction[o.FactionID] {
				seenFaction[o.FactionID] = true
				factionIDs = append(factionIDs, o.FactionID)
			}
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
		if enq != nil && len(factionIDs) > 0 {
			enq.Enqueue(factionIDs...)
		}
	})
}
