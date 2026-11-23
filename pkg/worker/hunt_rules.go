package worker

import (
	"context"
	"fmt"
	"slices"

	"github.com/rsned/spacemolt/pkg/huntdecide"
)

// Option ids a decision may offer.
const (
	// huntOptContinue keeps hunting.
	huntOptContinue = "continue"
	// huntOptRearm returns to restock ammunition.
	huntOptRearm = "rearm"
	// huntOptRepair returns to repair the hull.
	huntOptRepair = "repair"
)

// huntProvisionOptionID is the option id for provisioning reloads per weapon.
func huntProvisionOptionID(reloads int) string {
	return fmt.Sprintf("reloads_%d", reloads)
}

// huntRules is the rule-based decider. It reads only d.State and d.Options:
// no client, no knowledge base, no globals, so any other decider is handed
// exactly the same inputs.
type huntRules struct{}

// Decide answers a provision or return decision from the state alone.
func (huntRules) Decide(_ context.Context, d huntdecide.Decision) huntdecide.Choice {
	switch d.Kind {
	case huntdecide.KindProvision:
		return rulesProvision(d)
	case huntdecide.KindReturn:
		return rulesReturn(d)
	}
	if len(d.Options) > 0 {
		return huntdecide.Choice{OptionID: d.Options[0].ID, Reason: "unknown decision kind; first option"}
	}
	return huntdecide.Choice{Reason: "no options"}
}

// huntDecider is the decider a pass uses: deps.Decider, or huntRules{}.
func huntDecider(deps HuntDeps) huntdecide.Decider {
	if deps.Decider != nil {
		return deps.Decider
	}
	return huntRules{}
}

func rulesOffered(d huntdecide.Decision, id string) bool {
	return slices.ContainsFunc(d.Options, func(o huntdecide.Option) bool { return o.ID == id })
}

// rulesProvision picks reloads_2 if offered, else the largest offered below
// it, else the smallest offered.
func rulesProvision(d huntdecide.Decision) huntdecide.Choice {
	if rulesOffered(d, huntProvisionOptionID(huntProvisionDefault)) {
		return huntdecide.Choice{
			OptionID: huntProvisionOptionID(huntProvisionDefault),
			Reason:   fmt.Sprintf("default of %d reloads per weapon", huntProvisionDefault),
		}
	}
	best, bestBelow := 0, 0 // smallest overall, largest below the default
	for _, n := range huntProvisionBuckets {
		if !rulesOffered(d, huntProvisionOptionID(n)) {
			continue
		}
		if best == 0 || n < best {
			best = n
		}
		if n < huntProvisionDefault && n > bestBelow {
			bestBelow = n
		}
	}
	switch {
	case bestBelow > 0:
		return huntdecide.Choice{
			OptionID: huntProvisionOptionID(bestBelow),
			Reason:   fmt.Sprintf("%d reloads per weapon: the most offered below the default", bestBelow),
		}
	case best > 0:
		return huntdecide.Choice{
			OptionID: huntProvisionOptionID(best),
			Reason:   fmt.Sprintf("%d reloads per weapon: the smallest offered", best),
		}
	}
	if len(d.Options) > 0 {
		return huntdecide.Choice{OptionID: d.Options[0].ID, Reason: "no reload bucket offered; first option"}
	}
	return huntdecide.Choice{Reason: "no options"}
}

// rulesReturn: repair below huntReturnRepairHull, else rearm without a
// reserve once the magazines run low, else continue; each only when offered.
func rulesReturn(d huntdecide.Decision) huntdecide.Choice {
	s := d.State
	if rulesOffered(d, huntOptRepair) && s.MaxHull > 0 && s.Hull/s.MaxHull < huntReturnRepairHull {
		return huntdecide.Choice{
			OptionID: huntOptRepair,
			Reason:   fmt.Sprintf("hull %.0f/%.0f is below %.0f%%", s.Hull, s.MaxHull, huntReturnRepairHull*100),
		}
	}
	if rulesOffered(d, huntOptRearm) && !huntHasReserve(s) && huntMagazinesLow(s) {
		return huntdecide.Choice{OptionID: huntOptRearm, Reason: "no reserve ammunition aboard and no magazine at half or better"}
	}
	if rulesOffered(d, huntOptContinue) {
		return huntdecide.Choice{OptionID: huntOptContinue, Reason: "hull and ammunition adequate"}
	}
	if len(d.Options) > 0 {
		return huntdecide.Choice{OptionID: d.Options[0].ID, Reason: "continue not offered; first option"}
	}
	return huntdecide.Choice{Reason: "no options"}
}

// huntFitsAmmo reports whether a stack of ammo feeds any fitted ammo-fed weapon.
func huntFitsAmmo(s huntdecide.State, a huntdecide.Ammo) bool {
	return slices.ContainsFunc(s.Weapons, func(w huntdecide.Weapon) bool {
		return w.AmmoType != "" && w.AmmoType == a.AmmoType
	})
}

// huntCanFire reports whether the ship can fire now: an ammo-free weapon, or
// an ammo-fed one that is loaded or has a matching stack aboard.
func huntCanFire(s huntdecide.State) bool {
	for _, w := range s.Weapons {
		if w.AmmoType == "" || w.Loaded > 0 {
			return true
		}
		if slices.ContainsFunc(s.Ammo, func(a huntdecide.Ammo) bool { return a.AmmoType == w.AmmoType && a.Aboard > 0 }) {
			return true
		}
	}
	return false
}

// huntMagazinesLow reports whether no ammo-fed weapon still holds at least half
// its magazine. A ship with a full magazine and no spare stack has plenty of
// fight left: observed live 2026-11-22, pirate-5 went home "dry" with 647/650
// in both autocannons. A weapon with no known magazine size counts as low only
// when it is empty.
func huntMagazinesLow(s huntdecide.State) bool {
	for _, w := range s.Weapons {
		if w.AmmoType == "" {
			continue
		}
		if w.MagazineSize <= 0 {
			if w.Loaded > 0 {
				return false
			}
			continue
		}
		if w.Loaded*2 >= w.MagazineSize {
			return false
		}
	}
	return true
}

// huntHasReserve reports whether the ship has fire beyond what is loaded: an
// ammo-free weapon, or any ammo stack of at least one item matching a fitted weapon.
func huntHasReserve(s huntdecide.State) bool {
	if slices.ContainsFunc(s.Weapons, func(w huntdecide.Weapon) bool { return w.AmmoType == "" }) {
		return true
	}
	return slices.ContainsFunc(s.Ammo, func(a huntdecide.Ammo) bool { return a.Aboard >= 1 && huntFitsAmmo(s, a) })
}
