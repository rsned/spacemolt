package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// missionRecordCompletionKB stores what a worker's complete_mission paid --
// including the per-empire reputation_changes the mission runs log never
// kept -- and the v0.685 unlock edges. Best-effort: the completion already
// landed, so a KB miss is logged and never fails the pass.
func missionRecordCompletionKB(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate) {
	kb, ok := deps.KB.(*knowledge.SQLiteKB)
	if !ok {
		return
	}
	raw := deps.Client.GetRawJSON("complete_mission")
	if len(raw) == 0 {
		return
	}
	var outer struct {
		Tick int64 `json:"tick"`
	}
	_ = json.Unmarshal(raw, &outer)
	var res serverapi.CompleteMissionResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &res); err != nil || res.MissionID != c.ActiveID {
		return // unparsable, or a stale frame from an earlier completion
	}
	tmpl := c.Entry.TemplateID
	if err := kb.RecordMissionCompletion(ctx, knowledge.MissionCompletion{
		AgentID: deps.AgentID, MissionID: res.MissionID, TemplateID: tmpl, Title: res.Title,
		CreditsEarned: res.CreditsEarned, CreditsPromised: res.CreditsPromised,
		ReputationChanges: res.ReputationChanges,
		SkillXPGained:     intMap(res.SkillXPGained),
		ItemsReceived:     intMap(res.ItemsReceived),
		ChainNext:         res.ChainNext, Tick: outer.Tick,
		RecipesUnlocked: res.RecipesUnlocked,
	}); err != nil {
		fmt.Fprintf(out, "missions: KB completion record: %v\n", err) //nolint:errcheck
	}
	if len(res.Unlocks) > 0 {
		if _, err := kb.RecordMissionUnlocks(ctx, tmpl, res.Unlocks, deps.AgentID, outer.Tick); err != nil {
			fmt.Fprintf(out, "missions: KB unlock record: %v\n", err) //nolint:errcheck
		}
	}
}

// intMap decodes a {"key":N} object, or nil when absent or another shape.
func intMap(raw json.RawMessage) map[string]int {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]int
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}
