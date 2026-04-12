package knowledge

import (
	"context"
	"log"

	"github.com/rsned/spacemolt/pkg/game"
)

// XPTracker records skill XP changes to the knowledge base. It is designed
// to be wired into a game.Client's XPCallback field so that every successful
// mutation command automatically records XP deltas.
type XPTracker struct {
	kb      Base
	agentID string
	logger  *log.Logger
}

// NewXPTracker creates a tracker and wires it into the game client's XPCallback.
// If kb is nil, no tracker is created and the client is left unchanged.
func NewXPTracker(client *game.Client, kb Base, agentID string, logger *log.Logger) *XPTracker {
	if kb == nil {
		return nil
	}

	t := &XPTracker{
		kb:      kb,
		agentID: agentID,
		logger:  logger,
	}

	client.XPCallback = t.onXPChange
	return t
}

// onXPChange is the callback fired by the game client after each successful action.
// It receives both the full Skill map (level+XP) and the flat SkillXP map.
// The SkillXP map is updated more frequently by the server, so we prefer it
// for detecting XP changes, falling back to the Skill map for level info.
func (t *XPTracker) onXPChange(action, target string, beforeSkills, afterSkills map[string]game.Skill, beforeXP, afterXP map[string]float64, gameTick int64) {
	source := "action"
	var missionID string
	if action == "complete_mission" {
		source = "mission_reward"
		missionID = target
	}

	ctx := context.Background()

	// Track changes from the SkillXP map (updated on most action responses)
	xpTracked := make(map[string]bool)
	for skillID, xpAfter := range afterXP {
		xpBefore := beforeXP[skillID]
		xpDelta := xpAfter - xpBefore
		if xpDelta == 0 {
			continue
		}

		// Get level info from the Skills map if available
		levelBefore, levelAfter := 0, 0
		if s, ok := beforeSkills[skillID]; ok {
			levelBefore = s.Level
		}
		if s, ok := afterSkills[skillID]; ok {
			levelAfter = s.Level
		}

		obs := XPObservation{
			AgentID:     t.agentID,
			Action:      action,
			Target:      target,
			Source:      source,
			SkillID:     skillID,
			XPDelta:     xpDelta,
			LevelDelta:  levelAfter - levelBefore,
			LevelBefore: levelBefore,
			LevelAfter:  levelAfter,
			GameTick:    gameTick,
			MissionID:   missionID,
		}

		if err := t.kb.RecordXPObservation(ctx, obs); err != nil {
			if t.logger != nil {
				t.logger.Printf("Warning: failed to record XP observation for %s: %v", skillID, err)
			}
		} else if t.logger != nil {
			t.logger.Printf("XP change: %s %+.1f XP (level %d->%d) from %s %s",
				skillID, xpDelta, levelBefore, levelAfter, action, target)
		}
		xpTracked[skillID] = true
	}

	// Also check the Skills map for any changes not caught by SkillXP
	// (e.g., level-up where XP resets)
	for skillID, skillAfter := range afterSkills {
		if xpTracked[skillID] {
			continue
		}
		skillBefore, existed := beforeSkills[skillID]
		if !existed {
			skillBefore = game.Skill{Level: 0, XP: 0}
		}

		levelDelta := skillAfter.Level - skillBefore.Level
		xpDelta := skillAfter.XP - skillBefore.XP

		if levelDelta == 0 && xpDelta == 0 {
			continue
		}

		obs := XPObservation{
			AgentID:     t.agentID,
			Action:      action,
			Target:      target,
			Source:      source,
			SkillID:     skillID,
			XPDelta:     xpDelta,
			LevelDelta:  levelDelta,
			LevelBefore: skillBefore.Level,
			LevelAfter:  skillAfter.Level,
			GameTick:    gameTick,
			MissionID:   missionID,
		}

		if err := t.kb.RecordXPObservation(ctx, obs); err != nil {
			if t.logger != nil {
				t.logger.Printf("Warning: failed to record XP observation for %s: %v", skillID, err)
			}
		} else if t.logger != nil {
			t.logger.Printf("XP change: %s %+.1f XP (level %d->%d) from %s %s",
				skillID, xpDelta, skillBefore.Level, skillAfter.Level, action, target)
		}
	}
}
