package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// RecordMissionUnlocks stores the prerequisite edges a complete_mission /
// view_completed_mission "unlocks" list reveals (v0.685.0): each unlocked
// mission requires completedTemplateID and every mission in its
// still_requires. completedTemplateID may be empty when the caller cannot map
// the completed instance back to its template; only the still_requires edges
// are then known. Returns the number of edges written. Idempotent.
func (kb *SQLiteKB) RecordMissionUnlocks(ctx context.Context, completedTemplateID string,
	unlocks []serverapi.MissionUnlock, agentID string, tick int64,
) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	n := 0
	edge := func(m serverapi.MissionPointer, requiresID, requiresTitle string) error {
		if m.MissionID == "" || requiresID == "" || m.MissionID == requiresID {
			return nil
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO mission_prerequisites (
				mission_id, requires_mission_id, mission_title, requires_title,
				mission_base_id, mission_system_id, agent_id,
				first_seen_tick, last_seen_tick, first_seen_at, last_seen_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (mission_id, requires_mission_id) DO UPDATE SET
				mission_title = CASE WHEN excluded.mission_title != '' THEN excluded.mission_title ELSE mission_title END,
				requires_title = CASE WHEN excluded.requires_title != '' THEN excluded.requires_title ELSE requires_title END,
				mission_base_id = CASE WHEN excluded.mission_base_id != '' THEN excluded.mission_base_id ELSE mission_base_id END,
				mission_system_id = CASE WHEN excluded.mission_system_id != '' THEN excluded.mission_system_id ELSE mission_system_id END,
				last_seen_tick = excluded.last_seen_tick,
				last_seen_at = excluded.last_seen_at`,
			m.MissionID, requiresID, m.Title, requiresTitle,
			m.BaseID, m.SystemID, agentID,
			tick, tick, now, now)
		if err != nil {
			return fmt.Errorf("record prerequisite %s -> %s: %w", m.MissionID, requiresID, err)
		}
		n++
		return nil
	}
	for _, u := range unlocks {
		if err := edge(u.Mission, completedTemplateID, ""); err != nil {
			return 0, err
		}
		for _, r := range u.StillRequires {
			if err := edge(u.Mission, r.Mission.MissionID, r.Mission.Title); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return n, nil
}

// MissionCompletion is what one complete_mission reply paid out.
type MissionCompletion struct {
	AgentID           string
	MissionID         string // procedural instance hash from the reply
	TemplateID        string // when the caller knows it (from the accept)
	Title             string
	CreditsEarned     int64
	CreditsPromised   int64
	ReputationChanges map[string]int // empire -> standing delta
	SkillXPGained     map[string]int
	ItemsReceived     map[string]int
	ChainNext         string
	RecipesUnlocked   []string // v0.687.0: recipe ids the completion taught
	Message           string   // the completion dialog the giver speaks
	Tick              int64
}

// RecordMissionCompletion stores one completion, keyed by the mission
// instance id so the same reply recorded twice is one row. A later record
// fills a template_id the first one lacked.
func (kb *SQLiteKB) RecordMissionCompletion(ctx context.Context, c MissionCompletion) error {
	if c.MissionID == "" {
		return fmt.Errorf("record mission completion: no mission_id")
	}
	js := func(m map[string]int) string {
		if len(m) == 0 {
			return "{}"
		}
		b, err := json.Marshal(m)
		if err != nil {
			return "{}"
		}
		return string(b)
	}
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO mission_completions (
			mission_id, template_id, agent_id, title,
			credits_earned, credits_promised,
			reputation_changes, skill_xp_gained, items_received,
			chain_next, recipes_unlocked, message, tick, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (mission_id) DO UPDATE SET
			template_id = CASE WHEN template_id = '' THEN excluded.template_id ELSE template_id END,
			recipes_unlocked = CASE WHEN recipes_unlocked = '[]' THEN excluded.recipes_unlocked ELSE recipes_unlocked END,
			message = CASE WHEN message = '' THEN excluded.message ELSE message END`,
		c.MissionID, c.TemplateID, c.AgentID, c.Title,
		c.CreditsEarned, c.CreditsPromised,
		js(c.ReputationChanges), js(c.SkillXPGained), js(c.ItemsReceived),
		c.ChainNext, recipesJSON(c.RecipesUnlocked), c.Message, c.Tick, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record mission completion %s: %w", c.MissionID, err)
	}
	// The board never shows the completion dialog; this reply is the only
	// source, so teach it to the template when the template is known.
	if c.TemplateID != "" && c.Message != "" {
		if _, err := kb.db.ExecContext(ctx, `
			UPDATE mission_templates SET dialog_complete = ?
			WHERE id = ? AND COALESCE(dialog_complete, '') = ''`, c.Message, c.TemplateID); err != nil {
			return fmt.Errorf("record mission completion %s: template dialog: %w", c.MissionID, err)
		}
	}
	return nil
}

// recipesJSON renders recipe ids as a JSON array ("[]" when none).
func recipesJSON(ids []string) string {
	if len(ids) == 0 {
		return "[]"
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return "[]"
	}
	return string(b)
}
