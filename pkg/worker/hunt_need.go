package worker

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// huntNeedMaxDifficulty is the hardest Station Need the hunt worker accepts.
const huntNeedMaxDifficulty = 2

// huntDroppedByMarker introduces the source creature in a Station Need
// description: "Dropped by: Frost-Moth (only where it still feeds on ...)."
const huntDroppedByMarker = "Dropped by:"

// huntNeed is a parsed "Station Need: <part>" delivery mission: a station
// asking for creature parts.
type huntNeed struct {
	ItemID       string
	Quantity     int
	TargetBaseID string
	SystemID     string
	Species      []string // species ids that drop ItemID
	Reward       int

	// Filled in after parsing: the board entry it came from, and the hunting
	// ground huntNeedGround chose for it (empty when none was found).
	MissionID    string
	GroundSystem string
	GroundPOI    string
	// Radius is the ground search radius (jumps) the need was taken at; every
	// re-pick of its ground stays within it.
	Radius int
}

// huntParseNeed reads a Station Need from a board entry. It accepts only a
// `delivery` mission with exactly one `deliver_item` objective. Species come
// from the description's "Dropped by:" line (display names resolved through
// speciesByName, keyed as kb.SpeciesByDisplayName keys them), else from
// dropsOf(itemID). ok is false when no species can be found: a need we cannot
// attribute to a creature is not hunted.
func huntParseNeed(e serverapi.MissionBoardEntry, speciesByName map[string]string, dropsOf func(itemID string) []string) (huntNeed, bool) {
	if e.Type != "delivery" || len(e.Objectives) != 1 || e.Objectives[0].Type != "deliver_item" {
		return huntNeed{}, false
	}
	o := e.Objectives[0]
	if o.ItemID == "" || o.Quantity <= 0 {
		return huntNeed{}, false
	}

	n := huntNeed{
		ItemID:       o.ItemID,
		Quantity:     o.Quantity,
		TargetBaseID: o.TargetBaseID,
		SystemID:     o.SystemID,
	}
	if e.Rewards != nil {
		n.Reward = e.Rewards.Credits
	}

	species, found, ok := huntDroppedBySpecies(e.Description, speciesByName)
	if found && !ok {
		return huntNeed{}, false
	}
	n.Species = species
	if !found && dropsOf != nil {
		n.Species = dropsOf(o.ItemID)
	}
	if len(n.Species) == 0 {
		return huntNeed{}, false
	}
	return n, true
}

var huntParenGroup = regexp.MustCompile(`\([^)]*\)`)

// huntDroppedBySpecies resolves the creature names on a "Dropped by:" line.
// ok is false when the marker is absent or ANY name fails to resolve: a
// partial list could hide a predator, so refuse rather than guess.
func huntDroppedBySpecies(desc string, speciesByName map[string]string) (species []string, found bool, ok bool) {
	_, rest, found := strings.Cut(desc, huntDroppedByMarker)
	if !found {
		return nil, false, true
	}
	line, _, _ := strings.Cut(rest, "\n")
	line = huntParenGroup.ReplaceAllString(line, "")
	line = strings.TrimRight(strings.TrimSpace(line), ".")
	line = strings.NewReplacer(" or ", ",", " and ", ",").Replace(line)

	var out []string
	for name := range strings.SplitSeq(line, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		sp, ok := speciesByName[knowledge.NormalizeCreatureName(name)]
		if !ok {
			// The board hyphenates ("Frost-Moth"); the guide may not.
			sp, ok = speciesByName[knowledge.NormalizeCreatureName(strings.ReplaceAll(name, "-", " "))]
		}
		if !ok {
			return nil, true, false
		}
		if !huntContains(out, sp) {
			out = append(out, sp)
		}
	}
	return out, true, len(out) > 0
}

func huntContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// huntAdmissibleNeed decides whether a need may be hunted. It refuses
// difficulty above huntNeedMaxDifficulty and any species whose role is not in
// huntQuarryRoles: the no-predator rule, where an unknown role is refused too.
func huntAdmissibleNeed(n huntNeed, difficulty int, roleOf func(species string) string) (bool, string) {
	if difficulty > huntNeedMaxDifficulty {
		return false, fmt.Sprintf("difficulty %d exceeds %d", difficulty, huntNeedMaxDifficulty)
	}
	if len(n.Species) == 0 {
		return false, "no source species"
	}
	for _, sp := range n.Species {
		role := roleOf(sp)
		if !huntQuarryRoles[role] {
			if role == "" {
				role = "unknown"
			}
			return false, fmt.Sprintf("species %s has role %s, not a quarry role", sp, role)
		}
	}
	return true, ""
}
