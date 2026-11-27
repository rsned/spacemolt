package worker

import (
	"context"
	"encoding/json"
	"io"
	"slices"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/huntdecide"
	"github.com/rsned/spacemolt/pkg/reload"
)

// Why a creature at the ground is not offered as a target.
const (
	huntRefusedTooTough  = "too_tough"
	huntRefusedInCombat  = "in_combat"
	huntRefusedNotQuarry = "not_quarry"
)

// huntOutcomeNames are the fight results a target outcome record carries.
var huntOutcomeNames = map[huntOutcome]string{
	huntResolved:    "resolved",
	huntFled:        "fled",
	huntGaveUp:      "gave_up",
	huntBattleError: "battle_error",
}

// huntTargetOptions lists every creature last read at the ground: the quarry
// first, in the order given, then the ones refused, each with why. A refused
// creature is in the record so that "there was a Cloudwhale and it was left
// alone" is an example too.
func huntTargetOptions(quarry, all []serverapi.NearbyCreature, th huntThreat) []huntdecide.Option {
	opts := make([]huntdecide.Option, 0, len(all)+len(quarry))
	features := func(c serverapi.NearbyCreature) map[string]any {
		f := map[string]any{"species": c.Species, "role": c.Role, "hull": c.Hull, "max_hull": c.MaxHull}
		// The inputs go in with the score: the record must stand without the KB.
		if score, known := th.danger(c.Species, c.MaxHull); known {
			f["danger"] = score
			f["creature_damage_per_tick"] = th.perTick[c.Species]
			f["hunter_damage_per_tick"] = th.hunterPerTick
		}
		return f
	}
	for _, c := range quarry {
		f := features(c)
		f["admissible"] = true
		opts = append(opts, huntdecide.Option{ID: c.CreatureID, Features: f})
	}
	for _, c := range all {
		if slices.ContainsFunc(quarry, func(q serverapi.NearbyCreature) bool { return q.CreatureID == c.CreatureID }) {
			continue
		}
		f := features(c)
		f["admissible"] = false
		tough, _ := th.tooTough(c.Species, c.MaxHull)
		switch {
		case tough:
			f["refused"] = huntRefusedTooTough
		case c.InCombat:
			f["refused"] = huntRefusedInCombat
		default:
			f["refused"] = huntRefusedNotQuarry
		}
		opts = append(opts, huntdecide.Option{ID: c.CreatureID, Features: f, NoExplore: true})
	}
	return opts
}

// rulesTarget picks the admissible option with the least hull, the first of
// equals: huntPickQuarry, read from the options alone.
func rulesTarget(d huntdecide.Decision) huntdecide.Choice {
	best, bestHull := "", 0
	for _, o := range d.Options {
		if ok, _ := o.Features["admissible"].(bool); !ok {
			continue
		}
		hull, _ := o.Features["hull"].(int)
		if best == "" || hull < bestHull {
			best, bestHull = o.ID, hull
		}
	}
	if best == "" {
		return huntdecide.Choice{Reason: "no admissible creature"}
	}
	return huntdecide.Choice{OptionID: best, Reason: "least hull among the admissible"}
}

// huntLastNearby is the creature list of the last get_nearby, from the
// client's cache: no server call.
func huntLastNearby(deps HuntDeps) []serverapi.NearbyCreature {
	var nearby serverapi.GetNearbyResponse
	if raw := deps.Client.GetRawJSON("nearby"); len(raw) > 0 {
		_ = json.Unmarshal(raw, &nearby) // a bad cache only loses the refused options
	}
	return nearby.Creatures
}

// huntTargetDecision asks the seam which of quarry to engage and returns its
// index, with the decision's seq for the outcome record. With no sortie file
// to write to, or an answer that names no quarry, the rule's pick stands and
// seq is 0.
func huntTargetDecision(ctx context.Context, deps HuntDeps, out io.Writer, job huntJob, s *huntdecide.Sortie, quarry []serverapi.NearbyCreature) (idx, seq int) {
	_, idx = huntPickQuarry(quarry)
	if idx < 0 || deps.Decisions == nil || huntAgentDir(deps) == "" {
		return idx, 0
	}
	lo, err := reload.FetchLoadout(ctx, deps.Client, deps.KB)
	if err != nil {
		return idx, 0
	}
	seq = s.NextSeq()
	choice := huntDecider(deps).Decide(ctx, huntdecide.Decision{
		Kind: huntdecide.KindTarget, SortieID: s.ID, Seq: seq,
		State:   huntDecisionState(ctx, deps, job, s, lo),
		Options: huntTargetOptions(quarry, huntLastNearby(deps), huntLoadThreat(ctx, deps)),
	})
	huntSaveSortie(deps, out, s)
	if i := slices.IndexFunc(quarry, func(c serverapi.NearbyCreature) bool { return c.CreatureID == choice.OptionID }); i >= 0 {
		idx = i
	}
	return idx, seq
}

// huntTargetFight is what a target outcome record says of one engagement.
type huntTargetFight struct {
	seq                   int
	creature              serverapi.NearbyCreature
	pursuit               bool
	outcome               huntOutcome
	ticks                 int
	killed                bool
	hullBefore, hullAfter float64
	shieldBefore, maxHull float64
	shipBefore, shipAfter string
}

// huntTargetOutcome writes the outcome of one engagement: what was fought,
// how it ended, and what it cost in hull. A ship that comes out in another
// hull was lost.
func huntTargetOutcome(deps HuntDeps, s *huntdecide.Sortie, f huntTargetFight) {
	if deps.Decisions == nil {
		return
	}
	now := huntNow(deps)
	_ = deps.Decisions.Append(now, huntdecide.OutcomeRecord{
		Type: "outcome", At: now, SortieID: s.ID, Seq: f.seq, Phase: string(huntdecide.KindTarget),
		Data: map[string]any{
			"creature_id": f.creature.CreatureID, "species": f.creature.Species, "role": f.creature.Role,
			"creature_hull": f.creature.Hull, "creature_max_hull": f.creature.MaxHull,
			"pursuit": f.pursuit, "result": huntOutcomeNames[f.outcome], "ticks": f.ticks, "killed": f.killed,
			"hull_before": f.hullBefore, "hull_after": f.hullAfter, "max_hull": f.maxHull,
			"shield_before": f.shieldBefore,
			"ship_lost":     f.shipBefore != "" && f.shipAfter != "" && f.shipBefore != f.shipAfter,
		},
	})
}
