package worker

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// The danger score (operator decision 2026-11-27) is the share of a hunter's
// hull and shield a creature is expected to take off before it dies:
//
//	ticks to kill = creature max hull / hunter damage per tick
//	danger        = ticks to kill * creature damage per tick / (max hull + max shield)
//
// Both damage figures are measured, so a stronger ship qualifies for harder
// quarry as its own kills are recorded, and nothing has to be edited. A
// Gauntlet scores about 2% against a Chlorophage, 24% against a Pilot-Whale
// and 3,100% against a Cloudwhale.
const (
	// huntDangerMax is the most a single fight may be expected to cost.
	huntDangerMax = 0.35
	// huntDamagePerTickDefault is the fleet's measured Gauntlet figure. A
	// hunter with fewer than huntThreatMinKills recent kills uses it, and so
	// does a new hull until its own kills say otherwise.
	huntDamagePerTickDefault = 18.0
	// huntThreatKills is how many of a hunter's latest kills are averaged.
	huntThreatKills = 50
	// huntThreatMinKills is the fewest kills a measured figure may rest on.
	huntThreatMinKills = 10
	// huntPredatorMinShots is how many recorded shots a predator's score must
	// rest on before it can clear the predator as quarry. On 2026-11-27 a
	// Hoarfrost Leviathan hatchling (312 hull) scored 29% for a Gauntlet on
	// six shots, two of them hits.
	huntPredatorMinShots = 20
	// huntQuarryMaxHull caps a species with no recorded attacks: one never
	// measured is never assumed safe. The toughest quarry the fleet kills is
	// the Pilot-Whale at 220; the Cloudwhale is next at 3,501.
	huntQuarryMaxHull = 500
)

// huntTooToughSpecies are never quarry, whatever the score says. On
// 2026-11-26 pirate-7 attacked a Cloudwhale for a Lumen Oil need: a grazer
// that "does not attack ships first" and hits back for 80 a shot. The
// Gauntlet (285 hull, 85 shield) was destroyed while fleeing, 8 ticks in.
var huntTooToughSpecies = map[string]bool{"cloudwhale": true}

// huntDangerAllowed lists, per ship class, species a hull may take whatever
// the score says. Empty until an operator clears one.
var huntDangerAllowed = map[string]map[string]bool{}

// huntThreatKB is the part of the knowledge base the score reads.
type huntThreatKB interface {
	WildlifeDamagePerShot(ctx context.Context) (map[string]knowledge.WildlifeDamage, error)
	HunterDamagePerTick(ctx context.Context, agentID string, lastKills int) (perTick float64, kills int, err error)
}

// huntThreat is what one hunter needs to score a creature. The zero value
// scores nothing: the never-list and the hull cap alone decide.
type huntThreat struct {
	shipClass     string
	buffer        float64            // max hull + max shield
	hunterPerTick float64            // damage the hunter deals per tick
	perTick       map[string]float64 // species -> damage it deals per tick
	shots         map[string]int     // species -> shots that figure rests on
}

// huntLoadThreat reads the hunter's ship and both damage figures. Whatever
// cannot be read is left zero, which falls back to the hull cap.
func huntLoadThreat(ctx context.Context, deps HuntDeps) huntThreat {
	var th huntThreat
	if st := deps.Client.GetState(); st != nil {
		th.shipClass = st.Ship.ClassID
		th.buffer = st.Ship.MaxHull + st.Ship.MaxShield
	}
	tk, ok := deps.KB.(huntThreatKB)
	if !ok {
		return th
	}
	if m, err := tk.WildlifeDamagePerShot(ctx); err == nil {
		th.perTick, th.shots = make(map[string]float64, len(m)), make(map[string]int, len(m))
		for species, d := range m {
			th.perTick[species], th.shots[species] = d.PerShot, d.Shots
		}
	}
	th.hunterPerTick = huntDamagePerTickDefault
	if per, kills, err := tk.HunterDamagePerTick(ctx, deps.AgentID, huntThreatKills); err == nil && kills >= huntThreatMinKills && per > 0 {
		th.hunterPerTick = per
	}
	return th
}

// danger scores a creature of species with maxHull. known is false when the
// species has no recorded attacks or the ship could not be read.
func (t huntThreat) danger(species string, maxHull int) (score float64, known bool) {
	per, ok := t.perTick[species]
	if !ok || t.buffer <= 0 || t.hunterPerTick <= 0 || maxHull <= 0 {
		return 0, false
	}
	return float64(maxHull) / t.hunterPerTick * per / t.buffer, true
}

// tooTough reports a creature this hunter should not engage, and why.
func (t huntThreat) tooTough(species string, maxHull int) (bool, string) {
	if huntTooToughSpecies[species] {
		return true, "on the never-hunt list"
	}
	if huntDangerAllowed[t.shipClass][species] {
		return false, ""
	}
	if score, known := t.danger(species, maxHull); known {
		if score > huntDangerMax {
			return true, fmt.Sprintf("danger %.0f%% of hull and shield, over %.0f%%", score*100, huntDangerMax*100)
		}
		return false, ""
	}
	if maxHull > huntQuarryMaxHull {
		return true, fmt.Sprintf("hull %d over the %d cap for a species with no recorded attacks", maxHull, huntQuarryMaxHull)
	}
	return false, ""
}

// huntPredatorRole is the role of wildlife that attacks first.
const huntPredatorRole = "predator"

// beatable reports a predator this hunter may take as quarry (operator
// decision 2026-11-27): only on a score within huntDangerMax that rests on
// huntPredatorMinShots recorded shots, or an operator's clearance. The hull cap never clears a predator: one with no
// recorded attacks is left alone.
func (t huntThreat) beatable(species string, maxHull int) bool {
	if huntTooToughSpecies[species] {
		return false
	}
	if huntDangerAllowed[t.shipClass][species] {
		return true
	}
	score, known := t.danger(species, maxHull)
	return known && score <= huntDangerMax && t.shots[species] >= huntPredatorMinShots
}

// quarryRole reports whether a creature's role lets this hunter hunt it: a
// grazer or scavenger, or a predator it can beat.
func (t huntThreat) quarryRole(role, species string, maxHull int) bool {
	return huntQuarryRoles[role] || (role == huntPredatorRole && t.beatable(species, maxHull))
}
