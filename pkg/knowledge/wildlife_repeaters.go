package knowledge

import (
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// RepeaterRole is the field-guide role given to Frontier Repeaters.
const RepeaterRole = "repeater"

// RepeaterSpecies is the field-guide species key for a Frontier Repeater of
// the given ship_class ("pane", "spire", "monolith" seen 2026-11-12):
// "repeater_pane". Empty when the class is unknown, so an unclassified
// repeater is skipped rather than filed under a phantom species.
func RepeaterSpecies(shipClass string) string {
	c := strings.ToLower(strings.TrimSpace(shipClass))
	if c == "" {
		return ""
	}
	return "repeater_" + c
}

// NearbyWithRepeaters returns get_nearby's creatures plus its Frontier
// Repeaters folded in as creatures, so they reach the same species, sighting
// and coverage rows. Repeaters are a separate list on the wire (v0.687) but
// they fight like wildlife — a Repeater Monolith destroyed craftsman-1's
// Survey Vessel at Gloaming on 2026-11-12 — so the field guide should know
// where they sit and how hard they are. The input slice is not modified.
func NearbyWithRepeaters(creatures []serverapi.NearbyCreature, repeaters []serverapi.RepeaterInfo) []serverapi.NearbyCreature {
	if len(repeaters) == 0 {
		return creatures
	}
	out := make([]serverapi.NearbyCreature, 0, len(creatures)+len(repeaters))
	out = append(out, creatures...)
	for _, r := range repeaters {
		sp := RepeaterSpecies(r.ShipClass)
		if sp == "" {
			continue
		}
		out = append(out, serverapi.NearbyCreature{
			CreatureID: r.RepeaterID,
			Species:    sp,
			Name:       r.Name,
			Role:       RepeaterRole,
			Hull:       r.Hull,
			MaxHull:    r.MaxHull,
			InCombat:   r.InBattle,
		})
	}
	return out
}
