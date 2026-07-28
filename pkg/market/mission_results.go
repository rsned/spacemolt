package market

import (
	"context"
	"database/sql"
	"fmt"
)

// RecordMissionResult writes one finished-mission outcome row (completed or
// abandoned), the mission-fleet analogue of RecordHaulResult.
func (c *Collector) RecordMissionResult(ctx context.Context, r MissionResult) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO mission_results
 (agent_id, mission_id, template_id, mission_type, title, from_base_id, to_base_id,
  item_id, qty, expected_reward, credits_earned, item_cost, fuel_cost, jumps, outcome,
  reason, accepted_at, finished_at, accepted_tick, finished_tick, created_at,
  expiry_budget_ticks)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.AgentID, r.MissionID, r.TemplateID, r.MissionType, r.Title, r.FromBaseID,
			r.ToBaseID, r.ItemID, r.Qty, r.ExpectedReward, r.CreditsEarned, r.ItemCost,
			r.FuelCost, r.Jumps, r.Outcome, r.Reason, r.AcceptedAt, r.FinishedAt, r.AcceptedTick,
			r.FinishedTick, r.CreatedAt, r.ExpiryBudgetTicks)
		return err
	})
}

// GetMissionResults returns the most recent mission results for agentID (all
// agents if empty), newest finished first, capped at limit (<=0 -> 500).
func (c *Collector) GetMissionResults(ctx context.Context, agentID string, limit int) ([]MissionResult, error) {
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT id, agent_id, mission_id, template_id, mission_type, title, from_base_id,
 to_base_id, item_id, qty, expected_reward, credits_earned, item_cost, fuel_cost, jumps,
 outcome, COALESCE(reason, ''), accepted_at, finished_at, accepted_tick, finished_tick, created_at,
 COALESCE(expiry_budget_ticks, 0)
 FROM mission_results`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id = ?`
		args = append(args, agentID)
	}
	q += ` ORDER BY finished_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get mission results: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []MissionResult
	for rows.Next() {
		var r MissionResult
		if err := rows.Scan(&r.ID, &r.AgentID, &r.MissionID, &r.TemplateID, &r.MissionType,
			&r.Title, &r.FromBaseID, &r.ToBaseID, &r.ItemID, &r.Qty, &r.ExpectedReward,
			&r.CreditsEarned, &r.ItemCost, &r.FuelCost, &r.Jumps, &r.Outcome, &r.Reason,
			&r.AcceptedAt, &r.FinishedAt, &r.AcceptedTick, &r.FinishedTick, &r.CreatedAt,
			&r.ExpiryBudgetTicks); err != nil {
			return nil, fmt.Errorf("scan mission result: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
