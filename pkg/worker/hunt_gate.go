package worker

import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

const (
	// huntDefaultMaxDifficulty is gate 1: the highest mission difficulty the
	// hunt fleet will accept. It starts at 1 — first_hunt_belt_grazers, passive
	// quarry — and is raised deliberately as weapons level climbs. It is NEVER
	// derived from a reward score.
	//
	// Reward-based selection is the failure mode this exists to prevent.
	// leviathan_bounty is difficulty 6, pays 8,000cr, carries the best XP in the
	// table, and is REPEATABLE — so a selector optimising reward would choose it
	// not once but forever, and the docs describe the Molt Leviathan as a
	// predator that hunts ships and fights to the death.
	huntDefaultMaxDifficulty = 1

	// huntWildlifeOnlyDefault is gate 2. Difficulty 2 holds both the safe
	// repeatable wildlife culls and pirate_bounty / convoy_defense, which shoot
	// back. A single numeric cap admits both or neither; this second gate is what
	// lets gate 1 rise to 2 for the culls alone.
	huntWildlifeOnlyDefault = true

	// missionTypeDelivery already exists at mission_select.go:175; these are new.
	missionTypeCombat     = "combat"
	objectiveKillCreature = "kill_creature"
)

// huntWildlifeMissions are the combat missions whose quarry is wildlife.
// Everything else of type combat fights back.
var huntWildlifeMissions = map[string]bool{
	"first_hunt_belt_grazers": true,
	"grazer_cull":             true,
	"ice_field_thinning":      true,
	"nebula_drift_hunt":       true,
}

// huntMissionSpecies maps a wildlife mission to the ONE species the server
// counts for its kill_creature objective. Species — not role — decides mission
// credit: one belt held slag_tortoise, patina_grazer and belt_grazer side by
// side, all role "grazer", two of the three sharing the target's 60 hull, and
// only belt_grazer counted. That is why an evening of clean kills at correct
// belts never moved first_hunt_belt_grazers.
//
// This is a CURATED table, deliberately, and not a regex over the objective
// description. The wire carries no machine-readable target on any wildlife
// mission (see huntObjectiveTarget), the description is prose written for
// humans, and a parser over prose is the fragile inference this branch has
// already been burned by twice. Four entries, each verifiable on both sides:
// the mission text names the quarry and the operator has seen belt_grazer,
// rime_grazer and sift_ray in live get_nearby lists.
//
// leviathan_bounty is absent on purpose — difficulty 6 excludes it at the gate
// above, and it is not a wildlife mission.
//
// A mission NOT in this table hunts anything eligible; the table narrows, it
// never widens. The server's own target_id, if a mission ever carries one,
// outranks this table (see huntRequiredSpecies).
var huntMissionSpecies = map[string]string{
	"first_hunt_belt_grazers": "belt_grazer",
	"grazer_cull":             "belt_grazer",
	"ice_field_thinning":      "rime_grazer",
	"nebula_drift_hunt":       "sift_ray",
}

// huntRequiredSpecies resolves the species a mission's kills must be, and says
// where the answer came from. An empty species means "unscoped": hunt any
// eligible wildlife rather than refusing everything, which is the right
// behaviour for a wildlife mission nobody has curated yet.
//
// The server's own objective target_id wins over the curated table whenever it
// is populated: the table is a stopgap for missions that omit it, not a
// competing opinion.
func huntRequiredSpecies(e serverapi.MissionBoardEntry) (species, source string) {
	for _, o := range e.Objectives {
		if o.Type == objectiveKillCreature && o.TargetID != "" {
			return o.TargetID, "objective target_id"
		}
	}
	if s, ok := huntMissionSpecies[e.MissionID]; ok {
		return s, "curated mission table"
	}
	return "", "unscoped"
}

// huntAdmissible reports whether the hunt fleet may accept this board entry.
// A non-empty reason explains every refusal, so a skipped mission is never
// silent.
func huntAdmissible(e serverapi.MissionBoardEntry, maxDifficulty int, wildlifeOnly bool) (bool, string) {
	if e.Type != missionTypeCombat {
		return false, fmt.Sprintf("not a combat mission (type %q)", e.Type)
	}
	if e.Difficulty > maxDifficulty {
		return false, fmt.Sprintf("difficulty %d over cap %d", e.Difficulty, maxDifficulty)
	}
	if wildlifeOnly && !huntWildlifeMissions[e.MissionID] {
		return false, fmt.Sprintf("%s is not a wildlife mission and wildlife-only is set", e.MissionID)
	}
	if huntKillQuantity(e) == 0 {
		return false, "no kill_creature objective"
	}

	return true, ""
}

// huntKillQuantity totals the creatures this mission asks to be killed.
// Objectives are summed rather than taking the first, so a multi-objective hunt
// reports the real target count.
func huntKillQuantity(e serverapi.MissionBoardEntry) int {
	total := 0
	for _, o := range e.Objectives {
		if o.Type != objectiveKillCreature {
			continue
		}
		q := o.Quantity
		if q <= 0 {
			q = 1 // an objective with no quantity still means one kill
		}
		total += q
	}

	return total
}
