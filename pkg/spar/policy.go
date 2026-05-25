// Package spar provides a controlled PvP sparring harness: it logs in two or
// more agents, equips and moves them to a non-empire arena, and runs scripted
// (or human-partnered) battles so combat mechanics are observable and testable.
package spar

import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// View is the read-only battle picture handed to a Policy each tick.
type View struct {
	Self     game.BattleParticipant
	Enemies  []game.BattleParticipant
	Allies   []game.BattleParticipant
	Tick     int64
	BattleID string
}

// Action is a Policy's chosen move. Kind is "battle" (dispatch via
// client.Battle with BattleAction+Payload) or "noop" (do nothing this tick).
type Action struct {
	Kind         string         // "battle" | "noop"
	BattleAction string         // advance | retreat | stance | target
	Payload      map[string]any // stance, target_id
}

// Policy decides one battle action from a View. Implementations are pure
// functions of the View, which keeps them unit-testable. This interface is the
// seam where custom .smolt-style battle scripts plug in later.
type Policy interface {
	Name() string
	Decide(View) Action
}

func noop() Action { return Action{Kind: "noop"} }

// zoneIndex orders the tactical zones from far (0) to close (3).
var zoneIndex = map[string]int{"outer": 0, "mid": 1, "inner": 2, "engaged": 3}

// BuildView assembles a View for the participant whose PlayerID is selfID.
// Returns ok=false if selfID is not among the participants.
func BuildView(b *game.BattleState, selfID string) (View, bool) {
	if b == nil {
		return View{}, false
	}
	var self game.BattleParticipant
	found := false
	var allies, enemies []game.BattleParticipant
	for _, p := range b.Participants {
		if p.PlayerID == selfID {
			self = p
			found = true
		}
	}
	if !found {
		return View{}, false
	}
	for _, p := range b.Participants {
		if p.PlayerID == selfID {
			continue
		}
		if p.SideID == self.SideID {
			allies = append(allies, p)
		} else {
			enemies = append(enemies, p)
		}
	}
	return View{Self: self, Allies: allies, Enemies: enemies, BattleID: b.BattleID}, true
}

// --- presets ---

type aggressor struct{}

// NewAggressor advances to the engaged zone, then targets the nearest enemy and
// holds the fire stance.
func NewAggressor() Policy { return aggressor{} }
func (aggressor) Name() string { return "aggressor" }
func (aggressor) Decide(v View) Action {
	if zoneIndex[v.Self.Zone] < zoneIndex["engaged"] {
		return Action{Kind: "battle", BattleAction: "advance"}
	}
	if v.Self.TargetID == "" && len(v.Enemies) > 0 {
		return Action{Kind: "battle", BattleAction: "target", Payload: map[string]any{"target_id": v.Enemies[0].PlayerID}}
	}
	if v.Self.Stance != "fire" {
		return Action{Kind: "battle", BattleAction: "stance", Payload: map[string]any{"stance": "fire"}}
	}
	return noop()
}

type skirmisher struct{ hullFloor int }

// NewSkirmisher holds the mid zone and fires, but retreats one zone when its
// own hull percentage drops below hullFloor.
func NewSkirmisher(hullFloor int) Policy { return skirmisher{hullFloor: hullFloor} }
func (skirmisher) Name() string { return "skirmisher" }
func (s skirmisher) Decide(v View) Action {
	if v.Self.HullPct < s.hullFloor && zoneIndex[v.Self.Zone] > zoneIndex["outer"] {
		return Action{Kind: "battle", BattleAction: "retreat"}
	}
	switch {
	case zoneIndex[v.Self.Zone] < zoneIndex["mid"]:
		return Action{Kind: "battle", BattleAction: "advance"}
	case zoneIndex[v.Self.Zone] > zoneIndex["mid"]:
		return Action{Kind: "battle", BattleAction: "retreat"}
	}
	if v.Self.TargetID == "" && len(v.Enemies) > 0 {
		return Action{Kind: "battle", BattleAction: "target", Payload: map[string]any{"target_id": v.Enemies[0].PlayerID}}
	}
	if v.Self.Stance != "fire" {
		return Action{Kind: "battle", BattleAction: "stance", Payload: map[string]any{"stance": "fire"}}
	}
	return noop()
}

type retreater struct{}

// NewRetreater immediately adopts the flee stance (which auto-retreats over
// several ticks), exercising the escape mechanic.
func NewRetreater() Policy { return retreater{} }
func (retreater) Name() string { return "retreater" }
func (retreater) Decide(v View) Action {
	if v.Self.Stance != "flee" {
		return Action{Kind: "battle", BattleAction: "stance", Payload: map[string]any{"stance": "flee"}}
	}
	return noop()
}

type dummy struct{}

// NewDummy braces and never advances or fires — a low-risk practice partner.
func NewDummy() Policy { return dummy{} }
func (dummy) Name() string { return "dummy" }
func (dummy) Decide(v View) Action {
	if v.Self.Stance != "brace" {
		return Action{Kind: "battle", BattleAction: "stance", Payload: map[string]any{"stance": "brace"}}
	}
	return noop()
}

// PolicyByName resolves a preset name to a Policy. The skirmisher uses a 40%
// hull floor by default.
func PolicyByName(name string) (Policy, error) {
	switch name {
	case "aggressor":
		return NewAggressor(), nil
	case "skirmisher":
		return NewSkirmisher(40), nil
	case "retreater":
		return NewRetreater(), nil
	case "dummy":
		return NewDummy(), nil
	default:
		return nil, fmt.Errorf("unknown policy %q (want: aggressor, skirmisher, retreater, dummy)", name)
	}
}
