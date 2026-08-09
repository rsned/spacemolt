package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

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
	// cracking_the_shell is the captured chain continuation of first_hunt:
	// difficulty 2, "Hunt 3 Slag-Tortoises at an asteroid belt", chaining on to
	// ghosts_in_the_cloud. Wildlife, so gate 2 admits it; difficulty 2, so it
	// passes gate 1 only as an earned continuation (see huntAdmissible).
	"cracking_the_shell": true,
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
	// From the captured cracking_the_shell payload: "Hunt 3 Slag-Tortoises at
	// an asteroid belt". slag_tortoise is confirmed live (90 hull, role
	// grazer) and shares the iron belts with belt_grazer, so the belt rule
	// covers it unchanged.
	"cracking_the_shell": "slag_tortoise",
	// ghosts_in_the_cloud is cracking_the_shell's chain_next and is
	// deliberately ABSENT: never seen on a board, quarry unknown, difficulty
	// unknown. Nothing goes in this table on speculation.
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

// huntEarnedContinuations returns the missions this agent has EARNED the right
// to attempt over the difficulty cap: continuation mission id -> the completed
// predecessor that unlocked it.
//
// Evidence is the agent's own completions, and nothing weaker. Seeing a mission
// on a board, or accepting one, is not evidence — an exemption that fired on
// those would admit an unearned difficulty-2 mission, which is the single
// outcome the cap exists to prevent. Every failure path returns an EMPTY map,
// so the gate falls closed to the plain difficulty rule.
//
// There are two sources, and their standing is not equal:
//
//   - PRIMARY: this agent's own record of completion replies, written as each
//     mission was finished (hunt_chain.go). The server names the continuation
//     in the reply to the complete_mission that earned it, so that frame is
//     first-hand, correlated evidence which cannot exist without a completion.
//   - SECONDARY: the completed-missions listing below, kept because it costs
//     one query and would cover a server that populates the list. It is the
//     weaker source: no capture proves the shape, and the storage key it reads
//     is shape-gated.
//
// The primary source wins. The list only contributes continuations the record
// does not already hold.
func huntEarnedContinuations(ctx context.Context, deps HuntDeps, out io.Writer) map[string]string {
	earned := huntRecordedContinuations(deps, out)
	for next, predecessor := range huntListedContinuations(ctx, deps, out) {
		if _, have := earned[next]; !have {
			earned[next] = predecessor
		}
	}
	return earned
}

// huntListedContinuations is the secondary source: the agent's completed-mission
// listing, read once per pass and treated as empty on any failure.
//
// The evidence is a POSITIVE per-entry marker, completion_time, and not the
// container the entries arrived in. That matters because the raw store keys
// completed_missions on payload SHAPE, and the active-missions reply is one
// omitted `max_missions` away from matching it — both fields are omitempty. An
// active mission carries no completion_time, so even if the wrong list lands
// under this key nothing in it is credited, and the exemption cannot be forged
// out of a mission that was merely ACCEPTED.
//
// Two caveats a reader must know:
//
//   - The completed_missions list shape is not modelled. Entries are decoded
//     through serverapi.ViewCompletedMissionResponse, the existing struct for a
//     completed mission — it carries template_id, chain_next and
//     completion_time. No field name here is invented, but no capture proves
//     the server sends them on THIS command either.
//   - Whether a completed entry carries chain_next at all is unverified. If it
//     does not, this map is always empty — which no longer stalls the chain,
//     because the completion-reply record above is the source that actually
//     fires.
func huntListedContinuations(ctx context.Context, deps HuntDeps, out io.Writer) map[string]string {
	earned := map[string]string{}
	if err := deps.Client.CompletedMissions(ctx); err != nil {
		fmt.Fprintf(out, "hunt: completed_missions: %v; no continuations from the listing\n", err) //nolint:errcheck
		return earned
	}
	raw := deps.Client.GetRawJSON("completed_missions")
	if len(raw) == 0 {
		fmt.Fprintln(out, "hunt: completed_missions returned no data; no continuations from the listing") //nolint:errcheck
		return earned
	}
	var resp struct {
		Missions []serverapi.ViewCompletedMissionResponse `json:"missions"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(out, "hunt: parse completed missions: %v; no continuations from the listing\n", err) //nolint:errcheck
		return earned
	}
	for _, m := range resp.Missions {
		if m.ChainNext == "" || m.TemplateID == "" {
			continue
		}
		if m.CompletionTime == "" {
			// No completion marker, no credit. This is the clause that stops
			// an accepted-but-unfinished mission from buying a difficulty
			// waiver if the active list ever lands under this key.
			fmt.Fprintf(out, "hunt: ignoring %s as chain evidence: no completion_time\n", m.TemplateID) //nolint:errcheck
			continue
		}
		earned[m.ChainNext] = m.TemplateID
	}
	return earned
}

// huntAdmissible reports whether the hunt fleet may accept this board entry.
// A non-empty reason explains every refusal, so a skipped mission is never
// silent. waived names the completed predecessor when the difficulty cap was
// waived for a chain continuation, so an admitted exemption is never silent
// either.
//
// The two gates stay INDEPENDENT. The chain exemption waives gate 1 (the
// difficulty cap) and nothing else: a chain that ever continues into a
// non-wildlife mission is still refused by gate 2, whatever it was earned by.
func huntAdmissible(e serverapi.MissionBoardEntry, maxDifficulty int, wildlifeOnly bool, earned map[string]string) (ok bool, reason, waived string) {
	if e.Type != missionTypeCombat {
		return false, fmt.Sprintf("not a combat mission (type %q)", e.Type), ""
	}
	if e.Difficulty > maxDifficulty {
		predecessor := earned[e.MissionID]
		if predecessor == "" && e.TemplateID != "" {
			predecessor = earned[e.TemplateID]
		}
		if predecessor == "" {
			return false, fmt.Sprintf("difficulty %d over cap %d", e.Difficulty, maxDifficulty), ""
		}
		waived = predecessor
	}
	if wildlifeOnly && !huntWildlifeMissions[e.MissionID] {
		return false, fmt.Sprintf("%s is not a wildlife mission and wildlife-only is set", e.MissionID), ""
	}
	if huntKillQuantity(e) == 0 {
		return false, "no kill_creature objective", ""
	}

	return true, "", waived
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
