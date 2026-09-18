package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// A mission's chain link exists in exactly one place: the `complete_mission`
// reply. `action_log_events` carries no chain key on any of the five mission.*
// event types, and `mission_templates.chain_next` is populated on 76 of 11,080
// rows. Printing the reply and dropping the bytes loses the link permanently --
// which is why frontier_extension's successor is unknown today.
//
// This appends each completion to a per-agent JSONL ledger. A flat file rather
// than a KB table on purpose: the reply's mission_id is the PROCEDURAL INSTANCE
// hash, not the template slug, and there is no template_id, so the only join
// back to mission_templates is the title. Recording first and reconciling later
// keeps a fragile join out of the write path -- and never risks the catalogue.

// missionCompletionRecord is one line of the ledger.
type missionCompletionRecord struct {
	AgentID     string `json:"agent_id"`
	ObservedUTC string `json:"observed_utc"`
	Tick        int64  `json:"tick,omitempty"`

	MissionID string `json:"mission_id,omitempty"` // procedural instance hash
	Title     string `json:"title,omitempty"`      // the only join to the catalogue
	ChainNext string `json:"chain_next,omitempty"`

	CreditsEarned    int `json:"credits_earned,omitempty"`
	CreditsPromised  int `json:"credits_promised,omitempty"`
	CreditsShortfall int `json:"credits_shortfall,omitempty"`

	SkillXPGained     map[string]int `json:"skill_xp_gained,omitempty"`
	ReputationChanges map[string]int `json:"reputation_changes,omitempty"`
	ItemsReceived     map[string]int `json:"items_received,omitempty"`

	Message string `json:"message,omitempty"`
}

// captureMissionCompletion appends one completion to the ledger at path.
// Returns an error without writing anything when the reply cannot be parsed.
func captureMissionCompletion(path, agentID string, raw []byte) error {
	var outer struct {
		Tick   int64           `json:"tick"`
		Result json.RawMessage `json:"result"`
	}
	body := raw
	if err := json.Unmarshal(raw, &outer); err == nil && len(outer.Result) > 0 {
		body = outer.Result
	}

	var r struct {
		MissionID         string         `json:"mission_id"`
		Title             string         `json:"title"`
		ChainNext         string         `json:"chain_next"`
		Message           string         `json:"message"`
		CreditsEarned     int            `json:"credits_earned"`
		CreditsPromised   int            `json:"credits_promised"`
		CreditsShortfall  int            `json:"credits_shortfall"`
		SkillXPGained     map[string]int `json:"skill_xp_gained"`
		ReputationChanges map[string]int `json:"reputation_changes"`
		ItemsReceived     map[string]int `json:"items_received"`
	}
	if err := json.Unmarshal(unwrapActionResult(body), &r); err != nil {
		return fmt.Errorf("parse complete_mission: %w", err)
	}
	if r.Title == "" && r.MissionID == "" {
		return fmt.Errorf("complete_mission reply named no mission")
	}

	rec := missionCompletionRecord{
		AgentID:           agentID,
		ObservedUTC:       time.Now().UTC().Format(time.RFC3339),
		Tick:              outer.Tick,
		MissionID:         r.MissionID,
		Title:             r.Title,
		ChainNext:         r.ChainNext,
		CreditsEarned:     r.CreditsEarned,
		CreditsPromised:   r.CreditsPromised,
		CreditsShortfall:  r.CreditsShortfall,
		SkillXPGained:     r.SkillXPGained,
		ReputationChanges: r.ReputationChanges,
		ItemsReceived:     r.ItemsReceived,
		Message:           r.Message,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode completion: %w", err)
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("ledger dir: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append ledger: %w", err)
	}
	return nil
}

// missionCompletionLedgerPath is the per-agent ledger location.
func missionCompletionLedgerPath(agentID string) string {
	return filepath.Join("data", "agents", agentID, "mission_completions.jsonl")
}
