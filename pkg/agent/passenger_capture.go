package agent

import (
	"log"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// WirePassengerObserver registers a PassengerObserver on the given game client
// that persists each batch of passenger observations into the knowledge base's
// identity catalog. Errors from RecordPassengers are logged and swallowed — a
// failed write must never break the game response path.
func WirePassengerObserver(c *game.Client, kb knowledge.Base) {
	if c == nil || kb == nil {
		return
	}
	c.SetPassengerObserver(func(obs []game.ObservedPassenger) {
		if len(obs) == 0 {
			return
		}
		pax := make([]knowledge.SeenPassenger, 0, len(obs))
		for _, o := range obs {
			pax = append(pax, knowledge.SeenPassenger{
				CitizenID:   o.CitizenID,
				Name:        o.Name,
				Citizenship: o.Citizenship,
				Bio:         o.Bio,
				Class:       o.Class,
				Source:      o.Source,
				SeenAt:      o.SeenAt,
			})
		}
		if err := kb.RecordPassengers(pax); err != nil {
			log.Printf("[passengers] RecordPassengers: %v", err)
		}
	})
}
