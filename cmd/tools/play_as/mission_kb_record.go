package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// completionKBRecord is the slice of a complete_mission reply the KB keeps.
type completionKBRecord struct {
	MissionID         string                    `json:"mission_id"`
	Title             string                    `json:"title"`
	ChainNext         string                    `json:"chain_next"`
	CreditsEarned     int64                     `json:"credits_earned"`
	CreditsPromised   int64                     `json:"credits_promised"`
	ReputationChanges map[string]int            `json:"reputation_changes"`
	SkillXPGained     map[string]int            `json:"skill_xp_gained"`
	ItemsReceived     map[string]int            `json:"items_received"`
	Unlocks           []serverapi.MissionUnlock `json:"unlocks"`
}

// acceptedTemplateID maps a mission instance id to the template id recorded
// when this agent accepted it (the completion reply carries no template_id).
// "" when the ledger has no matching accept.
func acceptedTemplateID(ledgerPath, missionID string) string {
	f, err := os.Open(ledgerPath)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	found := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var r missionCompletionRecord
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		if r.Event == missionEventAccepted && r.MissionID == missionID && r.TemplateID != "" {
			found = r.TemplateID
		}
	}
	return found
}

// recordCompletionInKB writes a complete_mission reply's payout (with the
// per-empire reputation_changes) and its unlock edges to the KB. raw may be
// the {"command","result"} envelope or the bare result.
func recordCompletionInKB(ctx context.Context, kb *knowledge.SQLiteKB, agentID, ledgerPath string, raw []byte) error {
	var outer struct {
		Tick int64 `json:"tick"`
	}
	_ = json.Unmarshal(raw, &outer)
	var r completionKBRecord
	if err := json.Unmarshal(unwrapActionResult(raw), &r); err != nil {
		return fmt.Errorf("parse complete_mission: %w", err)
	}
	if r.MissionID == "" {
		return fmt.Errorf("complete_mission reply has no mission_id")
	}
	tmpl := acceptedTemplateID(ledgerPath, r.MissionID)
	if err := kb.RecordMissionCompletion(ctx, knowledge.MissionCompletion{
		AgentID: agentID, MissionID: r.MissionID, TemplateID: tmpl, Title: r.Title,
		CreditsEarned: r.CreditsEarned, CreditsPromised: r.CreditsPromised,
		ReputationChanges: r.ReputationChanges, SkillXPGained: r.SkillXPGained,
		ItemsReceived: r.ItemsReceived, ChainNext: r.ChainNext, Tick: outer.Tick,
	}); err != nil {
		return err
	}
	if len(r.Unlocks) > 0 {
		if _, err := kb.RecordMissionUnlocks(ctx, tmpl, r.Unlocks, agentID, outer.Tick); err != nil {
			return err
		}
	}
	return nil
}

// recordViewedUnlocksInKB stores the unlock edges of a view_completed_mission
// reply, whose template_id names the completed mission directly.
func recordViewedUnlocksInKB(ctx context.Context, kb *knowledge.SQLiteKB, agentID string, raw []byte) error {
	var r serverapi.ViewCompletedMissionResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &r); err != nil {
		return fmt.Errorf("parse view_completed_mission: %w", err)
	}
	if len(r.Unlocks) == 0 {
		return nil
	}
	_, err := kb.RecordMissionUnlocks(ctx, r.TemplateID, r.Unlocks, agentID, 0)
	return err
}
