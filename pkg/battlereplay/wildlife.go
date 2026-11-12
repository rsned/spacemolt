package battlereplay

import (
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// WildlifeAttacks extracts, from a replay model, what each creature in the
// battle was observed shooting with.
//
// This is the only source for a species' offence: get_nearby reports hull and
// role but nothing about weapons, and scan reports a danger rating rather than a
// damage type. The battle log carries the damage type per shot, which is what a
// resistance fit is chosen against.
//
// Creatures are identified by Participant.Kind == "creature". Their species is
// NOT on the wire — a creature participant carries a display Username ("Rainbow
// Leviathan") and an empty ShipClass — so the caller supplies the mapping from
// creature id to species, normally from the get_nearby entry that named the
// quarry. Creatures absent from that map are skipped rather than filed under a
// guessed species id.
func WildlifeAttacks(m *ReplayModel, speciesByCreatureID map[string]string) []knowledge.WildlifeAttack {
	if m == nil {
		return nil
	}

	// Which participants are creatures we can name.
	species := make(map[string]string, len(m.Participants))
	for _, p := range m.Participants {
		if p.Kind != "creature" {
			continue
		}
		if s := speciesByCreatureID[p.PlayerID]; s != "" {
			species[p.PlayerID] = s
		}
	}
	if len(species) == 0 {
		return nil
	}

	// Aggregate within this battle, keyed the way the table is: one row per
	// species/weapon/damage-type/kind. A species that fires two weapons in one
	// battle produces two rows rather than an averaged one.
	type key struct {
		species, weapon, damageType, kind string
	}
	agg := make(map[key]*knowledge.WildlifeAttack)
	order := make([]key, 0, 4)

	for _, f := range m.Frames {
		for _, sh := range f.Shots {
			sp, ok := species[sh.FromID]
			if !ok {
				continue
			}
			k := key{sp, sh.WeaponName, sh.DamageType, string(sh.Kind)}
			a, ok := agg[k]
			if !ok {
				a = &knowledge.WildlifeAttack{
					Species:    sp,
					BattleID:   m.BattleID,
					WeaponName: sh.WeaponName,
					DamageType: sh.DamageType,
					ShotKind:   string(sh.Kind),
				}
				agg[k] = a
				order = append(order, k)
			}
			a.Shots++
			if !sh.Hit {
				continue
			}
			// Only hits carry damage, and only hits should shape the damage
			// range: a miss reports zero and would drag the observed minimum to
			// a value the weapon never deals.
			a.Hits++
			d := float64(sh.Damage)
			a.DamageTotal += d
			if a.Hits == 1 || d < a.DamageMin {
				a.DamageMin = d
			}
			if d > a.DamageMax {
				a.DamageMax = d
			}
		}
	}

	out := make([]knowledge.WildlifeAttack, 0, len(order))
	for _, k := range order {
		out = append(out, *agg[k])
	}
	return out
}

// WildlifeDefences extracts the creatures' own hull and shield maxima, which is
// the other half of a fit decision: the docs say creatures carry hull and armor
// but no shields, and this is what confirms or refutes that per species.
//
// As with WildlifeAttacks the species mapping is supplied by the caller, since
// the wire never names a creature's species inside a battle.
func WildlifeDefences(m *ReplayModel, speciesByCreatureID map[string]string) []knowledge.WildlifeSpecies {
	if m == nil {
		return nil
	}
	out := make([]knowledge.WildlifeSpecies, 0, len(m.Participants))
	for _, p := range m.Participants {
		if p.Kind != "creature" {
			continue
		}
		sp := speciesByCreatureID[p.PlayerID]
		if sp == "" {
			continue
		}
		out = append(out, knowledge.WildlifeSpecies{
			Species:   sp,
			Name:      p.Username,
			MaxHull:   p.MaxHull,
			MaxShield: p.MaxShield,
		})
	}
	return out
}
