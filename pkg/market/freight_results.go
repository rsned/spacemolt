package market

import (
	"context"
	"database/sql"
	"fmt"
)

// RecordFreightResult writes one settled shipping-contract outcome row, the
// carrier analogue of RecordMissionResult.
func (c *Collector) RecordFreightResult(ctx context.Context, r FreightResult) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO freight_results
 (agent_id, contract_id, package_id, from_base_id, to_base_id, service_level,
  route_hops, base_reward, max_speed_bonus, fuel_cost, carrier_payout, outcome,
  reason, accepted_at, finished_at, accepted_tick, finished_tick, created_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.AgentID, r.ContractID, r.PackageID, r.FromBaseID, r.ToBaseID, r.ServiceLevel,
			r.RouteHops, r.BaseReward, r.MaxSpeedBonus, r.FuelCost, r.CarrierPayout, r.Outcome,
			r.Reason, r.AcceptedAt, r.FinishedAt, r.AcceptedTick, r.FinishedTick, r.CreatedAt)
		return err
	})
}

// GetFreightResults returns the most recent freight results for agentID (all
// agents if empty), newest finished first, capped at limit (<=0 -> 500).
func (c *Collector) GetFreightResults(ctx context.Context, agentID string, limit int) ([]FreightResult, error) {
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT id, agent_id, contract_id, COALESCE(package_id, ''), COALESCE(from_base_id, ''),
 COALESCE(to_base_id, ''), COALESCE(service_level, ''), route_hops, base_reward, max_speed_bonus,
 fuel_cost, carrier_payout, outcome, COALESCE(reason, ''), accepted_at, finished_at,
 accepted_tick, finished_tick, created_at
 FROM freight_results`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id = ?`
		args = append(args, agentID)
	}
	q += ` ORDER BY finished_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get freight results: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []FreightResult
	for rows.Next() {
		var r FreightResult
		if err := rows.Scan(&r.ID, &r.AgentID, &r.ContractID, &r.PackageID, &r.FromBaseID,
			&r.ToBaseID, &r.ServiceLevel, &r.RouteHops, &r.BaseReward, &r.MaxSpeedBonus,
			&r.FuelCost, &r.CarrierPayout, &r.Outcome, &r.Reason, &r.AcceptedAt, &r.FinishedAt,
			&r.AcceptedTick, &r.FinishedTick, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan freight result: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
